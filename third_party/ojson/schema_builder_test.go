package ojson

import (
	"strings"
	"testing"
)

func TestSchemaObjectBuilderBuildsUsableSchema(t *testing.T) {
	schema, err := NewSchemaObjectBuilder(
		Description(LangEN, "Schema for one pet record."),
	).
		ObjectField("pet", func(pet *SchemaObjectBuilder) {
			pet.StringField(
				"name",
				Description(LangEN, "The pet's display name."),
				Required(),
				MinLength(1),
				MaxLength(80),
			)
			pet.NumberField("age", Integer(), Min("0"))
			pet.NumberField("height", DefaultNumber("0"))
			pet.StringField("height_units", Enum("inches", "centimeters"), DefaultString("inches"))
			pet.StringField("contact_email", Nullable(), Format(FormatEmail), DefaultNull())
			pet.BooleanField("safe", DefaultBool(true))
		}, Description(LangEN, "Information about one pet.")).
		Build()
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	doc, err := ReadStringWithSchema(`{"pet":{"name":"Whiffles","age":3}}`, schema)
	if err != nil {
		t.Fatalf("ReadStringWithSchema returned error: %v", err)
	}

	want := `{"pet":{"name":"Whiffles","age":3,"height":0,"height_units":"inches","contact_email":null,"safe":true}}`
	if got := doc.ToJSON(); got != want {
		t.Fatalf("schema-backed JSON = %q, want %q", got, want)
	}

	pet := schema.root.childByName["pet"]
	if pet == nil || pet.Descriptions["en"] != "Information about one pet." {
		t.Fatalf("pet description missing: %#v", pet)
	}
	if pet.childByName["contact_email"].Format != "email" {
		t.Fatalf("contact_email format = %q", pet.childByName["contact_email"].Format)
	}
}

func TestSchemaArrayBuilderBuildsObjectItems(t *testing.T) {
	schema, err := NewSchemaArrayBuilder(
		Description(LangEN, "Pets found from a search."),
	).
		ObjectItems(func(pet *SchemaObjectBuilder) {
			pet.StringField("id", Required(), MinLength(1))
			pet.StringField("name", Required(), MinLength(1), MaxLength(80))
			pet.StringField("species", Required(), Enum("cat", "dog", "bird", "other"))
			pet.NumberField("age", Integer(), Min("0"))
			pet.BooleanField("adoptable", DefaultBool(false))
		}).
		Build()
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	doc, err := ReadStringWithSchema(`[{"species":"cat","name":"Mochi","id":"1"}]`, schema)
	if err != nil {
		t.Fatalf("ReadStringWithSchema returned error: %v", err)
	}

	want := `[{"id":"1","name":"Mochi","species":"cat","adoptable":false}]`
	if got := doc.ToJSON(); got != want {
		t.Fatalf("schema-backed array JSON = %q, want %q", got, want)
	}
}

func TestSchemaBuilderRuntimeValidation(t *testing.T) {
	tests := []struct {
		name        string
		build       func() (JSONSchema, error)
		errorSubstr string
	}{
		{
			name: "bad min number",
			build: func() (JSONSchema, error) {
				return NewSchemaObjectBuilder().
					NumberField("age", Min("bad")).
					Build()
			},
			errorSubstr: `invalid min number "bad"`,
		},
		{
			name: "duplicate fields",
			build: func() (JSONSchema, error) {
				return NewSchemaObjectBuilder().
					StringField("name").
					NumberField("name").
					Build()
			},
			errorSubstr: `"name": duplicate child schema name`,
		},
		{
			name: "bad default null",
			build: func() (JSONSchema, error) {
				return NewSchemaObjectBuilder().
					StringField("name", DefaultNull()).
					Build()
			},
			errorSubstr: `"name": default null requires nullable true`,
		},
		{
			name: "empty field name",
			build: func() (JSONSchema, error) {
				return NewSchemaObjectBuilder().
					StringField("").
					Build()
			},
			errorSubstr: `object child schema name must not be empty`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.build()
			if err == nil {
				t.Fatal("Build returned nil error")
			}
			if !strings.Contains(err.Error(), test.errorSubstr) {
				t.Fatalf("error = %v, want substring %q", err, test.errorSubstr)
			}
		})
	}
}

func TestParseLanguageCodeAndCustomDescription(t *testing.T) {
	lang, err := ParseLanguageCode("pt-BR")
	if err != nil {
		t.Fatalf("ParseLanguageCode returned error: %v", err)
	}

	schema, err := NewSchemaObjectBuilder(
		Description(lang, "Registro de pet."),
	).Build()
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if got := schema.root.Descriptions["pt-BR"]; got != "Registro de pet." {
		t.Fatalf("description = %q", got)
	}

	if _, err := ParseLanguageCode(""); err == nil {
		t.Fatal("ParseLanguageCode empty returned nil error")
	}
}

func TestSchemaBuilderMustBuildPanicsOnError(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustBuild did not panic")
		}
	}()

	NewSchemaObjectBuilder().
		NumberField("age", Max("bad")).
		MustBuild()
}

func TestSchemaArrayBuilderMixedItemsWhenUndefined(t *testing.T) {
	schema, err := NewSchemaArrayBuilder().Build()
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	doc, err := ReadStringWithSchema(`[1,"two",true,null]`, schema)
	if err != nil {
		t.Fatalf("ReadStringWithSchema returned error: %v", err)
	}
	if got := doc.ToJSON(); got != `[1,"two",true,null]` {
		t.Fatalf("mixed array JSON = %q", got)
	}
}
