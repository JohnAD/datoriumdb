# Access Language

The DatoriumDB access language is a command language, not a query language. It is designed around explicit document access rather than open-ended searches or relational joins.

Every command has the same top-level shape:

```text
<word> <target> <parm> <detail>
```

The first three parts are always single words with no spaces:

- `<word>` is the command being performed.
- `<target>` is the collection, document, or other database object being addressed.
- `<parm>` is the primary parameter for the command.
- `<detail>` is a pseudo-JSON object containing the remaining command details.

Whitespace separates the first three parts. The `<detail>` object may contain spaces internally because it is parsed as structured data.

## Detail Object

The `<detail>` section is a pseudo-JSON object. It follows JSON-like structure, but quotes are optional when a field or value does not contain spaces.

For example, this:

```text
create Movies 01TESTMOVIES00000000000001 {$: Movies:0, title: "The Matrix", releaseYear: 1999}
```

is equivalent to:

```text
create Movies 01TESTMOVIES00000000000002 {"$": "Movies:0", "title": "The Matrix", "releaseYear": 1999}
```

## Commands

The initial access command set is:

- `create` creates a new document.
- `read` reads an existing document.
- `patch` changes an existing document through explicit patch operations.
- `delete` deletes an existing document.
- `search` reads a precompiled search result list.

There is no `update` command. Blind whole-document updates are bad practice because they can overwrite data unintentionally and bypass the more precise patch model.

Collection creation and schema upgrades are administrative operations, not access-language commands. They are performed through command-line tools that validate and update the establishment config files.

## HTTP Transport

Smart clients submit access-language commands over HTTP:

```text
POST /datoriumdb/v1/command
Content-Type: text/plain; charset=utf-8
Authorization: Bearer {token}
```

The request body is exactly one access-language command line:

```text
create Movies 01TESTMOVIES00000000000003 {$: Movies:0, title: "The Matrix", releaseYear: 1999}
```

The response is always HTTP `200` with `Content-Type: application/json` and a DatoriumDB result envelope.

The command layer remains separate from this transport. Later transports may carry the same command text and envelope shape.

Wrong-machine refusals use `ok: false` with a stable error code such as `wrongMachine` and include enough routing information for the client to retry:

```text
{
  ok: false,
  command: create,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  errors: [
    {
      code: wrongMachine,
      message: "This server is not assigned to the target shard.",
      shardSlot: "7A",
      correctServer: "serverB",
      baseURL: "https://s32.datoriumdb.com",
      configVersion: 12
    }
  ]
}
```

## API Response Shape

All DatoriumDB API endpoints return application-level success or failure in the response body.

Successful API calls return HTTP `200` with a JSON object containing `ok: true`.

Failed API calls also return HTTP `200`, but with `ok: false` and an `errors` array.

Authentication, authorization, validation, wrong-machine routing, stale-version checks, and other application-level failures should use the same `ok: false` envelope.

This keeps DatoriumDB command results consistent across access-language commands, establishment endpoints, and server-to-server endpoints. Transport-level failures can still happen when the server cannot be reached or the HTTP request itself cannot be processed.

## Create

The `create` command creates a new document in an existing collection.

```text
create {collection} {id} {content}
```

The four command parts are:

- `create` is the command word.
- `{collection}` is the target collection.
- `{id}` is the client-supplied document ID to create. The server never generates create IDs; `null` is rejected. Smart clients should mint a ULID (or other allowed ID) before calling create.
- `{content}` is the document content to create.

The collection and schema are validated before the document is created:

- If the collection does not exist, the command returns an error.
- The schema/version marker uses `{CollectionName}:{schemaVersion}`. New collections start at schema version `0`, so a new `Movies` document uses `$: Movies:0`.
- If `{content}` omits `$`, the server fills it with the collection's current schema marker.
- If `{content}` includes `$`, it must match the collection's current schema marker.
- Create may include a client-supplied `operationId` for correlation. It is echoed in the response when present (or generated when omitted), but create does not keep durable per-operation retry state. Retries with the same document ID receive `documentExists`.

