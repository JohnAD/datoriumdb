package ojson

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type structSchemaPet struct {
	Name        string        `json:"name"`
	Age         json.Number   `json:"age,omitempty"`
	HeightUnits string        `json:"height_units,omitempty"`
	Safe        bool          `json:"safe"`
	Tags        []string      `json:"tags,omitempty"`
	Child       *nestedAnimal `json:"child,omitempty"`
	Ignored     string        `json:"-"`
}

type nestedAnimal struct {
	Name string `json:"name"`
}

func TestInspectStructTagsTry(t *testing.T) {
	fields, err := InspectStructTagsTry(reflect.TypeOf(structSchemaPet{}))
	if err != nil {
		t.Fatalf("InspectStructTagsTry returned error: %v", err)
	}
	if len(fields) != 6 {
		t.Fatalf("field count = %d, want 6", len(fields))
	}
	if fields[0].JSONName != "name" || fields[0].SchemaKind != KindString || fields[0].Optional {
		t.Fatalf("first field = %#v", fields[0])
	}
	if fields[1].JSONName != "age" || fields[1].SchemaKind != KindNumber || !fields[1].Optional {
		t.Fatalf("age field = %#v", fields[1])
	}
	if fields[4].SchemaKind != KindArray || fields[4].Item == nil || fields[4].Item.SchemaKind != KindString {
		t.Fatalf("tags field = %#v", fields[4])
	}
	if fields[5].SchemaKind != KindObject || len(fields[5].Children) != 1 {
		t.Fatalf("child field = %#v", fields[5])
	}
}

