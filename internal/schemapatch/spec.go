package schemapatch

import (
	"fmt"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/oklog/ulid/v2"
)

// UpdateOp is one entry in the "updates" array of a schema-upgrade request.
type UpdateOp struct {
	Op       string
	Path     string
	From     string // used by move/copy
	Schema   *omap  // used by add/import/convert
	HasValue bool
	Value    any // plain Go value (map[string]any/[]any/string/float64/bool/nil), valid when HasValue
	Failover any
}

// UpdateSpec is a parsed schema-upgrade request file, following the shape
// documented in UPDATE-SCHEMA.md and COMMAND-LINE-TOOLS.md.
type UpdateSpec struct {
	From     int
	To       *int
	NewVerID string
	Updates  []UpdateOp
}

var supportedOps = map[string]bool{
	"add":     true,
	"import":  true,
	"remove":  true,
	"abandon": true,
	"replace": true,
	"move":    true,
	"copy":    true,
	"convert": true,
}

// ParseUpdateSpec decodes a schema-upgrade request file.
func ParseUpdateSpec(raw []byte) (*UpdateSpec, error) {
	decoded, err := decodeOrdered(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	root, ok := decoded.(*omap)
	if !ok {
		return nil, fmt.Errorf("upgrade request must be a JSON object")
	}
	spec := &UpdateSpec{}
	from, ok, err := getInt(root, "from")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("from is required")
	}
	spec.From = from

	if to, ok, err := getInt(root, "to"); err != nil {
		return nil, err
	} else if ok {
		spec.To = &to
	}

	newVerID, ok := getString(root, "new_ver_id")
	if !ok || newVerID == "" {
		return nil, fmt.Errorf("new_ver_id is required")
	}
	spec.NewVerID = newVerID

	updatesRaw, ok := getArray(root, "updates")
	if !ok {
		return nil, fmt.Errorf("updates is required and must be an array")
	}
	for i, raw := range updatesRaw {
		opMap, ok := raw.(*omap)
		if !ok {
			return nil, fmt.Errorf("updates[%d] must be an object", i)
		}
		op := UpdateOp{}
		opName, _ := getString(opMap, "op")
		op.Op = opName
		path, _ := getString(opMap, "path")
		op.Path = path
		from, _ := getString(opMap, "from")
		op.From = from
		if schemaObj, ok := getObject(opMap, "schema"); ok {
			op.Schema = schemaObj
		}
		if v, ok := opMap.Get("value"); ok {
			op.HasValue = true
			op.Value = toPlainValue(v)
		}
		if failover, ok := opMap.Get("failover"); ok {
			op.Failover = toPlainValue(failover)
		}
		spec.Updates = append(spec.Updates, op)
	}
	return spec, nil
}

// Validate checks UPDATE-SCHEMA.md / COMMAND-LINE-TOOLS.md structural rules
// against currentVersion, returning every problem found.
func (s *UpdateSpec) Validate(currentVersion int) []envelope.Error {
	var errs []envelope.Error
	if s.From != currentVersion {
		errs = append(errs, envelope.Error{
			Code:     "staleSchemaVersion",
			Path:     "/from",
			Message:  "Collection schema version is older or newer than the current database version.",
			Expected: currentVersion,
			Actual:   s.From,
		})
	}
	target := s.From + 1
	if s.To != nil && *s.To != target {
		errs = append(errs, envelope.Error{
			Code:     "invalidSchemaUpgrade",
			Path:     "/to",
			Message:  "Schema upgrades always advance by exactly one version.",
			Expected: target,
			Actual:   *s.To,
		})
	}
	if _, err := ulid.ParseStrict(s.NewVerID); err != nil {
		errs = append(errs, envelope.Error{
			Code:    "invalidSchemaUpgrade",
			Path:    "/new_ver_id",
			Message: "new_ver_id must be a valid ULID-like ID string",
			Actual:  s.NewVerID,
		})
	}
	if len(s.Updates) == 0 {
		errs = append(errs, envelope.Error{
			Code:    "invalidSchemaUpgrade",
			Path:    "/updates",
			Message: "updates must be a non-empty array",
		})
	}
	for i, op := range s.Updates {
		path := fmt.Sprintf("/updates/%d", i)
		if !supportedOps[op.Op] {
			errs = append(errs, envelope.Error{
				Code:    "invalidSchemaUpgrade",
				Path:    path + "/op",
				Message: "unsupported update operation",
				Actual:  op.Op,
			})
			continue
		}
		switch op.Op {
		case "move", "copy":
			if op.From == "" {
				errs = append(errs, envelope.Error{Code: "invalidSchemaUpgrade", Path: path + "/from", Message: "from is required for move/copy"})
			} else if err := validatePathTarget(op.From); err != nil {
				errs = append(errs, envelope.Error{Code: "invalidSchemaUpgrade", Path: path + "/from", Message: err.Error()})
			}
		}
		if op.Path == "" {
			errs = append(errs, envelope.Error{Code: "invalidSchemaUpgrade", Path: path + "/path", Message: "path is required"})
		} else if err := validatePathTarget(op.Path); err != nil {
			errs = append(errs, envelope.Error{Code: "invalidSchemaUpgrade", Path: path + "/path", Message: err.Error()})
		}
		switch op.Op {
		case "add", "import", "convert":
			if op.Schema == nil {
				errs = append(errs, envelope.Error{Code: "invalidSchemaUpgrade", Path: path + "/schema", Message: "schema object is required for " + op.Op})
			}
		}
	}
	return errs
}

// validatePathTarget rejects database-owned metadata fields and numeric
// array-index path segments, which are not valid schema field paths.
func validatePathTarget(path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must start with /")
	}
	segments := splitPath(path)
	if len(segments) == 0 {
		return fmt.Errorf("path must not be the document root")
	}
	for _, seg := range segments {
		if seg == "!" || seg == "$" || seg == "#" {
			return fmt.Errorf("path must not target database-owned metadata field %q", seg)
		}
		if isAllDigits(seg) {
			return fmt.Errorf("path segment %q looks like an array index, which is not a valid schema field path", seg)
		}
	}
	return nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func splitPath(path string) []string {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}
