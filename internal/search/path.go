package search

import (
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
