# Searching

DatoriumDB does not support open-ended searches across arbitrary fields. Searches are precompiled and maintained as stored files.

This document currently assumes the filesystem-backed `plain-text-json` storage method.

The core idea is that `create`, `patch`, and `delete` operations update both the source document and any stored search files affected by that document. Reads against a search should therefore be fast because the search result structure already exists.

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

The exact file format is still to be determined.

## Search Definitions (v1)

Precompiled searches are declared explicitly. The MVP search model supports AND'ed clauses only.

For example:

```text
compile Movies byReleasedGenre {v1: {clauses: [{field: /status, op: equals, value: $status}, {field: /genre, op: in, parm: $genres}, {field: /highRated, op: isTrue}], sort: [{field: /releaseYear, dir: desc}, {field: /title, dir: asc}, {field: "/!", dir: asc}]}}
```

This declares a `byReleasedGenre` search for the `Movies` collection.

The clauses are evaluated as an AND:

- `status` must equal the provided `$status`.
- `genre` must be in the provided `$genres`.
- `highRated` must be `true`.

The sort definition determines the stored order of document IDs inside matching search result files.

## Search File Paths

For filesystem-backed `plain-text-json` storage, precompiled search files live under the collection's `.search` directory.

For example, a document matching `status: released` and `genre: scifi` for the `byReleasedGenre` search is upserted to:

```text
$/db/Movies/.search/byReleasedGenre/released/scifi.json
```

Because string values are open-ended and filesystems have naming restrictions, path components derived from field values should be encoded as simple hex.

Using encoded path components, the same example becomes:

```text
$/db/Movies/.search/byReleasedGenre/72656c6561736564/7363696669.json
```

Each search result file contains a JSON object. The object stores a version for the search index file itself, metadata about the search, the decoded search key, and sorted index items.

For example:

```text
{
  #: 01KWE2M2W3JY8TKY2P3V4X5A6B,
  $: SearchResult:v1,
  search: byReleasedGenre,
  collection: Movies,
  key: [released, scifi],
  items: [
    {
      sort: [1999, "The Matrix", 01KWDRHGK2GXE2B0VZ85GT546T],
      id: 01KWDRHGK2GXE2B0VZ85GT546T
    }
  ]
}
```

The `#` field is the version of the search index file. The `$` field identifies the file shape. The `items` array is sorted according to the search definition.

Each item stores:

- `sort`, the values needed to maintain the declared sort order.
- `id`, the matching document ID.

## Write-Time Maintenance

When a document is created, patched, or deleted, the database determines which precompiled searches are affected.

For each affected search, the database updates the stored search files as part of the same logical write.

Write operations should follow this general pattern:

1. Load the current document state, if any.
2. Apply the requested create, patch, or delete.
3. Compare old and new values for fields used by precompiled searches.
4. Stage updated document files and search files.
5. Commit staged files with filesystem move operations.

## Create

On `create`, the new document is added to every precompiled search that depends on fields present in the document.

If a search field is absent, the document is not added to that search key unless the search definition explicitly defines missing-field behavior.

For example, creating this document:

```text
{
  !: 01KWDRHGK2GXE2B0VZ85GT546T,
  title: "The Matrix",
  status: released,
  genre: scifi,
  highRated: true,
  releaseYear: 1999
}
```

would upsert the document ID into:

```text
$/db/Movies/.search/byReleasedGenre/72656c6561736564/7363696669.json
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
- boolean clauses such as `isTrue`
- sorted document ID lists
- no joins
- no arbitrary predicates
- no full-text search

## Open Questions

- How should searches be declared in the access language?
- Should search definitions live in the collection schema, a separate metadata file, or both?
- Should hex path encoding use raw UTF-8 bytes, normalized Unicode, or another canonical form?
- Should missing fields be omitted or indexed under a special key?
- Should search result lists store document IDs only, or cached summaries as well?
- How should search maintenance failures be recovered after an interrupted write?
