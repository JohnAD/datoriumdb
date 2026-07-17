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

func TestEvaluateDocumentEqualsVariable(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/status","op":"equals","value":"$status"}],"sort":[]}}`)

	res, err := EvaluateDocument(def, map[string]any{"status": "released"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Matched || len(res.Segments) != 1 || res.Segments[0] != EncodeStringValue("released") {
		t.Fatalf("unexpected result: %+v", res)
	}

	// missing field: no match for a variable-valued equals clause.
	res2, err := EvaluateDocument(def, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.Matched {
		t.Fatalf("expected no match when field missing")
	}
}

func TestEvaluateDocumentEqualsConstantFilter(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/highRated","op":"equals","value":true}],"sort":[]}}`)

	matched, err := EvaluateDocument(def, map[string]any{"highRated": true})
	if err != nil || !matched.Matched || len(matched.Segments) != 0 {
		t.Fatalf("expected match with zero segments, got %+v err=%v", matched, err)
	}

	unmatched, err := EvaluateDocument(def, map[string]any{"highRated": false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unmatched.Matched {
		t.Fatalf("expected the document to be excluded entirely when the constant filter fails")
	}
}

func TestEvaluateDocumentEqualsConstantWithTruth(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/highRated","op":"equals","value":true,"truth":"$wantHigh"}],"sort":[]}}`)

	trueBucket, err := EvaluateDocument(def, map[string]any{"highRated": true})
	if err != nil || !trueBucket.Matched || trueBucket.Segments[0] != "true" {
		t.Fatalf("expected true bucket, got %+v err=%v", trueBucket, err)
	}
	falseBucket, err := EvaluateDocument(def, map[string]any{"highRated": false})
	if err != nil || !falseBucket.Matched || falseBucket.Segments[0] != "false" {
		t.Fatalf("expected false bucket, got %+v err=%v", falseBucket, err)
	}
}

func TestEvaluateDocumentEqualsNullWithTruth(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/retiredAt","op":"equals","value":null,"truth":"$isRetired"}],"sort":[]}}`)

	nullCase, err := EvaluateDocument(def, map[string]any{"retiredAt": nil})
	if err != nil || !nullCase.Matched || nullCase.Segments[0] != "true" {
		t.Fatalf("expected true bucket for null value, got %+v err=%v", nullCase, err)
	}
	knownCase, err := EvaluateDocument(def, map[string]any{"retiredAt": "2020"})
	if err != nil || !knownCase.Matched || knownCase.Segments[0] != "false" {
		t.Fatalf("expected false bucket for known value, got %+v err=%v", knownCase, err)
	}
	missingCase, err := EvaluateDocument(def, map[string]any{})
	if err != nil || !missingCase.Matched || missingCase.Segments[0] != "false" {
		t.Fatalf("expected false bucket for missing value, got %+v err=%v", missingCase, err)
	}
}

func TestEvaluateDocumentIn(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"in","value":["scifi","drama"],"select":"$genre"}],"sort":[]}}`)

	inBucket, err := EvaluateDocument(def, map[string]any{"genre": "scifi"})
	if err != nil || !inBucket.Matched || inBucket.Segments[0] != EncodeStringValue("scifi") {
		t.Fatalf("expected scifi bucket, got %+v err=%v", inBucket, err)
	}
	notAllowed, err := EvaluateDocument(def, map[string]any{"genre": "horror"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notAllowed.Matched {
		t.Fatalf("expected no match when value is not one of the allowed constants")
	}
	missing, err := EvaluateDocument(def, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if missing.Matched {
		t.Fatalf("expected no match when field is missing")
	}
}

func TestEvaluateDocumentExists(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"exists","value":"$hasGenre"}],"sort":[]}}`)

	present, err := EvaluateDocument(def, map[string]any{"genre": "scifi"})
	if err != nil || !present.Matched || present.Segments[0] != "true" {
		t.Fatalf("expected true bucket, got %+v err=%v", present, err)
	}
	absent, err := EvaluateDocument(def, map[string]any{})
	if err != nil || !absent.Matched || absent.Segments[0] != "false" {
		t.Fatalf("expected false bucket, got %+v err=%v", absent, err)
	}
}

func TestEvaluateDocumentExistsHideNulls(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"exists","value":"$hasGenre","hideNulls":true}],"sort":[]}}`)

	nullCase, err := EvaluateDocument(def, map[string]any{"genre": nil})
	if err != nil || !nullCase.Matched || nullCase.Segments[0] != "false" {
		t.Fatalf("expected false bucket when hideNulls treats null as absent, got %+v err=%v", nullCase, err)
	}
}

func TestEvaluateDocumentNilDocument(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/status","op":"equals","value":"released"}],"sort":[]}}`)
	res, err := EvaluateDocument(def, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Matched {
		t.Fatalf("expected no match for a nil (deleted) document")
	}
}
