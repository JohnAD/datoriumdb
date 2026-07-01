# Searching V1: Number

This document describes v1 search behavior for schema fields with `kind: number`.

Only schema-defined source-of-truth fields are searchable.

Numbers need special care because JSON number text and numeric scalar value are not always the same concept. For example, `3` and `3.00` have the same scalar value but different textual representations.

## Supported Clauses

- `scalarEquals`
- `preciselyEquals`
- `scalarIn`
- `preciselyIn`
- `exists`
- `missing`

## `scalarEquals`

`scalarEquals` matches numbers by numeric scalar value.

For example, `3`, `3.0`, and `3.00` are scalar-equal.

This should be the default number equality behavior for most searches.

## `preciselyEquals`

`preciselyEquals` matches the exact stored number representation.

For example, `3` and `3.00` are not precisely equal.

This should only be used when the textual representation of the number is meaningful.

## `scalarIn`

`scalarIn` matches any of several numeric scalar values.

## `preciselyIn`

`preciselyIn` matches any of several exact stored number representations.

## `exists`

`exists` matches documents where the field is present.

## `missing`

`missing` matches documents where the field is absent.

## Not Supported In V1

V1 number searching does not support:

- `greaterThan`
- `lessThan`
- `greaterThanOrEqual`
- `lessThanOrEqual`
- `between`
- caller-supplied ranges

Variable range behavior should be modeled with sorting, exact buckets, or precomputed boolean fields.
