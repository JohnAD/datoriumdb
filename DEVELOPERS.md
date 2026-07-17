# DatoriumDB Developer Guide

This document is for people building, testing, or releasing DatoriumDB.
End-user documentation lives in [README.md](README.md). Design details live
under [tech-docs/](tech-docs/).

## Building

```text
go test ./...
go build -o bin/datoriumdb ./cmd/datoriumdb
go build -o bin/datoriumctl ./cmd/datoriumctl
```

Sample establishment config lives in `testdata/sample-config`. A local
single-node server can start with:

```text
datoriumdb serverA http://127.0.0.1:8080 \
  --config-dir testdata/sample-config \
  --data-dir /tmp/datoriumdb-data \
  --listen 127.0.0.1:8080
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
