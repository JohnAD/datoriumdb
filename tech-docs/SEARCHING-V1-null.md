# Searching V1: Null

This document describes v1 search behavior for schema fields with `kind: null`.

Only schema-defined source-of-truth fields are searchable.

## Supported Clauses

- `equals`
- `exists`
- `missing`

## `equals`

`equals` matches the JSON value `null`.

For example:

```text
{field: /retiredAt, op: equals, value: null}
```

## `exists`

`exists` matches documents where the field is present.

For a null field, `exists` means the field is present with the value `null`.

## `missing`

`missing` matches documents where the field is absent.

## Null Versus Missing

`null` and missing are different.

`null` means the field is present and explicitly unknown or empty. Missing means the field is not present in the document.
