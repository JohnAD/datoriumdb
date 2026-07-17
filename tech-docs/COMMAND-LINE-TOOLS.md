# Command-Line Tools

This document describes the administrative command-line tools for DatoriumDB.

These tools are separate from the access language. The access language is for document operations such as `create`, `read`, `patch`, and `delete`. Administrative tasks such as creating collections, changing schemas, and updating establishment config files are performed by command-line tools.

## Purpose

Command-line tools should make config and schema changes explicit, validated, and Git-friendly.

They should:

- validate requested changes before writing files
- write plain JSON config files under `/db/.config`
- preserve readable, stable formatting for Git diffs
- use safe file replacement so readers do not observe partially written config
- preserve versioned schema history
- increment `general.version` when the served establishment config changes

The establishment server serves the resulting files; it does not provide a general write API for editing them. Collection creation and schema upgrades are also command-line tool responsibilities. They are not access-language commands and are not general server-to-server API operations.

## Tool Name And Invocation

The provisional administrative binary is `datoriumctl`.

`datoriumctl` and `datoriumdb` are separate binaries. They may share a Go module and packages, but they must not be one binary with different entry modes.

The server starts with a server name and establishment base URL. The CLI updates the establishment config directory that the establishment server later serves.

Basic form:

```text
datoriumctl <command> [subcommand] [args...] [options...]
```

Examples:

```text
datoriumctl config validate
datoriumctl collection create Movies ./Movies.schema.json
datoriumctl collection upgrade Movies ./Movies.upgrade.0-1.json
datoriumctl server set serverB --base-url https://s32.datoriumdb.com
datoriumctl shard-map set ./shard-map.json
```

## Global Options

Unless a command says otherwise, these options are available everywhere:

- `--config-dir <path>`: directory containing establishment config files. Defaults to `/db/.config`.
- `--data-dir <path>`: local collection storage root. Defaults to the parent of `--config-dir` when that directory is named `.config`, otherwise `/db`.
- `--dry-run`: validate and report the intended file changes without writing anything.
- `--json`: print machine-readable JSON envelopes to stdout.
- `--quiet`: suppress non-error human output.
- `--yes`: skip interactive confirmation prompts for destructive or irreversible operations.

The CLI always operates on the config directory named by `--config-dir`. It does not talk to live DatoriumDB servers for MVP admin writes.

## Output And Exit Codes

Human-readable output is the default.

With `--json`, success and failure use the same application envelope style used by DatoriumDB APIs:

```text
{
  ok: true,
  command: collection.create,
  collection: Movies,
  schemaVersion: 0,
  generalVersion: 12,
  filesWritten: [
    Movies.schema.json,
    Movies.schema.0.json,
    __general.json
  ],
  directoriesCreated: [
    /db/Movies
  ]
}
```

```text
{
  ok: false,
  command: collection.upgrade,
  collection: Movies,
  errors: [
    {
      code: staleSchemaVersion,
      path: /from,
      message: "Collection schema version is older than the current database version.",
      expected: 1,
      actual: 0
    }
  ]
}
```

Exit codes:

- `0`: success
- `1`: validation or user-input failure
- `2`: filesystem, lock, or unexpected runtime failure
- `3`: dry-run completed successfully with no writes

## Config Files Touched

The CLI manages these files under `--config-dir`:

```text
/db/.config/__general.json
/db/.config/__servers.json
/db/.config/__shard-map.json
/db/.config/__auth.json
/db/.config/{CollectionName}.schema.json
/db/.config/{CollectionName}.schema.{ver}.json
/db/.config/{CollectionName}.search.{SearchName}.json
/db/.config/{CollectionName}.search.{SearchName}.{ver}.json
```

`__auth.json` holds public authentication trust material. Private signing keys and bootstrap secrets are not written there. See [AUTHENTICATION.md](AUTHENTICATION.md).

Search definitions are managed under `{CollectionName}.search.{SearchName}.json`. Search result trees under `{data-dir}/{CollectionName}/.search/` are maintained by servers and agents, not by the CLI.

All written `.json` files are strict JSON with stable, Git-friendly formatting:

- 2-space indentation
- trailing newline
- object field order preserved where the source object already has an order
- no insignificant whitespace churn beyond the formatter's fixed style

