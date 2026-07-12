# Searching V1: Boolean

This document describes v1 search behavior for schema fields with `kind: boolean`.

Only schema-defined source-of-truth fields are searchable.

## Sorting

When a boolean field is referenced for the `sort` field in a search, the sort order is `false` before `true`.

## Supported Clauses

- `equals` with a field-path constant and value constant
- `equals` with a field-path constant and value variable
- `exists` with a field-path constant and truth variable

## `equals` With A Constant Value

```text
{field: {field_path_constant}, op: equals, value: {value_constant}}
```

For example:

```text
{field: /highRated, op: equals, value: true}
```

This `equals` form matches documents where the boolean field equals a value fixed in the search definition.

## `equals` With A Variable Value

```text
{field: {field_path_constant}, op: equals, value: {value_variable}}
```

For example:

```text
{field: /highRated, op: equals, value: $ratingFlag}
```

This `equals` form matches documents where the boolean field equals a value provided by the live query.

## `exists`

```text
{field: {field_path_constant}, op: exists, value: {truth_variable}}
```

For example:

```text
{field: /highRated, op: exists, value: $hasHighRated}
```

`exists` matches documents based on whether the boolean field is present.

An `exists` clause may include the constant option `hideNulls: true` to treat `null` as absent for that clause.

The truth value is a live query variable:

- `true` means the field must exist.
- `false` means the field must not exist.

Because the truth variable handles both cases, v1 boolean search does not need a separate `missing` clause.

## Encoding Note

When a truth variable determines an encoded clause directory, the directory name is the literal `true` or `false`.

Boolean value variables also encode to the literal `true` or `false`.