func TestInspectStructTagsRequiresExplicitTags(t *testing.T) {
	type NoTags struct {
		Name string
	}

	if _, err := InspectStructTagsTry(NoTags{}); err == nil {
		t.Fatal("InspectStructTagsTry without explicit tag returned nil error")
	}

	fields, err := InspectStructTagsTry(NoTags{}, AllowDefaultFieldNames())
	if err != nil {
		t.Fatalf("InspectStructTagsTry AllowDefaultFieldNames returned error: %v", err)
	}
	if len(fields) != 1 || fields[0].JSONName != "Name" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestNewSchemaFromStructTry(t *testing.T) {
	schemaDoc, err := NewSchemaFromStructTry(structSchemaPet{}, RequiredFromNonOmitEmpty())
	if err != nil {
		t.Fatalf("NewSchemaFromStructTry returned error: %v", err)
	}

	want := `{"kind":"object","children":[{"name":"name","kind":"string","required":true},{"name":"age","kind":"number"},{"name":"height_units","kind":"string"},{"name":"safe","kind":"boolean","required":true},{"name":"tags","kind":"array","items":{"kind":"string"}},{"name":"child","kind":"object","children":[{"name":"name","kind":"string","required":true}]}]}`
	if got := schemaDoc.ToJSON(); got != want {
		t.Fatalf("schema JSON = %q, want %q", got, want)
	}

	schema, err := CompileSchemaFromStructTry(structSchemaPet{}, RequiredFromNonOmitEmpty())
	if err != nil {
		t.Fatalf("CompileSchemaFromStructTry returned error: %v", err)
	}
	if schema.Kind() != KindObject {
		t.Fatalf("schema.Kind() = %v, want object", schema.Kind())
	}
}

func TestStructSuggestionFromSchema(t *testing.T) {
	schema, err := CompileSchemaJSON(`{
		"kind": "object",
		"children": [
			{ "name": "name", "kind": "string", "required": true },
			{ "name": "age", "kind": "number" },
			{ "name": "safe", "kind": "boolean", "default": true }
		]
	}`)
	if err != nil {
		t.Fatalf("CompileSchemaJSON returned error: %v", err)
	}

	suggestion, err := NewStructSuggestionFromSchemaTry(schema, "Pet")
	if err != nil {
		t.Fatalf("NewStructSuggestionFromSchemaTry returned error: %v", err)
	}

	wantCode := `type Pet struct {
	Name string      ` + "`json:\"name\"`" + `
	Age  json.Number ` + "`json:\"age,omitempty\"`" + `
	Safe bool        ` + "`json:\"safe,omitempty\"`" + `
}
`
	if suggestion.Code != wantCode {
		t.Fatalf("suggestion.Code =\n%s\nwant\n%s", suggestion.Code, wantCode)
	}
	if len(suggestion.Imports) != 1 || suggestion.Imports[0] != "encoding/json" {
		t.Fatalf("suggestion.Imports = %#v, want encoding/json", suggestion.Imports)
	}
	if len(suggestion.Notes) != 2 {
		t.Fatalf("notes = %#v, want 2 notes", suggestion.Notes)
	}
}

func TestStructSuggestionFromSchemaWithObjectsAndObjectArrays(t *testing.T) {
	schema, err := CompileSchemaJSON(`{
		"kind": "object",
		"children": [
			{
				"name": "pet",
				"kind": "object",
				"children": [
					{ "name": "name", "kind": "string", "required": true },
					{ "name": "age", "kind": "number" }
				]
			},
			{
				"name": "search_results",
				"kind": "array",
				"items": {
					"kind": "object",
					"children": [
						{ "name": "id", "kind": "string", "required": true },
						{ "name": "score", "kind": "number" }
					]
				}
			}
		]
	}`)
	if err != nil {
		t.Fatalf("CompileSchemaJSON returned error: %v", err)
	}

	suggestion, err := NewStructSuggestionFromSchemaTry(schema, "SearchResponse")
	if err != nil {
		t.Fatalf("NewStructSuggestionFromSchemaTry returned error: %v", err)
	}

	wantCode := `type SearchResponse struct {
	Pet           Pet            ` + "`json:\"pet,omitempty\"`" + `
	SearchResults []SearchResult ` + "`json:\"search_results,omitempty\"`" + `
}

type Pet struct {
	Name string      ` + "`json:\"name\"`" + `
	Age  json.Number ` + "`json:\"age,omitempty\"`" + `
}

type SearchResult struct {
	Id    string      ` + "`json:\"id\"`" + `
	Score json.Number ` + "`json:\"score,omitempty\"`" + `
}
`
	if suggestion.Code != wantCode {
		t.Fatalf("suggestion.Code =\n%s\nwant\n%s", suggestion.Code, wantCode)
	}
	if len(suggestion.Imports) != 1 || suggestion.Imports[0] != "encoding/json" {
		t.Fatalf("suggestion.Imports = %#v, want encoding/json", suggestion.Imports)
	}
}

func TestCompareStructToSchema(t *testing.T) {
	type PetMismatch struct {
		Name string `json:"name"`
		Safe bool   `json:"safe"`
		Age  string `json:"age,omitempty"`
	}

	schema, err := CompileSchemaJSON(`{
		"kind": "object",
		"children": [
			{ "name": "name", "kind": "string" },
			{ "name": "age", "kind": "number" },
			{ "name": "safe", "kind": "boolean" }
		]
	}`)
	if err != nil {
		t.Fatalf("CompileSchemaJSON returned error: %v", err)
	}

	report := CompareStructToSchema(PetMismatch{}, schema)
	if report.OK {
		t.Fatal("CompareStructToSchema OK = true, want false")
	}

	categories := findingCategories(report.Findings)
	if !categories["order_mismatch"] {
		t.Fatalf("missing order_mismatch in %#v", report.Findings)
	}
	if !categories["kind_mismatch"] {
		t.Fatalf("missing kind_mismatch in %#v", report.Findings)
	}
}

func TestCompareStructToSchemaFileTry(t *testing.T) {
	schemaText := `{
		"kind": "object",
		"children": [
			{ "name": "name", "kind": "string" },
			{ "name": "age", "kind": "number" },
			{ "name": "height_units", "kind": "string" },
			{ "name": "safe", "kind": "boolean" },
			{ "name": "tags", "kind": "array", "items": { "kind": "string" } },
			{ "name": "child", "kind": "object", "children": [
				{ "name": "name", "kind": "string" }
			] }
		]
	}`
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(schemaText), 0o600); err != nil {
		t.Fatalf("WriteFile fixture returned error: %v", err)
	}

	report, err := CompareStructToSchemaFileTry(structSchemaPet{}, path)
	if err != nil {
		t.Fatalf("CompareStructToSchemaFileTry returned error: %v", err)
	}
	if !report.OK {
		t.Fatalf("report findings = %#v", report.Findings)
	}
}

func TestCompareStructToSchemaReportsMissingFields(t *testing.T) {
	type ExtraPet struct {
		Name     string `json:"name"`
		Nickname string `json:"nickname"`
	}

	schema, err := CompileSchemaJSON(`{
		"kind": "object",
		"children": [
			{ "name": "name", "kind": "string" },
			{ "name": "age", "kind": "number" }
		]
	}`)
	if err != nil {
		t.Fatalf("CompileSchemaJSON returned error: %v", err)
	}

	report := CompareStructToSchema(ExtraPet{}, schema)
	categories := findingCategories(report.Findings)
	if !categories["missing_in_schema"] || !categories["missing_in_struct"] {
		t.Fatalf("missing expected categories in %#v", report.Findings)
	}
}

func findingCategories(findings []StructSchemaFinding) map[string]bool {
	result := map[string]bool{}
	for _, finding := range findings {
		result[finding.Category] = true
	}
	return result
}
