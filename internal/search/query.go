package search

import (
	"fmt"
	"unicode/utf8"
)

// ResolveQueryPath computes the encoded search bucket path for a live
// query against def, given the caller-supplied variable bindings. It walks
// clauses in declaration order exactly like EvaluateDocument does for
// documents, so the returned segments address the same on-disk bucket a
// matching document would have been upserted into.
func ResolveQueryPath(def *Definition, vars map[string]any) ([]string, error) {
	var segments []string
	for i, c := range def.Clauses {
		seg, ok, err := resolveClauseQuery(c, vars)
		if err != nil {
			return nil, fmt.Errorf("clause %d (%s %s): %w", i, c.Op, c.Field, err)
		}
		if ok {
			segments = append(segments, seg)
		}
	}
	return segments, nil
}

func resolveClauseQuery(c Clause, vars map[string]any) (string, bool, error) {
	switch c.Op {
	case OpEquals:
		return resolveEqualsQuery(c, vars)
	case OpIn:
		return resolveInQuery(c, vars)
	case OpExists:
		return resolveExistsQuery(c, vars)
	default:
		return "", false, fmt.Errorf("unsupported op %q", c.Op)
	}
}

func resolveEqualsQuery(c Clause, vars map[string]any) (string, bool, error) {
	if varName, isVar := IsVariable(c.Value); isVar {
		v, ok := lookupVar(vars, varName)
		if !ok {
			return "", false, fmt.Errorf("missing required variable %s", varName)
		}
		switch tv := v.(type) {
		case string:
			if utf8.RuneCountInString(tv) > 63 {
				return "", false, fmt.Errorf("variable %s exceeds the 63-rune limit for equals", varName)
			}
			return EncodeStringValue(tv), true, nil
		case bool:
			return EncodeTruth(tv), true, nil
		default:
			return "", false, fmt.Errorf("variable %s must be a string or boolean", varName)
		}
	}
	if c.Value == nil {
		if c.Truth == "" {
			return "", false, fmt.Errorf("equals value:null requires a truth variable")
		}
		b, err := lookupBoolVar(vars, c.Truth)
		if err != nil {
			return "", false, err
		}
		return EncodeTruth(b), true, nil
	}
	if c.Truth == "" {
		// Pure constant filter: no path segment, always satisfied by
		// construction (the definition itself is the constant).
		return "", false, nil
	}
	b, err := lookupBoolVar(vars, c.Truth)
	if err != nil {
		return "", false, err
	}
	return EncodeTruth(b), true, nil
}

func resolveInQuery(c Clause, vars map[string]any) (string, bool, error) {
	if c.Select == "" {
		return "", false, fmt.Errorf("in clause requires a select variable")
	}
	v, ok := lookupVar(vars, c.Select)
	if !ok {
		return "", false, fmt.Errorf("missing required variable %s", c.Select)
	}
	s, ok := v.(string)
	if !ok {
		return "", false, fmt.Errorf("variable %s must be a string", c.Select)
	}
	values, _ := c.Value.([]any)
	for _, allowed := range values {
		if as, ok := allowed.(string); ok && as == s {
			return EncodeStringValue(s), true, nil
		}
	}
	return "", false, fmt.Errorf("variable %s value %q is not one of the search definition's allowed constants", c.Select, s)
}

func resolveExistsQuery(c Clause, vars map[string]any) (string, bool, error) {
	varName, isVar := IsVariable(c.Value)
	if !isVar {
		return "", false, fmt.Errorf("exists clause requires a truth variable")
	}
	b, err := lookupBoolVar(vars, varName)
	if err != nil {
		return "", false, err
	}
	return EncodeTruth(b), true, nil
}

func lookupVar(vars map[string]any, name string) (any, bool) {
	key := name
	if len(key) > 0 && key[0] == '$' {
		key = key[1:]
	}
	v, ok := vars[key]
	return v, ok
}

func lookupBoolVar(vars map[string]any, name string) (bool, error) {
	v, ok := lookupVar(vars, name)
	if !ok {
		return false, fmt.Errorf("missing required variable %s", name)
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("variable %s must be a boolean", name)
	}
	return b, nil
}