## Locking And Safe Writes

Administrative writes must not race with each other.

Before any mutating command writes files:

1. Acquire an exclusive lock file at `{config-dir}/.datoriumctl.lock`.
2. Load the current config directory into memory.
3. Apply the requested change in memory.
4. Validate the complete resulting config set.
5. If `--dry-run`, report the planned writes and exit without changing files.
6. Write changed files using temporary files plus atomic rename.
7. Update `__general.json` last, including the new `general.version`.
8. Release the lock.

If the lock is already held, the command fails with a clear error rather than waiting indefinitely.

Safe replacement means:

1. Write the new contents to a temporary file in the same directory.
2. Flush the temporary file.
3. Rename the temporary file onto the final filename.
4. Readers either see the old file or the new file, never a partial write.

## Version Bumps

`general.version` is a monotonically increasing integer.

Any successful mutating command that changes the establishment-served config increments `general.version` by exactly `1`.

`--dry-run` does not change `general.version`.

Commands that only validate, list, or inspect config do not change `general.version`.

## Validation Rules

### Complete Config Validation

`datoriumctl config validate` and every mutating write path validate the complete candidate config, not only the one file being edited.

Validation includes:

- every config file is valid strict JSON
- `__general.json` contains required fields and references a known server as `establishmentServer`
- `__servers.json` contains a map of server names to objects with `baseURL`
- every `baseURL` is an absolute URL with scheme and host
- `__auth.json` contains public auth trust material with issuer, audience, and at least one active public signing key
- `__auth.json` does not contain private signing keys or bootstrap secrets
- `__shard-map.json` contains `shardMap.default`
- `shardMap.default` covers all 256 shard slots `00` through `FF`
- shard slot ranges do not overlap
- every `SHARD_SOT_MEMBER`, `SHARD_READ_MEMBER`, and `PROXY_READ_MEMBER` entry names a known server
- every collection current schema file is valid OJSON and has root `kind: object`
- every collection current schema has a matching versioned history file for its current version
- collection names follow [CONVENTIONS.md](CONVENTIONS.md)

### Collection Schema Validation

When creating or upgrading a collection schema, the CLI also checks DatoriumDB-specific rules:

- root schema must be `kind: object`
- no `any` kind
- `DatoriumDirectRef` and `DatoriumCachedRef` formats are accepted only on `kind: string`
- `DatoriumCachedRef` fields must include `custom.collections` as a non-empty array
- `DatoriumCachedRef` fields must include `custom.summary` as an array of strings
- each cached-reference summary path must be valid in at least one allowed target collection already present in config, when those target collections already exist
- unknown OJSON schema fields are rejected by the OJSON compiler

### Schema Upgrade Validation

Schema upgrades use the update model in [UPDATE-SCHEMA.md](UPDATE-SCHEMA.md).

Before writing anything, the CLI validates that:

- `from` matches the collection's current schema version
- schema versions always advance by exactly `1`, so the effective target version is `from + 1`
- if `to` is present, it must equal `from + 1`; if omitted, the CLI infers `to` as `from + 1`
- `new_ver_id` is present and is a valid ULID-like ID string
- `updates` is a non-empty array
- every update operation is one of the supported ops
- no update targets database-owned metadata fields `!`, `$`, or `#`
- every fallback and border case needed by the update can be resolved from the stated rules
- applying the updates to the current schema produces a valid DatoriumDB collection schema

If validation fails, the schema is not advanced and no documents are migrated.

## Commands

### `config validate`

Validate the complete establishment config directory.

```text
datoriumctl config validate [--config-dir /db/.config]
```

This command writes nothing.

On success it reports that the config is valid and prints the current `general.version`.

### `config show`

Print a summary of the current config.

```text
datoriumctl config show [--config-dir /db/.config]
```

Human output should include:

- database name
- establishment server
- `general.version`
- server count
- collection count
- whether `shardMap.default` is present and complete

With `--json`, return a compact summary object rather than the full establishment document.

### `collection create`

Create a new collection from an initial schema file.

```text
datoriumctl collection create <CollectionName> <schema-file.json> [--config-dir /db/.config] [--data-dir /db] [--dry-run]
```

Behavior:

