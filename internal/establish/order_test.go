package establish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEstablishAndWriteSchemaPreservesOrder(t *testing.T) {
	// Compact schema with intentional field order (kind before children,
	// name before kind inside each child).
	body := []byte(`{
  "ok": true,
  "general": {"name": "x", "establishmentServer": "serverA", "version": 1},
  "servers": {"serverA": {"baseURL": "http://127.0.0.1:1"}},
  "shardMap": {"default": {}},
  "auth": {"issuer": "iss", "audience": "aud", "keys": []},
  "schemas": {
    "Items": {
      "version": 0,
      "schema": {"kind":"object","children":[{"name":"title","kind":"string","required":true},{"name":"owner","kind":"string","format":"DatoriumDirectRef","required":true}]}
    }
  },
  "searches": {}
}`)
	doc, err := parseEstablishResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Items.schema.json")
	if err := writeRaw(path, doc.Schemas["Items"].Schema); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "{\n  \"kind\": \"object\"") {
		t.Fatalf("expected pretty schema, got:\n%s", s)
	}
	if strings.Index(s, `"kind"`) > strings.Index(s, `"children"`) {
		t.Fatalf("expected kind before children:\n%s", s)
	}
	childRegion := s[strings.Index(s, "["):]
	if strings.Index(childRegion, `"name"`) > strings.Index(childRegion, `"kind"`) {
		t.Fatalf("expected child name before kind:\n%s", s)
	}
}
