# Searching V1: Array

This document describes v1 search behavior for schema fields with `kind: array`.

Only schema-defined source-of-truth fields are searchable.

## Supported Clauses

- `contains`
- `exists`
- `missing`

## `contains`

`contains` matches documents where the array contains a specific scalar value.

For v1, `contains` is limited to arrays whose item schema is one of:

- `string`
- `number`
- `boolean`
- `null`

Arrays of objects are not supported by `contains` in v1.

Number array items should follow the same scalar-versus-precise distinction described in `SEARCHING-V1-number.md`, but the exact operation names for numeric array matching are still to be decided.

## `exists`

`exists` matches documents where the array field is present.

## `missing`

`missing` matches documents where the array field is absent.

## Not Supported In V1

V1 array searching does not support:

- searching inside arrays of objects
- nested array search
- array length comparisons
- all-items matching
- ordered subsequence matching
