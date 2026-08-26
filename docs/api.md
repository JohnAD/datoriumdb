# Access API

Clients talk to DatoriumDB with a small command set over one fixed HTTP URL.
Document and search commands are `create`, `read`, `patch`, `delete`, and
`search`. Binary attachments use `fileCreate`, `fileUpdate`, `fileRead`,
`fileList`, and `fileDelete` on the same endpoint. There is no whole-document
`update` — use fine-grained patches instead.

## HTTP transport

```text
POST /datoriumdb/v1/command
Content-Type: application/json
Authorization: Bearer {token}
```

The request body is one four-field JSON object (metadata/document payload
capped at **1 MiB**):

```json
{
  "command": "create",
  "target": "Movies",
  "parameter": "01TESTMOVIES00000000000001",
  "detail": {
    "$": "Movies:0",
    "title": "The Matrix",
    "releaseYear": 1999
  }
}
```

| Field | Meaning |
| --- | --- |
| `command` | Command name (`create`, `read`, `patch`, `delete`, `search`, a `file*` command, or an admin ensure command) |
| `target` | Collection (or other addressed object) |
| `parameter` | Primary parameter (document ID, search name, …); may be empty for `collectionEnsure` |
| `detail` | Always a JSON **object** (use `{}` when empty). Unknown root fields are rejected. |

`fileCreate` / `fileUpdate` use `multipart/form-data` instead: a small
`command` JSON part (same four fields) plus a streamed `content` part. See
[Binary Attachment Storage](../tech-docs/BINARY-FILES.md).

**Breaking change:** `text/plain` command lines and public
`/datoriumdb/v1/files/...` routes are removed. There is no compatibility mode.

## Response envelope

Every API endpoint returns HTTP `200` with a JSON envelope. Success is
signalled in the body, not the status code:

- Success: `{ "ok": true, ... }` with command-specific fields.
- Failure: `{ "ok": false, "errors": [ { "code": ..., "message": ... } ] }`.

Authentication, validation, routing, and version failures all use the same
`ok: false` envelope. The exception is a successful **file download**: raw
bytes stream with metadata headers (failures remain JSON envelopes).

## Commands

### create

```json
{
  "command": "create",
  "target": "{collection}",
  "parameter": "{id}",
  "detail": { "$": "Movies:0", "title": "..." }
}
```

The client always supplies the ID (mint a ULID); the server never generates
one. If `detail` omits the `$` schema marker, the server fills in the
collection's current schema version. On success the response includes the new
document version `#` and informational `distributionComplete` (true when
document, search, and cache distribution all finished in the one-shot
window; false is not an error).

### read

```json
{
  "command": "read",
  "target": "{collection}",
  "parameter": "{id}",
  "detail": { "extraFields": true, "cacheSummaries": true }
}
```

Reads return the document's schema-defined ("source-of-truth") fields under
`sot`. An empty `detail` (`{}`) returns only `sot`. Optional scope flags:

- `extraFields: true` — also return non-schema fields under `extraFields`.
- `cacheSummaries: true` — also return cached summaries of referenced
  documents under `cacheSummaries`.

Direct references (strings like `@__People__01...`) are **not** resolved by
the server; the client reads the referenced document itself.

### patch

```json
{
  "command": "patch",
  "target": "{collection}",
  "parameter": "{id}",
  "detail": {
    "$": "Movies:0",
    "#": "<current-version>",
    "RFC6902": [ { "op": "add", "path": "/status", "value": "released" } ]
  }
}
```

