# Searching V1: Null

This document describes v1 search behavior for matching the JSON value `null`.

`null` is not a valid target kind in a collection schema. These clauses are used against nullable schema fields of another kind.

Only schema-defined source-of-truth fields are searchable.

## Supported Clauses

- `equals` with a field-path constant, `null` value constant, and truth variable

## `equals`

```text
{field: {field_path_constant}, op: equals, value: null, truth: {truth_variable}}
```

`equals` matches the JSON value `null`.

For example:

```text
{field: /retiredAt, op: equals, value: null, truth: $isRetiredAtUnknown}
```

The truth value is a live query variable:

- `true` means the field must be present with the value `null`.
- `false` means the field must not be `null`.

Because `null` is a value rather than a schema kind, v1 null search does not need separate `exists` or `missing` clauses.

## Encoding Note

When a truth variable determines an encoded clause directory, the directory name is the literal `true` or `false`.

A `null` comparison value encodes to the literal `null`.

This does not conflict with string values because string values are hex-encoded.

## Null Versus Missing

`null` and missing are different.

`null` means the field is present and not yet known. Missing means the field is not present in the document.
