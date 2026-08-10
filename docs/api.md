# Access API

Clients talk to DatoriumDB with a small command language over HTTP. There are
five commands: `create`, `read`, `patch`, `delete`, `search`. There is no
`update` command — use fine-grained patches instead of replacing whole
documents.

## HTTP transport

```text
POST /datoriumdb/v1/command
Content-Type: text/plain; charset=utf-8
Authorization: Bearer {token}
```

The request body is exactly one command line (maximum 1 MiB):

```text
create Movies 01TESTMOVIES00000000000001 {$: Movies:0, title: "The Matrix", releaseYear: 1999}
```

Every command has the same shape:

```text
<word> <target> <parm> <detail>
```

`<detail>` is a pseudo-JSON object: quotes are optional on field names and
values that contain no spaces and are not otherwise ambiguous. Unquoted
`true`, `false`, `null`, and values starting with a digit keep their JSON
meaning; quote them (e.g. `"true"`) to force a string.

## Response envelope

Every API endpoint returns HTTP `200` with a JSON envelope. Success is
signalled in the body, not the status code:

- Success: `{ "ok": true, ... }` with command-specific fields.
- Failure: `{ "ok": false, "errors": [ { "code": ..., "message": ... } ] }`.

Authentication, validation, routing, and version failures all use the same
`ok: false` envelope.

## Commands

### create

```text
create {collection} {id} {content}
```

The client always supplies the ID (mint a ULID); the server never generates
one. If `{content}` omits the `$` schema marker, the server fills in the
collection's current schema version. On success the response includes the new
document version `#`.

### read

```text
read {collection} {id} {extraFields: true, cacheSummaries: true}
```

Reads return the document's schema-defined ("source-of-truth") fields under
`sot`. An empty read scope returns only `sot`. Optional scope flags:

- `extraFields: true` — also return non-schema fields under `extraFields`.
- `cacheSummaries: true` — also return cached summaries of referenced
  documents under `cacheSummaries`.

Direct references (strings like `@__People__01...`) are **not** resolved by
the server; the client reads the referenced document itself.

### patch

```text
patch {collection} {id} {$: Movies:0, #: <current-version>, RFC6902: [ ...ops... ]}
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

```text
delete {collection} {id} {#: <current-version>}
```

Also version-checked: a stale `#` is refused with `versionMismatch`.

### search

See [Searching](search.md).

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
| `notReady` | Server has not loaded establishment config yet | Wait and retry |
| `contentTypeRequired` | Wrong Content-Type | Use `text/plain; charset=utf-8` |

`operationId`: create, patch, and delete accept a client-supplied
`operationId`, echoed in the response for correlation. The server keeps **no**
durable per-operation retry state — retries are detected naturally
(`documentExists`, `versionMismatch`, `documentNotFound`).
