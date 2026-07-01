# Searching V1

This document is a quick reference for the operations supported by v1 precompiled searches.

V1 searches are AND-only. Every clause targets a schema-defined source-of-truth field. Non-schema fields are not searchable.

## Operation Support

| Operation | String | Number | Boolean | Null | Array | Object | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `equals` | yes | yes | yes | yes | no | no | Exact value match. |
| `in` | yes | yes | yes | no | no | no | Same-field alternatives. |
| `isTrue` | no | no | yes | no | no | no | Equivalent to `equals true`. |
| `isFalse` | no | no | yes | no | no | no | Equivalent to `equals false`. |
| `exists` | yes | yes | yes | yes | yes | yes | Field is present. |
| `missing` | yes | yes | yes | yes | yes | yes | Field is absent. |
| `contains` | no | no | no | no | limited | no | Only for arrays of scalar values. |

## Not Supported In V1

V1 does not support:

- `greaterThan`
- `lessThan`
- `between`
- `notEquals`
- `notIn`
- `startsWith`
- `containsText`
- `regex`
- caller-defined OR clauses
- cross-field boolean expressions

Variable range behavior should be modeled with sorting, exact buckets, or precomputed boolean fields.

## Sort Support

Sorting is defined separately from filter clauses.

Sort fields must be schema-defined source-of-truth fields, except that `!` may be used as a final deterministic tie-breaker.
