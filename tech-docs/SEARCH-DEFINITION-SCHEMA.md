# Search Definition Schema

This document defines the shape of stored search definitions.

Search definitions describe precompiled searches. They are not live query objects. Every clause is declared ahead of time, and v1 searches are AND-only.

## Storage

Search definitions should be stored as config files under `/db/.config`.

Tentative filenames:

```text
/db/.config/{CollectionName}.search.{SearchName}.json
/db/.config/{CollectionName}.search.{SearchName}.{ver}.json
```

The unversioned file stores the current search definition. The versioned file preserves search definition history.

The exact filename convention can still change, but search definitions belong with the other establishment-served config because smart clients and servers need to understand them.

The collection name and search name are sufficient to identify a stored search definition and its stored search directories.

## MVP Immutability

For the MVP, search definitions are immutable.

An existing search definition cannot be updated or changed in place. If a user needs a different search definition, they must delete the old search and create a new one.

This avoids having to migrate old search result directories or reinterpret existing `matches.json` files under changed clause rules.

## Root Shape

Conceptually:

```json
{
  "$": "SearchDefinition:v1",
  "collection": "Movies",
  "name": "byReleasedGenre",
  "version": 1,
  "v1": {
    "clauses": [
      {"field": "/status", "op": "equals", "value": "$status"},
      {"field": "/genre", "op": "in", "value": ["scifi", "fantasy"], "truth": "$useGenreFilter"},
      {"field": "/highRated", "op": "equals", "value": true}
    ],
    "sort": [
      {"field": "/releaseYear", "dir": "desc"},
      {"field": "/title", "dir": "asc"},
      {"field": "/!", "dir": "asc"}
    ]
  }
}
```

## Root Fields

- `$`: identifies the stored document shape. For v1 search definitions, use `SearchDefinition:v1`.
- `collection`: the collection the search belongs to.
- `name`: the search name.
- `version`: the search definition version.
- `v1`: the v1 search definition body.

## V1 Body

The `v1` body contains:

- `clauses`: an ordered array of clause objects.
- `sort`: an ordered array of sort objects.

Clause order is significant. Search result directory paths embed clause encodings in the same order as the `clauses` array.

## Clause Shape

Every clause has:

- `field`: a schema-defined source-of-truth field path.
- `op`: the operation name.

Most clauses also have:

- `value`: a constant value, a live variable, or an array of constant values.

Some constant-value clauses also have:

- `truth`: a live truth variable.

## Field Paths

Field paths use slash-style paths:

```text
/status
/rating/source
/user/ratings/3
```

The field path must resolve to a schema-defined source-of-truth field or to an indexed entry inside a schema-defined array. Non-schema fields are not searchable.

Indexed array paths target the array entry, not the array itself. The entry kind controls which clause and encoding rules apply.

## Variables

Variables are strings beginning with `$`.

For example:

```text
$status
$hasRating
$specificRating
```

Variables are supplied by the live query. Not all clauses support variables.

## Truth

`truth` is used when a constant-value clause can be enabled in a positive or negative sense at live query time.

For example:

```text
{field: /rating, op: greaterThan, value: 7.5, truth: $aboveMinimum}
```

When `$aboveMinimum` is `true`, the number must be greater than `7.5`.

When `$aboveMinimum` is `false`, the number must not be greater than `7.5`.

Truth variables encode as the literal path components `true` and `false`.

## Exists Clauses

`exists` uses `value` as its truth variable:

```text
{field: /rating, op: exists, value: $hasRating}
```

`true` means the field must exist.

`false` means the field must not exist.

An `exists` clause may include the constant option `hideNulls: true`.

`hideNulls` is not a live variable. The only supported value is the literal constant `true`.

## Supported V1 Ops

V1 operation support is defined in `SEARCHING-V1.md` and the kind-specific documents:

- `SEARCHING-V1-array.md`
- `SEARCHING-V1-boolean.md`
- `SEARCHING-V1-null.md`
- `SEARCHING-V1-number.md`
- `SEARCHING-V1-object.md`
- `SEARCHING-V1-string.md`

## Sort Shape

Each sort item has:

- `field`: a schema-defined source-of-truth field path, or `!` as a final deterministic tie-breaker.
- `dir`: `asc` or `desc`.

For example:

```text
{field: /releaseYear, dir: desc}
{field: /title, dir: asc}
{field: "/!", dir: asc}
```

Sort fields are not filter clauses. They determine stored item order inside `matches.json`.

## Validation Rules

A stored search definition is invalid if:

- `collection` does not name an existing collection.
- `name` is not a valid search name.
- `version` is not a positive integer.
- `clauses` is empty.
- any clause targets a non-schema field.
- any clause uses an operation not supported by the field kind.
- any variable appears in a clause form that does not support variables.
- any constant option is supplied with a variable value.
- `sort` refers to a non-schema field, except for the final `!` tie-breaker.

## Open Questions

- Should search definition files be versioned with an integer, a ULID, or the establishment config version?
- Should search definition file names include the search version?
- Should search definitions be included directly in the combined establishment response?
