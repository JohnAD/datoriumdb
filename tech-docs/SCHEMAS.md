# Schemas

This document is a rough draft explaining how DatoriumDB collection schemas are built.

DatoriumDB schemas are based on the OJSON schema format defined at <https://github.com/JohnAD/ojson>. An OJSON schema is itself a JSON document. DatoriumDB uses that schema model, but adds database-specific rules around collection schemas, source-of-truth fields, document references, and metadata.

## Root Schema

Every DatoriumDB collection schema MUST have a root `kind: object`.

OJSON may support other root kinds, such as arrays or strings, but DatoriumDB collection documents are always objects.

The root object contains an ordered `children` array. Each child describes one schema-defined field.

For example:

```text
{
  kind: object,
  children: [
    {name: title, kind: string, required: true},
    {name: releaseYear, kind: number, integer: true},
    {name: status, kind: string},
    {name: highRated, kind: boolean}
  ]
}
```

## Supported Kinds

DatoriumDB follows OJSON's basic schema kinds:

- `object`
- `array`
- `string`
- `number`
- `boolean`
- `null`

There is no `any` kind.

## Field Order

Schema field order matters.

OJSON preserves object field order, and DatoriumDB uses schema order as the canonical order for schema-defined fields. Unknown or non-schema fields may still exist in a document, but schema-defined fields come first and follow the schema's `children` order.

## Source-Of-Truth Fields

Schema-defined fields are source-of-truth fields unless a later schema convention says otherwise.

Searches may only use schema-defined source-of-truth fields. Non-schema fields are not searchable.

## Required And Optional Fields

A field can be marked as required:

```text
{name: title, kind: string, required: true}
```

If `required` is absent or false, the field is optional.

## Defaults

A field can define a default value:

```text
{name: highRated, kind: boolean, default: false}
```

Defaults must match the field kind.

## Null Versus Missing

`null` and missing are different.

`null` means a field is present with the JSON value `null`. Missing means the field is not present in the document.

If a field can be explicitly `null`, it should use OJSON's nullable behavior:

```text
{name: retiredAt, kind: string, nullable: true}
```

## Strings And Formats

String fields may use a `format` value to describe string semantics beyond ordinary text.

DatoriumDB currently defines two custom string formats:

- `DatoriumDirectRef`
- `DatoriumCachedRef`

These are still stored as strings in document content.

## Direct Document References

A direct document reference uses `format: DatoriumDirectRef`.

Direct references can be optionally returned from a `read` command. They are computationally expensive because they are pulled directly from live data. They are slower, but guaranteed to be accurate.

For example:

```text
{name: director, kind: string, format: DatoriumDirectRef}
```

The stored value should use the direct document reference convention:

```text
@__{collection}__{id}
```

For example:

```text
@__People__01KWD65CFQPEZS7H1WJE4MK990
```

You can add an optional `custom` object to direct reference fields. Specifically:

- `collection: {collection}` locks the reference to a single collection.
- `summary: [...]` specifies which fields should be included from the referenced document. Non-schemed fields from the other collection cannot be returned.

If the `custom` field is missing, any collection can be referenced, and all the SOT fields are returned from the live referenced document.

## Cached Document Summary References

A cached document summary reference uses `format: DatoriumCachedRef`.

A cache is a local copy of the referenced document that is later updated by the background agent. These fields are fast to retrieve, but they are only "eventually correct". Cached references can be optionally returned from a `read` command.

For example:

```text
{name: directorSummary, kind: string, format: DatoriumCachedRef}
```

The stored value should use the cached document summary reference convention:

```text
@@__{collection}__{id}
```

For example:

```text
@@__People__01KWD65CFQPEZS7H1WJE4MK990
```

You can add an optional `custom` object to cached reference fields. Specifically:

- `collection: {collection}` locks the reference to a single collection.
- `summary: [...]` specifies which fields should be locally cached from the referenced document. Non-schemed fields from the other collection cannot be returned.

For example:

```text
{
  name: directorSummary,
  kind: string,
  format: DatoriumCachedRef,
  custom: {
    collection: People,
    summary: [name, birthYear]
  }
}
```

If the `custom` field is missing, any collection can be referenced, and all the SOT fields are cached from the referenced document.

## Arrays

Array schemas may define an `items` schema.

For example:

```text
{
  name: genres,
  kind: array,
  items: {kind: string}
}
```

Arrays may contain values of the item kind. Search support for arrays is limited in v1 and should be checked against `SEARCHING-V1-array.md`.

## Objects

Object fields may contain their own ordered `children`.

For example:

```text
{
  name: rating,
  kind: object,
  children: [
    {name: source, kind: string},
    {name: value, kind: number}
  ]
}
```

Searches can target schema-defined child fields by path, such as `/rating/source`.

## Database-Owned Metadata

DatoriumDB owns these document metadata fields:

- `!` is the document ID.
- `$` is the schema/version marker.
- `#` is the document version.

When creating documents, DatoriumDB can automatically create `!` and `$` if they are missing. User-supplied document content cannot include `#`, because document versions are created by the database.

## Example Collection Schema

```text
{
  kind: object,
  children: [
    {name: title, kind: string, required: true},
    {name: status, kind: string, required: true},
    {name: genre, kind: string},
    {name: highRated, kind: boolean, default: false},
    {name: releaseYear, kind: number, integer: true},
    {name: director, kind: string, format: DatoriumDirectRef},
    {name: directorSummary, kind: string, format: DatoriumCachedRef}
  ]
}
```

This schema describes a `Movies` collection document. The root object is required, field order is explicit, and the reference fields remain strings while carrying DatoriumDB-specific formats.
