package search

import "strings"

// SortValue is one computed sort-key component for a document, tracking
// the Missing/Null/Known three-state model from SEARCHING.md so ordering
// can apply the "null sorts after known; missing sorts after both" rule
// for both asc and desc directions.
type SortValue struct {
	Present bool
	Null    bool
	Value   any
}

func rank(v SortValue) int {
	if !v.Present {
		return 2
	}
	if v.Null {
		return 1
	}
	return 0
}

// ComputeSortValues resolves def.Sort against doc.
func ComputeSortValues(def *Definition, doc map[string]any) []SortValue {
	out := make([]SortValue, len(def.Sort))
	for i, s := range def.Sort {
		if s.Field == "/!" {
			id, _ := doc["!"].(string)
			out[i] = SortValue{Present: true, Value: id}
			continue
		}
		v, present := PointerGet(doc, s.Field)
		if !present {
			out[i] = SortValue{Present: false}
			continue
		}
		if v == nil {
			out[i] = SortValue{Present: true, Null: true}
			continue
		}
		out[i] = SortValue{Present: true, Value: v}
	}
	return out
}

// SortValuesToJSON converts computed sort values into the plain JSON-safe
// array stored in a ResultFile item's "sort" field. Per SEARCHING.md's
// example, this is a plain value array; both Null and Missing serialize as
// JSON null (a documented simplification — see AGENT-FOR-CHANGE-DISTRIBUTION
// gaps in the project summary for the resulting narrow edge case).
func SortValuesToJSON(vals []SortValue) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		if !v.Present || v.Null {
			out[i] = nil
			continue
		}
		out[i] = v.Value
	}
	return out
}

// SortValuesFromJSON reconstructs candidate SortValue comparison inputs
// from a plain JSON sort array (e.g. one delivered over the wire by a
// remote change-agent). Per SortValuesToJSON's note, a JSON null is always
// interpreted as an explicit null (rank 1), never as missing (rank 2);
// this is a documented narrow simplification of the null/missing
// distinction across process boundaries.
func SortValuesFromJSON(raw []any) []SortValue {
	out := make([]SortValue, len(raw))
	for i, v := range raw {
		if v == nil {
			out[i] = SortValue{Present: true, Null: true}
			continue
		}
		out[i] = SortValue{Present: true, Value: v}
	}
	return out
}

// CompareStoredSort compares a freshly computed candidate sort-value list
// against a previously stored plain JSON sort array (as decoded from
// matches.json), applying dirs (asc/desc per position) and the
// null/missing-after-known rule. Stored JSON null is treated as rank 1
// (null), never rank 2 (missing); see SortValuesToJSON's note.
func CompareStoredSort(candidate []SortValue, stored []any, dirs []string) int {
	n := len(candidate)
	if len(stored) < n {
		n = len(stored)
	}
	for i := 0; i < n; i++ {
		c := compareOne(candidate[i], stored[i], dirs[i])
		if c != 0 {
			return c
		}
	}
	if len(candidate) != len(stored) {
		return len(candidate) - len(stored)
	}
	return 0
}

func compareOne(cand SortValue, storedRaw any, dir string) int {
	candRank := rank(cand)
	storedRank := 0
	if storedRaw == nil {
		storedRank = 1
	}
	if candRank != storedRank {
		return candRank - storedRank
	}
	if candRank != 0 {
		return 0
	}
	cmp := compareKnown(cand.Value, storedRaw)
	if dir == "desc" {
		return -cmp
	}
	return cmp
}

func compareKnown(a, b any) int {
	switch av := a.(type) {
	case string:
		bv, _ := b.(string)
		return strings.Compare(av, bv)
	case bool:
		bv, _ := b.(bool)
		if av == bv {
			return 0
		}
		if !av { // false sorts before true
			return -1
		}
		return 1
	case float64:
		bv := toFloat64(b)
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		default:
			return 0
		}
	default:
		return 0
	}
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}
