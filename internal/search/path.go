package search

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/shard"
	"github.com/JohnAD/ojson"
)

// ResolveFieldSchema walks a slash-style field path from root, matching
// object children by name and array indices by descending into the array's
// item schema (SEARCH-DEFINITION-SCHEMA.md: "Indexed array paths target the
// array entry, not the array itself").
func ResolveFieldSchema(root ojson.SchemaEntry, path string) (ojson.SchemaEntry, bool) {
	segments := splitFieldPath(path)
	node := root
	for _, seg := range segments {
		if !node.Valid() {
			return ojson.SchemaEntry{}, false
		}
		if node.Kind() == ojson.KindArray && looksLikeIndex(seg) {
			node = node.Items()
			continue
		}
		node = node.Child(seg)
	}
	if !node.Valid() {
		return ojson.SchemaEntry{}, false
	}
	return node, true
}

func looksLikeIndex(seg string) bool {
	if seg == "" {
		return false
	}
	for _, r := range seg {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func splitFieldPath(path string) []string {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// PointerGet reads the value at a slash-style path inside a generic decoded
// document (map[string]any / []any tree), reporting whether the path is
// present (as opposed to Missing, per SEARCHING.md's Missing/Null/Known
// three-state model).
func PointerGet(doc map[string]any, path string) (value any, present bool) {
	segments := splitFieldPath(path)
	var cur any = doc
	for _, seg := range segments {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// EncodeStringValue encodes a string value as an uppercase-hex search
// directory path component, per SEARCHING.md: "path components derived
// from string values should be encoded as uppercase hex from the UTF-8
// bytes... An empty string encodes as the literal empty."
func EncodeStringValue(s string) string {
	if s == "" {
		return "empty"
	}
	return strings.ToUpper(fmt.Sprintf("%x", []byte(s)))
}

// EncodeTruth encodes a boolean truth/select-gate value as the literal path
// component "true" or "false".
func EncodeTruth(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// EncodeNull is the literal path component used for a null comparison
// value clause.
const EncodeNull = "null"

// EncodeScalarNumber encodes a JSON number using SEARCHING-V1-number.md's
// scalar (float64 canonical) directory form.
func EncodeScalarNumber(v any) (string, error) {
	f, err := asFloat64(v)
	if err != nil {
		return "", err
	}
	return strconv.FormatFloat(f, 'g', -1, 64), nil
}

// EncodeScalarByKind encodes a scalar JSON value for a search path segment
// using the array item's schema kind (SEARCHING-V1-array.md).
func EncodeScalarByKind(kind ojson.JSONKind, v any) (string, error) {
	switch kind {
	case ojson.KindString:
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("expected string value")
		}
		return EncodeStringValue(s), nil
	case ojson.KindBoolean:
		b, ok := v.(bool)
		if !ok {
			return "", fmt.Errorf("expected boolean value")
		}
		return EncodeTruth(b), nil
	case ojson.KindNull:
		if v != nil {
			return "", fmt.Errorf("expected null value")
		}
		return EncodeNull, nil
	case ojson.KindNumber:
		return EncodeScalarNumber(v)
	default:
		return "", fmt.Errorf("unsupported scalar kind %s", kind)
	}
}

// ScalarsEqual reports whether two decoded JSON scalars are equal under the
// comparison rules for the given item schema kind. Numbers use scalar
// (float64) equality.
func ScalarsEqual(kind ojson.JSONKind, a, b any) bool {
	switch kind {
	case ojson.KindString:
		as, aok := a.(string)
		bs, bok := b.(string)
		return aok && bok && as == bs
	case ojson.KindBoolean:
		ab, aok := a.(bool)
		bb, bok := b.(bool)
		return aok && bok && ab == bb
	case ojson.KindNull:
		return a == nil && b == nil
	case ojson.KindNumber:
		af, aerr := asFloat64(a)
		bf, berr := asFloat64(b)
		return aerr == nil && berr == nil && af == bf
	default:
		return false
	}
}

func isJSONNumber(v any) bool {
	_, err := asFloat64(v)
	return err == nil
}

func asFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	case string:
		return strconv.ParseFloat(n, 64)
	default:
		return 0, fmt.Errorf("not a number: %T", v)
	}
}

// ShardInput joins encoded path segments (with no leading/trailing slash)
// into the shard input string described in SEARCHING.md's "Search
// Sharding": "the encoded search directory path below the search name,
// with leading and trailing slashes removed. The final matches.json
// filename is not part of the shard input."
func ShardInput(segments []string) string {
	return strings.Join(segments, "/")
}

// ShardSlot computes the 8-bit shard slot for a resolved, encoded search
// bucket path.
func ShardSlot(segments []string) byte {
	return shard.RawSlot(ShardInput(segments))
}
