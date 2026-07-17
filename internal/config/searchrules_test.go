package config

import (
	"encoding/json"
	"testing"
)

func moviesSchemaForSearch() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"Movies": json.RawMessage(`{
			"kind": "object",
			"children": [
				{"name": "title", "kind": "string"},
				{"name": "genre", "kind": "string"},
				{"name": "releaseYear", "kind": "number", "integer": true},
				{"name": "highRated", "kind": "boolean"}
			]
		}`),
	}
}

func TestValidateSearchDefinitionValid(t *testing.T) {
	raw := []byte(`{
		"$": "SearchDefinition:v1",
		"collection": "Movies",
		"name": "byReleasedGenre",
		"version": 1,
		"v1": {
			"clauses": [
				{"field": "/genre", "op": "equals", "value": "scifi"}
			],
			"sort": [
				{"field": "/releaseYear", "dir": "desc"},
				{"field": "/!", "dir": "asc"}
			]
		}
	}`)
	errs := ValidateSearchDefinition(raw, "Movies", "byReleasedGenre", moviesSchemaForSearch())
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %#v", errs)
	}
}

func TestValidateSearchDefinitionBadSchemaTag(t *testing.T) {
	raw := []byte(`{"$": "wrong", "collection": "Movies", "name": "x", "version": 1, "v1": {"clauses":[{"field":"/genre","op":"equals"}]}}`)
	errs := ValidateSearchDefinition(raw, "Movies", "x", moviesSchemaForSearch())
	if !hasCode(errs, "invalidSearchDefinition") {
		t.Fatalf("expected invalidSearchDefinition, got %#v", errs)
	}
}

func TestValidateSearchDefinitionCollectionMismatch(t *testing.T) {
	raw := []byte(`{"$": "SearchDefinition:v1", "collection": "People", "name": "x", "version": 1, "v1": {"clauses":[{"field":"/genre","op":"equals"}]}}`)
	errs := ValidateSearchDefinition(raw, "Movies", "x", moviesSchemaForSearch())
	if !hasCode(errs, "invalidSearchDefinition") {
		t.Fatalf("expected invalidSearchDefinition for collection mismatch, got %#v", errs)
	}
}

func TestValidateSearchDefinitionNameMismatch(t *testing.T) {
	raw := []byte(`{"$": "SearchDefinition:v1", "collection": "Movies", "name": "other", "version": 1, "v1": {"clauses":[{"field":"/genre","op":"equals"}]}}`)
	errs := ValidateSearchDefinition(raw, "Movies", "x", moviesSchemaForSearch())
	if !hasCode(errs, "invalidSearchDefinition") {
		t.Fatalf("expected invalidSearchDefinition for name mismatch, got %#v", errs)
	}
}

func TestValidateSearchDefinitionInvalidVersion(t *testing.T) {
	raw := []byte(`{"$": "SearchDefinition:v1", "collection": "Movies", "name": "x", "version": 0, "v1": {"clauses":[{"field":"/genre","op":"equals"}]}}`)
	errs := ValidateSearchDefinition(raw, "Movies", "x", moviesSchemaForSearch())
	if !hasCode(errs, "invalidSearchDefinition") {
		t.Fatalf("expected invalidSearchDefinition for bad version, got %#v", errs)
	}
}

func TestValidateSearchDefinitionUnknownCollection(t *testing.T) {
	raw := []byte(`{"$": "SearchDefinition:v1", "collection": "Ghosts", "name": "x", "version": 1, "v1": {"clauses":[{"field":"/genre","op":"equals"}]}}`)
	errs := ValidateSearchDefinition(raw, "Ghosts", "x", moviesSchemaForSearch())
	if !hasCode(errs, "collectionNotFound") {
		t.Fatalf("expected collectionNotFound, got %#v", errs)
	}
}

func TestValidateSearchDefinitionEmptyClauses(t *testing.T) {
	raw := []byte(`{"$": "SearchDefinition:v1", "collection": "Movies", "name": "x", "version": 1, "v1": {"clauses":[]}}`)
	errs := ValidateSearchDefinition(raw, "Movies", "x", moviesSchemaForSearch())
	if !hasCode(errs, "invalidSearchDefinition") {
		t.Fatalf("expected invalidSearchDefinition for empty clauses, got %#v", errs)
	}
}

func TestValidateSearchDefinitionNonSchemaField(t *testing.T) {
	raw := []byte(`{"$": "SearchDefinition:v1", "collection": "Movies", "name": "x", "version": 1, "v1": {"clauses":[{"field":"/notAField","op":"equals"}]}}`)
	errs := ValidateSearchDefinition(raw, "Movies", "x", moviesSchemaForSearch())
	if !hasCode(errs, "invalidSearchDefinition") {
		t.Fatalf("expected invalidSearchDefinition for non-schema field, got %#v", errs)
	}
}

func TestValidateSearchDefinitionUnsupportedOp(t *testing.T) {
	raw := []byte(`{"$": "SearchDefinition:v1", "collection": "Movies", "name": "x", "version": 1, "v1": {"clauses":[{"field":"/genre","op":"frobnicate"}]}}`)
	errs := ValidateSearchDefinition(raw, "Movies", "x", moviesSchemaForSearch())
	if !hasCode(errs, "invalidSearchDefinition") {
		t.Fatalf("expected invalidSearchDefinition for unsupported op, got %#v", errs)
	}
}

func TestValidateSearchDefinitionBadSortField(t *testing.T) {
	raw := []byte(`{"$": "SearchDefinition:v1", "collection": "Movies", "name": "x", "version": 1, "v1": {"clauses":[{"field":"/genre","op":"equals"}], "sort": [{"field":"/notAField","dir":"asc"}]}}`)
	errs := ValidateSearchDefinition(raw, "Movies", "x", moviesSchemaForSearch())
	if !hasCode(errs, "invalidSearchDefinition") {
		t.Fatalf("expected invalidSearchDefinition for bad sort field, got %#v", errs)
	}
}

func TestValidateSearchDefinitionBadSortDir(t *testing.T) {
	raw := []byte(`{"$": "SearchDefinition:v1", "collection": "Movies", "name": "x", "version": 1, "v1": {"clauses":[{"field":"/genre","op":"equals"}], "sort": [{"field":"/genre","dir":"sideways"}]}}`)
	errs := ValidateSearchDefinition(raw, "Movies", "x", moviesSchemaForSearch())
	if !hasCode(errs, "invalidSearchDefinition") {
		t.Fatalf("expected invalidSearchDefinition for bad sort dir, got %#v", errs)
	}
}

func TestValidateSearchDefinitionInvalidJSON(t *testing.T) {
	errs := ValidateSearchDefinition([]byte(`not json`), "Movies", "x", moviesSchemaForSearch())
	if !hasCode(errs, "invalidJSON") {
		t.Fatalf("expected invalidJSON, got %#v", errs)
	}
}
