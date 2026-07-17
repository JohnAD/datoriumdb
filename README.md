# DatoriumDB

This is a document-oriented database server. Document-oriented databases are often used when one of the following is a design goal of the project:

- Fast read latencies of `O(1)`, with slower write latencies of `O(n)` or more. This is often the reverse of relational databases.
- Individual records/documents that can carry large amounts of unstructured data in addition to schema-defined data.

## New JSON library

DatoriumDB uses [OJSON](https://github.com/JohnAD/ojson) for ordered, optionally schemed JSON.

## Building The MVP Scaffold

```text
go test ./...
go build -o bin/datoriumdb ./cmd/datoriumdb
go build -o bin/datoriumctl ./cmd/datoriumctl
```

Sample establishment config lives in `testdata/sample-config`. A local single-node server can start with:

```text
datoriumdb serverA http://127.0.0.1:8080 --config-dir testdata/sample-config --data-dir /tmp/datoriumdb-data --listen 127.0.0.1:8080
```

Access-language commands are accepted at `POST /datoriumdb/v1/command`.

To publish versioned `datoriumdb` / `datoriumctl` archives (and optional
container images) from this repository, see
[tech-docs/GITHUB-BINARY-DISTRIBUTION.md](tech-docs/GITHUB-BINARY-DISTRIBUTION.md).

## Testing

DatoriumDB's test suites are layered by build tag so `go test ./...` (no
tags) stays fast and dependency-free, while heavier suites opt in
explicitly. See `test/TRACEABILITY.md` for how each suite maps to
documented requirements.

```text
# Default: fast unit tests only (internal/* packages). This is what CI
# runs on every push/PR as the "unit" job, with -race.
go test ./...

# Contract: in-process, normalized golden-envelope tests
# (test/contract). No network, no subprocesses.
go test -tags contract ./test/contract/...

# Integration: real `datoriumdb` subprocesses talking real HTTP
# (test/integration), including a two-node bootstrap+replication cluster.
go test -tags integration ./test/integration/...

# Crash: SIGKILL / hard-interruption tests against real subprocesses
# (test/crash). Linux-oriented (relies on process-group signals).
go test -tags crash ./test/crash/...

# Compose: full multi-container topologies (test/compose), requires a
# working Docker + `docker compose`. Skips gracefully (t.Skip) if Docker
# is not usable in the current environment.
go test -tags compose ./test/compose/...
```

Run everything that doesn't need Docker in one shot:

```text
go test ./... -race && go test -tags "contract integration crash" ./test/...
```

### Coverage

`scripts/coverage.sh` gates statement coverage for DatoriumDB's core
packages (`engine`, `fsstore`, `accesslang`, `config`, `shard`), computed
across the whole module (so code exercised by other packages' tests still
counts) using the default unit suite plus the fast, in-process `contract`
suite:

```text
bash scripts/coverage.sh
```

The MVP aim is `>=80%` per package. As of this writing, `engine`
(~82%) and `accesslang` (~86%) already clear that bar; `fsstore` (~76%),
`config` (~76%), and `shard` (~75%) do not yet, mostly because some
error-path branches (rare filesystem failures, CLI-only validation paths)
aren't exercised by tests yet. The script enforces slightly-below-current
floors for those three packages (see the `FLOORS` table in
`scripts/coverage.sh`) so coverage cannot silently regress further while
still leaving room to raise the floors as more tests land. Re-run with
`--update-floors` to print current numbers without failing, after adding
coverage.

## Container / Compose Quick Starts

A production-like multi-stage `Dockerfile` builds both `datoriumdb` and
`datoriumctl` into a minimal, non-root runtime image with a health check
and a persistent `/db` volume. Six ready-to-run Compose topologies live
under `deploy/`, backed by fixture establishment configs under
`test/compose/fixtures/`:

| File | Topology |
| --- | --- |
| `deploy/docker-compose.single-node.yml` | One server, its own establishment/SOT/read roles |
| `deploy/docker-compose.five-shard.yml` | Five servers, one shard-fifth each |
| `deploy/docker-compose.split-sot-read.yml` | SOT-member + dedicated read-member |
| `deploy/docker-compose.proxy-read.yml` | SOT + read + proxy (analysis) member |
| `deploy/docker-compose.degraded-replication.yml` | Same as proxy-read, tuned for a fast degrade/recover drill |
| `deploy/docker-compose.auth-bootstrap.yml` | Establishment server + a bootstrapping machine (plus a `bad-secret` profile for the negative case) |

Quick start:

```text
docker compose -f deploy/docker-compose.single-node.yml up --build
curl http://127.0.0.1:8080/datoriumdb/v1/health
```

Each Compose file's header comment documents its published ports and any
manual drill steps (e.g. `docker compose ... stop serverB` for the
degraded-replication scenario). `deploy/secrets/` holds a **dev/test-only**
shared bootstrap secret consumed via Docker Compose `secrets:` and the
`DATORIUMDB_MACHINE_BOOTSTRAP_SECRET_FILE` convention implemented by
`docker-entrypoint.sh`; the Ed25519 signing key secret reuses
`testdata/sample-config/dev-signing-key.pem` directly. Never reuse either
outside local development or CI.

`test/compose` drives these topologies as automated end-to-end tests
(`go test -tags compose ./test/compose/...`), covering bootstrap, CRUD
across shards, wrongMachine routing, proxy-member behavior, and the
degraded-replication repair drill. It requires a real Docker daemon and
skips gracefully otherwise.

### Known Gaps

The sandboxed development environment this scaffold was built in could
not reach a Docker daemon (`permission denied` on the Docker socket, and
rootless Podman could not create a user namespace), so `docker build` and
`docker compose up` were validated structurally (`docker compose config`,
and every fixture config directory loading via `internal/config.Load`)
but not executed end-to-end there. CI (`.github/workflows/ci.yml`) runs
the real `docker build` and Compose E2E jobs on a GitHub-hosted runner
where Docker is available.

## Features

- **Separation of Truth** - A document in any document-oriented database can contain information that is either (1) authoritative, (2) cached, or (3) untracked. Those roles are never mixed. Data in this service is separated by **Collections**, and each collection has a schema enforced on all of its documents that separates these data roles. For example, if a document has an authoritative "Year" field for a particular object, then no other collection has that same "Year" data authoritatively. However, other collections can have cached "copies" of the "Year". Documents can also have non-schemed data that is untracked and is only visible when the individual document is pulled.
- **Intrinsic Encryption** - Future encryption support is expected to be smart-client driven, with document-level key selection based on tenant, user, policy, or application context. Breaking one document's key should not automatically expose other documents.
- **Agentic Update of Cache** - This service keeps an "update queue" of work needed to update the cached copies of authoritative data.
- **Automatic Forward Schema Migration** - Collection schemas are versioned and can be migrated forward.
- **Trackable** - With filesystem-backed JSON storage, state changes in the database are git-trackable and human-readable.
- **Atomic Update** - Multiple document updates, possibly crossing collections, can be submitted, and nothing is written until all updates are ready/confirmed.
- **Patches Stressed** - While clients can make a simple "replace this whole document" request, this is frowned upon and documented as bad practice. Instead, the service supports much more finely tuned arguments such as "add object to array and sort by {x}".
- **Prefix Sharding** - Sharding is done by the prefix of a document's ID rather than the full ID. That way, multiple documents can be shepherded to individual shards in a server farm, regardless of collection.

## Drawbacks

- **Eventually Correct** - A document's cached fields are not guaranteed to be true, but they will be "eventually" correct. This is a common trait of most document-oriented databases. ^1
- **All Searches Must Be Planned** - The database service does NOT let you openly search on any field. Searches are pre-composed and involve database migration. This is one of the reasons why reading is fast but writing is slow. ^2
- **Loose Locking** - Updates/deletes must match document version references. A client can send an update request and simply be told "no" if any discrepancy is found. The calling software must be written to handle this, possibly forcing it to re-examine the updated documents.

[1] A client could, of course, simply read multiple documents to get around caching. But that should be a fairly rare circumstance.

[2] A client could, of course, simply open every document in the collection to do a search. But that should be a fairly rare circumstance.