Changes a document with [RFC 6902](https://datatracker.ietf.org/doc/html/rfc6902)
JSON Patch operations. The detail object **must** include:

- `$` — the current schema marker, and
- `#` — the document version you based the patch on.

If the document has changed since, the patch is refused with
`versionMismatch`; re-read and retry (see
[Atomic Updates and Batching](atomic-updates.md)).

Patch operations cannot touch the metadata fields `!` (ID), `$` (schema), or
`#` (version). The server assigns the new version and returns it under
`versions.after`.

### delete

```json
{
  "command": "delete",
  "target": "{collection}",
  "parameter": "{id}",
  "detail": { "#": "<current-version>" }
}
```

Also version-checked: a stale `#` is refused with `versionMismatch`.

### search

See [Searching](search.md). Search uses the same four-field JSON shape with
`command: "search"`, `target` = collection, `parameter` = search name, and
`detail` = live variables.

## Binary attachments

File operations share `POST /datoriumdb/v1/command`:

| Command | Transport | `detail` highlights |
| --- | --- | --- |
| `fileCreate` | multipart (`command` + `content`) | `filename`, optional `contentType`, `operationId` |
| `fileUpdate` | multipart | same + required `version` |
| `fileRead` | JSON POST; success = raw stream | `filename` |
| `fileList` | JSON | `{}` |
| `fileDelete` | JSON | `filename`, `version`, optional `operationId` |

Writes route to the parent document's SOT member; reads/list route to a read
member. Full semantics: [Binary Attachment Storage](../tech-docs/BINARY-FILES.md).

## Admin commands (establishment only)

Declarative catalog mutations use the same `/command` URL but require an
**admin** JWT (`datoriumdb.kind=admin`) and must be sent to the
**establishment server**. The response returns after config files are written
and the establishment engine reloads; document `$` migration continues in the
background.

| Command | Purpose |
| --- | --- |
| `collectionEnsure` | Create collection at schema v0, or apply one upgrade step |
| `searchEnsure` | Create a search definition (immutable; identical = no-op) |
| `searchDelete` | Delete a search definition (absent = no-op) |

```json
{
  "command": "collectionEnsure",
  "target": "Movies",
  "parameter": "",
  "detail": {
    "schema": { "kind": "object", "children": [ /* ... */ ] }
  }
}
```

One-step upgrade (client loops for multi-version jumps):

```json
{
  "command": "collectionEnsure",
  "target": "Movies",
  "parameter": "",
  "detail": {
    "upgrade": {
      "from": 0,
      "new_ver_id": "01...",
      "updates": [ /* schemapatch ops */ ]
    }
  }
}
```

If the desired state already matches, the response is `ok: true` with
`changed: false`. `datoriumctl collection create|upgrade` and
`search create|delete` call these HTTP commands (they no longer write schemas
directly under `--config-dir`).

## Common error codes

| Code | Meaning | What to do |
| --- | --- | --- |
| `versionMismatch` | `#` in patch/delete is stale | Re-read the document, retry with the new version |
| `documentExists` | Create ID already used | Pick a new ID (or treat as already-created) |
| `documentNotFound` | No such document | Check ID; delete retries see this |
| `schemaMismatch` | `$` does not match the collection's current schema | Refresh config; migrate or re-target the document |
| `wrongMachine` | This server does not own the target shard | Re-fetch establishment config, recompute routing, retry (bounded) |
| `documentStale` | Read member knows this document is pending an update | Retry shortly, or read from the SOT member |
| `readMemberStale` | Read member has lost contact with its SOT | Retry later or route elsewhere |
| `fileNotFound` | No such attachment | Check filename / create first |
| `fileExists` | `fileCreate` but file already exists | Use `fileUpdate` with current `detail.version` |
| `fileVersionMismatch` | `detail.version` does not match current file version | List or re-download metadata, retry |
| `invalidFileName` | Unsafe or empty filename | Use a single basename without path separators or leading `.` |
| `fileTooLarge` | Upload exceeds `maxFileBytes` | Reduce size or raise establishment `maxFileBytes` |
| `fileStale` | Read member knows this file is pending catch-up | Retry shortly |
| `adminRequired` | Admin command without an admin token | Issue `--kind admin` and retry |
| `establishmentRequired` | Admin command not sent to the establishment server | Use the establishment base URL |
| `schemaDrift` | `collectionEnsure` schema does not match and no upgrade was supplied | Supply a valid one-step `upgrade` or matching schema |
| `invalidRequest` | Malformed JSON, unknown root field, non-object `detail` | Fix the request body |
| `notReady` | Server has not loaded establishment config yet | Wait and retry |
| `contentTypeRequired` | Wrong Content-Type | Use `application/json`, or multipart for file create/update |

`operationId`: create, patch, delete, and file mutations accept a
client-supplied `operationId`, echoed in the response for correlation. The
server keeps **no** durable per-operation retry state — retries are detected
naturally (`documentExists`, `versionMismatch`, `documentNotFound`,
`fileExists`, `fileVersionMismatch`).
