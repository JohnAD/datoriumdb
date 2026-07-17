package ojson

import (
	"os"
	"path/filepath"
	"testing"
)

func TestREADMEBasicReadingWorkflow(t *testing.T) {
	jsonString := `{
  "user": {
    "name": "Larry",
    "customer_number": 383827
  },
  "ratings": [3.2, 7.8, 7.2, null, 8.9]
}`

	doc, err := ReadStringNoSchema(jsonString)
	if err != nil {
		t.Fatalf("ReadStringNoSchema returned error: %v", err)
	}

	username := doc.Get("user").Get("name").GetString("User name is missing")
	city := doc.Get("location").Get("postal_city").GetString("Location city is missing")
	fourth := doc.Get("ratings").At(3)

	if username != "Larry" {
		t.Fatalf("username = %q, want Larry", username)
	}
	if city != "Location city is missing" {
		t.Fatalf("city = %q", city)
	}
	if !fourth.IsNull() {
		t.Fatalf("fourth kind = %v, want null", fourth.Kind())
	}
}

func TestExamplesWriteWithoutSchemaWorkflow(t *testing.T) {
	doc := NewObject()
	doc.Set("pet", NewObject())
	doc.Get("pet").Set("name", NewString("Whiffles"))

	age, err := NewNumberTry("3.2")
	if err != nil {
		t.Fatalf("NewNumberTry returned error: %v", err)
	}

	doc.Get("pet").Set("age", age)
	doc.Get("pet").Set("height", NewNumber("21.5"))
	doc.Get("pet").Set("height_units", NewString("inches"))
	doc.Get("pet").Set("safe", NewBoolean(true))

	want := "{\n" +
		"  \"pet\": {\n" +
		"    \"name\": \"Whiffles\",\n" +
		"    \"age\": 3.2,\n" +
		"    \"height\": 21.5,\n" +
		"    \"height_units\": \"inches\",\n" +
		"    \"safe\": true\n" +
		"  }\n" +
		"}"
	if got := doc.ToPrettyJSON(2); got != want {
		t.Fatalf("ToPrettyJSON(2) =\n%s\nwant\n%s", got, want)
	}
}

func TestExamplesSchemaWorkflowJSONAndBuilderMatch(t *testing.T) {
	schemaText := `{
  "kind": "object",
  "children": [
    {
      "name": "pet",
      "kind": "object",
      "children": [
        { "name": "name", "kind": "string", "required": true },
        { "name": "age", "kind": "number", "integer": true },
        { "name": "height", "kind": "number", "default": 0 },
        { "name": "height_units", "kind": "string", "default": "inches" },
        { "name": "safe", "kind": "boolean", "default": true }
      ]
    }
  ]
}`
	jsonText := `{
  "pet": {
    "safe": false,
    "name": "Whiffles",
    "age": 3
  }
}`

	jsonSchema, err := CompileSchemaJSON(schemaText)
	if err != nil {
		t.Fatalf("CompileSchemaJSON returned error: %v", err)
	}
	builderSchema, err := NewSchemaObjectBuilder(
		Description(LangEN, "Schema for pet records."),
	).
		ObjectField("pet", func(pet *SchemaObjectBuilder) {
			pet.StringField("name", Required(), MinLength(1), MaxLength(80))
			pet.NumberField("age", Integer(), Min("0"))
			pet.NumberField("height", DefaultNumber("0"))
			pet.StringField("height_units", Enum("inches", "centimeters"), DefaultString("inches"))
			pet.BooleanField("safe", DefaultBool(true))
		}, Description(LangEN, "Information about one pet.")).
		Build()
	if err != nil {
		t.Fatalf("builder Build returned error: %v", err)
	}

	jsonDoc, err := ReadStringWithSchema(jsonText, jsonSchema)
	if err != nil {
		t.Fatalf("ReadStringWithSchema JSON schema returned error: %v", err)
	}
	builderDoc, err := ReadStringWithSchema(jsonText, builderSchema)
	if err != nil {
		t.Fatalf("ReadStringWithSchema builder schema returned error: %v", err)
	}

	want := `{"pet":{"name":"Whiffles","age":3,"height":0,"height_units":"inches","safe":false}}`
	if got := jsonDoc.ToJSON(); got != want {
		t.Fatalf("JSON schema output = %q, want %q", got, want)
	}
	if got := builderDoc.ToJSON(); got != want {
		t.Fatalf("builder schema output = %q, want %q", got, want)
	}
}

func TestExamplesUnknownFieldPreservation(t *testing.T) {
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

	doc, err := ReadStringWithSchema(`{"nickname":"Whiff","age":3.2,"name":"Whiffles"}`, schema)
	if err != nil {
		t.Fatalf("ReadStringWithSchema returned error: %v", err)
	}

	want := `{"name":"Whiffles","age":3.2,"nickname":"Whiff"}`
	if got := doc.ToJSON(); got != want {
		t.Fatalf("normalized JSON = %q, want %q", got, want)
	}
}

func TestWriteFileWritesPrettyJSON(t *testing.T) {
	doc := NewObject()
	doc.Set("name", NewString("Whiffles"))

	path := filepath.Join(t.TempDir(), "pet.json")
	if err := doc.WriteFile(path); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if got := string(bytes); got != "{\n  \"name\": \"Whiffles\"\n}" {
		t.Fatalf("file contents = %q", got)
	}
}
