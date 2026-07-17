package config

import (
	"net/url"
	"unicode"
)

// ValidCollectionName reports whether name follows CONVENTIONS.md:
// UTF-8, no whitespace, first rune is a letter, and no two underscores in a row.
func ValidCollectionName(name string) bool {
	if name == "" {
		return false
	}
	runes := []rune(name)
	if !unicode.IsLetter(runes[0]) {
		return false
	}
	prevUnderscore := false
	for _, r := range runes {
		if unicode.IsSpace(r) {
			return false
		}
		if r == '_' {
			if prevUnderscore {
				return false
			}
			prevUnderscore = true
		} else {
			prevUnderscore = false
		}
	}
	return true
}

// ValidServerName reports whether name is a non-empty identifier without whitespace.
func ValidServerName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// ValidBaseURL reports whether raw is an absolute URL with scheme and host.
func ValidBaseURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.IsAbs() && u.Scheme != "" && u.Host != ""
}

// ValidSearchName reports whether name is a non-empty identifier without whitespace.
func ValidSearchName(name string) bool {
	return ValidServerName(name)
}
