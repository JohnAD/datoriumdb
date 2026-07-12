# Searching V1: Number

This document describes v1 search behavior for schema fields with `kind: number`.

Only schema-defined source-of-truth fields are searchable.

Numbers need special care because JSON number text and numeric scalar value are not always the same concept. For example, `3` and `3.00` have the same scalar value but different textual representations.

## Sorting

When a number field is referenced for the `sort` field in a search, the sort order is scalar (not precise) in nature.

## Supported Clauses

- `scalarEquals` with a field-path constant, value constant, and truth variable
- `scalarEquals` with a field-path constant and value variable
- `preciselyEquals` with a field-path constant, value constant, and truth variable
- `preciselyEquals` with a field-path constant and value variable
- `scalarIn` with a field-path constant, value constants, and truth variable
- `preciselyIn` with a field-path constant, value constants, and truth variable
- `greaterThan` with a field-path constant, value constant, and truth variable
- `lessThan` with a field-path constant, value constant, and truth variable
- `greaterThanOrEqual` with a field-path constant, value constant, and truth variable
- `lessThanOrEqual` with a field-path constant, value constant, and truth variable
- `between` with a field-path constant, value constants, and truth variable
- `exists` with a field-path constant and truth variable

## `scalarEquals` With A Constant Value

```text
{field: {field_path_constant}, op: scalarEquals, value: {value_constant}, truth: {truth_variable}}
```

`scalarEquals` matches numbers by numeric scalar value.

For example, `3`, `3.0`, and `3.00` are scalar-equal.

This should be the default number equality behavior for most searches.

For example:

```text
{field: /rating, op: scalarEquals, value: 5, truth: $isFive}
```

The truth value is a live query variable:

- `true` means the number must be scalar-equal to the constant value.
- `false` means the number must not be scalar-equal to the constant value.

## `scalarEquals` With A Variable Value

```text
{field: {field_path_constant}, op: scalarEquals, value: {value_variable}}
```

For example:

```text
{field: /rating, op: scalarEquals, value: $rating}
```

This `scalarEquals` form matches documents where the number is scalar-equal to a value provided by the live query.

There is currently no variable-value "does not scalar-equal" clause.

## `preciselyEquals` With A Constant Value

```text
{field: {field_path_constant}, op: preciselyEquals, value: {value_constant}, truth: {truth_variable}}
```

`preciselyEquals` matches the exact stored number representation.

For example, `3` and `3.00` are not precisely equal.

This should only be used when the textual representation of the number is meaningful.

For example:

```text
{field: /rating, op: preciselyEquals, value: 5.0, truth: $isPreciselyFivePointZero}
```

The truth value is a live query variable:

- `true` means the number must precisely equal the constant value.
- `false` means the number must not precisely equal the constant value.

## `preciselyEquals` With A Variable Value

```text
{field: {field_path_constant}, op: preciselyEquals, value: {value_variable}}
```

For example:

```text
{field: /rating, op: preciselyEquals, value: $ratingText}
```

This `preciselyEquals` form matches documents where the number precisely equals a value provided by the live query.

There is currently no variable-value "does not precisely equal" clause.

## `scalarIn` With Constant Values

```text
{field: {field_path_constant}, op: scalarIn, value: [{value_constants}], truth: {truth_variable}}
```

`scalarIn` matches any of several numeric scalar values.

For example:

```text
{field: /rating, op: scalarIn, value: [3, 4, 5], truth: $isCommonRating}
```

The truth value is a live query variable:

- `true` means the number must be scalar-equal to one of the constant values.
- `false` means the number must not be scalar-equal to any of the constant values.

There is currently no variable-value `scalarIn` clause. For live lists, callers should perform repeated `scalarEquals` searches and union the matching document IDs.

## `preciselyIn` With Constant Values

```text
{field: {field_path_constant}, op: preciselyIn, value: [{value_constants}], truth: {truth_variable}}
```

`preciselyIn` matches any of several exact stored number representations.

For example:

```text
{field: /rating, op: preciselyIn, value: [3, 3.0, 3.00], truth: $hasSpecificRepresentation}
```

The truth value is a live query variable:

- `true` means the number must precisely equal one of the constant values.
- `false` means the number must not precisely equal any of the constant values.

There is currently no variable-value `preciselyIn` clause. For live lists, callers should perform repeated `preciselyEquals` searches and union the matching document IDs.

## Scalar Range Comparisons

