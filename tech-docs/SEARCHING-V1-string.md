# Searching V1: String

This document describes v1 search behavior for schema fields with `kind: string`.

Only schema-defined source-of-truth fields are searchable.

## Supported Clauses

- `equals`
- `in`
- `exists`
- `missing`

## `equals`

`equals` matches one exact string value.

String comparison should be byte-for-byte unless a later version explicitly defines normalization or collation behavior.

## `in`

`in` matches one of several exact string values for the same field.

This is the v1 way to express same-field alternatives without supporting arbitrary OR clauses.

## `exists`

`exists` matches documents where the field is present.

## `missing`

`missing` matches documents where the field is absent.

## Not Supported In V1

V1 string searching does not support:

- `startsWith`
- `containsText`
- `regex`
- locale-aware collation
- case-insensitive matching
- normalized Unicode matching
