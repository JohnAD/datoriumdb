# Searching

DatoriumDB has **no ad-hoc queries**. Every search is a *precompiled search*:
an operator declares a search definition with `datoriumctl search create`, and
clients can then run it by name. Searches are maintained on write, so reads
are fast, but results are **eventually correct** — a search may briefly lag
behind a just-committed write (see [Consistency Model](consistency.md)).

## Running a search

```text
search {collection} {searchName} {search-parms}
```

Example:

```text
search Movies byReleasedGenre {status: released, useGenreFilter: true, genre: scifi}
```

Success response:

```json
{
  "ok": true,
  "command": "search",
  "collection": "Movies",
  "search": "byReleasedGenre",
  "matches": ["01KWD65CFQPEZS7H1WJE4MK990", "01KWD65EJ5F61CE0GS9SX4V4FT"]
}
```

`matches` is the **complete list of matching document IDs**, in the sort order
declared by the search definition. There is no pagination, no result limit,
and no cursor — design searches so result sets stay a reasonable size. To get
document contents, `read` each returned ID; there is no multi-get or batch
read.

There is also no command to list all documents in a collection. Enumerating a
collection requires a precompiled search that matches what you need.

## Defining a search

A search definition is an immutable JSON document installed with
`datoriumctl search create`. Example:

```json
{
  "$": "SearchDefinition:v1",
  "collection": "Movies",
  "name": "byReleasedGenre",
  "version": 1,
  "v1": {
    "clauses": [
      {"field": "/status", "op": "equals", "value": "$status"},
      {"field": "/genre", "op": "in", "value": ["scifi", "fantasy"], "truth": "$useGenreFilter", "select": "$genre"},
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

Rules to know:

- **AND-only.** All clauses must match. There is no OR, no `notEquals`, no
  regex, no full-text search.
- **Schema fields only.** Clauses and sort fields must target schema-defined
  source-of-truth fields. Non-schema (`extraFields`) data is not searchable.
- **Constants vs. variables.** A clause value may be a constant fixed at
  definition time, or a `$variable` supplied by the live query. Constants
  narrow the definition; variables select among precomputed result buckets.
  Range operators accept constants only.
- **`in` with multiple constants requires a `select` variable.** The live
  query picks exactly one allowed value; the server never unions buckets.
- **`truth` variables** enable/disable a constant clause at query time
  (`true`/`false`).
- **Immutable.** To change a search, `datoriumctl search delete` it and create
  a new one.

## Supported operations (implemented today)

| Operation | Kinds | Notes |
| --- | --- | --- |
| `equals` | string, boolean, null | Exact match. Variable strings limited to 63 runes. |
| `in` / `scalarIn` | string, number | Alternatives for one field; multi-value constant form needs a live `select` variable. |
| `exists` | all | Field present (true) or absent (false). `null` counts as present unless the clause sets `hideNulls: true`. |
| `contains` | arrays of scalars | Array contains a scalar value; variable-value form indexes multiple buckets. |

Specified but **not yet implemented** (rejected by validation until they are):
`hashEquals`, `preciselyEquals`/`preciselyIn`, `endsWith`, `startsWith`,
`containsText`, and the number range operators `greaterThan`, `lessThan`,
`greaterThanOrEqual`, `lessThanOrEqual`, `between` — range ops are planned as
constant-only.

Practical guidance for range-like queries today: model them with exact
buckets, boolean fields computed at write time (e.g. a `highRated` flag), or
sort order plus client-side filtering.

## Sorting and null/missing

Sorting is declared in the definition (`sort`), not at query time. `matches`
is stored and returned in that order. Use `/!` (document ID) as a final
tie-breaker for deterministic order.

For both `asc` and `desc`: known values sort first, then `null`, then missing
fields.

## Missing vs. null

- **Missing**: the field path is absent.
- **Null**: the path is present with value `null` (meaning "not yet known").
- An `exists` clause is structural by default — `null` counts as existing.
  Add the constant option `hideNulls: true` to treat `null` as absent for that
  clause.

## Routing

Searches are sharded separately from documents. A smart client computes the
search shard from the encoded search parameters and queries the machine that
owns that search shard. Querying the wrong machine returns `ok: false` with
`wrongMachine`; refresh establishment config and retry.
