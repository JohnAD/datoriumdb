# Operator Reference

Definitive rules and limits for running a DatoriumDB server: schemas,
identifiers, value limits, data management, tokens, transport security, and
versioning.

## 1. Schema reference

Every collection schema **must have root `kind: object`**.

**Kinds — the complete list** (from OJSON):

| Kind | Notes |
|---|---|
| `object` | Nested fields via `children`. |
| `array` | Element kind via `items`. |
| `string` | Optional `min_length` / `max_length` (rune counts), `enum`, `format`. |
| `number` | Optional `min` / `max`, `integer: true` for integers. |
| `boolean` | |
| `null` | |

There is **no `any` kind**.

**Required / optional.** `{"name": "title", "kind": "string", "required": true}`
makes a field mandatory at `create`. If `required` is absent or false, the
field is optional.

**Defaults.** `default` supplies a value used when a schema upgrade migrates
existing documents. Defaults must match the field kind. Do **not** rely on
defaults being filled in at `create` time — a plain `create` does not invent
missing optional fields.

**Nullable.** `null` and missing are different. To store an explicit null,
declare the field `nullable: true` (or `kind: null`).

**Ref formats** (allowed on `kind: string` only):

- `format: DatoriumDirectRef` — stored string `@__{collection}__{id}`. The
  server does **not** resolve it on read; the client resolves it.
- `format: DatoriumCachedRef` — stored string `@@__{collection}__{id}`.
  Requires `custom.collections` (an explicit, non-empty list of target
  collections — cached refs cannot target all collections) and
  `custom.summary` (schema paths copied into the cache).

**Extra fields.** Documents may contain fields not declared in the schema;
they are stored (after schema fields in canonical order) but are **not
searchable** and are hidden from `read` unless `extraFields: true` is
requested.

**Searching by document ID.** Search clauses **cannot** target `/!`; every
clause path must resolve to a schema-defined field. `/!` is permitted only as
the final sort tie-breaker. Consequently there is **no ID-based match-all
scan** — and no command to list all documents in a collection. Enumerating
documents requires a precompiled search that matches what you need, with no
pagination and no result limit.

## 2. ID and naming conventions

**Document IDs.** Client-supplied; the server never generates them.

- Charset: ASCII letters, digits, underscore, period, dash
  (`[A-Za-z0-9_.-]`) — nothing else.
- Maximum 255 runes **and** 249 UTF-8 bytes (filesystem filename budget).
- Must not be empty, `.`, `..`, or `null`; must not begin with a period; no
  path separators.

**Period-prefix shard co-location.** Only the part of the ID before the first
period determines the shard (`shard = crc32(prefix) & 0xFF`; periods in the
first six positions are ignored for prefix detection). So IDs shaped
`{ulid}.suffix` with a shared prefix of length ≥ 6 — e.g.
`01KWJYMCTDNTF4MKNHD92FWPGW.settings` and `...PGW.mailbox` — are guaranteed
to co-locate on the same shard.

**Collection names.** UTF-8, no whitespace, first character must be a letter,
no two consecutive underscores. Plural, title-case names are recommended.

**Field names.** camelCase is preferred for Latin scripts; whitespace is
allowed in field names when it aids clarity.

## 3. Value types and limits

**Timestamps.** There is **no native date/time type** and no server-defined
timestamp format. Choose your own representation (`string` or `number`) and
document it per application. Note that number range search operators are not
yet implemented, so range queries over timestamps are not currently available
regardless of representation.

**Maximum document size.** The access-language command body is capped at
**1 MiB** on `/datoriumdb/v1/command` (the auth endpoint is capped at
16 KiB). Because `create` carries the whole document inline in the command,
**1 MiB is the effective maximum document content size**. There is no
separate per-document limit in storage.

**String limits.** No global string cap; use per-field `max_length` (runes)
in the schema. Search-specific: variable-value `equals` and array `contains`
clauses match only values up to **63 runes** — documents with longer values
are skipped from that clause's index.

