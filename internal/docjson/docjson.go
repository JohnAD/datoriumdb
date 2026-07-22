// Package docjson canonicalizes collection documents for on-disk storage
// using OJSON so object field order is stable and schema-defined
// (source-of-truth) fields follow schema children order per tech-docs/SCHEMAS.md.
package docjson

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/ojson"
)

// metaFieldOrder is the enforced order of database-owned metadata fields
// at the root of every collection document.
var metaFieldOrder = []string{"!", "$", "#"}

// Canonicalize returns pretty-printed JSON bytes for a collection document.
//
// Root order is:
//  1. database-owned metadata in enforced order: !, $, #
//  2. schema-defined source-of-truth fields in schema children order
//     (including nested objects/array items)
//  3. non-schema extra fields in the order they already appear on doc
//
// Missing optional schema fields are not invented here (no default fill-in);
// validation remains the caller's responsibility. doc must be an object.
func Canonicalize(schemaRaw []byte, doc ojson.JSONValue) ([]byte, error) {
	if !doc.IsObject() {
		return nil, fmt.Errorf("docjson: document must be an object")
	}
	schema, err := config.CompileSchemaBytes(schemaRaw)
	if err != nil {
		return nil, fmt.Errorf("docjson: compile schema: %w", err)
	}

	keys, err := objectFieldKeys(doc)
	if err != nil {
		return nil, err
	}

	meta := map[string]ojson.JSONValue{}
	content := ojson.NewObject()
	for _, key := range keys {
		value := doc.Get(key)
		switch key {
		case "!", "$", "#":
			meta[key] = value
		default:
			if err := content.SetTry(key, value); err != nil {
				return nil, err
			}
		}
	}

	orderedContent, err := reorderBySchema(schema.Root(), content)
	if err != nil {
		return nil, err
	}

	out := ojson.NewObject()
	for _, key := range metaFieldOrder {
		if v, ok := meta[key]; ok && !v.IsVoid() {
			if err := out.SetTry(key, v); err != nil {
				return nil, err
			}
		}
	}
	contentKeys, err := objectFieldKeys(orderedContent)
	if err != nil {
		return nil, err
	}
	for _, key := range contentKeys {
		if err := out.SetTry(key, orderedContent.Get(key)); err != nil {
			return nil, err
		}
	}

	pretty := out.ToPrettyJSONBytes(2)
	if len(pretty) == 0 || pretty[len(pretty)-1] != '\n' {
		pretty = append(pretty, '\n')
	}
	return pretty, nil
}

// reorderBySchema returns a copy of value with object fields ordered by
// schema children. Present fields only — no defaults, no required checks.
func reorderBySchema(entry ojson.SchemaEntry, value ojson.JSONValue) (ojson.JSONValue, error) {
	if !entry.Valid() || value.IsVoid() || value.IsNull() {
		return value, nil
	}
	switch entry.Kind() {
	case ojson.KindObject:
		if !value.IsObject() {
			return value, nil
		}
		keys, err := objectFieldKeys(value)
		if err != nil {
			return ojson.NewVoid(), err
		}
		source := map[string]ojson.JSONValue{}
		for _, key := range keys {
			fieldValue := value.Get(key)
			if fieldValue.IsVoid() {
				continue
			}
			source[key] = fieldValue
		}
		out := ojson.NewObject()
		seen := map[string]bool{}
		for _, child := range entry.Children() {
			name := child.Name()
			fieldValue, ok := source[name]
			if !ok {
				continue
			}
			nested, err := reorderBySchema(child, fieldValue)
			if err != nil {
				return ojson.NewVoid(), err
			}
			if err := out.SetTry(name, nested); err != nil {
				return ojson.NewVoid(), err
			}
			seen[name] = true
		}
		for _, key := range keys {
			if seen[key] {
				continue
			}
			fieldValue := value.Get(key)
			if fieldValue.IsVoid() {
				continue
			}
			if err := out.SetTry(key, fieldValue); err != nil {
				return ojson.NewVoid(), err
			}
		}
		return out, nil
	case ojson.KindArray:
		if !value.IsArray() {
			return value, nil
		}
		items := entry.Items()
		out := ojson.NewArray()
		for i := 0; i < value.Len(); i++ {
			item := value.At(i)
			if item.IsVoid() {
				continue
			}
			nested, err := reorderBySchema(items, item)
			if err != nil {
				return ojson.NewVoid(), err
			}
			out.Append(nested)
		}
		return out, nil
	default:
		return value, nil
	}
}

// ObjectFieldKeys returns object keys in document order by decoding the
// ojson serialization. ojson v1.0.0 does not expose a public field iterator.
func ObjectFieldKeys(doc ojson.JSONValue) ([]string, error) {
	return objectFieldKeys(doc)
}

func objectFieldKeys(doc ojson.JSONValue) ([]string, error) {
	if doc.IsVoid() || doc.IsMissing() {
		return nil, nil
	}
	if !doc.IsObject() {
		return nil, fmt.Errorf("docjson: value is not an object")
	}
	dec := json.NewDecoder(bytes.NewReader(doc.ToJSONBytes()))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("docjson: decode object: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("docjson: expected object start")
	}
	keys := make([]string, 0)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("docjson: decode object key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("docjson: object key is not a string")
		}
		keys = append(keys, key)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, fmt.Errorf("docjson: skip object value: %w", err)
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("docjson: decode object end: %w", err)
	}
	return keys, nil
}

// CanonicalizeBytes parses raw with OJSON (preserving input field order for
// extras), then Canonicalize.
func CanonicalizeBytes(schemaRaw, raw []byte) ([]byte, error) {
	doc, err := ojson.ReadBytesNoSchema(raw)
	if err != nil {
		return nil, fmt.Errorf("docjson: parse document: %w", err)
	}
	return Canonicalize(schemaRaw, doc)
}

// PrettyBytes pretty-prints raw JSON with OJSON without applying a schema.
// Use for non-collection JSON or when order is already correct.
func PrettyBytes(raw []byte) ([]byte, error) {
	doc, err := ojson.ReadBytesNoSchema(raw)
	if err != nil {
		return nil, err
	}
	pretty := doc.ToPrettyJSONBytes(2)
	if len(pretty) == 0 || pretty[len(pretty)-1] != '\n' {
		pretty = append(pretty, '\n')
	}
	return pretty, nil
}

// EncodeMap marshals a Go map and pretty-prints with OJSON. Field order of
// map keys is not stable; use only for non-collection blobs (e.g. cache
// stubs) or tests that do not assert field order.
func EncodeMap(doc map[string]any) ([]byte, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return PrettyBytes(raw)
}

// CanonicalizeMap marshals an unordered Go map and then Canonicalize.
// Schema-defined field order is restored; relative order among unknown
// extra fields is not stable through a Go map. Prefer Canonicalize on an
// ojson.JSONValue when extra-field order matters.
func CanonicalizeMap(schemaRaw []byte, doc map[string]any) ([]byte, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return CanonicalizeBytes(schemaRaw, raw)
}
