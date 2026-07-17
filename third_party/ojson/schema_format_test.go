package ojson

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type DirectRef string

func TestCompileSchemaPreservesCustomMetadata(t *testing.T) {
	schema, err := CompileSchemaJSON(`{
		"kind": "object",
		"custom": {"source": "app", "version": 2},
		"children": [
			{
				"name": "title",
				"kind": "string",
				"custom": "note"
			},
			{
				"name": "tags",
				"kind": "array",
				"items": {
					"kind": "string",
					"custom": ["a", "b"]
				}
			}
		]
	}`)
	if err != nil {
		t.Fatalf("CompileSchemaJSON returned error: %v", err)
	}

	rootCustom := schema.Root().Custom()
	if !rootCustom.IsObject() {
		t.Fatalf("root custom kind = %v, want object", rootCustom.Kind())
	}
	if got := rootCustom.Get("source").String(); got != "app" {
		t.Fatalf("root custom source = %q, want app", got)
	}

	title := schema.Root().Child("title")
	if got := title.Custom().String(); got != "note" {
		t.Fatalf("title custom = %q, want note", got)
	}

	itemCustom := schema.Root().Child("tags").Items().Custom()
	if !itemCustom.IsArray() || itemCustom.Len() != 2 {
		t.Fatalf("item custom = %v, want array of length 2", itemCustom)
	}

	mutated := title.Custom()
	mutated = NewString("changed")
	_ = mutated
	if got := title.Custom().String(); got != "note" {
		t.Fatalf("Custom() clone mutated compiled schema: got %q", got)
	}
}

func TestCompileSchemaRejectsUnknownFieldsButAllowsCustom(t *testing.T) {
	if _, err := CompileSchemaJSON(`{"kind":"string","custon":true}`); err == nil {
		t.Fatal("expected typo field to fail")
	} else if !strings.Contains(err.Error(), `unsupported schema field "custon"`) {
		t.Fatalf("unexpected error: %v", err)
	}

	schema, err := CompileSchemaJSON(`{"kind":"string","custom":true}`)
	if err != nil {
		t.Fatalf("custom metadata should compile: %v", err)
	}
	if !schema.Root().Custom().IsBoolean() || !schema.Root().Custom().ToBool() {
		t.Fatalf("custom boolean not preserved: %v", schema.Root().Custom())
	}
}

func TestBuilderCustomMetadata(t *testing.T) {
	meta := NewObject()
	meta.Set("indexed", NewBoolean(true))
	schema, err := NewSchemaObjectBuilder(Custom(meta)).
		StringField("name", CustomString("display")).
		Build()
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if !schema.Root().Custom().Get("indexed").ToBool() {
		t.Fatal("root custom missing indexed")
	}
	if got := schema.Root().Child("name").Custom().String(); got != "display" {
		t.Fatalf("name custom = %q, want display", got)
	}
}

func TestStringFormatRegistryRegistration(t *testing.T) {
	registry := NewStringFormatRegistry()
	validator := StringFormatFunc(func(value string) error {
		if value == "" {
			return errors.New("empty")
		}
		return nil
	})

	if err := registry.Register("", validator, nil); err == nil {
		t.Fatal("expected empty name rejection")
	}
	if err := registry.Register("email", validator, nil); err == nil {
		t.Fatal("expected built-in override rejection")
	}
	if err := registry.Register("Time", nil, nil); err == nil {
		t.Fatal("expected nil validator rejection")
	}
	if err := registry.Register("Time", validator, reflect.TypeOf(time.Time{})); err != nil {
		t.Fatalf("Register Time returned error: %v", err)
	}
	if err := registry.Register("Time", validator, nil); err == nil {
		t.Fatal("expected duplicate rejection")
	}
}

func TestCustomFormatsCompileValidateAndSnapshot(t *testing.T) {
	registry := NewStringFormatRegistry()
	calls := 0
	if err := registry.Register("Time", StringFormatFunc(func(value string) error {
		calls++
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return err
		}
		return nil
	}), reflect.TypeOf(time.Time{})); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.Register("DirectRef", StringFormatFunc(func(value string) error {
		if !strings.HasPrefix(value, "@__") {
			return fmt.Errorf("missing prefix")
		}
		return nil
	}), reflect.TypeOf(DirectRef(""))); err != nil {
		t.Fatalf("Register DirectRef returned error: %v", err)
	}

	schema, err := CompileSchemaJSON(`{
		"kind": "object",
		"children": [
			{"name": "released_at", "kind": "string", "format": "Time", "default": "2024-01-02T03:04:05Z"},
			{"name": "director", "kind": "string", "format": "DirectRef"},
			{"name": "email", "kind": "string", "format": "email"}
		]
	}`, WithStringFormats(registry))
	if err != nil {
		t.Fatalf("CompileSchemaJSON returned error: %v", err)
	}
	if schema.Root().Child("released_at").Format() != "Time" {
		t.Fatal("Time format missing on compiled entry")
	}

	if err := registry.Register("Other", StringFormatFunc(func(string) error { return nil }), nil); err != nil {
		t.Fatalf("post-compile Register returned error: %v", err)
	}
	if _, err := CompileSchemaJSON(`{"kind":"string","format":"Other"}`); err == nil {
		t.Fatal("unregistered format without option should fail")
	}

	doc, err := ReadStringWithSchema(`{
		"released_at": "2024-01-02T03:04:05Z",
		"director": "@__people__1",
		"email": "a@b.com"
	}`, schema)
	if err != nil {
		t.Fatalf("ReadStringWithSchema returned error: %v", err)
	}
	if err := schema.Validate(doc); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	if err := doc.SetTry("director", NewString("bad")); err == nil {
		t.Fatal("expected mutation format validation failure")
	} else if !strings.Contains(err.Error(), "invalid DirectRef format") {
		t.Fatalf("unexpected mutation error: %v", err)
	}

	nested, err := CompileSchemaJSON(`{
		"kind": "object",
		"children": [
			{
				"name": "movie",
				"kind": "object",
				"children": [
					{
						"name": "tags",
						"kind": "array",
						"items": {"kind": "string", "format": "DirectRef"}
					}
				]
			}
		]
	}`, WithStringFormats(registry))
	if err != nil {
		t.Fatalf("nested compile returned error: %v", err)
	}
	badDoc, err := ReadStringNoSchema(`{"movie":{"tags":["bad"]}}`)
	if err != nil {
		t.Fatalf("ReadStringNoSchema returned error: %v", err)
	}
	if err := nested.Validate(badDoc); err == nil {
		t.Fatal("expected deep path validation failure")
	} else if !strings.Contains(err.Error(), `"movie"."tags".0`) {
		t.Fatalf("expected deep path in error, got %v", err)
	}
}