The database owns some document metadata fields:

- The document ID is taken from `{id}` and must be a non-empty, filesystem-safe ID.
- If `{content}` includes an ID in the `!` field, it must match `{id}`.
- If `{content}` omits `!`, the server fills it from `{id}`.
- `{content}` cannot include a `#` version field because document versions are created by the database.

On a sharded deployment, after the SOT commits the document locally it makes one live delivery attempt to each assigned read/proxy member. Targets that acknowledge are done; targets that do not get a `.pendingWrites` entry, after which the SOT stops worrying about them. A response `note` may name unacknowledged targets. Separating SOT and READ so they scale independently means a successful create does not guarantee every READ already has the document — see [SHARDING.md](SHARDING.md).

### Create Returns

The `create` response is a result envelope.

On success, it returns command metadata and the newly created document version:

```text
{
  ok: true,
  command: create,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  $: Movies:0,
  #: 01KWD65D94Y5M8C2Z7HJ3N6VQK,
  operationId: 01KWHM7R7D3T50G0GH6XN4CRZT
}
```

On failure, it returns `ok: false` and an `errors` array:

```text
{
  ok: false,
  command: create,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  errors: [
    {
      code: schemaMismatch,
      path: /$,
      message: "Document schema marker does not match the collection schema.",
      expected: Movies:0,
      actual: Movies:1
    }
  ]
}
```

## Read

The `read` command reads an existing document from a collection.

```text
read {collection} {id} {read-scope}
```

The four command parts are:

- `read` is the command word.
- `{collection}` is the target collection.
- `{id}` is the document ID to read.
- `{read-scope}` is an object describing what should be returned.

Source-of-truth fields are always returned.

The read scope can request additional data:

- `extraFields: true` returns non-schemed fields.
- `cacheSummaries: true` returns local cached reference summaries.

For example:

```text
read Movies 01KWD65CFQPEZS7H1WJE4MK990 {extraFields: true, cacheSummaries: true}
```

### Read Returns

The read response is always a result envelope.

If `{read-scope}` is empty, the response includes the source-of-truth document fields in `sot`.

For example:

```text
{
  ok: true,
  command: read,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  sot: {
    !: 01KWD65CFQPEZS7H1WJE4MK990,
    $: Movies:0,
    #: 01KWD65D94Y5M8C2Z7HJ3N6VQK,
    title: "The Matrix",
    status: released,
    genre: scifi,
    highRated: true,
    releaseYear: 1999,
    director: @__People__01KWD65ABCDEF,
    directorSummary: @@__People__01KWD65ABCDEF
  }
}
```

If `{read-scope}` requests additional data, the response includes additional fields in the same envelope.

The read envelope can contain:

- `ok`, whether the read succeeded.
- `command`, the command that produced the response.
- `collection`, the collection that was read.
- `id`, the requested document ID.
- `sot`, the requested document's source-of-truth fields with reference strings left in place.
- `extraFields`, non-schemed fields from the requested document.
- `cacheSummaries`, cached summary objects grouped by collection and document ID.

`cacheSummaries` uses this shape:

```text
{
  {collection}: {
    {id}: {summary object}
  }
}
```

If multiple cached references target the same document, their requested fields are combined into one returned object.

If a cached referenced document cannot be resolved, `cacheSummaries` returns a full summary record for that collection and ID with a `null` revision (`#`). Requested SOT summary fields are omitted from that summary object because the referenced document does not currently exist. The source-of-truth document is not automatically patched to remove lost references.

Direct references are not resolved by `read`. They remain source-of-truth strings in `sot`. Smart clients are responsible for reading those referenced documents from the correct machines.

For example:

