package search

import "testing"

func defFrom(t *testing.T, body string) *Definition {
	t.Helper()
	def, err := ParseDefinition([]byte(body))
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	return def
}

func mustOne(t *testing.T, buckets []EvalResult, err error) EvalResult {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("expected exactly one bucket, got %#v", buckets)
	}
	return buckets[0]
}

func TestEvaluateDocumentEqualsVariable(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/status","op":"equals","value":"$status"}],"sort":[]}}`)

	_buckets, _err := EvaluateDocument(def, map[string]any{"status": "released"})
	res := mustOne(t, _buckets, _err)
	if !res.Matched || len(res.Segments) != 1 || res.Segments[0] != EncodeStringValue("released") {
		t.Fatalf("unexpected result: %+v", res)
	}

	buckets, err := EvaluateDocument(def, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buckets) != 0 {
		t.Fatalf("expected no match when field missing")
	}
}

func TestEvaluateDocumentEqualsConstantFilter(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/highRated","op":"equals","value":true}],"sort":[]}}`)

	_buckets, _err := EvaluateDocument(def, map[string]any{"highRated": true})
	matched := mustOne(t, _buckets, _err)
	if !matched.Matched || len(matched.Segments) != 0 {
		t.Fatalf("expected match with zero segments, got %+v", matched)
	}

	buckets, err := EvaluateDocument(def, map[string]any{"highRated": false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buckets) != 0 {
		t.Fatalf("expected the document to be excluded entirely when the constant filter fails")
	}
}

func TestEvaluateDocumentEqualsConstantWithTruth(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/highRated","op":"equals","value":true,"truth":"$wantHigh"}],"sort":[]}}`)

	_buckets, _err := EvaluateDocument(def, map[string]any{"highRated": true})
	trueBucket := mustOne(t, _buckets, _err)
	if !trueBucket.Matched || trueBucket.Segments[0] != "true" {
		t.Fatalf("expected true bucket, got %+v", trueBucket)
	}
	_buckets, _err = EvaluateDocument(def, map[string]any{"highRated": false})
	falseBucket := mustOne(t, _buckets, _err)
	if !falseBucket.Matched || falseBucket.Segments[0] != "false" {
		t.Fatalf("expected false bucket, got %+v", falseBucket)
	}
}

func TestEvaluateDocumentEqualsNullWithTruth(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/retiredAt","op":"equals","value":null,"truth":"$isRetired"}],"sort":[]}}`)

	_buckets, _err := EvaluateDocument(def, map[string]any{"retiredAt": nil})
	nullCase := mustOne(t, _buckets, _err)
	if !nullCase.Matched || nullCase.Segments[0] != "true" {
		t.Fatalf("expected true bucket for null value, got %+v", nullCase)
	}
	_buckets, _err = EvaluateDocument(def, map[string]any{"retiredAt": "2020"})
	knownCase := mustOne(t, _buckets, _err)
	if !knownCase.Matched || knownCase.Segments[0] != "false" {
		t.Fatalf("expected false bucket for known value, got %+v", knownCase)
	}
	_buckets, _err = EvaluateDocument(def, map[string]any{})
	missingCase := mustOne(t, _buckets, _err)
	if !missingCase.Matched || missingCase.Segments[0] != "false" {
		t.Fatalf("expected false bucket for missing value, got %+v", missingCase)
	}
}

func TestEvaluateDocumentIn(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"in","value":["scifi","drama"],"select":"$genre"}],"sort":[]}}`)

	_buckets, _err := EvaluateDocument(def, map[string]any{"genre": "scifi"})
	inBucket := mustOne(t, _buckets, _err)
	if !inBucket.Matched || inBucket.Segments[0] != EncodeStringValue("scifi") {
		t.Fatalf("expected scifi bucket, got %+v", inBucket)
	}
	buckets, err := EvaluateDocument(def, map[string]any{"genre": "horror"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buckets) != 0 {
		t.Fatalf("expected no match when value is not one of the allowed constants")
	}
	buckets, err = EvaluateDocument(def, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buckets) != 0 {
		t.Fatalf("expected no match when field is missing")
	}
}

func TestEvaluateDocumentExists(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"exists","value":"$hasGenre"}],"sort":[]}}`)

	_buckets, _err := EvaluateDocument(def, map[string]any{"genre": "scifi"})
	present := mustOne(t, _buckets, _err)
	if !present.Matched || present.Segments[0] != "true" {
		t.Fatalf("expected true bucket, got %+v", present)
	}
	_buckets, _err = EvaluateDocument(def, map[string]any{})
	absent := mustOne(t, _buckets, _err)
	if !absent.Matched || absent.Segments[0] != "false" {
		t.Fatalf("expected false bucket, got %+v", absent)
	}
}

