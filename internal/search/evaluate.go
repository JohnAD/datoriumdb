package search

import (
	"fmt"
	"unicode/utf8"

	"github.com/JohnAD/ojson"
)

// EvalResult is the outcome of evaluating one document against one search
// bucket (used by the change-agent). Variable `contains` may produce
// multiple EvalResults for the same document.
type EvalResult struct {
	// Matched reports whether the document belongs in this bucket.
	// When false, Segments and Key are empty.
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

// clauseOutcome is one clause's contribution while building bucket sets.
type clauseOutcome struct {
	included    bool
	contributes bool
	// options are (seg, key) pairs when contributes; empty when not.
	options []segmentOption
}

type segmentOption struct {
	seg string
	key any
}

// EvaluateDocument evaluates every clause in def against doc and returns
// every bucket the document belongs in. Most MVP ops yield zero or one
// bucket; variable array `contains` may yield many (one per distinct
// scalar array element).
func EvaluateDocument(def *Definition, doc map[string]any) ([]EvalResult, error) {
	if doc == nil {
		return nil, nil
	}
	var outcomes []clauseOutcome
	for i, c := range def.Clauses {
		out, err := evaluateClause(c, doc)
		if err != nil {
			return nil, fmt.Errorf("clause %d (%s %s): %w", i, c.Op, c.Field, err)
		}
		if !out.included {
			return nil, nil
		}
		outcomes = append(outcomes, out)
	}
	return expandBuckets(outcomes), nil
}

func expandBuckets(outcomes []clauseOutcome) []EvalResult {
	type prefix struct {
		segs []string
		keys []any
	}
	cur := []prefix{{}}
	for _, out := range outcomes {
		if !out.contributes {
			continue
		}
		if len(out.options) == 0 {
			return nil
		}
		next := make([]prefix, 0, len(cur)*len(out.options))
		for _, p := range cur {
			for _, opt := range out.options {
				segs := append(append([]string{}, p.segs...), opt.seg)
				keys := append(append([]any{}, p.keys...), opt.key)
				next = append(next, prefix{segs: segs, keys: keys})
			}
		}
		cur = next
	}
	results := make([]EvalResult, 0, len(cur))
	for _, p := range cur {
		results = append(results, EvalResult{Matched: true, Segments: p.segs, Key: p.keys})
	}
	return results
}

func evaluateClause(c Clause, doc map[string]any) (clauseOutcome, error) {
	switch c.Op {
	case OpEquals:
		return evaluateEquals(c, doc)
	case OpIn:
		return evaluateIn(c, doc)
	case OpExists:
		return evaluateExists(c, doc)
	case OpContains:
		return evaluateContains(c, doc)
	default:
		return clauseOutcome{}, fmt.Errorf("unsupported op %q", c.Op)
	}
}

func singleSegment(seg string, key any, contributes, included bool) clauseOutcome {
	out := clauseOutcome{included: included, contributes: contributes}
	if contributes && included {
		out.options = []segmentOption{{seg: seg, key: key}}
	}
	return out
}

func evaluateEquals(c Clause, doc map[string]any) (clauseOutcome, error) {
	if varName, isVar := IsVariable(c.Value); isVar {
		_ = varName
		val, present := PointerGet(doc, c.Field)
		if !present {
			return clauseOutcome{included: false}, nil
		}
		switch v := val.(type) {
		case string:
			if utf8.RuneCountInString(v) > 63 {
				return clauseOutcome{included: false}, nil
			}
			return singleSegment(EncodeStringValue(v), v, true, true), nil
		case bool:
			return singleSegment(EncodeTruth(v), v, true, true), nil
		case nil:
			return clauseOutcome{included: false}, nil
		default:
			return clauseOutcome{}, fmt.Errorf("equals variable clause requires a string or boolean field value")
		}
	}
	if c.Value == nil {
		val, present := PointerGet(doc, c.Field)
		isNull := present && val == nil
		return singleSegment(EncodeTruth(isNull), isNull, true, true), nil
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
		return clauseOutcome{}, fmt.Errorf("unsupported equals constant kind %T", cv)
	}
	if c.Truth != "" {
		return singleSegment(EncodeTruth(matches), matches, true, true), nil
	}
	return singleSegment("", nil, false, matches), nil
}

func evaluateIn(c Clause, doc map[string]any) (clauseOutcome, error) {
	values, ok := c.Value.([]any)
	if !ok {
		return clauseOutcome{}, fmt.Errorf("in clause requires a constant value array")
	}
	val, present := PointerGet(doc, c.Field)
	if !present {
		return clauseOutcome{included: false}, nil
	}
	s, ok := val.(string)
	if !ok {
		return clauseOutcome{included: false}, nil
	}
	for _, v := range values {
		vs, ok := v.(string)
		if ok && vs == s {
			return singleSegment(EncodeStringValue(s), s, true, true), nil
		}
	}
	return clauseOutcome{included: false}, nil
}

func evaluateExists(c Clause, doc map[string]any) (clauseOutcome, error) {
	val, present := PointerGet(doc, c.Field)
	exists := present
	if c.HideNulls && present && val == nil {
		exists = false
	}
	return singleSegment(EncodeTruth(exists), exists, true, true), nil
}

func evaluateContains(c Clause, doc map[string]any) (clauseOutcome, error) {
	val, present := PointerGet(doc, c.Field)
	if !present {
		return clauseOutcome{included: false}, nil
	}
	arr, ok := val.([]any)
	if !ok {
		return clauseOutcome{included: false}, nil
	}

	if _, isVar := IsVariable(c.Value); isVar {
		seen := map[string]bool{}
		var options []segmentOption
		for _, item := range arr {
			kind, ok := inferScalarKind(item)
			if !ok {
				continue
			}
			if kind == ojson.KindString {
				if s, ok := item.(string); ok && utf8.RuneCountInString(s) > 63 {
					continue
				}
			}
			seg, err := EncodeScalarByKind(kind, item)
			if err != nil {
				continue
			}
			if seen[seg] {
				continue
			}
			seen[seg] = true
			options = append(options, segmentOption{seg: seg, key: normalizeKey(kind, item)})
		}
		if len(options) == 0 {
			return clauseOutcome{included: false}, nil
		}
		return clauseOutcome{included: true, contributes: true, options: options}, nil
	}

	kind, ok := inferScalarKind(c.Value)
	if !ok {
		return clauseOutcome{}, fmt.Errorf("unsupported contains constant kind %T", c.Value)
	}
	matches := false
	for _, item := range arr {
		if ScalarsEqual(kind, item, c.Value) {
			matches = true
			break
		}
	}
	if c.Truth == "" {
		return clauseOutcome{}, fmt.Errorf("contains constant clause requires truth")
	}
	return singleSegment(EncodeTruth(matches), matches, true, true), nil
}

func inferScalarKind(v any) (ojson.JSONKind, bool) {
	switch v.(type) {
	case string:
		return ojson.KindString, true
	case bool:
		return ojson.KindBoolean, true
	case nil:
		return ojson.KindNull, true
	default:
		if isJSONNumber(v) {
			return ojson.KindNumber, true
		}
		return ojson.KindVoid, false
	}
}

func normalizeKey(kind ojson.JSONKind, v any) any {
	switch kind {
	case ojson.KindNumber:
		f, err := asFloat64(v)
		if err != nil {
			return v
		}
		return f
	default:
		return v
	}
}
