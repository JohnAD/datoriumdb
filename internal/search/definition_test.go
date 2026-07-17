package search

import (
	"encoding/json"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/ojson"
)

const testMoviesSchema = `{
  "kind": "object",
  "children": [
    {"name": "title", "kind": "string", "required": true},
    {"name": "releaseYear", "kind": "number", "integer": true},
    {"name": "status", "kind": "string"},
    {"name": "genre", "kind": "string"},
    {"name": "highRated", "kind": "boolean", "default": false},
    {"name": "retiredAt", "kind": "string", "nullable": true}
  ]
}`

func compiledRoot(t *testing.T) ojson.SchemaEntry {
	t.Helper()
	schema, err := config.CompileSchemaBytes([]byte(testMoviesSchema))
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return schema.Root()
}

func existingCollections() map[string]json.RawMessage {
	return map[string]json.RawMessage{"Movies": json.RawMessage(testMoviesSchema)}
}

func defJSON(t *testing.T, body string) *Definition {
	t.Helper()
	def, err := ParseDefinition([]byte(body))
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	return def
}

func TestParseDefinitionBasic(t *testing.T) {
	def := defJSON(t, `{
		"$": "SearchDefinition:v1",
		"collection": "Movies",
		"name": "byStatus",
		"version": 1,
		"v1": {
			"clauses": [
				{"field": "/status", "op": "equals", "value": "$status"}
			],
			"sort": [{"field": "/title", "dir": "asc"}]
		}
	}`)
	if def.Collection != "Movies" || def.Name != "byStatus" || def.Version != 1 {
		t.Fatalf("unexpected parsed definition: %+v", def)
	}
	if len(def.Clauses) != 1 || def.Clauses[0].Op != OpEquals {
		t.Fatalf("unexpected clauses: %+v", def.Clauses)
	}
	if len(def.Sort) != 1 || def.Sort[0].Dir != "asc" {
		t.Fatalf("unexpected sort: %+v", def.Sort)
	}
}

func TestValidateEqualsVariants(t *testing.T) {
	root := compiledRoot(t)
	cols := existingCollections()

	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "string variable ok",
			body: `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
				"v1":{"clauses":[{"field":"/status","op":"equals","value":"$status"}],"sort":[]}}`,
		},
		{
			name: "string constant ok",
			body: `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
				"v1":{"clauses":[{"field":"/status","op":"equals","value":"released"}],"sort":[]}}`,
		},
		{
			name: "boolean constant with truth ok",
			body: `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
				"v1":{"clauses":[{"field":"/highRated","op":"equals","value":true,"truth":"$wantHigh"}],"sort":[]}}`,
		},
		{
			name: "null without truth rejected",
			body: `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
				"v1":{"clauses":[{"field":"/retiredAt","op":"equals","value":null}],"sort":[]}}`,
			wantErr: true,
		},
		{
			name: "null with truth ok",
			body: `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
				"v1":{"clauses":[{"field":"/retiredAt","op":"equals","value":null,"truth":"$isRetired"}],"sort":[]}}`,
		},
		{
			name: "number constant unsupported in MVP",
			body: `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
				"v1":{"clauses":[{"field":"/releaseYear","op":"equals","value":1999}],"sort":[]}}`,
			wantErr: true,
		},
		{
			name: "kind mismatch rejected",
			body: `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
				"v1":{"clauses":[{"field":"/status","op":"equals","value":true}],"sort":[]}}`,
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def := defJSON(t, c.body)
			errs := def.Validate(root, cols)
			if c.wantErr && len(errs) == 0 {
				t.Fatalf("expected validation errors, got none")
			}
			if !c.wantErr && len(errs) != 0 {
				t.Fatalf("unexpected validation errors: %+v", errs)
			}
		})
	}
}

func TestValidateInRequiresSelect(t *testing.T) {
	root := compiledRoot(t)
	cols := existingCollections()

	def := defJSON(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"in","value":["scifi","drama"]}],"sort":[]}}`)
	if errs := def.Validate(root, cols); len(errs) == 0 {
		t.Fatalf("expected error for in without select")
	}

	def2 := defJSON(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"in","value":["scifi","drama"],"select":"$genre"}],"sort":[]}}`)
	if errs := def2.Validate(root, cols); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
}

func TestValidateExistsRequiresTruthVariable(t *testing.T) {
	root := compiledRoot(t)
	cols := existingCollections()

	def := defJSON(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"exists","value":true}],"sort":[]}}`)
	if errs := def.Validate(root, cols); len(errs) == 0 {
		t.Fatalf("expected error: exists value must be a variable, not a literal")
	}

	def2 := defJSON(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/genre","op":"exists","value":"$hasGenre"}],"sort":[]}}`)
	if errs := def2.Validate(root, cols); len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
}

func TestValidateRejectsUnsupportedV1Ops(t *testing.T) {
	root := compiledRoot(t)
	cols := existingCollections()
	def := defJSON(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/releaseYear","op":"greaterThan","value":1990}],"sort":[]}}`)
	errs := def.Validate(root, cols)
	if len(errs) == 0 {
		t.Fatalf("expected unsupportedInMVP error")
	}
	found := false
	for _, e := range errs {
		if e.Code == "unsupportedInMVP" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unsupportedInMVP error code, got %+v", errs)
	}
}

func TestValidateUnknownCollection(t *testing.T) {
	root := compiledRoot(t)
	def := defJSON(t, `{"$":"SearchDefinition:v1","collection":"Nope","name":"n","version":1,
		"v1":{"clauses":[{"field":"/status","op":"equals","value":"released"}],"sort":[]}}`)
	errs := def.Validate(root, map[string]json.RawMessage{})
	found := false
	for _, e := range errs {
		if e.Code == "collectionNotFound" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected collectionNotFound, got %+v", errs)
	}
}
