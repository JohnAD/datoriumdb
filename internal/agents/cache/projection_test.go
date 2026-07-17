package cache

import (
	"encoding/json"
	"testing"
)

func TestParseAndEncodeRef(t *testing.T) {
	s := EncodeRef("Movies", "id1")
	if s != "@@__Movies__id1" {
		t.Fatalf("unexpected encoding: %q", s)
	}
	col, id, ok := ParseRef(s)
	if !ok || col != "Movies" || id != "id1" {
		t.Fatalf("unexpected parse: col=%q id=%q ok=%v", col, id, ok)
	}
	if _, _, ok := ParseRef("not-a-ref"); ok {
		t.Fatalf("expected a non-prefixed string to fail to parse")
	}
	if _, _, ok := ParseRef("@@__onlyCollection"); ok {
		t.Fatalf("expected a ref missing the id half to fail to parse")
	}
}

const reviewSchemaWithRef = `{
  "kind": "object",
  "children": [
    {"name": "movieRef", "kind": "string", "format": "DatoriumCachedRef",
      "custom": {"collections": ["Movies"], "summary": ["title", "status"]}},
    {"name": "text", "kind": "string"}
  ]
}`

func TestFindRefFields(t *testing.T) {
	doc := map[string]any{
		"movieRef": EncodeRef("Movies", "id1"),
		"text":     "great movie",
	}
	fields, err := FindRefFields(json.RawMessage(reviewSchemaWithRef), doc)
	if err != nil {
		t.Fatalf("FindRefFields: %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("expected exactly one ref field, got %+v", fields)
	}
	f := fields[0]
	if f.FieldName != "movieRef" || f.TargetCollection != "Movies" || f.TargetID != "id1" {
		t.Fatalf("unexpected ref field: %+v", f)
	}
	if len(f.Summary) != 2 || f.Summary[0] != "title" || f.Summary[1] != "status" {
		t.Fatalf("unexpected summary paths: %v", f.Summary)
	}
}

func TestFindRefFieldsSkipsUnresolvableValues(t *testing.T) {
	doc := map[string]any{"movieRef": "not-a-ref", "text": "x"}
	fields, err := FindRefFields(json.RawMessage(reviewSchemaWithRef), doc)
	if err != nil {
		t.Fatalf("FindRefFields: %v", err)
	}
	if len(fields) != 0 {
		t.Fatalf("expected no ref fields for an unparsable ref value, got %+v", fields)
	}
}

func TestBuildSummary(t *testing.T) {
	cached := map[string]any{
		"!":      "id1",
		"$":      "Movies:0",
		"#":      "v3",
		"title":  "The Matrix",
		"status": "released",
		"secret": "internal-only",
	}
	summary := BuildSummary(cached, []string{"title", "status", "missingField"})
	if summary["!"] != "id1" || summary["$"] != "Movies:0" || summary["#"] != "v3" {
		t.Fatalf("expected !/$/# to always ride along: %+v", summary)
	}
	if summary["title"] != "The Matrix" || summary["status"] != "released" {
		t.Fatalf("expected summary paths projected: %+v", summary)
	}
	if _, ok := summary["secret"]; ok {
		t.Fatalf("expected non-summary fields to be excluded: %+v", summary)
	}
	if _, ok := summary["missingField"]; ok {
		t.Fatalf("expected an unresolvable summary path to be skipped: %+v", summary)
	}
}

func TestBuildSummaryDeletedSourceHasNilVersion(t *testing.T) {
	cached := map[string]any{"!": "id1", "#": nil}
	summary := BuildSummary(cached, []string{"title"})
	if summary["#"] != nil {
		t.Fatalf("expected # to be nil for a deleted source, got %v", summary["#"])
	}
	if _, ok := summary["title"]; ok {
		t.Fatalf("expected no title in a lost-reference summary: %+v", summary)
	}
}

func TestLoadCacheFileAndEnsureStub(t *testing.T) {
	dir := t.TempDir()
	if _, ok, err := LoadCacheFile(dir, "Movies", "id1"); err != nil || ok {
		t.Fatalf("expected no cache file yet, ok=%v err=%v", ok, err)
	}

	stub, err := EnsureStub(dir, "Movies", "id1")
	if err != nil {
		t.Fatalf("EnsureStub: %v", err)
	}
	if stub["!"] != "id1" || stub["#"] != nil {
		t.Fatalf("unexpected stub: %+v", stub)
	}

	loaded, ok, err := LoadCacheFile(dir, "Movies", "id1")
	if err != nil || !ok {
		t.Fatalf("expected the stub to now be loadable, ok=%v err=%v", ok, err)
	}
	if loaded["!"] != "id1" {
		t.Fatalf("unexpected loaded stub: %+v", loaded)
	}

	// EnsureStub must not clobber an existing cache file (e.g. one already
	// populated by a prior Apply call).
	if err := writeRealCache(dir, "id1"); err != nil {
		t.Fatalf("writeRealCache: %v", err)
	}
	again, err := EnsureStub(dir, "Movies", "id1")
	if err != nil {
		t.Fatalf("EnsureStub (existing): %v", err)
	}
	if again["title"] != "T" {
		t.Fatalf("expected EnsureStub to return the existing populated cache, got %+v", again)
	}
}

func writeRealCache(dir, id string) error {
	item := WorkItem{SourceCollection: "Movies", SourceDocumentID: id, Command: "create", Payload: map[string]any{"!": id, "#": "v1", "title": "T"}}
	_, err := Apply(dir, item)
	return err
}