1. Reject the command if `{CollectionName}.schema.json` already exists.
2. Load and compile the schema file as strict JSON OJSON.
3. Require root `kind: object`.
4. Validate the complete candidate config with the new collection included.
5. Write:
   - `{CollectionName}.schema.json`
   - `{CollectionName}.schema.0.json`
6. Create the empty local collection directory `{data-dir}/{CollectionName}` as soon as the schema files are written, if it does not already exist.
7. Increment `general.version`.

Example schema file:

```json
{
  "kind": "object",
  "children": [
    {"name": "title", "kind": "string", "required": true},
    {"name": "releaseYear", "kind": "number", "integer": true},
    {"name": "status", "kind": "string"},
    {"name": "highRated", "kind": "boolean", "default": false}
  ]
}
```

The initial schema version is always `0`.

`collection create` prepares local storage on the machine where the CLI runs, typically the establishment server. It does not remotely create directories on other nodes.

In multi-node systems, each `datoriumdb` server creates any missing local collection directories as soon as it learns about those collections from the establishment server. That happens on startup and whenever the server refreshes establishment config. Creating a missing directory is idempotent: if the directory already exists, the server leaves it alone.

### `collection upgrade`

Advance an existing collection schema by exactly one version.

```text
datoriumctl collection upgrade <CollectionName> <upgrade-file.json> [--config-dir /db/.config] [--dry-run]
```

`<upgrade-file.json>` is only an input path. Its filename can be anything; the CLI reads the file contents. The files written under `--config-dir` still use the fixed names `{CollectionName}.schema.json` and `{CollectionName}.schema.{ver}.json`.

The upgrade file contents are strict JSON and follow the shape used by [UPDATE-SCHEMA.md](UPDATE-SCHEMA.md):

```json
{
  "from": 0,
  "new_ver_id": "01KWHM7R7D3T50G0GH6XN4CRZT",
  "updates": [
    {
      "op": "add",
      "path": "/rating",
      "value": 0,
      "schema": {
        "kind": "number",
        "default": 0
      }
    }
  ]
}
```

`to` is optional. Schema upgrades always advance by exactly one version, so the effective target is `from + 1`. If `to` is present and is not `from + 1`, the command fails.

Behavior:

1. Load the current `{CollectionName}.schema.json`.
2. Reject the command unless `from` matches the current schema version.
3. Resolve the target version as `from + 1`, using an explicit `to` only as a consistency check.
4. Validate the upgrade operations against the current schema.
5. Apply the operations in memory to produce the next schema.
6. Validate the resulting schema.
7. Write:
   - updated `{CollectionName}.schema.json`
   - new `{CollectionName}.schema.{from+1}.json`
8. Leave older `{CollectionName}.schema.{ver}.json` history files in place.
9. Increment `general.version`.

After the schema files are advanced, document migration is handled by the normal upgrade process described in [AGENT-FOR-COLLECTION-UPGRADE.md](AGENT-FOR-COLLECTION-UPGRADE.md). The CLI does not rewrite every document itself.

### `collection list`

List collections known to establishment config.

```text
datoriumctl collection list [--config-dir /db/.config]
```

Human output should show each collection name and its current schema version.

### `collection show`

Show one collection's current schema summary.

```text
datoriumctl collection show <CollectionName> [--config-dir /db/.config] [--version <ver>]
```

Without `--version`, show the current schema.

With `--version`, show the historic schema file `{CollectionName}.schema.{ver}.json` if it exists.

### `server list`

List servers from `__servers.json`.

```text
datoriumctl server list [--config-dir /db/.config]
```

### `server set`

Add or replace a server entry.

```text
datoriumctl server set <serverName> --base-url <url> [--config-dir /db/.config] [--dry-run]
```

Example:

```text
datoriumctl server set analysisA --base-url https://analysis.datoriumdb.com
```

Behavior:

1. Validate that `<serverName>` is a non-empty identifier without whitespace.
2. Validate that `--base-url` is an absolute URL.
3. Insert or replace the entry in `__servers.json`.
4. Validate the complete config.
5. Write `__servers.json`.
6. Increment `general.version`.

### `server remove`

Remove a server entry.

```text
datoriumctl server remove <serverName> [--config-dir /db/.config] [--dry-run] [--yes]
```

