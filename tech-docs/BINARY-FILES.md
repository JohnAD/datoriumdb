# Binary Attachment Storage

Document-scoped large binary attachments live beside their parent document.
They are non-searchable source-of-truth data: manifests are never fed to schema
or search evaluation. Parent document deletion cascades to all attached files.

This document is the authoritative API, storage, replication, and consistency
specification for binary attachments. See also [Access API](../docs/api.md),
[Consistency Model](../docs/consistency.md), [Filesystem Storage](FILESTYSTEM-STORAGE.md),
[Sharding](SHARDING.md), and [Server-to-Server API](SERVER-TO-SERVER-API.md).

## Public HTTP API

All public file operations use the unified command endpoint
`POST /datoriumdb/v1/command`. There are **no** public
`/datoriumdb/v1/files/...` routes.

| Command | Content-Type | Role |
|---------|--------------|------|
| `fileCreate` | `multipart/form-data` | create only (fails with `fileExists` if present) |
| `fileUpdate` | `multipart/form-data` | update; requires `detail.version` = current file version |
| `fileRead` | `application/json` | stream bytes on success (READ member) |
| `fileList` | `application/json` | list current manifest entries only |
| `fileDelete` | `application/json` | version-checked delete |

There is **no rename** command.

### Request shape

Every command uses the same four root fields. Example create:

```http
POST /datoriumdb/v1/command
Authorization: Bearer {token}
Content-Type: multipart/form-data; boundary=...

--...
Content-Disposition: form-data; name="command"

{"command":"fileCreate","target":"Movies","parameter":"01DOC...","detail":{"filename":"photo.png","contentType":"image/png"}}
--...
Content-Disposition: form-data; name="content"; filename="blob"
Content-Type: image/png

<raw bytes>
--...--
```

The `command` part must precede `content`. The JSON metadata part is capped at
1 MiB; the streamed body is limited by `general.maxFileBytes` (default 1 GiB).

List / delete / read examples:

```json
{"command":"fileList","target":"Movies","parameter":"01DOC...","detail":{}}
{"command":"fileDelete","target":"Movies","parameter":"01DOC...","detail":{"filename":"photo.png","version":"01VER..."}}
{"command":"fileRead","target":"Movies","parameter":"01DOC...","detail":{"filename":"photo.png"}}
```

`detail` fields that replace former REST headers:

- `filename` (required for create/update/read/delete)
- `contentType` (optional on upload; defaults from the content part or `application/octet-stream`)
- `version` (required on `fileUpdate` / `fileDelete`; was `If-Match`)
- `operationId` (optional correlation id)

### Routing

Shard by the **parent document ID**:

- `fileCreate` / `fileUpdate` / `fileDelete` → `SHARD_SOT_MEMBER`
- `fileRead` / `fileList` → an assigned `SHARD_READ_MEMBER`

Wrong-machine refusals use the standard `wrongMachine` envelope with
diagnostic-only `configVersion`. Clients must re-establish and recompute the
next hop; bounce `correctServer` / `baseURL` hints are not authoritative.

### Mutation envelopes

Successful create/update/delete return a normal JSON envelope with:

- `command`: `fileCreate`, `fileUpdate`, or `fileDelete`
- `collection`, `id`, `filename`, `version`, `byteSize`, `sha256`, `contentType`, `operationId`
- `distributionComplete`: `true` only when every required binary replication
  target acknowledges; `false` remains `ok: true` and may include a
  `note` naming unacknowledged targets

### Download

`fileRead` is a JSON POST. Successful downloads stream raw bytes (not a JSON
body) with headers:

- `Content-Type`, `Content-Length`
- `X-DatoriumDB-SHA256`
- `X-DatoriumDB-File-Version`
- `X-DatoriumDB-Operation-Id`
- `ETag` (quoted file version)

Failures always use the standard JSON error envelope (still typically HTTP 200).

### Stable error codes

`documentNotFound`, `fileNotFound`, `fileExists`, `fileVersionMismatch`,
`invalidFileName`, `fileTooLarge`, `fileStale`, `contentTypeRequired`,
`invalidRequest`, plus existing routing/auth codes.

### Filename rules

One UTF-8 basename preserved exactly. Reject empty names, path separators,
NUL/control characters, `.` / `..`, leading `.`, and names over the filesystem
byte limit (255).

### Upload limit

`general.maxFileBytes` in establishment config (default **1 GiB**). The
1 MiB JSON/metadata and 8 MiB JSON response caps are unchanged; downloads
bypass the JSON response cap by streaming.

## Storage layout

```text
{dataDir}/{collection}/lfs/{docId}/{filename}     # bytes
{dataDir}/{collection}/{docId}__files.jsonl       # manifest
{dataDir}/{collection}/.fileOps/{docId}/          # local crash journal
{dataDir}/{collection}/.pendingFileWrites/        # SOT catch-up backlog
```

### Manifest

One current object per file, fixed field order, lines sorted by `name`, each
terminated with `\n`, rewritten atomically on create/update/delete:

```json
{"name":"photo.png","contentType":"image/png","byteSize":123,"sha256":"...","version":"...","operationId":"..."}
```

### Local commit

Uploads stage outside the document lock (stream to a temp under `.fileOps`),
then under the document lock: re-check parent existence and file version,
journal, atomic rename into `lfs/`, rewrite manifest, clear journal. Startup
runs `RecoverAllFileOps` to finish or roll back interrupted commits.

### Parent delete cascade

Soft-deleting a parent document removes `lfs/{docId}/` and
`{docId}__files.jsonl` (and `.fileOps/{docId}/`). Replicated document deletes
perform the same idempotent cleanup on READ/proxy members.

## Binary replication

Attachments use a separate source-of-truth lane (not `DocumentWorkItem`,
search, or cache agents). Machine `/sys` wire formats are unchanged:

- Happy-path: bounded parallel one-shot push to every assigned READ/proxy
  member via `POST /datoriumdb/v1/sys/apply-file-write` (raw body + metadata
  headers; SHA-256 verified before atomic apply). Deletes are metadata-only.
- Before each push, persist a coalescing pending record under
  `.pendingFileWrites` keyed by target + collection + document + filename.
  Delete the pending record only after acknowledgement. A later update
  replaces older pending state; delete supersedes pending content writes.
- Catch-up: machine-authenticated list / fetch metadata / fetch content /
  complete endpoints for `.pendingFileWrites`. Read members refuse known-stale
  file reads/lists with `fileStale`.

Parent document deletion combines local attachment cascade with existing
document/search/cache `distributionComplete` accounting; file mutations set
`distributionComplete` only from the binary replication outcome.

## Git LFS archival (administrator-operated)

DatoriumDB is filesystem- and Git-friendly but **never** invokes Git or Git LFS,
initializes repositories, creates commits, manages remotes, writes
`.gitattributes`, or depends on either executable being installed.

Administrators may optionally initialize a VCS repository over a data tree for
archival or proxy purposes. Suggested attributes for attachment bytes:

```gitattributes
**/lfs/** filter=lfs diff=lfs merge=lfs -text
```

Track manifests (`*__files.jsonl`) normally. Exclude internal dot directories
(`.fileOps`, `.pendingFileWrites`, `.pendingWrites`, `.changeQueue`, …) from
commits as appropriate for your operational policy.