V1 range comparisons are strictly constant expressions with a truth variable.

All range comparisons are scalar comparisons. The numbers do not need to match precision.

For example, scalar-wise `5.0 < 5.01` is `true`.

With precision-aware comparison, `5.0 < 5.01` would not be treated the same way. The value `5.0` is rounded to the nearest tenth, so a precision-aware comparison would be equivalent to `5.0 < 5.0`, which is `false`.

V1 does not support precision-aware range comparisons. A later version may add a constant option such as `withPrecision: true`.

## `greaterThan`

```text
{field: {field_path_constant}, op: greaterThan, value: {value_constant}, truth: {truth_variable}}
```

For example:

```text
{field: /rating, op: greaterThan, value: 7.5, truth: $isAboveSevenPointFive}
```

The truth value is a live query variable:

- `true` means the number must be scalar-greater than the constant value.
- `false` means the number must not be scalar-greater than the constant value.

## `lessThan`

```text
{field: {field_path_constant}, op: lessThan, value: {value_constant}, truth: {truth_variable}}
```

For example:

```text
{field: /rating, op: lessThan, value: 7.5, truth: $isBelowSevenPointFive}
```

The truth value is a live query variable:

- `true` means the number must be scalar-less than the constant value.
- `false` means the number must not be scalar-less than the constant value.

## `greaterThanOrEqual`

```text
{field: {field_path_constant}, op: greaterThanOrEqual, value: {value_constant}, truth: {truth_variable}}
```

For example:

```text
{field: /rating, op: greaterThanOrEqual, value: 7.5, truth: $isAtLeastSevenPointFive}
```

The truth value is a live query variable:

- `true` means the number must be scalar-greater than or scalar-equal to the constant value.
- `false` means the number must not be scalar-greater than or scalar-equal to the constant value.

## `lessThanOrEqual`

```text
{field: {field_path_constant}, op: lessThanOrEqual, value: {value_constant}, truth: {truth_variable}}
```

For example:

```text
{field: /rating, op: lessThanOrEqual, value: 7.5, truth: $isAtMostSevenPointFive}
```

The truth value is a live query variable:

- `true` means the number must be scalar-less than or scalar-equal to the constant value.
- `false` means the number must not be scalar-less than or scalar-equal to the constant value.

## `between`

```text
{field: {field_path_constant}, op: between, value: [{low_value_constant}, {high_value_constant}], truth: {truth_variable}}
```

For example:

```text
{field: /rating, op: between, value: [7.5, 9], truth: $isStrongRating}
```

The truth value is a live query variable:

- `true` means the number must be inside the scalar range.
- `false` means the number must not be inside the scalar range.

The `between` range is inclusive of both endpoints.

## `exists`

```text
{field: {field_path_constant}, op: exists, value: {truth_variable}}
```

For example:

```text
{field: /rating, op: exists, value: $hasRating}
```

`exists` matches documents based on whether the number field is present.

An `exists` clause may include the constant option `hideNulls: true` to treat `null` as absent for that clause.

The truth value is a live query variable:

- `true` means the field must exist.
- `false` means the field must not exist.

Because the truth variable handles both cases, v1 number search does not need a separate `missing` clause.

## Encoding Note

When a truth variable determines an encoded clause directory, the directory name is the literal `true` or `false`.

For `preciselyEquals`, the encoded directory name is the number text as expressed in the JSON document, with small normalizations:

- directory names may contain dashes and dots, so negative signs and decimal points are kept as-is
- uppercase exponent `E` is normalized to lowercase `e`
- a leading `+` is removed

For scalar number comparisons, the encoded directory name is based on Go `float64` canonical formatting:

```text
strconv.FormatFloat(f, 'g', -1, 64)
```

where `f` is the result of parsing the JSON number text with `strconv.ParseFloat(..., 64)`.

For example, `5.0`, `5`, and `0.5e1` all encode as `5`.

This is intentionally not precision-preserving. Very large integers, very precise decimals, and values outside the `float64` range are not good candidates for v1 scalar number search. Use precise comparisons when textual numeric precision matters.

Range clauses do not have variable values in v1. Their values are constants in the search definition. The encoded directory component that changes at live query time is only the truth value, encoded as `true` or `false`.

## Not Supported In V1

V1 number searching does not support:

- caller-supplied ranges

Variable range behavior should be modeled with sorting, exact buckets, or precomputed boolean fields.
