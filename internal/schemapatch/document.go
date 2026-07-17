package schemapatch

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ApplyToDocument migrates one decoded document (a map[string]any /
// []any / scalar tree, matching encoding/json's default decoding, with
// "!"/"$"/"#" left untouched by the caller) forward by spec's update
// list, per UPDATE-SCHEMA.md's per-field value-selection, removal, and
// kind-conversion rules. It mutates and returns doc; every fallback this
// document-migration engine implements is described inline per operation.
//
// UPDATE-SCHEMA.md's "Schema Change Notes" subtleties that are not
// separately encoded in the update list itself (e.g. distinguishing "adds
// nullable" from "adds required" when both changed in the same update)
// are approximated by the single value-selection/removal-logic algorithm
// implemented here; see convertKind and selectFillValue for the exact
// fallback order applied.
func ApplyToDocument(doc map[string]any, spec *UpdateSpec) (map[string]any, error) {
	for i, op := range spec.Updates {
		if err := applyDocOp(doc, op); err != nil {
			return nil, fmt.Errorf("update[%d] (%s %s): %w", i, op.Op, op.Path, err)
		}
	}
	return doc, nil
}

func applyDocOp(doc map[string]any, op UpdateOp) error {
	switch op.Op {
	case "add", "import":
		return applyDocAdd(doc, op)
	case "remove":
		return applyDocRemove(doc, op)
	case "abandon":
		// The field simply stops being schema-defined; document content
		// is untouched and becomes a non-schemed extra field the next
		// time it is read (splitSOTAndExtra-style reclassification).
		return nil
	case "replace":
		return applyDocReplace(doc, op)
	case "move":
		return applyDocMove(doc, op)
	case "copy":
		return applyDocCopy(doc, op)
	case "convert":
		return applyDocConvert(doc, op)
	default:
		return fmt.Errorf("unsupported op %q", op.Op)
	}
}

func applyDocAdd(doc map[string]any, op UpdateOp) error {
	if op.Op == "import" {
		if existing, ok := docPointerGet(doc, op.Path); ok {
			if schemaKindMatches(op.Schema, existing) {
				// "the extra field's value is imported and used instead
				// of the supplied value" -- keep it as-is.
				return nil
			}
			// "the extra field is removed and its value is ignored. The
			// operation then follows the same value selection rules as
			// add."
			docPointerRemove(doc, op.Path)
		}
	}
	// "If a conflicting non-schemed extra field already exists at the
	// added path, the preexisting extra field is discarded" -- for plain
	// add (and for import once the extra-field-reuse case above didn't
	// apply), always recompute from scratch rather than keep stale data.
	value, ok := selectFillValue(op)
	if !ok {
		docPointerRemove(doc, op.Path)
		return nil // not required and no value supplied: leave field absent
	}
	docPointerSet(doc, op.Path, value)
	return nil
}

// selectFillValue implements UPDATE-SCHEMA.md's `add`/`import` value
// selection: "If value is present, that value is added... If value is not
// present and the new field is required, use the default, then the
// failover value, then null if nullable, then the empty value. If value
// is not present and the new field is not required, the new field is not
// added."
func selectFillValue(op UpdateOp) (any, bool) {
	if op.HasValue {
		return op.Value, true
	}
	if op.Schema == nil {
		return nil, false
	}
	if v, ok := op.Schema.Get("default"); ok {
		return toPlainValue(v), true
	}
	if op.Failover != nil {
		return op.Failover, true
	}
	required, _ := schemaBool(op.Schema, "required")
	nullable, _ := schemaBool(op.Schema, "nullable")
	if !required {
		return nil, false
	}
	if nullable {
		return nil, true
	}
	kind, _ := getString(op.Schema, "kind")
	return emptyValueForKind(kind), true
}

func applyDocRemove(doc map[string]any, op UpdateOp) error {
	docPointerRemove(doc, op.Path)
	return nil
}

func applyDocReplace(doc map[string]any, op UpdateOp) error {
	if !op.HasValue {
		return fmt.Errorf("replace requires a value")
	}
	docPointerSet(doc, op.Path, op.Value)
	return nil
}

func applyDocMove(doc map[string]any, op UpdateOp) error {
	v, ok := docPointerGet(doc, op.From)
	if !ok {
		return nil // nothing to move for this document
	}
	docPointerRemove(doc, op.From)
	docPointerSet(doc, op.Path, v)
	return nil
}

func applyDocCopy(doc map[string]any, op UpdateOp) error {
	v, ok := docPointerGet(doc, op.From)
	if !ok {
		return nil
	}
	docPointerSet(doc, op.Path, cloneValue(v))
	return nil
}

func applyDocConvert(doc map[string]any, op UpdateOp) error {
	if op.HasValue {
		docPointerSet(doc, op.Path, op.Value)
		return nil
	}
	current, existed := docPointerGet(doc, op.Path)
	newKind, _ := getString(op.Schema, "kind")
	if !existed {
		// UPDATE-SCHEMA.md's removal logic covers "the value is removed
		// or replaced"; an already-absent optional field stays absent
		// unless the new schema requires a value.
		fillOp := op
		fillOp.HasValue = false
		if v, ok := selectFillValue(fillOp); ok {
			docPointerSet(doc, op.Path, v)
		}
		return nil
	}
	converted := convertKind(current, newKind)
	docPointerSet(doc, op.Path, converted)
	return nil
}

// convertKind implements UPDATE-SCHEMA.md's "Kind Changes" table.
func convertKind(v any, newKind string) any {
	switch newKind {
	case "boolean":
		return !isEmptyOrNilValue(v)
	case "string":
		return stringifyValue(v)
	case "number":
		switch t := v.(type) {
		case bool:
			if t {
				return 1.0
			}
			return 0.0
		case string:
			return parseLeadingNumber(t)
		case []any:
			return float64(len(t))
		case map[string]any:
			return float64(len(t))
		case float64:
			return t
		default:
			return 0.0
		}
	case "array":
		if arr, ok := v.([]any); ok {
			return arr
		}
		return []any{v}
	case "object":
		if obj, ok := v.(map[string]any); ok {
			return obj
		}
		// UPDATE-SCHEMA.md: "the current key/value is embedded into the
		// object if allowed by the schema. Otherwise, use an empty
		// object." This narrow MVP does not have the destination
		// object's schema on hand here, so it always uses the empty
		// object; embedding the previous scalar under a matching field
		// name is a documented simplification gap.
		return map[string]any{}
	default:
		return v
	}
}

func isEmptyOrNilValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case bool:
		return !t
	case string:
		return t == ""
	case float64:
		return t == 0
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	default:
		return false
	}
}

func stringifyValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func parseLeadingNumber(s string) float64 {
	i := 0
	n := len(s)
	if i < n && (s[i] == '+' || s[i] == '-') {
		i++
	}
	start := i
	sawDigit := false
	for i < n && s[i] >= '0' && s[i] <= '9' {
		i++
		sawDigit = true
	}
	if i < n && s[i] == '.' {
		i++
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
			sawDigit = true
		}
	}
	if !sawDigit {
		return 0
	}
	_ = start
	f, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	return f
}

func emptyValueForKind(kind string) any {
	switch kind {
	case "string":
		return ""
	case "number":
		return 0.0
	case "boolean":
		return false
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	default:
		return nil
	}
}

func schemaBool(m *omap, key string) (bool, bool) {
	if m == nil {
		return false, false
	}
	v, ok := m.Get(key)
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func schemaKindMatches(schema *omap, value any) bool {
	if schema == nil {
		return true
	}
	kind, ok := getString(schema, "kind")
	if !ok {
		return true
	}
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	default:
		return true
	}
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = cloneValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = cloneValue(vv)
		}
		return out
	default:
		return v
	}
}

// --- generic document pointer helpers (object-key paths only; schema
// field paths never contain array-index segments, per
// schemapatch.validatePathTarget) ---------------------------------------

func docPointerGet(doc map[string]any, path string) (any, bool) {
	segments := splitPath(path)
	var cur any = doc
	for _, seg := range segments {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[seg]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

func docPointerSet(doc map[string]any, path string, value any) {
	segments := splitPath(path)
	if len(segments) == 0 {
		return
	}
	cur := doc
	for _, seg := range segments[:len(segments)-1] {
		next, ok := cur[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[seg] = next
		}
		cur = next
	}
	cur[segments[len(segments)-1]] = value
}

func docPointerRemove(doc map[string]any, path string) {
	segments := splitPath(path)
	if len(segments) == 0 {
		return
	}
	cur := doc
	for _, seg := range segments[:len(segments)-1] {
		next, ok := cur[seg].(map[string]any)
		if !ok {
			return
		}
		cur = next
	}
	delete(cur, segments[len(segments)-1])
}
