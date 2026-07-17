# Searching

DatoriumDB does not support open-ended searches across arbitrary fields. Searches are precompiled and maintained as stored files.

This document currently assumes the filesystem-backed `plain-text-json` storage method.

Document writes commit the source document first. The change-agent later updates any stored search result files affected by that document. Reads against a search are still fast because the search result structure already exists; search results are eventually correct relative to recent writes.

## Goals

- Keep read-time search behavior simple and fast.
- Move search cost to document writes.
- Make search state git-trackable when using JSON filestorage.
- Avoid SQL-style joins and MongoDB-style open-ended query objects.
- Make each supported search explicit before it can be used.

## Precompiled Search Model

A precompiled search is a named stored structure derived from one or more fields in a collection.

For example, a `Movies.byYear` search might maintain a stored file keyed by `Year`, with each key pointing to the matching document IDs.

Conceptually:

```text
Movies.byYear
1999 -> [01KWD65CFQPEZS7H1WJE4MK990, 01KWD65D94Y5M8C2Z7HJ3N6VQK]
2003 -> [01KWD65EJ5F61CE0GS9SX4V4FT]
```

## Search Definitions (v1)

Precompiled searches are declared explicitly through `datoriumctl search create`, not through the access language. The MVP search model supports AND'ed clauses only.

For example, an operator installs a search definition file that declares `byReleasedGenre` for the `Movies` collection. Live clients then call:

```text
search Movies byReleasedGenre {status: released, useGenreFilter: true, genre: scifi}
```

The clauses are evaluated as an AND:

- `status` must equal the provided `$status`.
- if `$useGenreFilter` is `true`, `genre` must equal the provided live selector `$genre`, and `$genre` must be one of the definition's allowed constants such as `scifi` or `fantasy`.
- `highRated` must be `true`.

A constant multi-value `in` clause always requires a live selector variable when the query enables that clause. The server resolves exactly one encoded result bucket per request and does not union buckets implicitly.

The sort definition determines the stored order of document IDs inside matching search result files.

The stored search definition shape is defined in `SEARCH-DEFINITION-SCHEMA.md`.

## Constants And Variables

A **constant** is a search parameter fixed when the search is defined.

A **variable** is a search parameter provided when a live query uses the precompiled search.

Variables are written with a leading `$` in search definitions. For example, `$status`, `$useGenreFilter`, and `$genre` are variables in the `byReleasedGenre` example above.

Constants and variables both keep searches precompiled. A constant narrows the search definition itself. A variable selects among the stored search result files that the precompiled search maintains.

Not all search clauses support variables. Some clauses only make sense with constants because allowing a live value would turn the search into an open-ended predicate rather than a precompiled lookup.

## Null, Missing, And Exists

DatoriumDB treats `null` as a known document state with a specific meaning: the value is not yet known.

Searches should distinguish three states:

- **Missing**: the field path is absent.
- **Null**: the field path is present with the value `null`.
- **Known value**: the field path is present with a non-null value.

By default, `exists` is structural. A field exists if the path is present, even when the value at that path is `null`.

For example:

```text
{field: /middleName, op: exists, value: true}
```

matches a document where `/middleName` is present with `null`.

Some searches may want to hide `null` values and treat them as if the path does not exist. An `exists` clause can opt into that behavior with the constant option `hideNulls: true`.

For example:

```text
{field: /middleName, op: exists, value: true, hideNulls: true}
```

`hideNulls` is not a live variable. The only supported value is the literal constant `true`.

With `hideNulls: true`, a document where `/middleName` is `null` does not match `exists true`.

## Search File Paths

For filesystem-backed `plain-text-json` storage, precompiled search files live under the collection's `.search` directory.

The collection name and search name are sufficient to identify the stored search directory.

Each directory below the search name encodes one search clause, in the same order as the clauses are declared in the search definition.

The final file is always named `matches.json` and contains the matching documents for that encoded clause path.

For example, a document matching `status: released` and `genre: scifi` for the `byReleasedGenre` search is upserted to:

```text
$/db/Movies/.search/byReleasedGenre/released/scifi/matches.json
```

Because string values are open-ended and filesystems have naming restrictions, path components derived from string values should be encoded as uppercase hex from the UTF-8 bytes.

An empty string encodes as the literal `empty`.

When a clause path component is determined by a truth variable, the directory name is simply `true` or `false`.

When a clause path component is determined by a `null` comparison value, the directory name is simply `null`.

This does not conflict with string values because string values are hex-encoded.

