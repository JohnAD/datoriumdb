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
create Movies null {$: Movies:1, Title: "The Matrix", Year: 1999}
```

is equivalent to:

```text
create Movies null {"$": "Movies:1", "Title": "The Matrix", "Year": 1999}
```

## Commands

The initial command set is:

- `collection` asserts or advances a collection schema.
- `create` creates a new document.
- `read` reads an existing document.
- `patch` changes an existing document through explicit patch operations.
- `delete` deletes an existing document.

There is no `update` command. Blind whole-document updates are bad practice because they can overwrite data unintentionally and bypass the more precise patch model.

## Collection

The `collection` command asserts the schema for a collection. Collections and schemas have the same name.

```text
collection {collection} {ver} {schema}
```

The four command parts are:

- `collection` is the command word.
- `{collection}` is the collection and schema name.
- `{ver}` is the integer schema version the caller believes the database is currently using.
- `{schema}` is the schema to assert or apply.

Schemas use the OJSON schema format defined at <https://github.com/JohnAD/ojson>. An OJSON schema is a JSON document. For DatoriumDB collections, the root schema MUST have `kind: object` and an ordered `children` array describing the document fields. OJSON may support other root kinds, but DatoriumDB does not allow them for collection schemas.

The command is version-aware:

- If the collection does not exist and `{ver}` is `0`, the collection is created with `{schema}`.
- If the collection does not exist and `{ver}` is not `0`, the command returns an error.
- If the collection exists and `{ver}` matches the current version, nothing happens.
- If the collection exists and `{ver}` is older than the current version, the command returns a useful error so the caller can catch up with the current database state.
- If the collection exists and `{ver}` is exactly one greater than the current version, `{schema}` is applied as the new schema.
- If the collection exists, `{ver}` is exactly one greater than the current version, and `{schema}` is the empty object `{}`, the command returns a distinct error.

This command assumes the caller is up-to-date with the state of the database. Schema changes must advance one version at a time.

### Collection Returns

The `collection` response is a result envelope.

On success, it returns command metadata and the resulting collection schema version:

```text
{
  ok: true,
  op: collection,
  collection: Movies,
  schema: Movies,
  version: 1,
  action: advanced
}
```

The `action` field can describe what happened, such as `created`, `matched`, or `advanced`.

On failure, it returns `ok: false` and an `errors` array:

```text
{
  ok: false,
  op: collection,
  collection: Movies,
  schema: Movies,
  version: 0,
  errors: [
    {
      code: staleSchemaVersion,
      path: /version,
      message: "Collection schema version is older than the current database version.",
      expected: 2,
      actual: 0
    }
  ]
}
```

## Create

The `create` command creates a new document in an existing collection.

```text
create {collection} {id} {content}
```

The four command parts are:

- `create` is the command word.
- `{collection}` is the target collection.
- `{id}` is the document ID to create. If `{id}` is `null`, the database automatically creates a ULID.
- `{content}` is the document content to create.

The collection and schema are validated before the document is created:

- If the collection does not exist, the command returns an error.
- If `{content}` does not include a `$` schema/version marker, the command returns an error.
- If the `$` schema/version marker does not match the collection's current schema, the command returns an error.
- If the schema version in `$` is wrong, the command returns an error.

The database owns some document metadata fields:

- The document ID is taken from `{id}`.
- If `{id}` is `null`, the database automatically creates a ULID.
- If `{content}` includes an ID in the `!` field, it must match `{id}` unless `{id}` is `null`.
- `{content}` MUST include a schema/version marker in the `$` field.
- `{content}` cannot include a `#` version field because document versions are created by the database.

### Create Returns

The `create` response is a result envelope.

On success, it returns command metadata and the newly created document version:

```text
{
  ok: true,
  op: create,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  $: Movies:1,
  #: 01KWD65D94Y5M8C2Z7HJ3N6VQK
}
```

On failure, it returns `ok: false` and an `errors` array:

```text
{
  ok: false,
  op: create,
  collection: Movies,
  id: null,
  errors: [
    {
      code: schemaMismatch,
      path: /$,
      message: "Document schema marker does not match the collection schema.",
      expected: Movies:1,
      actual: Movies:0
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
- `live: [...]` requests live indirect summary pulls from the listed collections. This is expected to be expensive.

For example:

```text
read Movies 01KWD65CFQPEZS7H1WJE4MK990 {extraFields: true, cacheSummaries: true, live: [People, Studios]}
```

### Read Returns

The read response is always a result envelope. This keeps the success or failure state inside the returned data instead of relying on transport-specific metadata such as HTTP status codes.

If `{read-scope}` is empty, the response includes the source-of-truth document fields in `sot`.

For example:

```text
{
  ok: true,
  op: read,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  sot: {
    !: 01KWD65CFQPEZS7H1WJE4MK990,
    $: Movies:1,
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
- `op`, the command that produced the response.
- `collection`, the collection that was read.
- `id`, the requested document ID.
- `sot`, the requested document's source-of-truth fields with reference strings left in place.
- `extraFields`, non-schemed fields from the requested document.
- `cacheSummaries`, cached summary objects grouped by collection and document ID.
- `live`, live summary objects grouped by collection and document ID.

Both `cacheSummaries` and `live` use this shape:

```text
{
  {collection}: {
    {id}: {summary object}
  }
}
```

If multiple references target the same document, their requested fields are combined into one returned object. This applies to both cached summaries and live summaries.

For example:

```text
{
  ok: true,
  op: read,
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
      }
    }
  },
  live: {
    People: {
      01KWD65DIRECTOR: {
        !: 01KWD65DIRECTOR,
        $: People:1,
        name: "Lana Wachowski",
        currentStatus: available
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

The initial patch operation format is based on RFC 6902 JSON Patch. To leave room for additional patch forms later, the RFC 6902 operation array is stored directly under an `RFC6902` field.

For example:

```text
patch Movies 01KWD65CFQPEZS7H1WJE4MK990 {$: Movies:1, #: 01KWD65D94Y5M8C2Z7HJ3N6VQK, RFC6902: [{op: replace, path: /Title, value: "The Matrix"}]}
```

### Patch Returns

The `patch` response is a result envelope.

On success, it returns command metadata and the document versions before and after the patch:

```text
{
  ok: true,
  op: patch,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  $: Movies:1,
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
  op: patch,
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

For example:

```text
delete Movies 01KWD65CFQPEZS7H1WJE4MK990 {#: 01KWD65D94Y5M8C2Z7HJ3N6VQK}
```

### Delete Returns

The `delete` response is a result envelope.

On success, it returns command metadata and the deleted document version:

```text
{
  ok: true,
  op: delete,
  collection: Movies,
  id: 01KWD65CFQPEZS7H1WJE4MK990,
  #: 01KWD65D94Y5M8C2Z7HJ3N6VQK
}
```

On failure, it returns `ok: false` and an `errors` array:

```text
{
  ok: false,
  op: delete,
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

## Examples

```text
collection Movies 0 {kind: object, children: [{name: Title, kind: string}, {name: Year, kind: number}]}
```

```text
create Movies null {$: Movies:1, Title: "The Matrix", Year: 1999}
```

```text
read Movies 01KWD65CFQPEZS7H1WJE4MK990 {extraFields: true, cacheSummaries: true, live: [People, Studios]}
```

```text
patch Movies 01KWD65CFQPEZS7H1WJE4MK990 {$: Movies:1, #: 01KWD65D94Y5M8C2Z7HJ3N6VQK, RFC6902: [{op: replace, path: /Title, value: "The Matrix"}]}
```

```text
delete Movies 01KWD65CFQPEZS7H1WJE4MK990 {#: 01KWD65D94Y5M8C2Z7HJ3N6VQK}
```

## Design Notes

The language should avoid SQL-like phrasing because joins are not supported and should not be implied. It should also avoid MongoDB-like open-ended query objects because arbitrary field searches are not supported.