```text
{
  ok: true,
  command: read,
  collection: Conversations,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  sot: {
    !: 01KWD65CFQPEZS7H1WJE4MK990,
    $: Conversations:1,
    #: 01KWD65D94Y5M8C2Z7HJ3N6VQK,
    title: "Production Notes",
    director: @__People__01KWD65DIRECTOR,
    messages: [
      {text: "Wake up.", user: @@__People__01KWD65ABCDEF},
      {text: "Follow the white rabbit.", user: @@__People__01KWD65ABCDEF}
    ]
  },
  extraFields: {
    localNote: "Imported from an early draft."
  },
  cacheSummaries: {
    People: {
      01KWD65ABCDEF: {
        !: 01KWD65ABCDEF,
        $: People:1,
        name: "Joe",
        avatar: "joe.png"
      },
      01KWD65DELETED: {
        !: 01KWD65DELETED,
        $: People:1,
        #: null
      }
    }
  }
}
```

## Patch

The `patch` command changes an existing document through explicit patch operations.

```text
patch {collection} {id} {patch-details}
```

The four command parts are:

- `patch` is the command word.
- `{collection}` is the target collection.
- `{id}` is the document ID to patch.
- `{patch-details}` is an object containing the schema marker, document version, and patch operation details.

The patch details object MUST include the `$` schema/version marker and the `#` document version field at the top level. This prevents blind patches by requiring the caller to confirm both the current schema and the exact version of the document being changed.

Patch may include a client-supplied `operationId` for correlation. It is echoed in the response when present (or generated when omitted), but patch does not keep durable per-operation retry state. Retries against the pre-patch `#` version receive `versionMismatch`.

On a sharded deployment, after the SOT commits the patch locally it makes one live delivery attempt to each assigned read/proxy member. Targets that acknowledge are done; targets that do not get a `.pendingWrites` entry. A response `note` may name unacknowledged targets.

The initial patch operation format is based on RFC 6902 JSON Patch. To leave room for additional patch forms later, the RFC 6902 operation array is stored directly under an `RFC6902` field.

For example:

```text
patch Movies 01KWD65CFQPEZS7H1WJE4MK990 {$: Movies:0, #: 01KWD65D94Y5M8C2Z7HJ3N6VQK, operationId: 01KWHM7R7D3T50G0GH6XN4CRZT, RFC6902: [{op: replace, path: /status, value: released}]}
```

The `RFC6902` field contains a JSON Patch operation list.

Patch operations cannot target database-owned metadata fields:

- `!`, the document ID
- `$`, the schema/version marker
- `#`, the document version

The top-level `#` in `patch-details` confirms the old document version. The caller cannot declare the new `#` value, test `/#`, or patch `/#`; the server creates the next document version.

This restriction applies to user-submitted access-language patches. Internal SOT-authored replication work may carry the resulting `/#` change to read members so every replica stores the same document version.

### Patch Returns

The `patch` response is a result envelope.

On success, it returns command metadata and the document versions before and after the patch:

```text
{
  ok: true,
  command: patch,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  $: Movies:0,
  operationId: 01KWHM7R7D3T50G0GH6XN4CRZT,
  versions: {
    before: 01KWD65D94Y5M8C2Z7HJ3N6VQK,
    after: 01KWD65EJ5F61CE0GS9SX4V4FT
  }
}
```

On failure, it returns `ok: false` and an `errors` array:

```text
{
  ok: false,
  command: patch,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  errors: [
    {
      code: versionMismatch,
      path: /#,
      message: "Document version does not match.",
      expected: 01KWD65D94Y5M8C2Z7HJ3N6VQK,
      actual: 01KWD65EJ5F61CE0GS9SX4V4FT
    }
  ]
}
```

## Delete

The `delete` command deletes an existing document from a collection.

```text
delete {collection} {id} {confirming-details}
```

The four command parts are:

- `delete` is the command word.
- `{collection}` is the target collection.
- `{id}` is the document ID to delete.
- `{confirming-details}` is an object containing the document version being deleted.

The confirming details object MUST include the `#` document version field. This prevents blind deletes by requiring the caller to confirm the exact version of the document being removed.

