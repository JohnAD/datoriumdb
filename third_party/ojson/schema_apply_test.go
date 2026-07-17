package ojson

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func phase8PetSchema(t *testing.T) JSONSchema {
	t.Helper()

	schema, err := CompileSchemaJSON(`{
		"kind": "object",
		"children": [
			{ "name": "name", "kind": "string", "required": true, "min_length": 1 },
			{ "name": "age", "kind": "number", "integer": true, "min": 0 },
			{ "name": "height_units", "kind": "string", "enum": ["inches", "centimeters"], "default": "inches" },
			{ "name": "email", "kind": "string", "format": "email", "nullable": true },
			{ "name": "tags", "kind": "array", "items": { "kind": "string", "min_length": 1 } }
		]
	}`)
	if err != nil {
		t.Fatalf("CompileSchemaJSON returned error: %v", err)
	}
	return schema
}

func TestReadStringWithSchemaNormalizesOrderDefaultsAndUnknowns(t *testing.T) {
	schema := phase8PetSchema(t)

	doc, err := ReadStringWithSchema(`{
		"nickname": "Whiff",
		"tags": ["small"],
		"age": 3,
		"name": "Whiffles"
	}`, schema)
	if err != nil {
		t.Fatalf("ReadStringWithSchema returned error: %v", err)
	}

	if !doc.HasSchema() {
		t.Fatal("schema-backed doc HasSchema() = false")
	}
	if doc.Schema() == nil || doc.Schema().Kind() != KindObject {
		t.Fatalf("doc.Schema() = %#v", doc.Schema())
	}

	want := `{"name":"Whiffles","age":3,"height_units":"inches","tags":["small"],"nickname":"Whiff"}`
	if got := doc.ToJSON(); got != want {
		t.Fatalf("normalized JSON = %q, want %q", got, want)
	}

	if !doc.Get("tags").HasSchema() {
		t.Fatal("nested schema-backed array HasSchema() = false")
	}

	without := doc.WithoutSchema()
	if without.HasSchema() {
		t.Fatal("WithoutSchema() still has schema")
	}
	if got := without.ToJSON(); got != want {
		t.Fatalf("WithoutSchema JSON = %q, want %q", got, want)
	}
}

func TestReadFileWithSchema(t *testing.T) {
	schema := phase8PetSchema(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "pet.json")
	if err := os.WriteFile(path, []byte(`{"name":"Whiffles"}`), 0o600); err != nil {
		t.Fatalf("WriteFile fixture returned error: %v", err)
	}

	doc, err := ReadFileWithSchema(path, schema)
	if err != nil {
		t.Fatalf("ReadFileWithSchema returned error: %v", err)
	}
	if got := doc.Get("height_units").String(); got != "inches" {
		t.Fatalf("height_units = %q, want inches", got)
	}
}

func TestApplySchemaValidationErrors(t *testing.T) {
	schema := phase8PetSchema(t)

	_, err := ReadStringWithSchema(`{"age":3}`, schema)
	if err == nil || !strings.Contains(err.Error(), `"name": required field is missing`) {
		t.Fatalf("missing required error = %v", err)
	}

	_, err = ReadStringWithSchema(`{"name":"Whiffles","age":3.5}`, schema)
	if err == nil || !strings.Contains(err.Error(), `"age": expected integer number`) {
		t.Fatalf("integer validation error = %v", err)
	}

	_, err = ReadStringWithSchema(`{"name":"Whiffles","tags":["ok",""]}`, schema)
	if err == nil || !strings.Contains(err.Error(), `"tags".1: string length is below min_length 1`) {
		t.Fatalf("array item validation error = %v", err)
	}
}

func TestSchemaValidateDoesNotMutateValue(t *testing.T) {
	schema := phase8PetSchema(t)
	doc, err := ReadStringNoSchema(`{"name":"Whiffles"}`)
	if err != nil {
		t.Fatalf("ReadStringNoSchema returned error: %v", err)
	}

	if err := schema.Validate(doc); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if got := doc.ToJSON(); got != `{"name":"Whiffles"}` {
		t.Fatalf("Validate mutated doc to %q", got)
	}
	if doc.HasSchema() {
		t.Fatal("Validate attached schema to doc")
	}
}

func TestSchemaBackedObjectMutation(t *testing.T) {
	schema := phase8PetSchema(t)
	doc, err := ReadStringWithSchema(`{"name":"Whiffles","nickname":"Whiff"}`, schema)
	if err != nil {
		t.Fatalf("ReadStringWithSchema returned error: %v", err)
	}

	if err := doc.SetTry("age", NewNumberFromInt(4)); err != nil {
		t.Fatalf("SetTry age returned error: %v", err)
	}
	if got := doc.ToJSON(); got != `{"name":"Whiffles","age":4,"height_units":"inches","nickname":"Whiff"}` {
		t.Fatalf("schema ordered mutation JSON = %q", got)
	}

	if err := doc.SetTry("height_units", NewString("feet")); err == nil {
		t.Fatal("SetTry invalid enum returned nil error")
	}
	if got := doc.Get("height_units").String(); got != "inches" {
		t.Fatalf("height_units changed after failed SetTry: %q", got)
	}

	if removed := doc.Remove("name"); !removed.IsVoid() {
		t.Fatalf("Remove required returned %s, want void", removed.Kind())
	}
	if got := doc.Get("name").String(); got != "Whiffles" {
		t.Fatalf("required name removed: %q", got)
	}
}

func TestSchemaBackedArrayMutation(t *testing.T) {
	schema := phase8PetSchema(t)
	doc, err := ReadStringWithSchema(`{"name":"Whiffles","tags":["small"]}`, schema)
	if err != nil {
		t.Fatalf("ReadStringWithSchema returned error: %v", err)
	}
	tags := doc.Get("tags")

	if err := tags.AppendTry(NewString("brown")); err != nil {
		t.Fatalf("AppendTry valid tag returned error: %v", err)
	}
	if err := tags.AppendTry(NewEmptyString()); err == nil {
		t.Fatal("AppendTry invalid tag returned nil error")
	}
	if err := tags.AppendTry(NewNumberFromInt(1)); err == nil {
		t.Fatal("AppendTry wrong kind returned nil error")
	}

	if got := tags.ToJSON(); got != `["small","brown"]` {
		t.Fatalf("tags JSON = %q", got)
	}
}