Behavior:

1. Reject removal if the server is still referenced by `general.establishmentServer`.
2. Reject removal if the server is still referenced by `shardMap.default`.
3. Otherwise remove the entry, validate, write `__servers.json`, and increment `general.version`.

### `shard-map set`

Replace the shard map from a strict JSON file.

```text
datoriumctl shard-map set <shard-map-file.json> [--config-dir /db/.config] [--dry-run]
```

Expected file shape for MVP:

```json
{
  "shardMap": {
    "default": {
      "00-7F": {
        "SHARD_SOT_MEMBER": "serverA",
        "SHARD_READ_MEMBER": ["serverB"],
        "PROXY_READ_MEMBER": ["analysisA"]
      },
      "80-FF": {
        "SHARD_SOT_MEMBER": "serverC",
        "SHARD_READ_MEMBER": ["serverD"],
        "PROXY_READ_MEMBER": ["analysisA"]
      }
    }
  }
}
```

Behavior:

1. Load and parse the shard map file as strict JSON.
2. Require `shardMap.default` for MVP.
3. Verify full coverage of shard slots `00` through `FF`.
4. Verify ranges do not overlap.
5. Verify all referenced servers exist in `__servers.json`.
6. Write `__shard-map.json`.
7. Increment `general.version`.

Collection-specific shard map overrides are not part of the MVP and should be rejected if present.

### `shard-map show`

Show the current shard map summary.

```text
datoriumctl shard-map show [--config-dir /db/.config]
```

Human output should list each range and its SOT, read-member, and proxy-read-member assignments.

### `general set`

Update selected fields in `__general.json`.

```text
datoriumctl general set [--name <string>] [--establishment-server <serverName>] [--read-member-checkin-seconds <n>] [--cache-update-checkin-seconds <n>] [--read-member-failed-checkins-before-stale <n>] [--config-dir /db/.config] [--dry-run]
```

Only provided fields are changed.

Behavior:

1. Apply the requested field changes in memory.
2. Validate that `establishmentServer` still names a known server.
3. Validate numeric fields are positive integers where required.
4. Write `__general.json`.
5. Increment `general.version`.

`general.version` itself is never set directly by this command. It is always incremented by the CLI after a successful mutating write.

### `auth show`

Show the public auth configuration from `__auth.json`.

```text
datoriumctl auth show [--config-dir /db/.config]
```

Human output should include issuer, audience, lifetime defaults, and each key's `kid`, algorithm, and status. It must not print private key material.

### `auth set`

Update selected public auth fields in `__auth.json`.

```text
datoriumctl auth set [--issuer <string>] [--audience <string>] [--client-token-lifetime-seconds <n>] [--machine-token-lifetime-seconds <n>] [--config-dir /db/.config] [--dry-run]
```

Only provided fields are changed. After validation, write `__auth.json` and increment `general.version`.

### `auth key add`

Add or replace a public signing key entry in `__auth.json`.

```text
datoriumctl auth key add --kid <id> --alg <alg> --public-key-file <path> [--status active|retired] [--config-dir /db/.config] [--dry-run]
```

Behavior:

1. Load the public key from `--public-key-file`.
2. Reject private-key PEM or other private key material.
3. Insert or replace the key entry identified by `--kid`.
4. Ensure at least one key remains `active` after the change.
5. Write `__auth.json` and increment `general.version`.

### `auth key retire`

Mark a public signing key as retired but keep it available for token grace periods.

```text
datoriumctl auth key retire <kid> [--config-dir /db/.config] [--dry-run]
```

Behavior:

1. Reject the command if retiring the key would leave zero active keys.
2. Set the key's `status` to `retired`.
3. Write `__auth.json` and increment `general.version`.

### `auth token issue`

Issue a short-lived client or machine token for MVP bootstrap and demos.

```text
datoriumctl auth token issue --kind client|machine [--subject <string>] [--server-name <serverName>] [--lifetime-seconds <n>] [--config-dir /db/.config]
```

Behavior:

