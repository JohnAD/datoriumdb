package config

import (
	"encoding/json"
	"testing"
)

func TestValidateCollectionSchemaRulesPlainSchema(t *testing.T) {
	raw := []byte(`{"kind":"object","children":[{"name":"title","kind":"string"}]}`)
	errs := ValidateCollectionSchemaRules(raw, map[string]json.RawMessage{})
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %#v", errs)
	}
}

func TestValidateCollectionSchemaRulesInvalidJSON(t *testing.T) {
	errs := ValidateCollectionSchemaRules([]byte(`not json`), map[string]json.RawMessage{})
	if len(errs) == 0 {
		t.Fatal("expected at least one error for invalid JSON")
	}
}

func TestValidateCollectionSchemaRulesNonObjectRoot(t *testing.T) {
	errs := ValidateCollectionSchemaRules([]byte(`{"kind":"string"}`), map[string]json.RawMessage{})
	if len(errs) == 0 {
		t.Fatal("expected error for non-object root")
	}
}

func TestValidateCollectionSchemaRulesDirectRefOnNonString(t *testing.T) {
	raw := []byte(`{"kind":"object","children":[
		{"name":"director","kind":"number","format":"DatoriumDirectRef","custom":{"collections":["People"]}}
	]}`)
	errs := ValidateCollectionSchemaRules(raw, map[string]json.RawMessage{})
	if !hasCode(errs, "invalidSchema") {
		t.Fatalf("expected invalidSchema for DatoriumDirectRef on non-string, got %#v", errs)
	}
}

func TestValidateCollectionSchemaRulesDirectRefOnString(t *testing.T) {
	raw := []byte(`{"kind":"object","children":[
		{"name":"director","kind":"string","format":"DatoriumDirectRef","custom":{"collections":["People"]}}
	]}`)
	errs := ValidateCollectionSchemaRules(raw, map[string]json.RawMessage{})
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %#v", errs)
	}
}

func TestValidateCollectionSchemaRulesCachedRefMissingCollections(t *testing.T) {
	raw := []byte(`{"kind":"object","children":[
		{"name":"directorSummary","kind":"string","format":"DatoriumCachedRef","custom":{"summary":["/name"]}}
	]}`)
	errs := ValidateCollectionSchemaRules(raw, map[string]json.RawMessage{})
	if !hasCode(errs, "invalidSchema") {
		t.Fatalf("expected invalidSchema for missing custom.collections, got %#v", errs)
	}
}

func TestValidateCollectionSchemaRulesCachedRefMissingSummary(t *testing.T) {
	raw := []byte(`{"kind":"object","children":[
		{"name":"directorSummary","kind":"string","format":"DatoriumCachedRef","custom":{"collections":["People"]}}
	]}`)
	errs := ValidateCollectionSchemaRules(raw, map[string]json.RawMessage{})
	if !hasCode(errs, "invalidSchema") {
		t.Fatalf("expected invalidSchema for missing custom.summary, got %#v", errs)
	}
}

func TestValidateCollectionSchemaRulesCachedRefTargetSkippedIfUnknown(t *testing.T) {
	raw := []byte(`{"kind":"object","children":[
		{"name":"directorSummary","kind":"string","format":"DatoriumCachedRef","custom":{"collections":["People"],"summary":["/name"]}}
	]}`)
	errs := ValidateCollectionSchemaRules(raw, map[string]json.RawMessage{})
	if len(errs) != 0 {
		t.Fatalf("expected no errors when target collection unknown, got %#v", errs)
	}
}

func TestValidateCollectionSchemaRulesCachedRefTargetResolves(t *testing.T) {
	existing := map[string]json.RawMessage{
		"People": json.RawMessage(`{"kind":"object","children":[{"name":"name","kind":"string"}]}`),
	}
	raw := []byte(`{"kind":"object","children":[
		{"name":"directorSummary","kind":"string","format":"DatoriumCachedRef","custom":{"collections":["People"],"summary":["/name"]}}
	]}`)
	errs := ValidateCollectionSchemaRules(raw, existing)
	if len(errs) != 0 {
		t.Fatalf("expected no errors when summary path resolves, got %#v", errs)
	}
}

func TestValidateCollectionSchemaRulesCachedRefTargetDoesNotResolve(t *testing.T) {
	existing := map[string]json.RawMessage{
		"People": json.RawMessage(`{"kind":"object","children":[{"name":"name","kind":"string"}]}`),
	}
	raw := []byte(`{"kind":"object","children":[
		{"name":"directorSummary","kind":"string","format":"DatoriumCachedRef","custom":{"collections":["People"],"summary":["/doesNotExist"]}}
	]}`)
	errs := ValidateCollectionSchemaRules(raw, existing)
	if !hasCode(errs, "invalidSchema") {
		t.Fatalf("expected invalidSchema for unresolved summary path, got %#v", errs)
	}
}
