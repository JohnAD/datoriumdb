package ojson

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileSchemaJSONCompleteExample(t *testing.T) {
	schema, err := CompileSchemaJSON(`{
		"kind": "object",
		"description-en": "A pet record.",
		"children": [
			{
				"name": "pet",
				"kind": "object",
				"children": [
					{
						"name": "name",
						"kind": "string",
						"required": true,
						"min_length": 1,
						"max_length": 80
					},
					{
						"name": "age",
						"kind": "number",
						"integer": true,
						"min": 0,
						"max": 130
					},
					{
						"name": "height_units",
						"kind": "string",
						"enum": ["inches", "centimeters"],
						"default": "inches"
					},
					{
						"name": "contact_email",
						"kind": "string",
						"format": "email",
						"nullable": true,
						"default": null
					},
					{
						"name": "safe",
						"kind": "boolean",
						"default": true
					}
				]
			}
		]
	}`)
	if err != nil {
		t.Fatalf("CompileSchemaJSON returned error: %v", err)
	}
	if schema.Kind() != KindObject {
		t.Fatalf("schema.Kind() = %v, want object", schema.Kind())
	}
	if len(schema.root.Children) != 1 {
		t.Fatalf("root children = %d, want 1", len(schema.root.Children))
	}
	pet := schema.root.childByName["pet"]
	if pet == nil {
		t.Fatal("pet child schema missing")
	}
	if got := pet.childByName["height_units"].Default.String(); got != "inches" {
		t.Fatalf("height_units default = %q, want inches", got)
	}
	if got := pet.childByName["contact_email"].Descriptions; got == nil {
		t.Fatal("descriptions map should be initialized")
	}
}

func TestCompileSchemaBytesArrayItems(t *testing.T) {
	schema, err := CompileSchemaBytes([]byte(`{
		"kind": "array",
		"items": {
			"kind": "object",
			"children": [
				{ "name": "name", "kind": "string", "min_length": 1 },
				{ "name": "age", "kind": "number", "integer": true }
			]
		}
	}`))
	if err != nil {
		t.Fatalf("CompileSchemaBytes returned error: %v", err)
	}
	if schema.root.Items == nil {
		t.Fatal("array item schema missing")
	}
	if schema.root.Items.Kind != KindObject {
		t.Fatalf("item kind = %v, want object", schema.root.Items.Kind)
	}
}

func TestCompileSchemaFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(path, []byte(`{"kind":"string","default":"ok"}`), 0o600); err != nil {
		t.Fatalf("WriteFile fixture returned error: %v", err)
	}

	schema, err := CompileSchemaFile(path)
	if err != nil {
		t.Fatalf("CompileSchemaFile returned error: %v", err)
	}
	if schema.Kind() != KindString {
		t.Fatalf("schema.Kind() = %v, want string", schema.Kind())
	}
}

func TestCompileSchemaErrorsIncludePaths(t *testing.T) {
	tests := []struct {
		name        string
		schema      string
		errorSubstr string
	}{
		{
			name:        "unsupported kind",
			schema:      `{"kind":"any"}`,
			errorSubstr: `$: unsupported schema kind "any"`,
		},
		{
			name: "missing child name",
			schema: `{
				"kind": "object",
				"children": [
					{ "kind": "string" }
				]
			}`,
			errorSubstr: `0: object child schema must have a string name`,
		},
		{
			name: "duplicate child name",
			schema: `{
				"kind": "object",
				"children": [
					{ "name": "pet", "kind": "string" },
					{ "name": "pet", "kind": "number" }
				]
			}`,
			errorSubstr: `"pet": duplicate child schema name`,
		},
		{
			name:        "unsupported field",
			schema:      `{"kind":"object","$ref":"elsewhere"}`,
			errorSubstr: `$: unsupported schema field "$ref"`,
		},
		{
			name:        "invalid min max",
			schema:      `{"kind":"number","min":10,"max":5}`,
			errorSubstr: `$: min must be less than or equal to max`,
		},
		{
			name:        "enum on non-string",
			schema:      `{"kind":"number","enum":["x"]}`,
			errorSubstr: `$: enum is only supported for string schemas`,
		},
		{
			name:        "bad description language",
			schema:      `{"kind":"string","description-":"bad"}`,
			errorSubstr: `$: invalid description language code ""`,
		},
		{
			name: "child default violates integer",
			schema: `{
				"kind": "object",
				"children": [
					{ "name": "age", "kind": "number", "integer": true, "default": 1.5 }
				]
			}`,
			errorSubstr: `"age": expected integer number`,
		},
		{
			name: "child default violates enum",
			schema: `{
				"kind": "object",
				"children": [
					{ "name": "status", "kind": "string", "enum": ["active"], "default": "draft" }
				]
			}`,
			errorSubstr: `"status": string is not in enum`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileSchemaJSON(test.schema)
			if err == nil {
				t.Fatal("CompileSchemaJSON returned nil error")
			}
			if !strings.Contains(err.Error(), test.errorSubstr) {
				t.Fatalf("error = %v, want substring %q", err, test.errorSubstr)
			}
		})
	}
}

func TestCompileSchemaDefaultValidation(t *testing.T) {
	if _, err := CompileSchemaJSON(`{"kind":"string","default":null}`); err == nil {
		t.Fatal("non-nullable null default returned nil error")
	}

	if _, err := CompileSchemaJSON(`{"kind":"string","nullable":true,"default":null}`); err != nil {
		t.Fatalf("nullable null default returned error: %v", err)
	}

	if _, err := CompileSchemaJSON(`{"kind":"string","format":"url","default":"not a url"}`); err == nil {
		t.Fatal("invalid url default returned nil error")
	}
}