Delete may include a client-supplied `operationId` for correlation. It is echoed in the response when present (or generated when omitted), but delete does not keep durable per-operation retry state. Retries after a successful delete receive `documentNotFound`.

On a sharded deployment, after the SOT soft-deletes the document locally it makes one live delivery attempt to each assigned read/proxy member. Targets that acknowledge are done; targets that do not get a `.pendingWrites` entry. A response `note` may name unacknowledged targets. READ members apply deletes idempotently (already-gone is success).

For example:

```text
delete Movies 01KWD65CFQPEZS7H1WJE4MK990 {#: 01KWD65D94Y5M8C2Z7HJ3N6VQK, operationId: 01KWHM7R7D3T50G0GH6XN4CRZT}
```

### Delete Returns

The `delete` response is a result envelope.

On success, it returns command metadata and the deleted document version:

```text
{
  ok: true,
  command: delete,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  #: 01KWD65D94Y5M8C2Z7HJ3N6VQK,
  operationId: 01KWHM7R7D3T50G0GH6XN4CRZT
}
```

On failure, it returns `ok: false` and an `errors` array:

```text
{
  ok: false,
  command: delete,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  errors: [
    {
      code: versionMismatch,
      path: /#,
      message: "Document version does not match.",
      expected: 01KWD65D94Y5M8C2Z7HJ3N6VQK,
      actual: 01KWD65EJ5F61CE0GS9SX4V4FT
    }
  ]
}
```

## Value Rules

Values inside the detail object are interpreted using these rules:

- If a value starts with a digit, it is a number.
- If a value is `true`, `false`, or `null`, it has the normal JSON meaning.
- If a value is `"true"`, `"false"`, or `"null"`, it is a string.
- If a value starts with `{`, it is an object.
- If a value starts with `[`, it is an array.
- Otherwise, the value is a string.

Strings may use quotes, but quotes are not required unless the string contains spaces or would otherwise be interpreted as a non-string value.

## Search

The `search` command reads a precompiled search result list.

```text
search {collection} {searchName} {search-parms}
```

The four command parts are:

- `search` is the command word.
- `{collection}` is the collection that owns the search definition.
- `{searchName}` is the precompiled search name.
- `{search-parms}` is an object of live variables required by the search definition.

For example:

```text
search Movies byReleasedGenre {status: released, useGenreFilter: true, genre: scifi}
```

The server computes the search shard from the encoded search parameter path, accepts or refuses the command based on shard ownership, and returns the matching document IDs from `matches.json`. Constant multi-value `in` clauses require their live selector variable so the request resolves one result bucket.

On success:

```text
{
  ok: true,
  command: search,
  collection: Movies,
  search: byReleasedGenre,
  matches: [
    01KWD65CFQPEZS7H1WJE4MK990,
    01KWD65EJ5F61CE0GS9SX4V4FT
  ]
}
```

Search definitions are created and deleted through `datoriumctl`, not through the access language. Search result maintenance is eventual through the change-agent.

## Examples

```text
create Movies 01TESTMOVIES00000000000004 {$: Movies:0, title: "The Matrix", releaseYear: 1999}
```

```text
read Movies 01KWD65CFQPEZS7H1WJE4MK990 {extraFields: true, cacheSummaries: true}
```

```text
patch Movies 01KWD65CFQPEZS7H1WJE4MK990 {$: Movies:0, #: 01KWD65D94Y5M8C2Z7HJ3N6VQK, RFC6902: [{op: replace, path: /status, value: released}]}
```

```text
delete Movies 01KWD65CFQPEZS7H1WJE4MK990 {#: 01KWD65D94Y5M8C2Z7HJ3N6VQK}
```

```text
search Movies byReleasedGenre {status: released, useGenreFilter: true, genre: scifi}
```

## Design Notes

The language should avoid SQL-like phrasing because joins are not supported and should not be implied. It should also avoid MongoDB-like open-ended query objects because arbitrary field searches are not supported.
