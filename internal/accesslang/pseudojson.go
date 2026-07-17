package accesslang

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ParseDetail parses a pseudo-JSON detail object into a generic map.
// Quotes are optional for keys and string values that do not contain spaces.
func ParseDetail(detail string) (map[string]any, error) {
	detail = strings.TrimSpace(detail)
	strict, err := pseudoToStrictJSON(detail)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(strict))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("invalid detail object: %w", err)
	}
	return out, nil
}

func pseudoToStrictJSON(input string) (string, error) {
	var b strings.Builder
	i := 0
	expectKey := false
	expectValue := false
	stack := []byte{} // '{', '['
	for i < len(input) {
		for i < len(input) && unicode.IsSpace(rune(input[i])) {
			b.WriteByte(input[i])
			i++
		}
		if i >= len(input) {
			break
		}
		r := rune(input[i])
		switch {
		case r == '{' || r == '[':
			b.WriteByte(input[i])
			stack = append(stack, input[i])
			expectKey = input[i] == '{'
			expectValue = input[i] == '['
			i++
		case r == '}' || r == ']':
			b.WriteByte(input[i])
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			expectKey = false
			expectValue = false
			i++
		case r == ',':
			b.WriteByte(input[i])
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				expectKey = true
				expectValue = false
			} else {
				expectValue = true
				expectKey = false
			}
			i++
		case r == ':':
			b.WriteByte(input[i])
			expectKey = false
			expectValue = true
			i++
		case r == '"' || r == '\'':
			str, next, err := readQuoted(input, i)
			if err != nil {
				return "", err
			}
			enc, _ := json.Marshal(str)
			b.Write(enc)
			i = next
			expectKey = false
			expectValue = false
		default:
			allowColon := expectValue
			token, next := readToken(input, i, allowColon)
			if expectKey {
				enc, _ := json.Marshal(token)
				b.Write(enc)
				expectKey = false
			} else {
				b.WriteString(encodeToken(token))
				expectValue = false
			}
			i = next
		}
	}
	return b.String(), nil
}

func readQuoted(input string, start int) (string, int, error) {
	quote := input[start]
	i := start + 1
	var b strings.Builder
	for i < len(input) {
		c := input[i]
		if c == '\\' && i+1 < len(input) {
			b.WriteByte(input[i+1])
			i += 2
			continue
		}
		if c == quote {
			return b.String(), i + 1, nil
		}
		b.WriteByte(c)
		i++
	}
	return "", 0, fmt.Errorf("unterminated string")
}

func readToken(input string, start int, allowColon bool) (string, int) {
	i := start
	for i < len(input) {
		r := rune(input[i])
		if unicode.IsSpace(r) || r == ',' || r == '}' || r == ']' {
			break
		}
		if r == ':' && !allowColon {
			break
		}
		i++
	}
	return input[start:i], i
}

func encodeToken(token string) string {
	switch token {
	case "true", "false", "null":
		return token
	}
	if looksLikeNumber(token) {
		return token
	}
	enc, _ := json.Marshal(token)
	return string(enc)
}

func looksLikeNumber(token string) bool {
	if token == "" {
		return false
	}
	if _, err := strconv.ParseFloat(token, 64); err == nil {
		return true
	}
	return false
}
