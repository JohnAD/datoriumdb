package config

import (
	"strings"
	"testing"
)

func TestPrettyJSONBytesPreservesOrderAndPrettyPrints(t *testing.T) {
	raw := []byte(`{"kind":"object","children":[{"name":"title","kind":"string","required":true},{"name":"owner","kind":"string","format":"DatoriumDirectRef","required":true}]}`)
	out, err := PrettyJSONBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "{\n  \"kind\": \"object\"") {
		t.Fatalf("expected pretty kind-first object, got:\n%s", s)
	}
	kindIdx := strings.Index(s, `"kind"`)
	childrenIdx := strings.Index(s, `"children"`)
	if kindIdx < 0 || childrenIdx < 0 || kindIdx > childrenIdx {
		t.Fatalf("expected kind before children, got:\n%s", s)
	}
	nameIdx := strings.Index(s, `"name"`)
	innerKindIdx := strings.LastIndex(s, `"kind"`)
	// First child should keep name before its kind.
	firstChild := s[strings.Index(s, "["):]
	if iName, iKind := strings.Index(firstChild, `"name"`), strings.Index(firstChild, `"kind"`); iName < 0 || iKind < 0 || iName > iKind {
		t.Fatalf("expected child name before kind, got:\n%s", s)
	}
	_ = nameIdx
	_ = innerKindIdx
	if !strings.HasSuffix(s, "\n") {
		t.Fatal("expected trailing newline")
	}
}

func TestPrettyJSONBytesRejectsInvalid(t *testing.T) {
	if _, err := PrettyJSONBytes([]byte(`{`)); err == nil {
		t.Fatal("expected error")
	}
}
