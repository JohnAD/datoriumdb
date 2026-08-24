# Getting Started

This guide takes an operator from zero to a running DatoriumDB server that
accepts commands. Installation options (release archives, Docker) are covered
in detail by [INSTALL.md](../INSTALL.md); this page focuses on the setup
sequence and running the server.

## What you need

DatoriumDB runs as named servers. Every server needs:

1. its own **server name** (e.g. `serverA`),
2. the **establishment server base URL**,
3. an **establishment config directory** (`--config-dir`, typically
   `/var/lib/datoriumdb/.config`),
4. a **data directory** (`--data-dir`, typically `/var/lib/datoriumdb`),
5. credentials — either the signing private key (establishment server) or the
   machine bootstrap secret (all other servers).

The **establishment server** is a special node: it serves the cluster's
configuration (schemas, shard map, auth trust material) to clients and other
servers. In a single-node setup, one machine is both the establishment server
and the only data server.

## Single-node setup

### 1. Create the establishment config

The config directory holds plain-JSON files. Create and modify them with
`datoriumctl`, not by hand:

```text
datoriumctl config validate --config-dir /var/lib/datoriumdb/.config
datoriumctl config show    --config-dir /var/lib/datoriumdb/.config
```

The config files are:

```text
__general.json                    database identity and settings
__servers.json                    named servers and base URLs
__shard-map.json                  which server owns which shard slots
__auth.json                       public auth trust material (never private keys)
{Collection}.schema.json          collection schemas
{Collection}.search.{Name}.json   precompiled search definitions
```

### 2. Set up authentication

Generate an Ed25519 signing key, keep the private key **outside** the config
directory, and register the public key:

```text
export DATORIUMDB_SIGNING_KEY_FILE=/path/to/ed25519-signing-key.pem

datoriumctl auth set --issuer <issuer> --audience <audience> \
  --config-dir /var/lib/datoriumdb/.config

datoriumctl auth key add --kid main --alg Ed25519 \
  --public-key-file /path/to/ed25519-public-key.pem \
  --config-dir /var/lib/datoriumdb/.config
```

Issue a client token when needed:

```text
datoriumctl auth token issue --kind client --subject my-app \
  --config-dir /var/lib/datoriumdb/.config
```

Tokens are short-lived (default 3600 seconds). Clients send them as
`Authorization: Bearer {token}`.

### 3. Start the server

```text
datoriumdb serverA http://127.0.0.1:8080 \
  --listen 127.0.0.1:8080 \
  --config-dir /var/lib/datoriumdb/.config \
  --data-dir /var/lib/datoriumdb
```

Check liveness and readiness:

```text
curl -sS http://127.0.0.1:8080/datoriumdb/v1/health
curl -sS http://127.0.0.1:8080/datoriumdb/v1/ready
```

### 4. Create a collection

Collections are created from a schema file:

```json
{
  "kind": "object",
  "children": [
    {"name": "title", "kind": "string", "required": true},
    {"name": "releaseYear", "kind": "number", "integer": true},
    {"name": "status", "kind": "string"}
  ]
}
```

```text
datoriumctl collection create Movies movies-schema.json \
  --config-dir /var/lib/datoriumdb/.config \
  --data-dir /var/lib/datoriumdb
```

New collections start at schema version `0`. Schema changes go through
`datoriumctl collection upgrade` (one version at a time); documents are
migrated in the background and on access — you do not rewrite documents
yourself.

### 5. Create a search (optional)

Searches must be declared before use. See [Searching](search.md) for the
definition shape.

```text
datoriumctl search create Movies byStatus search-def.json \
  --config-dir /var/lib/datoriumdb/.config \
  --data-dir /var/lib/datoriumdb

datoriumctl search list --config-dir /var/lib/datoriumdb/.config
```

Search definitions are immutable: to change one, delete it and create a new
one.

### 6. Send your first command

```text
TOKEN=...   # from datoriumctl auth token issue

curl -sS -X POST http://127.0.0.1:8080/datoriumdb/v1/command \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  --data '{"command":"create","target":"Movies","parameter":"01TESTMOVIES00000000000001","detail":{"$":"Movies:0","title":"The Matrix","releaseYear":1999}}'
```

## Multi-node setup

Additional servers are non-establishment nodes. For each:

1. Register the server: `datoriumctl server set <name> --base-url <url>`.
2. Assign shard slots in the shard map: `datoriumctl shard-map set
   shard-map.json`. Shard ranges are two hex digits (`00-FF`) and must cover
   all 256 slots without overlap.
3. Start the server with the **machine bootstrap secret** instead of the
   signing key:

   ```text
   export DATORIUMDB_MACHINE_BOOTSTRAP_SECRET=...

   datoriumdb serverB http://<establishment-host>:8080 \
     --listen 0.0.0.0:8080 \
     --config-dir /var/lib/datoriumdb/.config \
     --data-dir /var/lib/datoriumdb
   ```

   The second command-line argument points at the establishment server. The
   node fetches and caches the establishment config on startup and refreshes
   it periodically; you do not copy config files to other nodes.

Each shard slot has one `SHARD_SOT_MEMBER` (accepts writes) and optional
`SHARD_READ_MEMBER` machines (serve reads). One machine may hold both roles.
See [Consistency Model](consistency.md) for what this split means for reads.

## Running notes

- **Health:** `GET /datoriumdb/v1/health` (liveness), `GET
  /datoriumdb/v1/ready` (config loaded).
- **Useful operator commands:** `datoriumctl config validate`,
  `config show`, `collection list`, `collection show`, `search list`,
  `shard-map show`, `server list`, `auth show`.
- **Dry runs:** mutating `datoriumctl` commands accept `--dry-run` to preview
  the plan without writing.
- **Replication tuning:** `readMemberCheckinSeconds` and
  `readMemberFailedCheckinsBeforeStale` in `__general.json` (set via
  `datoriumctl general set`) control how quickly read members catch up and
  when they start refusing stale reads. Defaults are roughly: a read member
  goes stale after 3 failed check-ins, i.e. about 30 seconds without SOT
  contact at a 10-second check-in.
- **Docker:** see [INSTALL.md](../INSTALL.md) for image build/run and
  Compose topologies under `deploy/`.