Using encoded path components, the same example becomes:

```text
$/db/Movies/.search/byReleasedGenre/72656c6561736564/7363696669/matches.json
```

## Search Sharding

Search result files are sharded separately from documents.

The shard slot for a search result is computed from the encoded search directory path below the search name, with leading and trailing slashes removed. The final `matches.json` filename is not part of the shard input.

For example, this search target:

```text
$/db/Movies/.search/byReleasedGenre/72656c6561736564/7363696669/matches.json
```

uses this shard input:

```text
72656c6561736564/7363696669
```

Because clients can compute the search shard, a smart client should query the machine assigned to that search result shard.

For this reason, smart clients need to understand search clause rules and search value encoding rules. Without those rules, a client cannot know which shard slot contains the search result document.

Querying the wrong machine returns an `ok: false` error envelope.

Searches are eventually correct. When a document is updated, the SOT machine that accepted the document write does not update every affected search result immediately.

Instead, a later agent computes the related search changes and sends each affected search result update to the SOT machine for that search shard. The search update is sent as a patch to the search result file.

When the search SOT receives the patch, it applies the search result update and distributes that change to the read-members for the search shard before returning success to the agent.

Each search result file contains a JSON object. The object stores a version for the search index file itself, metadata about the search, the decoded search key, and sorted index items.

For example:

```json
{
  "#": "01KWE2M2W3JY8TKY2P3V4X5A6B",
  "$": "SearchResult:v1",
  "search": "byReleasedGenre",
  "collection": "Movies",
  "key": ["released", "scifi"],
  "items": [
    {
      "sort": [1999, "The Matrix", "01KWDRHGK2GXE2B0VZ85GT546T"],
      "id": "01KWDRHGK2GXE2B0VZ85GT546T"
    }
  ]
}
```

The `#` field is the version of the search index file. The `$` field identifies the file shape. The `items` array is sorted according to the search definition.

Each item stores:

- `sort`, the values needed to maintain the declared sort order.
- `id`, the matching document ID.

## Sort Null And Missing Order

Sort direction applies to known non-null values.

`null` sort values are always sorted after known non-null values.

Missing sort values are always sorted after both known non-null values and `null` values.

This rule applies for both `asc` and `desc` sorts.

## Write-Time Maintenance

When a document is created, patched, or deleted, the database determines which precompiled searches are affected.

For each affected search, the database eventually updates the stored search files through the change-agent.

Write operations should follow this general pattern:

1. Load the current document state, if any.
2. Apply the requested create, patch, or delete to source-of-truth storage.
3. Replicate the document write according to [REPLICATION-FAILURE-HANDLING.md](REPLICATION-FAILURE-HANDLING.md).
4. Enqueue change-agent work for affected searches and caches.
5. Return success for the document write. Search maintenance is not part of the synchronous document commit.

The change-agent later:

1. Compares old and new values for fields used by precompiled searches.
2. Sends search result patches to the SOT machines for the affected search shards.
3. Those search SOTs apply and replicate the search-result files.

## Create

On `create`, the new document is added to every precompiled search that depends on fields present in the document.

If a search field is absent, the document is not added to that search key unless the search definition explicitly defines missing-field behavior.

For example, creating this document:

```json
{
  "!": "01KWDRHGK2GXE2B0VZ85GT546T",
  "title": "The Matrix",
  "status": "released",
  "genre": "scifi",
  "highRated": true,
  "releaseYear": 1999
}
```

would upsert the document ID into:

```text
$/db/Movies/.search/byReleasedGenre/72656c6561736564/7363696669/matches.json
```

If `highRated` were `false`, the document would not be stored in this search result at all.

## Patch

On `patch`, the database compares the old document and the patched document.

If a field used by a precompiled search changes, the document is removed from the old search key and added to the new search key.

If the field did not change, the stored search file does not need to be updated for that field.

## Delete

On `delete`, the document is removed from every precompiled search that currently references it.

## MVP Search Scope

The MVP should support a simple version of precompiled searches.

Initial search support can be limited to:

- one collection
- any number of source-of-truth fields
- schema-defined source-of-truth fields only
- AND'ed clauses only
- exact-match clauses
- same-field `in` clauses
- boolean `equals` clauses
- sorted document ID lists
- no joins
- no arbitrary predicates
- no full-text search

## Open Questions

- Should search result lists eventually store cached summaries as well as document IDs?
- Should pending search-result delivery use the same on-disk pending-work pattern as document writes?
