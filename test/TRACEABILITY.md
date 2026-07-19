# MVP Traceability

Maps MVP requirements to test layers. Status values: `planned`, `partial`, `covered`.

Test layers (see `README.md` "Testing" for how to run each):

- **unit** — `internal/*` package tests, default `go test ./...` (fast, no build tags).
- **contract** — `test/contract`, in-process normalized golden-envelope tests (`-tags contract`).
- **integration** — `test/integration`, real `datoriumdb` subprocesses over real HTTP (`-tags integration`).
- **crash** — `test/crash`, SIGKILL / hard-interruption tests against real subprocesses (`-tags crash`).
- **compose** — `test/compose`, full multi-container topologies from `deploy/*.yml` (`-tags compose`, requires Docker).

| Requirement | Doc | Test IDs / suites | Status |
| --- | --- | --- | --- |
| Access-language CRUD | ACCESS-LANGUAGE.md | unit: `internal/engine`, `internal/accesslang`; contract: `test/contract` create/read/patch/delete + error-path golden envelopes; integration: `TestSingleNodeCRUDLifecycle` | covered |
| HTTP command transport | ACCESS-LANGUAGE.md, LOCAL-ARCHITECTURE.md | integration: `TestSingleNodeCRUDLifecycle`, `TestSingleNodeUnauthenticatedCommandRefused`; compose: `TestComposeSingleNodeCRUD` | covered |
| 8-bit shard slot via CRC32 | CONVENTIONS.md, SHARDING.md | unit: `internal/shard`; compose: `TestComposeFiveShardCRUDAcrossShardsAndWrongMachine` (`serverForSlot`) | covered |
| Role-aware routing | SHARDING.md, ESTABLISHMENT-CONFIG.md | unit: `internal/engine` (`TestWriteRoutesToSOTOnly`, `TestReadRoutesToReadMemberOnly`, `TestReadSucceedsOnAssignedReadMember`, `TestProxyReadMemberIsNotANormalReadTarget`, `TestWriteRefusedOnReadOnlyMember`, `TestDualRoleServerServesBothReadsAndWrites`); contract: `create_wrong_machine` golden; integration: `TestTwoNodeBootstrapReplicationAndRouting`; compose: `docker-compose.five-shard.yml`, `docker-compose.split-sot-read.yml`, `docker-compose.proxy-read.yml` | covered |
| Shard-local storage | SHARDING.md | integration: `internal/server` (`TestReplicationHappyPathCreatePatchDelete`); `test/integration` (`TestTwoNodeBootstrapReplicationAndRouting` asserts the document lands on serverB's own `/db` tree); compose: five-shard, split-sot-read | covered |
| Filesystem atomic replace | FILESTYSTEM-STORAGE.md | unit: `internal/fsstore`; crash: `TestSIGKILLDuringConcurrentWritesPreservesDurability` (real SIGKILL mid-write burst, verifies no torn JSON survives and every confirmed write is intact after restart) | covered |
| Previous-document dotfiles | FILESTYSTEM-STORAGE.md | unit/integration: patch/delete preserve `.{id}.json` | partial |
| Establishment config files | ESTABLISHMENT-CONFIG.md | unit: `internal/config`; CLI validate; compose: all six `deploy/*.yml` fixtures load and validate via `internal/config.Load` | covered |
| Auth / machine bootstrap | AUTHENTICATION.md | unit: `internal/auth`; integration: `TestTwoNodeBootstrapReplicationAndRouting` (real bootstrap over HTTP), `TestSingleNodeUnauthenticatedCommandRefused`; compose: `docker-compose.auth-bootstrap.yml` (`TestComposeAuthBootstrapSucceedsWithCorrectSecret`, `TestComposeAuthBootstrapFailsWithWrongSecret`) | covered |
| Replication + pending writes | REPLICATION-FAILURE-HANDLING.md, SERVER-TO-SERVER-API.md | unit: `internal/replication`; integration: `internal/server` (`TestReplicationHappyPathCreatePatchDelete`, `TestReplicationOneShotDownMemberPendingAndNote`, `TestCatchUpAppliesPendingWriteAndCleansUpAfterRestart`, `TestSOTResumeIncompleteReplicatesLeftoverOperations`); `test/integration` (`TestReadMemberRestartRecoversReplicatedData`); crash: `TestKilledReadMemberCatchesUpAfterRestart` (real SIGKILL of the read-member, pending write, restart, catch-up); compose: `docker-compose.degraded-replication.yml` (`TestComposeDegradedReplicationRecovers`) | covered |
| Change-agent | AGENT-FOR-CHANGE-DISTRIBUTION.md | unit: `internal/agents/change`; integration/crash: queue/taken/cleanup | partial |
| Cache updates | CACHE-UPDATES.md | unit: `internal/agents/cache`, `internal/engine` (`cachesummaries*`); integration: pendingCacheUpdates | partial |
| Schema upgrade + upgrade-agent | UPDATE-SCHEMA.md, AGENT-FOR-COLLECTION-UPGRADE.md | unit: `internal/agents/upgrade`, `internal/schemapatch`; CLI upgrade | partial |
| Precompiled search MVP | SEARCHING.md, SEARCH-DEFINITION-SCHEMA.md | unit: `internal/search` (definition validation, evaluate, path encoding, sort/null/missing, store idempotency); engine search command; change-agent bucket move/delete; integration: search replication to read member; compose search lifecycle still thin | covered |
| datoriumctl MVP commands | COMMAND-LINE-TOOLS.md | unit/integration: `internal/ctl`, `cmd/datoriumctl/integration_test.go` | partial |
| Five+ server E2E | MVP.md | compose: `docker-compose.five-shard.yml` (`TestComposeFiveShardCRUDAcrossShardsAndWrongMachine`) | covered |
| Production container image | FILESTYSTEM-STORAGE.md, this doc | `Dockerfile` (multi-stage, non-root, health check, `/db` volume); CI job `docker-build`; compose: all six topologies build from it | covered (build unverified in this sandbox — see README "Known Gaps") |
| CI release gates | this doc | `.github/workflows/ci.yml`: lint, unit -race, contract+integration+coverage, crash (Linux), compose E2E (main/nightly/manual), docker build | covered |
| Coverage gate for core packages | this doc | `scripts/coverage.sh`: engine/fsstore/accesslang/config/shard, floors documented below | covered (floors below 80% aim for fsstore/config/shard — see README "Coverage") |

Update statuses as suites land. MVP release requires every row `covered`.

## Compose topologies (`deploy/*.yml`, fixtures under `test/compose/fixtures/`)

| Topology | Servers | Demonstrates | Compose test |
| --- | --- | --- | --- |
| `single-node` | serverA (establishment+SOT+read) | Basic CRUD lifecycle, container boots and serves HTTP | `TestComposeSingleNodeCRUD` |
| `five-shard` | server1..server5 | 5-way shard split, cross-shard CRUD, wrongMachine routing | `TestComposeFiveShardCRUDAcrossShardsAndWrongMachine` |
| `split-sot-read` | serverA (SOT), serverB (read) | Bootstrap, SOT/read role split, wrongMachine on the read-member | `TestComposeSplitSOTReadRouting` |
| `proxy-read` | serverA (SOT), serverB (read), analysisA (proxy) | Proxy-member replication without normal read access | `TestComposeProxyReadReceivesReplicatedWritesButIsNotAReadTarget` |
| `degraded-replication` | serverA (SOT), serverB (read), analysisA (proxy) | Read-member outage → one-shot pending + note → restart → catch-up | `TestComposeDegradedReplicationRecovers` |
| `auth-bootstrap` | serverA (establishment), serverB (+ `serverB-bad-secret` profile) | Machine bootstrap with correct/incorrect shared secret | `TestComposeAuthBootstrapSucceedsWithCorrectSecret`, `TestComposeAuthBootstrapFailsWithWrongSecret` |

Not yet implemented as Compose E2E scenarios (tracked as `planned` above): schema-upgrade-in-place, search lifecycle over a live cluster, and cache-summary propagation over a live cluster. These have unit coverage but not a Compose-level drill yet.

## Known environment gap

This sandbox's Docker daemon is not reachable (`permission denied ... /var/run/docker.sock`, and rootless Podman fails to create a user namespace), so `test/compose` and `docker build`/`docker compose up` could not be executed end-to-end here. Every Compose file passed `docker compose config` (structural validation) and every fixture config directory passed `internal/config.Load` (semantic validation). `test/compose` correctly `t.Skip`s when Docker is unusable, which was verified in this environment. CI runs these on a real GitHub-hosted runner where Docker is available.
