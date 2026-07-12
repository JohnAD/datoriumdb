# Searching V1: Array

This document describes v1 search behavior for schema fields with `kind: array`.

Only schema-defined source-of-truth fields are searchable.

## Supported Clauses

- `exists` with a field-path constant and truth variable
- `contains` with a field-path constant, value constant, and truth variable
- `contains` with a field-path constant and value variable
- `endsWith` with a field-path constant, value constant, and truth variable
- `endsWith` with a field-path constant and value variable

## `exists`

```text
{field: {field_path_constant}, op: exists, value: {truth_variable}}
```

`exists` matches documents based on whether the array field is present.

An `exists` clause may include the constant option `hideNulls: true` to treat `null` as absent for that clause.

The field path is a constant fixed in the search definition. For example:

```text
{field: /user/address/zip_code, op: exists, value: $hasZipCode}
```

The truth value is a live query variable:

- `true` means the field must exist.
- `false` means the field must not exist.

Because the truth variable handles both cases, v1 array search does not need a separate `missing` clause.

An empty array `[]` is still an array that exists. To check for `null`, see `SEARCHING-V1-null.md`.

## `contains` With A Constant Value

```text
{field: {field_path_constant}, op: contains, value: {value_constant}, truth: {truth_variable}}
```

For example:

```text
{field: /user/ratings, op: contains, value: 5, truth: $hasAFive}
```

This `contains` form matches documents based on whether the array contains a specific scalar value fixed in the search definition.

The truth value is a live query variable:

- `true` means the array must contain the constant value.
- `false` means the array must not contain the constant value.

## `contains` With A Variable Value

```text
{field: {field_path_constant}, op: contains, value: {value_variable}}
```

For example:

```text
{field: /user/ratings, op: contains, value: $specificRating}
```

This `contains` form matches documents where the array contains a scalar value provided by the live query.

The encoded directory name for the live value is based on the array item's schema kind.

There is currently no "does not contain" clause.

## `endsWith` With A Constant Value

```text
{field: {field_path_constant}, op: endsWith, value: {value_constant}, truth: {truth_variable}}
```

For example:

```text
{field: /user/ratings, op: endsWith, value: 5, truth: $endsWithFive}
```

This `endsWith` form matches documents based on whether the array ends with a specific scalar value fixed in the search definition.

The truth value is a live query variable:

- `true` means the array must end with the constant value.
- `false` means the array must not end with the constant value.

## `endsWith` With A Variable Value

```text
{field: {field_path_constant}, op: endsWith, value: {value_variable}}
```

For example:

```text
{field: /user/ratings, op: endsWith, value: $specificRating}
```

This `endsWith` form matches documents where the array ends with a scalar value provided by the live query.

The encoded directory name for the live value is based on the array item's schema kind.

There is currently no variable-value "does not end with" clause.

For v1, `contains` and `endsWith` are limited to arrays whose item schema is one of:

- `string`
- `number`
- `boolean`
- `null`

Arrays of objects are not supported by `contains` or `endsWith` in v1.

Number array items should follow the same scalar-versus-precise distinction described in `SEARCHING-V1-number.md` when an indexed array path targets a number.

## Encoding Note

When a truth variable determines an encoded clause directory, the directory name is the literal `true` or `false`.

For `contains` and `endsWith`, constant and variable values use the encoding rules for the array item's schema kind.

The `hideNulls` option is not supported for `contains` or `endsWith` in v1. If an array contains `null`, `null` is treated as a real array item value and encodes according to the null encoding rules.

## Hints

Field paths can include array indexes.

An indexed path targets an entry inside the array, not the array itself. The indexed entry's kind controls which search and encoding rules apply.

For example, `{field: /user/ratings/3, op: exists, value: true}` is a way to check whether the array has at least four items.

Checking `{field: /user/ratings/0, op: preciselyEquals, value: 5}` is a way to check whether the array starts with `5`.

## Not Supported In V1

V1 array searching does not support:

- searching inside arrays of objects
- nested array search
- array length comparisons
- all-items matching
- ordered subsequence matching