1. Read public auth defaults from `__auth.json`.
2. Load the private signing key from `DATORIUMDB_SIGNING_KEY_FILE`.
3. For `--kind machine`, require `--server-name` and embed that server identity in the token.
4. Use `--lifetime-seconds` when provided; otherwise use the matching default from `__auth.json` (`3600` for both client and machine in the MVP).
5. Sign and print the token.
6. Do not write config files and do not increment `general.version`.

This command is an operator tool, not a user-management system. It must never write private key material into `__auth.json`.

### `search create`

Create an immutable precompiled search definition.

```text
datoriumctl search create <CollectionName> <SearchName> <search-definition-file.json> [--config-dir /db/.config] [--data-dir /db] [--dry-run]
```

Behavior:

1. Reject the command if `{CollectionName}.search.{SearchName}.json` already exists.
2. Load and validate the search definition against [SEARCH-DEFINITION-SCHEMA.md](SEARCH-DEFINITION-SCHEMA.md) and the collection schema.
3. Write `{CollectionName}.search.{SearchName}.json` and `{CollectionName}.search.{SearchName}.1.json`.
4. Ensure `{data-dir}/{CollectionName}/.search/{SearchName}` exists.
5. Increment `general.version`.

For the MVP, search definitions are immutable after create. To change a search, delete it and create a new one.

### `search delete`

Delete a search definition.

```text
datoriumctl search delete <CollectionName> <SearchName> [--config-dir /db/.config] [--yes] [--dry-run]
```

Behavior:

1. Remove `{CollectionName}.search.{SearchName}.json`.
2. Leave historic `{CollectionName}.search.{SearchName}.{ver}.json` files in place.
3. Increment `general.version`.

Local search result trees under `.search/{SearchName}` may be cleaned up by servers after they refresh establishment config and observe that the search no longer exists.

### `search list`

List search definitions known to establishment config.

```text
datoriumctl search list [--config-dir /db/.config]
```

## Dry-Run Behavior

`--dry-run` means:

- load current config
- compute the candidate result
- run full validation
- print the files that would be written or removed
- print the next `general.version` that would be published
- write nothing

A successful dry-run exits with code `3` so scripts can distinguish "validated but not applied" from "applied".

## Schema History Preservation

When a collection is created at version `0`:

- write `{CollectionName}.schema.json`
- write `{CollectionName}.schema.0.json` with the same content

When a collection is upgraded from `from` to `from + 1`:

- overwrite `{CollectionName}.schema.json` with the new schema
- write `{CollectionName}.schema.{from+1}.json`
- leave all older `{CollectionName}.schema.{ver}.json` files untouched

The CLI never deletes schema history files as part of create or upgrade.

## Error Reporting

Errors should be specific enough to fix without reading source code.

Each error should include:

- a stable `code`
- a `path` when the failure points at a JSON location or CLI argument
- a human `message`
- `expected` and `actual` when those are useful

Common codes:

- `invalidJSON`
- `invalidSchema`
- `collectionAlreadyExists`
- `collectionNotFound`
- `staleSchemaVersion`
- `invalidSchemaUpgrade`
- `serverNotFound`
- `serverStillReferenced`
- `invalidBaseURL`
- `incompleteShardMap`
- `overlappingShardRanges`
- `unknownServerReference`
- `invalidAuthConfig`
- `noActiveSigningKey`
- `privateKeyRejected`
- `configLockHeld`
- `filesystemError`

Human mode prints one error per line.

`--json` mode returns the full envelope with an `errors` array.

## MVP Scope

The MVP CLI should support:

- `config validate`
- `config show`
- `collection create`
- `collection upgrade`
- `collection list`
- `collection show`
- `server list`
- `server set`
- `server remove`
- `shard-map set`
- `shard-map show`
- `general set`
- `auth show`
- `auth set`
- `auth key add`
- `auth key retire`
- `auth token issue`
- `search create`
- `search delete`
- `search list`
- `--dry-run`
- `--config-dir`
- `--data-dir`
- `--json`
- exclusive config locking
- atomic file replacement
- `general.version` increments
- local collection directory creation on `collection create`

The MVP CLI does not need:

- live calls into running DatoriumDB servers
- remote creation of collection directories on other nodes
- automatic document migration during `collection upgrade`
- collection-specific shard map overrides
- interactive schema editors
- a full user-management or identity system beyond operator token issuance
- writing private signing keys into `__auth.json`
