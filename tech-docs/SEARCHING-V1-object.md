# Searching V1: Object

This document describes v1 search behavior for schema fields with `kind: object`.

Only schema-defined source-of-truth fields are searchable.

## Supported Clauses

- `exists` with a field-path constant and truth variable

## `exists`

```text
{field: {field_path_constant}, op: exists, value: {truth_variable}}
```

For example:

```text
{field: /rating, op: exists, value: $hasRatingObject}
```

`exists` matches documents based on whether the object field is present.

An `exists` clause may include the constant option `hideNulls: true` to treat `null` as absent for that clause.

The truth value is a live query variable:

- `true` means the field must exist.
- `false` means the field must not exist.

Because the truth variable handles both cases, v1 object search does not need a separate `missing` clause.

## Encoding Note

When a truth variable determines an encoded clause directory, the directory name is the literal `true` or `false`.

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
