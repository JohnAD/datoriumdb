# Common Methods And Procedures

This document covers ojson behavior shared across value kinds. Kind-specific methods live in the related method documents:

- [`methods-object.md`](methods-object.md)
- [`methods-array.md`](methods-array.md)
- [`methods-string.md`](methods-string.md)
- [`methods-number.md`](methods-number.md)
- [`methods-boolean.md`](methods-boolean.md)
- [`methods-null.md`](methods-null.md)
- [`methods-void.md`](methods-void.md)

## Failure Handling Convention

Method names should communicate how failure is handled:

- plain methods return a `Void` value on failure
- `*Try` methods return a result and an `error`
- `*OrDefault` methods return the caller-provided default on failure

Use plain methods for fluent document traversal, `*Try` methods when the caller needs the reason for failure, and `*OrDefault` methods when the caller has a meaningful replacement value.

## Reading Documents

### `ReadStringNoSchema(jsonText string) (JSONValue, error)`

Reads JSON text without applying a schema.

Expected behavior:

- parse JSON text into `JSONValue`
- preserve object field order from the source text
- preserve array item order
- store number values as decimal strings
- return an error for malformed JSON

```go
doc, err := ojson.ReadStringNoSchema(jsonText)
if err != nil {
    return err
}
```

### `ReadBytesNoSchema(jsonBytes []byte) (JSONValue, error)`

Reads JSON bytes without applying a schema.

This is the byte-slice equivalent of `ReadStringNoSchema` and is useful when JSON is already loaded from a file, network response, or embedded asset.

### `ReadFileNoSchema(path string) (JSONValue, error)`

Reads a JSON file without applying a schema.

This procedure should:

1. read the file as bytes
2. parse the bytes as JSON
3. preserve source object order
4. return the root `JSONValue`

## Compiling Schemas

Schemas should be compiled before use.

A compiled schema is the runtime form of a schema document: parsed, validated, normalized, and ready to apply to one or more JSON documents. Code should avoid reparsing schema JSON for every document.

### `CompileSchemaJSON(schemaText string, opts ...SchemaCompileOption) (JSONSchema, error)`

Compiles an ojson schema document from a string.

Expected behavior:

- parse the schema as JSON
- validate supported schema fields, kinds, and validation rules
- preserve child order exactly
- normalize internal lookup structures for repeated use
- return an error for malformed schema documents
- accept an optional `WithStringFormats(registry)` compile option for custom string formats

```go
schema, err := ojson.CompileSchemaJSON(schemaText, ojson.WithStringFormats(formats))
if err != nil {
    return err
}
```

### `CompileSchemaBytes(schemaBytes []byte, opts ...SchemaCompileOption) (JSONSchema, error)`

Compiles an ojson schema document from bytes.

Use this when schema JSON is already loaded in memory.

### `CompileSchemaFile(path string, opts ...SchemaCompileOption) (JSONSchema, error)`

Compiles an ojson schema document from a file.

Use this for project-managed schema files that define the canonical order for one or more JSON document types.

Compiled schemas expose a read-only entry view through `schema.Root()`, with `Child`, `Children`, `Items`, `Kind`, `Format`, and `Custom` accessors. `Custom()` returns a clone of opaque metadata, or Void when absent.

## Programmatic Schema Builders

Programmatic builders should be available for the two schema roots that commonly need child definitions: objects and arrays. Scalar schemas can be created through builder methods rather than through separate top-level scalar builders.

Use `NewSchemaObjectBuilder` and `NewSchemaArrayBuilder` for programmatic schema construction. Only object and array schema builders should be top-level constructors. Adding top-level builders for every scalar kind would likely add more API surface than clarity.

See [`schema-builders.md`](schema-builders.md) for the full builder method reference and examples.

## Reading Documents With Schemas

### `ReadStringWithSchema(jsonText string, schema JSONSchema) (JSONValue, error)`

Reads JSON text and normalizes it through a compiled ojson schema.

Expected behavior:

- parse the schema
- parse the JSON document
- validate known fields against schema kinds
- reorder schema-defined object fields into schema order
- insert default values for missing fields with defaults
- error when required fields are missing and no default exists
- preserve unknown fields after schema-defined fields

### `ReadBytesWithSchema(jsonBytes []byte, schema JSONSchema) (JSONValue, error)`

Reads JSON bytes and applies a compiled schema.

Use this form when both JSON and schema documents are already available in memory.

### `ReadFileWithSchema(path string, schema JSONSchema) (JSONValue, error)`

Reads a JSON file and applies a compiled schema.

Use this procedure for project data files that must conform to a canonical order before being edited or written back.

## Applying And Attaching Schemas

Schema application should be explicit, but the resulting document should remember the schema.

When a document is read with a schema, or when a schema is applied to an existing document, the returned `JSONValue` should be schema-backed. Schema-backed documents should use schema rules for field order, defaults, required fields, nullable fields, and validation during mutation.

### `ApplySchema(schema JSONSchema) (JSONValue, error)`

Applies a compiled schema to an existing `JSONValue`.

Expected behavior:

- validate the document against the schema
- normalize object fields into schema order
- insert defaults
- reject missing required fields without defaults
- preserve unknown fields after schema-defined fields
- attach the schema to the returned document

```go
doc, err := ojson.ReadStringNoSchema(jsonText)
if err != nil {
    return err
}

doc, err = doc.ApplySchema(schema)
if err != nil {
    return err
}
```

