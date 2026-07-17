package search

import (
	"testing"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/ojson"
)

func compiledRootFrom(t *testing.T, schemaJSON string) ojson.SchemaEntry {
	t.Helper()
	schema, err := config.CompileSchemaBytes([]byte(schemaJSON))
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return schema.Root()
}

func TestResolveFieldSchemaObjectAndArray(t *testing.T) {
	const schemaJSON = `{
		"kind": "object",
		"children": [
			{"name": "title", "kind": "string"},
			{"name": "reviews", "kind": "array", "items": {"kind": "object", "children": [
				{"name": "score", "kind": "number"}
			]}}
		]
	}`
	root := compiledRootFrom(t, schemaJSON)
	if _, ok := ResolveFieldSchema(root, "/title"); !ok {
		t.Fatalf("expected /title to resolve")
	}
	if _, ok := ResolveFieldSchema(root, "/missing"); ok {
		t.Fatalf("expected /missing to not resolve")
	}
	entry, ok := ResolveFieldSchema(root, "/reviews/0/score")
	if !ok {
		t.Fatalf("expected /reviews/0/score to resolve through array items")
	}
	if entry.Kind() != ojson.KindNumber {
		t.Fatalf("expected number kind, got %v", entry.Kind())
	}
}

func TestPointerGet(t *testing.T) {
	doc := map[string]any{
		"a": map[string]any{"b": []any{"x", "y"}},
	}
	v, ok := PointerGet(doc, "/a/b/1")
	if !ok || v != "y" {
		t.Fatalf("expected y, got %v ok=%v", v, ok)
	}
	if _, ok := PointerGet(doc, "/a/b/5"); ok {
		t.Fatalf("expected out-of-range index to be absent")
	}
	if _, ok := PointerGet(doc, "/z"); ok {
		t.Fatalf("expected missing top-level field to be absent")
	}
}

func TestEncodeStringValue(t *testing.T) {
	if got := EncodeStringValue(""); got != "empty" {
		t.Fatalf("expected empty encoding, got %q", got)
	}
	if got := EncodeStringValue("ab"); got != "6162" {
		t.Fatalf("expected uppercase hex 6162, got %q", got)
	}
}

func TestEncodeTruth(t *testing.T) {
	if EncodeTruth(true) != "true" || EncodeTruth(false) != "false" {
		t.Fatalf("unexpected truth encoding")
	}
}

func TestShardInputAndSlotDeterministic(t *testing.T) {
	segs := []string{"72656C6561736564", "7363696669"}
	if ShardInput(segs) != "72656C6561736564/7363696669" {
		t.Fatalf("unexpected shard input: %q", ShardInput(segs))
	}
	a := ShardSlot(segs)
	b := ShardSlot(segs)
	if a != b {
		t.Fatalf("expected deterministic shard slot")
	}
}
