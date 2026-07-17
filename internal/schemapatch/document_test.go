package schemapatch

import (
	"reflect"
	"testing"
)

func specFromJSON(t *testing.T, body string) *UpdateSpec {
	t.Helper()
	spec, err := ParseUpdateSpec([]byte(body))
	if err != nil {
		t.Fatalf("ParseUpdateSpec: %v", err)
	}
	return spec
}

const testULID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func TestApplyToDocumentAddWithValue(t *testing.T) {
	spec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"add","path":"/genre","value":"scifi","schema":{"kind":"string"}}
	]}`)
	doc := map[string]any{"title": "T"}
	out, err := ApplyToDocument(doc, spec)
	if err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if out["genre"] != "scifi" {
		t.Fatalf("expected genre=scifi, got %+v", out)
	}
}

func TestApplyToDocumentAddRequiredUsesDefaultThenFailoverThenEmpty(t *testing.T) {
	specDefault := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"add","path":"/genre","schema":{"kind":"string","required":true,"default":"unknown"}}
	]}`)
	doc := map[string]any{}
	if _, err := ApplyToDocument(doc, specDefault); err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if doc["genre"] != "unknown" {
		t.Fatalf("expected default value used, got %+v", doc)
	}

	specFailover := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"add","path":"/genre","failover":"fallback","schema":{"kind":"string","required":true}}
	]}`)
	doc2 := map[string]any{}
	if _, err := ApplyToDocument(doc2, specFailover); err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if doc2["genre"] != "fallback" {
		t.Fatalf("expected failover value used, got %+v", doc2)
	}

	specEmpty := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"add","path":"/genre","schema":{"kind":"string","required":true}}
	]}`)
	doc3 := map[string]any{}
	if _, err := ApplyToDocument(doc3, specEmpty); err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if doc3["genre"] != "" {
		t.Fatalf("expected empty-string fallback for a required field with no default/failover, got %+v", doc3)
	}
}

func TestApplyToDocumentAddNotRequiredWithoutValueStaysAbsent(t *testing.T) {
	spec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"add","path":"/genre","schema":{"kind":"string"}}
	]}`)
	doc := map[string]any{}
	if _, err := ApplyToDocument(doc, spec); err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if _, ok := doc["genre"]; ok {
		t.Fatalf("expected genre to stay absent, got %+v", doc)
	}
}

func TestApplyToDocumentImportReusesMatchingExtraField(t *testing.T) {
	spec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"import","path":"/genre","value":"placeholder","schema":{"kind":"string"}}
	]}`)
	doc := map[string]any{"genre": "already-here"}
	if _, err := ApplyToDocument(doc, spec); err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if doc["genre"] != "already-here" {
		t.Fatalf("expected the preexisting extra field to be reused as-is, got %+v", doc)
	}
}

func TestApplyToDocumentImportDiscardsMismatchedKindExtraField(t *testing.T) {
	spec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"import","path":"/genre","value":"fromValue","schema":{"kind":"string"}}
	]}`)
	doc := map[string]any{"genre": 42.0} // wrong kind: number instead of string
	if _, err := ApplyToDocument(doc, spec); err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if doc["genre"] != "fromValue" {
		t.Fatalf("expected the mismatched extra field discarded and value used instead, got %+v", doc)
	}
}

func TestApplyToDocumentRemove(t *testing.T) {
	spec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"remove","path":"/legacy"}
	]}`)
	doc := map[string]any{"legacy": "gone-soon", "title": "T"}
	if _, err := ApplyToDocument(doc, spec); err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if _, ok := doc["legacy"]; ok {
		t.Fatalf("expected legacy field removed, got %+v", doc)
	}
	if doc["title"] != "T" {
		t.Fatalf("expected unrelated fields untouched, got %+v", doc)
	}
}

func TestApplyToDocumentAbandonLeavesValueUntouched(t *testing.T) {
	spec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"abandon","path":"/legacy"}
	]}`)
	doc := map[string]any{"legacy": "still-here"}
	if _, err := ApplyToDocument(doc, spec); err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if doc["legacy"] != "still-here" {
		t.Fatalf("expected abandon to leave the value untouched, got %+v", doc)
	}
}

func TestApplyToDocumentReplace(t *testing.T) {
	spec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"replace","path":"/status","value":"archived"}
	]}`)
	doc := map[string]any{"status": "draft"}
	if _, err := ApplyToDocument(doc, spec); err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if doc["status"] != "archived" {
		t.Fatalf("expected status replaced, got %+v", doc)
	}
}