func TestEvaluateDocumentExistsHideNulls(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"exists","value":"$hasGenre","hideNulls":true}],"sort":[]}}`)

	_buckets, _err := EvaluateDocument(def, map[string]any{"genre": nil})
	nullCase := mustOne(t, _buckets, _err)
	if !nullCase.Matched || nullCase.Segments[0] != "false" {
		t.Fatalf("expected false bucket when hideNulls treats null as absent, got %+v", nullCase)
	}
}

func TestEvaluateDocumentNilDocument(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/status","op":"equals","value":"released"}],"sort":[]}}`)
	buckets, err := EvaluateDocument(def, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buckets) != 0 {
		t.Fatalf("expected no match for a nil (deleted) document")
	}
}

func TestEvaluateDocumentContainsVariableMultiBucket(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Todos","name":"n","version":1,
		"v1":{"clauses":[{"field":"/stateKeys","op":"contains","value":"$status"}],"sort":[]}}`)

	buckets, err := EvaluateDocument(def, map[string]any{"stateKeys": []any{"all", "todo", "all"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("expected two distinct buckets, got %#v", buckets)
	}
	seen := map[string]bool{}
	for _, b := range buckets {
		if len(b.Segments) != 1 {
			t.Fatalf("unexpected segments: %#v", b)
		}
		seen[b.Segments[0]] = true
	}
	if !seen[EncodeStringValue("all")] || !seen[EncodeStringValue("todo")] {
		t.Fatalf("missing expected segments: %#v", seen)
	}
}

func TestEvaluateDocumentContainsConstantTruth(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Todos","name":"n","version":1,
		"v1":{"clauses":[{"field":"/tags","op":"contains","value":"urgent","truth":"$hasUrgent"}],"sort":[]}}`)

	_buckets, _err := EvaluateDocument(def, map[string]any{"tags": []any{"urgent", "home"}})
	yes := mustOne(t, _buckets, _err)
	if yes.Segments[0] != "true" {
		t.Fatalf("expected true bucket, got %+v", yes)
	}
	_buckets, _err = EvaluateDocument(def, map[string]any{"tags": []any{"home"}})
	no := mustOne(t, _buckets, _err)
	if no.Segments[0] != "false" {
		t.Fatalf("expected false bucket, got %+v", no)
	}
}

func TestEvaluateDocumentContainsEmptyArray(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Todos","name":"n","version":1,
		"v1":{"clauses":[{"field":"/stateKeys","op":"contains","value":"$status"}],"sort":[]}}`)
	buckets, err := EvaluateDocument(def, map[string]any{"stateKeys": []any{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buckets) != 0 {
		t.Fatalf("expected no buckets for empty array")
	}
}

func TestEvaluateDocumentContainsNumberScalar(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/ratings","op":"contains","value":"$r"}],"sort":[]}}`)
	buckets, err := EvaluateDocument(def, map[string]any{"ratings": []any{5.0, 3.0}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("expected two number buckets, got %#v", buckets)
	}
	segs, err := ResolveQueryPath(def, map[string]any{"r": 5})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range buckets {
		if len(b.Segments) == 1 && b.Segments[0] == segs[0] {
			found = true
		}
	}
	if !found {
		t.Fatalf("query segment %v not among buckets %#v", segs, buckets)
	}
}