func TestCustomFormatDefaultValidation(t *testing.T) {
	registry := NewStringFormatRegistry()
	_ = registry.Register("Time", StringFormatFunc(func(value string) error {
		_, err := time.Parse(time.RFC3339, value)
		return err
	}), reflect.TypeOf(time.Time{}))

	if _, err := CompileSchemaJSON(`{
		"kind": "string",
		"format": "Time",
		"default": "not-a-time"
	}`, WithStringFormats(registry)); err == nil {
		t.Fatal("expected default format validation failure")
	}
}

func TestBuilderWithStringFormats(t *testing.T) {
	registry := NewStringFormatRegistry()
	_ = registry.Register("Time", StringFormatFunc(func(value string) error {
		_, err := time.Parse(time.RFC3339, value)
		return err
	}), reflect.TypeOf(time.Time{}))

	schema, err := NewSchemaObjectBuilder().
		StringField("released_at", Format(StringFormat("Time"))).
		Build(WithStringFormats(registry))
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if schema.Root().Child("released_at").Format() != "Time" {
		t.Fatal("builder format missing")
	}
}

func TestStructFormatTypeAssociation(t *testing.T) {
	type Movie struct {
		ReleasedAt time.Time   `json:"released_at"`
		Director   DirectRef   `json:"director"`
		Tags       []DirectRef `json:"tags"`
		Nested     struct {
			CreatedAt time.Time `json:"created_at"`
		} `json:"nested"`
	}

	registry := NewStringFormatRegistry()
	_ = registry.Register("Time", StringFormatFunc(func(value string) error {
		_, err := time.Parse(time.RFC3339, value)
		return err
	}), reflect.TypeOf(time.Time{}))
	_ = registry.Register("DirectRef", StringFormatFunc(func(value string) error {
		if !strings.HasPrefix(value, "@__") {
			return errors.New("bad ref")
		}
		return nil
	}), reflect.TypeOf(DirectRef("")))

	schemaDoc, err := NewSchemaFromStructTry(
		Movie{},
		StringFormatType(reflect.TypeOf(time.Time{}), "Time"),
		StringFormatType(reflect.TypeOf(DirectRef("")), "DirectRef"),
	)
	if err != nil {
		t.Fatalf("NewSchemaFromStructTry returned error: %v", err)
	}
	if got := schemaDoc.Get("children").At(0).Get("format").String(); got != "Time" {
		t.Fatalf("released_at format = %q, want Time", got)
	}

	schema, err := CompileSchemaFromStructTry(
		Movie{},
		StringFormatType(reflect.TypeOf(time.Time{}), "Time"),
		StringFormatType(reflect.TypeOf(DirectRef("")), "DirectRef"),
		StructStringFormats(registry),
	)
	if err != nil {
		t.Fatalf("CompileSchemaFromStructTry returned error: %v", err)
	}

	suggestion, err := NewStructSuggestionFromSchemaTry(schema, "Movie")
	if err != nil {
		t.Fatalf("NewStructSuggestionFromSchemaTry returned error: %v", err)
	}
	if !strings.Contains(suggestion.Code, "ReleasedAt time.Time") {
		t.Fatalf("suggestion missing time.Time:\n%s", suggestion.Code)
	}
	if !strings.Contains(suggestion.Code, "DirectRef") {
		t.Fatalf("suggestion missing DirectRef:\n%s", suggestion.Code)
	}
	if !strings.Contains(suggestion.Code, "[]") || !strings.Contains(suggestion.Code, "DirectRef") {
		t.Fatalf("suggestion missing []DirectRef:\n%s", suggestion.Code)
	}
	foundTimeImport := false
	for _, pkg := range suggestion.Imports {
		if pkg == "time" {
			foundTimeImport = true
		}
	}
	if !foundTimeImport {
		t.Fatalf("expected time import, got %v", suggestion.Imports)
	}
}

func TestSchemaEntryTraversal(t *testing.T) {
	schema, err := CompileSchemaJSON(`{
		"kind": "object",
		"children": [
			{
				"name": "items",
				"kind": "array",
				"items": {"kind": "string", "format": "email"}
			}
		]
	}`)
	if err != nil {
		t.Fatalf("CompileSchemaJSON returned error: %v", err)
	}
	items := schema.Root().Child("items")
	if !items.Valid() || items.Kind() != KindArray {
		t.Fatalf("items entry invalid: %+v", items)
	}
	if got := items.Items().Format(); got != "email" {
		t.Fatalf("items format = %q, want email", got)
	}
	if schema.Root().Child("missing").Valid() {
		t.Fatal("missing child should be invalid")
	}
}