func TestApplyToDocumentMoveAndCopy(t *testing.T) {
	moveSpec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"move","from":"/oldName","path":"/newName"}
	]}`)
	doc := map[string]any{"oldName": "value1"}
	if _, err := ApplyToDocument(doc, moveSpec); err != nil {
		t.Fatalf("ApplyToDocument (move): %v", err)
	}
	if _, ok := doc["oldName"]; ok {
		t.Fatalf("expected oldName removed after move, got %+v", doc)
	}
	if doc["newName"] != "value1" {
		t.Fatalf("expected newName set after move, got %+v", doc)
	}

	copySpec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"copy","from":"/newName","path":"/copyName"}
	]}`)
	if _, err := ApplyToDocument(doc, copySpec); err != nil {
		t.Fatalf("ApplyToDocument (copy): %v", err)
	}
	if doc["newName"] != "value1" || doc["copyName"] != "value1" {
		t.Fatalf("expected both source and destination present after copy, got %+v", doc)
	}

	moveMissingSpec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"move","from":"/doesNotExist","path":"/whatever"}
	]}`)
	doc2 := map[string]any{}
	if _, err := ApplyToDocument(doc2, moveMissingSpec); err != nil {
		t.Fatalf("ApplyToDocument (move missing): %v", err)
	}
	if len(doc2) != 0 {
		t.Fatalf("expected a no-op move of an absent field, got %+v", doc2)
	}
}

func TestApplyToDocumentConvertKindChanges(t *testing.T) {
	cases := []struct {
		name     string
		newKind  string
		input    any
		expected any
	}{
		{"string-to-boolean-nonempty", "boolean", "hi", true},
		{"empty-string-to-boolean", "boolean", "", false},
		{"number-to-string", "string", 3.5, "3.5"},
		{"bool-to-number-true", "number", true, 1.0},
		{"bool-to-number-false", "number", false, 0.0},
		{"string-to-number", "number", "42abc", 42.0},
		{"scalar-to-array", "array", "x", []any{"x"}},
		{"array-to-number-length", "number", []any{"a", "b"}, 2.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
				{"op":"convert","path":"/f","schema":{"kind":"`+c.newKind+`"}}
			]}`)
			doc := map[string]any{"f": c.input}
			if _, err := ApplyToDocument(doc, spec); err != nil {
				t.Fatalf("ApplyToDocument: %v", err)
			}
			if !reflect.DeepEqual(doc["f"], c.expected) {
				t.Fatalf("expected %v, got %v", c.expected, doc["f"])
			}
		})
	}
}

func TestApplyToDocumentConvertWithExplicitValueOverridesConversion(t *testing.T) {
	spec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"convert","path":"/f","value":"forced","schema":{"kind":"string"}}
	]}`)
	doc := map[string]any{"f": 123.0}
	if _, err := ApplyToDocument(doc, spec); err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if doc["f"] != "forced" {
		t.Fatalf("expected explicit value to override the automatic conversion, got %+v", doc)
	}
}

func TestApplyToDocumentMultipleUpdatesInOrder(t *testing.T) {
	spec := specFromJSON(t, `{"from":0,"to":1,"new_ver_id":"`+testULID+`","updates":[
		{"op":"add","path":"/genre","value":"scifi","schema":{"kind":"string"}},
		{"op":"move","from":"/genre","path":"/category"},
		{"op":"remove","path":"/legacy"}
	]}`)
	doc := map[string]any{"legacy": "x"}
	if _, err := ApplyToDocument(doc, spec); err != nil {
		t.Fatalf("ApplyToDocument: %v", err)
	}
	if doc["category"] != "scifi" {
		t.Fatalf("expected category=scifi after add+move chain, got %+v", doc)
	}
	if _, ok := doc["genre"]; ok {
		t.Fatalf("expected genre moved away, got %+v", doc)
	}
	if _, ok := doc["legacy"]; ok {
		t.Fatalf("expected legacy removed, got %+v", doc)
	}
}
