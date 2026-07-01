# OJSON TODO

This document tracks future changes to make in the `ojson` library so DatoriumDB can use richer schema metadata without adding DatoriumDB-specific behavior to `ojson`.

## Goals

- Keep `ojson` generic.
- Keep schemas strict enough to catch misspelled schema fields.
- Allow applications to attach opaque metadata to schema entries.
- Allow string fields to carry semantic formats that can validate values and support native Go conversion.

## Add `custom` Schema Metadata

Add an optional `custom` field to schema entries.

Recommended behavior:

- `custom` may contain any JSON value.
- `ojson` preserves `custom` when compiling schema documents.
- `ojson` does not interpret `custom`.
- Unknown schema fields should still be rejected.

Reasoning:

- A single known `custom` field keeps typo detection intact.
- Allowing any JSON value avoids needing another extension field later.
- DatoriumDB can use `custom` for database-specific policy or metadata without requiring `ojson` to understand it.

Implementation notes:

- Add `custom` to the known schema field list in `schema.go`.
- Add a `Custom JSONValue` field to the compiled schema entry if compiled schemas need to expose it.
- Copy the raw schema value into that field during schema compilation.
- Do not include `custom` in core validation logic.

Builder support:

- Add a builder option such as `Custom(value JSONValue)`.
- Optionally add convenience helpers such as `CustomString(value string)`.
- The current builder already stores arbitrary schema fields in `fields map[string]JSONValue`, so this should fit the existing design.

## Add Registered String Formats

Extend `format` for string schemas so applications can register custom formats.

Built-in formats should remain lowercase:

- `email`
- `tel`
- `url`

Custom formats should conventionally begin with an uppercase letter:

- `Time`
- `DatoriumDirectRef`
- `DatoriumCachedRef`

Recommended behavior:

- `kind: string` remains the storage type.
- `format` identifies semantic meaning and conversion behavior.
- Built-in formats continue to work without registration.
- Registered formats validate strings through application-provided logic.
- Unregistered formats should return a useful schema compilation error.

Possible Go API:

```go
type StringFormatValidator interface {
	ValidateString(value string) error
}
```

or, if conversion support belongs in the same mechanism:

```go
type StringFormatCodec interface {
	ValidateString(value string) error
	ToNative(value string) (any, error)
	FromNative(value any) (string, error)
}
```

Possible registration API:

```go
func RegisterStringFormat(name string, validator StringFormatValidator)
```

or:

```go
func RegisterStringFormatCodec(name string, codec StringFormatCodec)
```

## Support Native Go Type Association

DatoriumDB wants string-backed semantic types to convert cleanly to and from Go structs.

Examples:

```go
type DatoriumDirectRef string
type DatoriumCachedRef string
```

For time values, `format: Time` could map to Go's `time.Time`, with the stored JSON value remaining a UTC string.

Schema examples:

```text
{name: releasedAt, kind: string, format: Time}
{name: director, kind: string, format: DatoriumDirectRef}
{name: directorSummary, kind: string, format: DatoriumCachedRef}
```

Struct conversion considerations:

- `NewSchemaFromStruct` currently maps Go `string` and string aliases to `kind: string` behavior.
- Add an option to map specific Go types to string formats.
- Add an option to suggest specific Go types when reading schemas with registered formats.

Possible API:

```go
func StringFormatType(typeName string, format StringFormat) StructSchemaOption
```

Examples:

```go
NewSchemaFromStruct(Movie{}, StringFormatType("time.Time", "Time"))
NewSchemaFromStruct(Movie{}, StringFormatType("DatoriumDirectRef", "DatoriumDirectRef"))
```

Schema-to-struct suggestion should use registered type mappings when available:

```text
kind: string, format: Time -> time.Time
kind: string, format: DatoriumDirectRef -> DatoriumDirectRef
```

## Keep DatoriumDB Logic Out Of OJSON

Do not add direct DatoriumDB concepts to `ojson`, including:

- collection names
- document IDs
- direct document references
- cached document summary references
- DatoriumDB schema/version markers

Instead, DatoriumDB should register formats such as:

```text
DatoriumDirectRef
DatoriumCachedRef
```

and enforce DatoriumDB-specific string rules in its own validators.

## DatoriumDB Use Cases

Direct document reference:

```text
@__{collection}__{id}
```

Cached document summary reference:

```text
@@__{collection}__{id}
```

These should remain strings in stored document content. DatoriumDB should validate them through registered string formats and its own database-level rules.
