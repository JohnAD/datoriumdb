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

const userSchemaWithListArray = `{
  "kind": "object",
  "children": [
    {"name": "displayName", "kind": "string"},
    {"name": "todoLists", "kind": "array",
      "items": {"kind": "string", "format": "DatoriumCachedRef",
        "custom": {"collections": ["TodoLists"], "summary": ["title"]}}}
  ]
}`

func TestFindRefFieldsArrayOfCachedRefs(t *testing.T) {
	doc := map[string]any{
		"displayName": "Grace",
		"todoLists": []any{
			EncodeRef("TodoLists", "listA"),
			EncodeRef("TodoLists", "listB"),
			"not-a-ref",
		},
	}
	fields, err := FindRefFields(json.RawMessage(userSchemaWithListArray), doc)
	if err != nil {
		t.Fatalf("FindRefFields: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 array refs, got %+v", fields)
	}
	if fields[0].TargetID != "listA" || fields[1].TargetID != "listB" {
		t.Fatalf("unexpected targets: %+v", fields)
	}
	if fields[0].FieldName != "todoLists" || len(fields[0].Summary) != 1 || fields[0].Summary[0] != "title" {
		t.Fatalf("unexpected first field: %+v", fields[0])
	}
}

const nestedProfileSchema = `{
  "kind": "object",
  "children": [
    {"name": "displayName", "kind": "string"},
    {"name": "profile", "kind": "object", "children": [
      {"name": "lists", "kind": "array",
        "items": {"kind": "string", "format": "DatoriumCachedRef",
          "custom": {"collections": ["TodoLists"], "summary": ["title"]}}},
      {"name": "primary", "kind": "string", "format": "DatoriumCachedRef",
        "custom": {"collections": ["TodoLists"], "summary": ["title"]}}
    ]},
    {"name": "folders", "kind": "array", "items": {"kind": "object", "children": [
      {"name": "name", "kind": "string"},
      {"name": "ref", "kind": "string", "format": "DatoriumCachedRef",
        "custom": {"collections": ["TodoLists"], "summary": ["title"]}}
    ]}}
  ]
}`

func TestFindRefFieldsNestedObjectsAndArrays(t *testing.T) {
	doc := map[string]any{
		"displayName": "Ada",
		"profile": map[string]any{
			"lists": []any{
				EncodeRef("TodoLists", "nestedA"),
				EncodeRef("TodoLists", "nestedB"),
			},
			"primary": EncodeRef("TodoLists", "primary1"),
		},
		"folders": []any{
			map[string]any{
				"name": "Work",
				"ref":  EncodeRef("TodoLists", "folderWork"),
			},
			map[string]any{
				"name": "Home",
				"ref":  EncodeRef("TodoLists", "folderHome"),
			},
		},
	}
	fields, err := FindRefFields(json.RawMessage(nestedProfileSchema), doc)
	if err != nil {
		t.Fatalf("FindRefFields: %v", err)
	}
	got := map[string]string{}
	for _, f := range fields {
		got[f.FieldName+"|"+f.TargetID] = f.TargetCollection
		if len(f.Summary) != 1 || f.Summary[0] != "title" {
			t.Fatalf("unexpected summary on %+v", f)
		}
	}
	want := []string{
		"profile/lists|nestedA",
		"profile/lists|nestedB",
		"profile/primary|primary1",
		"folders/ref|folderWork",
		"folders/ref|folderHome",
	}
	if len(fields) != len(want) {
		t.Fatalf("expected %d refs, got %d (%+v)", len(want), len(fields), fields)
	}
	for _, key := range want {
		if got[key] != "TodoLists" {
			t.Fatalf("missing or wrong ref %q in %+v", key, fields)
		}
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
