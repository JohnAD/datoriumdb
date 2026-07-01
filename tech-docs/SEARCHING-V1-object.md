# Searching V1: Object

This document describes v1 search behavior for schema fields with `kind: object`.

Only schema-defined source-of-truth fields are searchable.

## Supported Clauses

- `exists`
- `missing`

## `exists`

`exists` matches documents where the object field is present.

## `missing`

`missing` matches documents where the object field is absent.

## Searching Object Children

V1 does not search an object value as a whole.

If a child field inside an object should be searchable, the search definition should target that schema-defined child field directly by path.

For example:

```text
{field: /rating/source, op: equals, value: imdb}
```

## Not Supported In V1

V1 object searching does not support:

- whole-object equality
- object containment
- arbitrary nested predicates
- searching non-schema object fields
