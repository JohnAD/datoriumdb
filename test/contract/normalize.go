//go:build contract

// Package contract holds normalized golden-envelope tests: they run
// access-language commands directly against an in-process engine.Engine
// (no HTTP, no subprocesses -- that is test/integration's job) and assert
// the resulting envelope matches a checked-in golden fixture after
// normalizing high-entropy fields (ULIDs) that are never byte-for-byte
// reproducible between runs.
//
// Build with `go test -tags contract ./test/contract/...`.
package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
)

// ulidPattern matches a Crockford-base32 ULID: 26 characters from
// [0-9A-HJKMNP-TV-Z].
var ulidPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// Normalize deep-copies v (expected to be a JSON-shaped value: map,
// slice, or scalar) and replaces every string that looks like a ULID with
// a stable per-value placeholder ("<ID:1>", "<ID:2>", ...), assigned in
// first-seen order so structurally identical envelopes normalize
// identically regardless of the concrete random IDs generated at runtime.
func Normalize(v any) any {
	seen := map[string]string{}
	n := 0
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			out := make(map[string]any, len(x))
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				out[k] = walk(x[k])
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, e := range x {
				out[i] = walk(e)
			}
			return out
		case string:
			if ulidPattern.MatchString(x) {
				if placeholder, ok := seen[x]; ok {
					return placeholder
				}
				n++
				placeholder := fmt.Sprintf("<ID:%d>", n)
				seen[x] = placeholder
				return placeholder
			}
			return x
		default:
			return x
		}
	}
	return walk(v)
}

// NormalizedJSON normalizes v and marshals it as stable, sorted-key,
// indented JSON for golden-file comparison / diffing. v is round-tripped
// through JSON first so named map/slice types (like envelope.Result,
// which is a `map[string]any` under the hood but not the same dynamic
// type for a type switch) normalize the same way plain
// map[string]any/[]any values do.
func NormalizedJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		panic(err)
	}
	normalized := Normalize(generic)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(normalized); err != nil {
		panic(err)
	}
	return buf.String()
}

// updateGolden is set via `go test -tags contract ./test/contract/... -run TestGolden -update`
// pattern by reading the DATORIUMDB_UPDATE_GOLDEN env var, keeping the
// test files themselves free of a custom -update flag wired through the
// standard test binary flag set.
func updateGolden() bool {
	return os.Getenv("DATORIUMDB_UPDATE_GOLDEN") == "1"
}
