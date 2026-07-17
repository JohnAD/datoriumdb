# Replication Failure Handling

This document considers how DatoriumDB should handle server failure during shard replication.

The central question is:

If a `SHARD_SOT_MEMBER` accepts a write and must reflect that write to one or more `SHARD_READ_MEMBER` servers before confirming success to the API caller, what should happen when one of those read-member servers is unreachable?

This cannot be treated as an MVP shortcut. The write path needs durable, predictable failure semantics from day one.

## Roles Involved

For a shard slot:

- `SHARD_SOT_MEMBER` is the only normal write authority.
- `SHARD_READ_MEMBER` is a normal read target for smart clients.
- `PROXY_READ_MEMBER` receives replicated data for proxy, analysis, Git reflection, or other full-copy workflows, but is not a normal smart-client read target.

Replication to `SHARD_READ_MEMBER` servers affects normal read correctness.

For replication handling, `SHARD_READ_MEMBER` and `PROXY_READ_MEMBER` servers are treated the same. Either kind of read member may temporarily lag, catch up through `.pendingWrites`, and declare itself too old to read from if it fails to check in.

## Lessons From Existing Systems

### MongoDB

MongoDB replica sets use write concern.

With `w: "majority"`, the primary waits for a calculated majority of data-bearing voting members to acknowledge the write. If the required acknowledgement does not happen before `wtimeout`, MongoDB returns a write concern error.

Important behavior:

- A write concern timeout does not necessarily mean the write failed.
- The primary may already have applied the write.
- Some replicas may already have applied the write.
- The write may continue replicating after the timeout.
- Applications must treat the result as potentially committed but not fully acknowledged.

MongoDB does not rely on the primary remembering a one-off failed delivery attempt to each unreachable secondary. Replication is log-based.

The primary records writes in its oplog. Secondaries asynchronously choose a sync source, fetch oplog entries, and apply those entries to their local data sets. If a secondary is unreachable during a write, it can later catch up by fetching the missing oplog entries, assuming the relevant oplog history is still available from a viable sync source.

If a member falls too far behind and no longer has access to the needed oplog history, it may require resynchronization. If a former primary accepted writes that did not replicate before failover and later rejoins under a different primary, MongoDB may roll back those divergent writes to return to the replica set's common history.

The DatoriumDB analogue could be a durable "missing patches by server" backlog maintained by the SOT-member.

Conceptually, when delivery to a `SHARD_READ_MEMBER` or `PROXY_READ_MEMBER` fails, the SOT-member records a pending patch targeted to that server. This is a database-owned pseudo-collection/internal file set, not a normal user collection.

Each collection directory on the SOT machine can have a `.pendingWrites` subdirectory. Pending write files are stored on a per-read-server and per-document basis:

```text
{readServerName}.{docId}.json
```

For example:

```json
{
  "operationId": "01KXYZOPERATION000000000001",
  "collection": "Movies",
  "id": "01KWDRHGK2GXE2B0VZ85GT546T",
  "beforeVersion": "01KXYZBEFORE00000000000001",
  "afterVersion": "01KXYZAFTER000000000000001",
  "command": "patch",
  "patch": [
    {"op": "replace", "path": "/status", "value": "released"},
    {"op": "replace", "path": "/#", "value": "01KXYZAFTER000000000000001"}
  ]
}
```

Presence of the file under `.pendingWrites/` means the work is pending. A separate `state` field is not required.

Each read-member can then run a catch-up agent:

1. Check in with SOT-members for pending patches targeted to itself.
2. Pull and apply those patches idempotently.
3. Delete the pending patch through the SOT-member API after durable success.

After the SOT creates a pending write file, it is the read server's responsibility to get, apply, and delete that pending write through the API.

This is robust, but it requires callers to understand that "write concern failed" is not the same as "write did not happen."

### CouchDB

CouchDB clusters use shard replicas and quorum parameters.

The number of replicas is `n`. Read and write quorums are `r` and `w`. The default quorum is usually a majority of replicas.

Important behavior:

- A coordinator forwards the request to shard replicas.
- Success depends on enough replicas responding.
- Quorum can be configured per request.
- If fewer than the required nodes respond, CouchDB may return an accepted-but-not-fully-committed style response depending on operation and mode.
- CouchDB's revision model can preserve conflicting histories and requires explicit conflict resolution in some replication scenarios.

CouchDB favors availability and replication tolerance, but its conflict model is more complex than DatoriumDB should adopt for normal document writes.

The quorum/voting approach is interesting, but it is not philosophically aligned with DatoriumDB's source-of-truth emphasis. DatoriumDB's normal write path should keep one clear SOT-member for each shard slot rather than treating peer replicas as equal voting authorities for the document state.

### PostgreSQL

PostgreSQL supports asynchronous and synchronous replication.

With synchronous replication, commits can wait for a standby depending on `synchronous_commit` and `synchronous_standby_names`.

