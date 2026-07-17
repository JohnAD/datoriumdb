package search

import (
	"fmt"
	"unicode/utf8"
)

// EvalResult is the outcome of evaluating one document against a search
// definition at write time (used by the change-agent).
type EvalResult struct {
	// Matched reports whether the document belongs in any bucket of this
	// search at all. When false, Segments and Key are empty.
	Matched bool
	// Segments holds the encoded, ordered path components contributed by
	// clauses that produce a directory segment (constant-only filter
	// clauses contribute none). Joined with ShardInput, this is the
	// search's on-disk bucket directory below the search name.
	Segments []string
	// Key holds the same values in decoded (unencoded) logical form, for
	// the stored ResultFile.Key field.
	Key []any
}

// EvaluateDocument evaluates every clause in def against doc (a decoded
// SOT+metadata document map, or nil for "no document") and returns whether
// the document belongs in a search bucket and, if so, which one.
func EvaluateDocument(def *Definition, doc map[string]any) (EvalResult, error) {
	if doc == nil {
		return EvalResult{Matched: false}, nil
	}
	var segments []string
	var key []any
	for i, c := range def.Clauses {
		seg, k, contributes, included, err := evaluateClause(c, doc)
		if err != nil {
			return EvalResult{}, fmt.Errorf("clause %d (%s %s): %w", i, c.Op, c.Field, err)
		}
		if !included {
			return EvalResult{Matched: false}, nil
		}
		if contributes {
			segments = append(segments, seg)
			key = append(key, k)
		}
	}
	return EvalResult{Matched: true, Segments: segments, Key: key}, nil
}

// evaluateClause returns the encoded path segment (seg) and decoded logical
// value (k) this clause contributes, whether it contributes a segment at
// all (contributes), and whether the document remains eligible for this
// search after this clause (included). included=false short-circuits the
// whole evaluation: the document is not in any bucket for this search.
func evaluateClause(c Clause, doc map[string]any) (seg string, k any, contributes bool, included bool, err error) {
	switch c.Op {
	case OpEquals:
		return evaluateEquals(c, doc)
	case OpIn:
		return evaluateIn(c, doc)
	case OpExists:
		return evaluateExists(c, doc)
	default:
		return "", nil, false, false, fmt.Errorf("unsupported op %q", c.Op)
	}
}

func evaluateEquals(c Clause, doc map[string]any) (string, any, bool, bool, error) {
	if varName, isVar := IsVariable(c.Value); isVar {
		_ = varName
		val, present := PointerGet(doc, c.Field)
		if !present {
			return "", nil, false, false, nil
		}
		switch v := val.(type) {
		case string:
			if utf8.RuneCountInString(v) > 63 {
				// SEARCHING-V1-string.md: field is skipped, no match of
				// any kind is recorded for that clause.
				return "", nil, false, false, nil
			}
			return EncodeStringValue(v), v, true, true, nil
		case bool:
			return EncodeTruth(v), v, true, true, nil
		case nil:
			return "", nil, false, false, nil
		default:
			return "", nil, false, false, fmt.Errorf("equals variable clause requires a string or boolean field value")
		}
	}
	if c.Value == nil {
		val, present := PointerGet(doc, c.Field)
		isNull := present && val == nil
		return EncodeTruth(isNull), isNull, true, true, nil
	}
	val, present := PointerGet(doc, c.Field)
	var matches bool
	switch cv := c.Value.(type) {
	case string:
		s, ok := val.(string)
		matches = present && ok && s == cv
	case bool:
		b, ok := val.(bool)
		matches = present && ok && b == cv
	default:
		return "", nil, false, false, fmt.Errorf("unsupported equals constant kind %T", cv)
	}
	if c.Truth != "" {
		return EncodeTruth(matches), matches, true, true, nil
	}
	// Pure constant filter with no truth/variable: contributes no path
	// segment; a non-match excludes the document from this search
	// entirely (SEARCHING.md Create example: "If highRated were false,
	// the document would not be stored in this search result at all.").
	return "", nil, false, matches, nil
}

func evaluateIn(c Clause, doc map[string]any) (string, any, bool, bool, error) {
	values, ok := c.Value.([]any)
	if !ok {
		return "", nil, false, false, fmt.Errorf("in clause requires a constant value array")
	}
	val, present := PointerGet(doc, c.Field)
	if !present {
		return "", nil, false, false, nil
	}
	s, ok := val.(string)
	if !ok {
		return "", nil, false, false, nil
	}
	for _, v := range values {
		vs, ok := v.(string)
		if ok && vs == s {
			return EncodeStringValue(s), s, true, true, nil
		}
	}
	// Locked MVP decision: selected constant `in` resolves exactly one
	// bucket; a document whose value is not one of the allowed constants
	// does not belong to any bucket of this search.
	return "", nil, false, false, nil
}

func evaluateExists(c Clause, doc map[string]any) (string, any, bool, bool, error) {
	val, present := PointerGet(doc, c.Field)
	exists := present
	if c.HideNulls && present && val == nil {
		exists = false
	}
	return EncodeTruth(exists), exists, true, true, nil
}
