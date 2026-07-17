package accesslang

import (
	"fmt"
	"strings"
	"unicode"
)

// Command is a parsed access-language command.
type Command struct {
	Word   string
	Target string
	Parm   string
	Detail string
}

// Parse splits one command line into word, target, parm, and detail text.
func Parse(line string) (Command, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Command{}, fmt.Errorf("empty command")
	}

	word, rest, ok := splitWord(line)
	if !ok {
		return Command{}, fmt.Errorf("missing command word")
	}
	target, rest, ok := splitWord(rest)
	if !ok {
		return Command{}, fmt.Errorf("missing command target")
	}
	parm, rest, ok := splitWord(rest)
	if !ok {
		return Command{}, fmt.Errorf("missing command parm")
	}
	detail := strings.TrimSpace(rest)
	if detail == "" {
		return Command{}, fmt.Errorf("missing command detail")
	}
	if !strings.HasPrefix(detail, "{") {
		return Command{}, fmt.Errorf("detail must be a pseudo-JSON object")
	}
	return Command{Word: word, Target: target, Parm: parm, Detail: detail}, nil
}

func splitWord(s string) (word, rest string, ok bool) {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	if s == "" {
		return "", "", false
	}
	i := 0
	for i < len(s) && !unicode.IsSpace(rune(s[i])) {
		i++
	}
	return s[:i], strings.TrimLeftFunc(s[i:], unicode.IsSpace), true
}
