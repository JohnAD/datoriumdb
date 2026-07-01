# Searching V1: Boolean

This document describes v1 search behavior for schema fields with `kind: boolean`.

Only schema-defined source-of-truth fields are searchable.

## Supported Clauses

- `equals`
- `in`
- `isTrue`
- `isFalse`
- `exists`
- `missing`

## `equals`

`equals` matches one boolean value.

For example:

```text
{field: /highRated, op: equals, value: true}
```

## `in`

`in` matches one of several boolean values.

This is allowed for consistency, though it is rarely useful because booleans only have two values.

## `isTrue`

`isTrue` matches documents where the field is present and `true`.

## `isFalse`

`isFalse` matches documents where the field is present and `false`.

## `exists`

`exists` matches documents where the field is present.

## `missing`

`missing` matches documents where the field is absent.