### `Validate(value JSONValue) error`

Validates a value against a compiled schema without changing the value.

Validation errors should include diagnostic paths using the format described in [`error-paths.md`](error-paths.md).

```go
if err := schema.Validate(doc); err != nil {
    return err
}
```

### `Schema() *JSONSchema`

Returns the schema attached to a document, or nil when no schema is attached.

### `HasSchema() bool`

Reports whether a document has an attached schema.

### `WithoutSchema() JSONValue`

Returns a copy of the value without an attached schema.

Use `WithoutSchema` when the caller wants ordinary ordered JSON behavior without schema-backed mutation rules.

## State Methods

### `IsKnown() bool`

Reports whether a value is known.

`IsKnown` returns `true` for object, array, string, number, and boolean values. It returns `false` for `Void`, because `Void` is absence, and for `Null`, because `Null` means the value is unknown. It does not check whether a known value is empty for its kind.

```go
if doc.Get("pet").Get("name").IsKnown() {
    fmt.Println("name is known")
}
```

### `NotEmpty() bool`

Reports whether a value is known and is not the empty state for its kind.

`NotEmpty` should return `false` for `Void` and `Null`. For all other kinds, it should compare the value against the kind's empty state.

```go
if doc.Get("pet").Get("name").NotEmpty() {
    fmt.Println("name has a non-empty value")
}
```

### `IsEmpty() bool`

Reports whether a value is empty.

`IsEmpty` should be the inverse of `NotEmpty`. It should return `true` for `Void`, `Null`, and values that match the empty state for their kind.

```go
if doc.Get("pet").Get("name").IsEmpty() {
    fmt.Println("name is missing, null, or empty")
}
```

### Empty States

| Kind | Empty state | `IsKnown` | `NotEmpty` when empty | Notes |
| --- | --- | --- | --- | --- |
| `KindVoid` | missing value | `false` | `false` | `Void` is absence and is never serialized. |
| `KindNull` | `null` | `false` | `false` | `Null` is explicit JSON `null`, which means unknown. |
| `KindObject` | `{}` | `true` | `false` | An empty object is known, but has no fields. |
| `KindArray` | `[]` | `true` | `false` | An empty array is known, but has no items. |
| `KindString` | `""` | `true` | `false` | An empty string is known, but its content length is zero. |
| `KindNumber` | `0` | `true` | `false` | Zero is known; the canonical empty number is zero with no decimal part. |
| `KindBoolean` | `false` | `true` | `false` | False is known, but is treated as empty by common convention. |

## Serialization

### `String() string`

Returns the string-equivalent content of a value.

`String` is not a JSON serialization method. It is for caller-friendly value content. For a JSON string value, it should return the contained string without JSON quotes.

```go
value := ojson.NewString("foo")

fmt.Println(value.String()) // foo
```

For non-string values, `String` should return the closest useful content representation for that value. For example, a number can return its decimal text, a boolean can return `true` or `false`, and `null` can return `null`. Use `ToJSON` when the caller needs valid JSON text.

### `ToJSON() string`

Returns minified JSON text.

`ToJSON` should return the JSON representation of the value. For a JSON string, that includes JSON quotes and escaping.

```go
value := ojson.NewString("foo")

fmt.Println(value.ToJSON()) // "foo"
```

Minified serialization should still preserve object order.

### `ToJSONBytes() []byte`

Returns minified JSON as bytes.

This is the byte-slice equivalent of `ToJSON` and is useful for file writes, network responses, and APIs that accept `[]byte`.

```go
data := doc.ToJSONBytes()
```

### `ToPrettyJSON(indent int) string`

Returns indented JSON text.

The indentation argument controls how many spaces are used for each nesting level.

```go
fmt.Println(doc.ToPrettyJSON(2))
```

Pretty output is the preferred form for Git-tracked JSON files because it makes line-oriented diffs easier to review.

### `ToPrettyJSONBytes(indent int) []byte`

Returns indented JSON as bytes.

This is the byte-slice equivalent of `ToPrettyJSON`.

```go
data := doc.ToPrettyJSONBytes(2)
```

### `WriteFile(path string) error`

Writes JSON text to a file.

`WriteFile` writes the same output as `ToPrettyJSON(2)`.

For project data files, prefer pretty output with stable indentation. Applications that need a different indentation policy can call `ToJSONBytes` or `ToPrettyJSONBytes` and write the file themselves.

## Schema Procedures

### Creating A Schema

1. Choose the root `kind`.
2. For objects, add ordered child schema entries.
3. Give each object child a `name`.
4. Add `required: true` for values that must be present.
5. Add `default` values for fields that should be inserted when missing.
6. Keep unsupported or application-specific validation outside the ojson schema document.

### Reading With A Schema

1. Compile or load the schema.
2. Load or parse the target JSON.
3. Validate known fields by kind.
4. Insert defaults.
5. Reject missing required fields that do not have defaults.
6. Store known fields in schema order.
7. Store unknown fields after known fields.

### Writing With A Schema

1. Mutate the document through schema-aware methods.
2. Preserve the schema-defined order.
3. Pretty-print the document.
4. Write the result to disk only after successful validation.

### Updating A Schema

When changing a schema used by Git-tracked JSON files:

1. append new fields where possible to minimize diffs
2. use defaults for newly introduced optional fields when a canonical value is desired
3. mark fields as required only when every existing document can supply them
4. run a normalization pass over existing documents
5. review the resulting diffs for unexpected reordering or data loss