Important behavior:

- `remote_write` waits for a standby to receive and write WAL to its operating system.
- `on` waits for a standby to flush WAL durably.
- `remote_apply` waits until the standby has replayed the change and made it visible to queries.
- If required synchronous standbys are unavailable, commits can block indefinitely unless configuration or operations intervene.
- PostgreSQL can use quorum-style synchronous standby definitions, such as requiring any one of several standbys.

PostgreSQL gives strong semantics when configured strictly, but strict synchronous replication trades availability for certainty.

A PostgreSQL-style synchronous model could work for DatoriumDB in principle, but it would be extremely difficult to handle all border cases cleanly in a system built around client-server HTTP calls. PostgreSQL's replication is integrated deeply into its WAL, transaction, and connection model. DatoriumDB would need to recreate enough of that machinery over HTTP to avoid ambiguous commit states, dangling waits, and hard-to-debug partial delivery behavior.

### Firestore

Cloud Firestore uses synchronous replication with consensus.

Writes are replicated to a majority of replicas using Paxos. A leader coordinates writes for each data split. In multi-region deployments, Firestore can use full replicas and witness replicas.

Important behavior:

- Writes are synchronously replicated.
- Commit success requires quorum.
- If a participant cannot commit, the transaction aborts.
- Leader failure is handled by election.
- The service hides much of the operational complexity from users.

Firestore is similar to MongoDB in that both systems use replicated writes and require acknowledgement from other machines for strong write confirmation.

The important difference is that Firestore is a consensus system. It does not need every replica to respond, but it does need the required quorum. If the required quorum cannot be reached, the write or transaction aborts instead of becoming an ambiguously committed write.

This differs from MongoDB write concern timeout behavior, where a client may receive an error even though the primary has applied the write and replication may continue afterward.

Firestore's model is robust, but it depends on a full consensus system. DatoriumDB should learn from the quorum concept without casually promising full consensus semantics before implementing them.

## DatoriumDB Design Options

### Rejected Option: Require Every Read-Member Before Success

The SOT-member only returns success after every `SHARD_READ_MEMBER` for the shard slot has durably applied the change.

Advantages:

- Simple mental model.
- Normal read-members are never knowingly stale after a successful write.
- Easy for smart clients to trust any read-member assigned to the shard.

Disadvantages:

- One unavailable read-member can block or fail all writes for that shard slot.
- Tail latency is determined by the slowest required read-member.
- If the SOT-member applies locally before discovering a read-member failure, the caller may receive an error for a write that partly happened.
- In any non-trivial cluster, write failures could escalate out of control as soon as one read-member or network path becomes unhealthy.

This is strict and understandable, but it is not viable for DatoriumDB. It makes read-member availability part of write availability and can turn a localized replica failure into a broad write outage.

### Option 2: Quorum Read-Member Acknowledgement

The SOT-member returns success after a configured quorum of read-members has applied the change.

Advantages:

- Better write availability than requiring every read-member.
- Familiar model from MongoDB, CouchDB, Firestore, and PostgreSQL quorum standbys.
- Can tolerate one or more read-member failures if enough replicas remain.

Disadvantages:

- Some read-members may be stale after a successful write.
- Smart clients need freshness rules when reading.
- The system must track which read-members are behind.

This could be considered for a future version, but it does not match DatoriumDB's SOT-first principles as a default write model. A quorum or voting system treats replicas more like peer authorities, while DatoriumDB is intentionally centered on a clear source-of-truth member for each shard slot.

### Rejected Option: SOT-Only Commit With Async Replication

The SOT-member returns success after local durable commit, then replicates later.

Advantages:

- High write availability.
- Simple write response path.
- Proxy read-members and derived data already behave this way.

Disadvantages:

- Normal read-members can be stale after a successful write.
- Smart clients cannot assume a read from any read-member sees the latest write.
- This weakens the reason for requiring read-members before API success.
- Normal reads effectively become cache reads.

This is not viable for DatoriumDB's normal document write path. It makes all normal reads behave like reads from caches. That is intentionally allowed for actual caches, search files, and proxy read-members, but it goes too far for all document writes.

### Rejected Option: Durable Pending Write With Two-Phase Apply

The SOT-member stages a write, asks required read-members to stage it, then commits it after required acknowledgement.

Conceptually:

1. SOT-member validates the command.
2. SOT-member writes a durable pending operation record.
3. SOT-member sends a prepare request to required read-members.
4. Each read-member durably stores the pending operation.
5. Once the required acknowledgement policy is satisfied, the SOT-member commits locally.
6. The SOT-member sends commit messages to prepared read-members.
7. Recovery logic finishes any prepared or committed operations after crashes.

Advantages:

- Gives the cleanest path toward strong write semantics.
- Avoids publishing partially applied reads when prepare has not committed.
- Gives recovery a durable state machine to resume.

Disadvantages:

