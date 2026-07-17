# Searching V1

This document is a quick reference for the operations supported by v1 precompiled searches.

V1 searches are AND-only. Every clause targets a schema-defined source-of-truth field. Non-schema fields are not searchable.

By default, `exists` is structural: a field exists if the path is present, even when the value is `null`. An `exists` clause may specify the constant option `hideNulls: true` to treat `null` as not existing for that specific search clause.

When a truth variable determines an encoded clause directory, the directory name is the literal `true` or `false`.

## Operation Support

Kind-specific documents are authoritative for operation names. This table is a quick index.

| Operation | String | Number | Boolean | Null | Array | Object | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `equals` | yes | no | yes | yes | no | no | Exact value match for non-number kinds. String variable `equals` is limited to 63 runes in v1. |
| `scalarEquals` / `preciselyEquals` | no | yes | no | no | no | no | Number equality forms. See `SEARCHING-V1-number.md`. |
| `hashEquals` | yes | no | no | no | no | no | String-only 32-bit hash comparison for long variable values. |
| `in` | yes | no | no | no | no | no | Same-field alternatives. Constant multi-value `in` requires a live `select` variable. |
| `scalarIn` / `preciselyIn` | no | yes | no | no | no | no | Number `in` forms. See `SEARCHING-V1-number.md`. |
| `exists` | yes | yes | yes | no | yes | yes | Field is present. Array `exists` uses a truth variable for present or absent. |
| `missing` | no | no | no | no | no | no | Use `exists false` instead. |
| `contains` | no | no | no | no | limited | no | Only for arrays of scalar values; array forms distinguish constants and variables. |
| `endsWith` | no | no | no | no | limited | no | Only for arrays of scalar values; checks the final array item. |
| `greaterThan` | no | yes | no | no | no | no | Number-only scalar comparison; constant value only in v1. |
| `lessThan` | no | yes | no | no | no | no | Number-only scalar comparison; constant value only in v1. |
| `greaterThanOrEqual` | no | yes | no | no | no | no | Number-only scalar comparison; constant value only in v1. |
| `lessThanOrEqual` | no | yes | no | no | no | no | Number-only scalar comparison; constant value only in v1. |
| `between` | no | yes | no | no | no | no | Number-only scalar comparison; constant values only in v1. |
| `startsWith` | yes | no | no | no | no | no | String-only byte-prefix match; constant value only in v1. |
| `containsText` | yes | no | no | no | no | no | String-only byte-substring match; constant value only in v1. |

## MVP Implementation Scope

The first implementation supports a narrow subset:

- string/boolean/null `equals`
- string/number constant `in` / `scalarIn` with live selector
- `exists`
- deterministic sort including `!`
- AND-only clauses

Broader V1 operations remain specified for later implementation and should be rejected by MVP validators until implemented.

## Not Supported In V1

V1 does not support:

- `notEquals`
- `notIn`
- `regex`
- caller-defined OR clauses
- cross-field boolean expressions

Variable range behavior should be modeled with sorting, exact buckets, or precomputed boolean fields.

## Sort Support

Sorting is defined separately from filter clauses.

Sort fields must be schema-defined source-of-truth fields, except that `!` may be used as a final deterministic tie-breaker.

For both `asc` and `desc`, known non-null values sort first, `null` values sort after known values, and missing values sort after `null` values.
