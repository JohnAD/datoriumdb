package rfc6902

import (
	"reflect"
	"testing"
)

func TestRFC6902NestedAndArray(t *testing.T) {
	doc := map[string]any{
		"a":    map[string]any{"b": "x"},
		"tags": []any{"one", "two"},
		"esc":  map[string]any{"a/b": 1, "c~d": 2},
	}
	if err := Apply(doc, map[string]any{"op": "replace", "path": "/a/b", "value": "y"}); err != nil {
		t.Fatal(err)
	}
	if doc["a"].(map[string]any)["b"] != "y" {
		t.Fatalf("nested replace failed: %#v", doc)
	}
	if err := Apply(doc, map[string]any{"op": "add", "path": "/tags/-", "value": "three"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc["tags"], []any{"one", "two", "three"}) {
		t.Fatalf("array append failed: %#v", doc["tags"])
	}
	if err := Apply(doc, map[string]any{"op": "remove", "path": "/tags/1"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc["tags"], []any{"one", "three"}) {
		t.Fatalf("array remove failed: %#v", doc["tags"])
	}
	if err := Apply(doc, map[string]any{"op": "test", "path": "/a/b", "value": "y"}); err != nil {
		t.Fatal(err)
	}
	if err := Apply(doc, map[string]any{"op": "replace", "path": "/esc/a~1b", "value": 9}); err != nil {
		t.Fatal(err)
	}
	if doc["esc"].(map[string]any)["a/b"] != 9 {
		t.Fatalf("escaped pointer failed: %#v", doc["esc"])
	}
}

func TestRFC6902RejectMissingReplace(t *testing.T) {
	doc := map[string]any{"a": 1}
	if err := Apply(doc, map[string]any{"op": "replace", "path": "/missing", "value": 2}); err == nil {
		t.Fatal("expected error")
	}
}