**Search results.** No pagination, no result limit, no cursor.

## 4. Backup, restore, reset, and retention

**Backup/restore/reset are not product features.** There are no backup,
restore, snapshot, dump, or reset commands in `datoriumctl`, no server
endpoints for them, and no "delete collection" tool. The supported approach
is file-level: all data is plain-text JSON at
`{data-dir}/{Collection}/{id}.json` with configuration under `/db/.config`,
deliberately human-readable and git-trackable. Back up by copying or
git-mirroring the data directory (a stopped server gives a consistent copy).
`PROXY_READ_MEMBER` servers can hold full replicas and are the suggested
mechanism for a remote, continuously-updated copy.

**TTL / expiry does not exist.** Documents never expire on their own; the
only removal path is the explicit, version-checked `delete` command. Any
retention obligation (e.g. Famicus) must be implemented application-side:
store the retention timestamp as a schema field, expose a search over it, and
issue `delete` commands from a scheduled job.

## 5. Service-token lifecycle

Tokens are short-lived JWTs; the default lifetime for **both client and
machine tokens is 3600 seconds (1 hour)**, adjustable per establishment via
`datoriumctl auth set --client-token-lifetime-seconds` /
`--machine-token-lifetime-seconds`.

**Issuance is an operator CLI operation:**

```
datoriumctl auth token issue --kind client|machine \
  [--subject ...] [--server-name ...] [--lifetime-seconds ...]
```

It requires the private signing key (`DATORIUMDB_SIGNING_KEY_FILE`), which
only the establishment server and operator workstations hold. This is a
bootstrap/demo path, not a full identity system.

**Staying authenticated.** There are no long-lived tokens and no separate
refresh tokens — renewal means getting a new token before the old one
expires:

- **Servers (machine tokens):** call `POST /datoriumdb/v1/auth/machine-token`
  on a timer before expiry. While the current token is still valid, present
  it as `Authorization: Bearer`; otherwise authenticate with the bootstrap
  secret (`DATORIUMDB_MACHINE_BOOTSTRAP_SECRET`). DatoriumDB's own worker
  servers already do this automatically.
- **Client services:** there is no client token endpoint. Re-run
  `datoriumctl auth token issue --kind client` (typically from a scheduled
  job or sidecar with access to the signing key) and distribute the new token
  before expiry.

Client applications should handle the auth error codes `unauthenticated`,
`invalidToken`, `tokenExpired`, and `machineIdentityMismatch` by obtaining a
fresh token and retrying once.

## 6. TLS

The server speaks **plaintext HTTP only** — it has no TLS configuration,
certificate, or key options. Deployments must choose one of:

- **Loopback:** listen on `127.0.0.1` (the default) and rely on local access
  only; or
- **External termination:** front the server with a TLS-terminating reverse
  proxy or load balancer, and publish the `https://` address as the server's
  `baseURL` in the establishment config. DNS names are preferred for
  production because they work better with TLS certificates, service
  migration, and operational changes.

The token mechanism authenticates requests but does not encrypt the channel —
any non-loopback exposure without a TLS front end sends bearer tokens in the
clear and is not supported.

## 7. Versioning and stability

- **Server.** Release binaries are built from semver tags with a leading `v`
  (`v0.1.0`, pre-releases like `v0.2.0-rc.1`) and embed the version at link
  time. Tags are never moved.
- **API.** All endpoints are namespaced `/datoriumdb/v1`; search definitions
  carry their own `$: "SearchDefinition:v1"` marker. However, **no
  backward-compatibility or stability guarantee is currently documented for
  API v1** — the project is pre-1.0 (`v0.x` tags) and explicitly MVP-scoped.
  Treat the v1 surface as subject to change between minor releases; pin the
  server and `datoriumctl` to the same version and review release notes
  before upgrading.
- **Config and schemas.** The establishment config carries a monotonic
  `general.version` integer for change detection, and collection schemas are
  versioned with preserved history, so schema evolution is safe even while
  the API surface is not yet frozen.