- More implementation complexity.
- Requires idempotent operation IDs.
- Requires cleanup of abandoned prepared operations.
- Still needs a policy for what happens if commit messages fail after the SOT commits.
- Does not really solve the two primary outage sources: network problems and server lockups.

This is too complex for DatoriumDB's replication model and does not sufficiently improve the important failure cases. A two-phase protocol still stalls when the network is partitioned or a required server locks up.

## Decided Direction

DatoriumDB writes are allowed to be slow.

On the happy path, a `SHARD_SOT_MEMBER` should:

1. validate the command
2. apply the write to its local source-of-truth storage
3. send the write to all read members for the shard slot
4. wait for acknowledgement from those read members, with a bounded timeout
5. return success to the API caller

A reasonable starting timeout is 10 seconds.

For replication, all read members are treated the same. This includes both `SHARD_READ_MEMBER` and `PROXY_READ_MEMBER`.

## Failure Semantics

DatoriumDB only returns write failure when the write itself fails before it can be accepted by the SOT-member.

Examples:

- invalid command parameters
- schema validation failure
- version conflict
- authentication failure
- local SOT storage failure
- local SOT write verification failure

If the SOT-member successfully applies the write locally but cannot reach one or more read members within the timeout, the API response still returns success.

In that case, the SOT-member records durable missing-patch entries in the collection's `.pendingWrites` directory and includes a `note` object in the response. The smart client may ignore the note or handle it, depending on the application code.

This makes the response honest:

- the source-of-truth write succeeded
- some read members may be temporarily stale
- repair has been scheduled

To make retries safe, every write operation still needs an idempotent operation ID.

## Required Operation State

The SOT-member should keep a durable per-operation record.

Suggested states:

- `received`
- `validated`
- `committedLocal`
- `replicating`
- `replicated`
- `failed`

The exact state names can change, but the implementation needs a durable recovery point.

After a crash or restart, the SOT-member scans incomplete operations and resumes replication until the operation reaches a terminal state.

## Idempotency

Every write should have an operation ID.

Operation IDs are ULIDs.

If the client does not provide one, the server can create one, but client-provided IDs are safer for retry after network failure.

Retries with the same operation ID must not apply the same patch twice.

This matters because replication failure may leave the caller uncertain whether the write happened.

## Pending Writes Layout

Each collection directory on the SOT machine may contain:

```text
.pendingWrites
```

Pending write files are named:

```text
{readServerName}.{docId}.json
```

For example:

```text
$/db/Movies/.pendingWrites/serverB.01KWDRHGK2GXE2B0VZ85GT546T.json
```

Each file contains a pending patch for one read server and one document.

Each replicated write should include:

- collection
- document ID
- before document version, if relevant
- after document version
- operation ID
- command type
- full-document `payload` or RFC 6902-compatible `patch` operation list

## Read-Member Catch-Up

All read members should apply replicated writes idempotently. This includes `SHARD_READ_MEMBER` and `PROXY_READ_MEMBER`.

After a pending write file exists, it is the read server's responsibility to get, apply, and delete that pending write through the SOT-member API.

Read servers are expected to check in for pending writes every `general.readMemberCheckinSeconds` seconds.

On check-in, the read-member asks for up to N pending write document references targeted to itself.

If the returned list is empty, the read-member is caught up.

If the returned list contains pending entries, the read-member gets one pending write at a time, applies it idempotently, and deletes it through the API after durable success.

If a read-member receives an operation it has already applied, it should acknowledge success and delete the pending write through the API.

If a read-member knows a specific document is out of date, it should refuse reads for that document until it catches up.

Otherwise, it should continue responding to reads.

A read-member should refuse all reads only after it has been unable to contact the relevant SOT server N times. The value of N comes from `general.readMemberFailedCheckinsBeforeStale` in establishment config. If `general.readMemberCheckinSeconds` is `10` and the stale threshold is `3`, the read-member becomes too old to read from after about 30 seconds.

## Client Response Shape

If the SOT write succeeds but read-member replication is incomplete, the command still returns `ok: true`.

The response includes a `note` object describing the replication issue.

Conceptually:

```text
{
  ok: true,
  command: patch,
  collection: Movies,
  id: 01KWDRHGK2GXE2B0VZ85GT546T,
  operationId: 01KXYZ...,
  note: {
    code: replication_retry_scheduled,
    message: "Write succeeded on the SOT-member, but one or more read members did not acknowledge within the timeout. Pending write work has been scheduled.",
    required: [serverB, serverD],
    acknowledged: [serverB],
    unacknowledged: [serverD],
    timeoutMs: 10000
  }
}
```

The smart client can ignore this note or surface it to the application.

If the local SOT write fails, the command returns `ok: false` with a normal command error.

## Open Design Decisions

- Server-to-server endpoint details for listing, fetching, applying, and deleting pending writes are tracked in [SERVER-TO-SERVER-API.md](SERVER-TO-SERVER-API.md).
