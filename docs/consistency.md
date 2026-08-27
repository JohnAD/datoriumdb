# Consistency Model

## The short version

- **Writes are strongly consistent at the source of truth.** Every shard slot
  has exactly one `SHARD_SOT_MEMBER` that accepts writes. When a `create`,
  `patch`, or `delete` returns `ok: true`, the change is durable on that
  machine.
- **Read-your-write is guaranteed only on the SOT member.** A machine serving
  both SOT and READ roles for a shard always returns current data.
- **Read members lag unless `distributionComplete` is true.** When SOT and
  READ roles are split across machines, the write path makes one live delivery
  attempt for documents, searches, and cached summaries. If any target does
  not acknowledge, the success response still returns `ok: true` with
  `distributionComplete: false` (and possibly a replication `note`); unread
  members catch up asynchronously.
- **`distributionComplete` is a hint, never an error.** Clients may use it to
  skip polling when true, but must not treat false as a failed write.
- **Binary attachments follow the same hint.** File create/update/delete set
  `distributionComplete` from binary replication only; known-stale file reads
  refuse with `fileStale`. See [Binary Attachment Storage](../tech-docs/BINARY-FILES.md).
- **Writes are version-checked.** `patch` and `delete` require the exact
  current `#` version; a stale version is refused with `versionMismatch`. See
  [Atomic Updates and Batching](atomic-updates.md).

## Write path

When the SOT member accepts a write it:

1. commits the change to local source-of-truth storage,
2. makes one live delivery attempt to each assigned read/proxy member,
3. records a pending-write entry for any member that did not acknowledge,
4. attempts synchronous search and cache distribution for the same document,
5. returns `ok: true` with `distributionComplete` summarizing steps 2–4.

If step 2 left anyone out, the success response includes a `note` object
naming `acknowledged` and `unacknowledged` members. The write is still
successful; unacknowledged members catch up later. Clients may ignore the
note or surface it to the application.

## Read refusals: `documentStale` and `readMemberStale`

Read members prefer refusing a read over silently returning data they know is
old:

- **`documentStale`** — this member knows the specific document has a pending
  update it has not applied yet. Retry shortly (catch-up normally takes
  seconds), or read from the SOT member if you need the latest value
  immediately.
- **`readMemberStale`** — the member has failed too many check-ins with its
  SOT (threshold: `readMemberFailedCheckinsBeforeStale`, default 3, at
  `readMemberCheckinSeconds` intervals) and refuses **all** reads until it
  re-establishes contact. With the defaults this happens after roughly 30
  seconds without SOT contact. Route to another read member or the SOT, and
  investigate connectivity.

Both are normal, recoverable conditions — build clients to retry with
backoff.

## Queue-claim and read-modify-write patterns

Because reads may be stale and writes are version-checked, the safe pattern
for "claim a work item" or any read-modify-write is:

1. Read the document (ideally from the SOT member for the freshest view).
2. Patch it with the claim, passing the `#` version you just read.
3. On `versionMismatch`, re-read and retry — someone else claimed it first.

The version check makes step 2–3 an atomic compare-and-swap per document: a
claim either wins or fails cleanly, even if the read in step 1 was stale.

## Choosing a topology

Freshness is a deployment decision:

- **Combined SOT+READ on one machine per shard** — all reads are current;
  simplest setup; read and write capacity scale together.
- **Split SOT/READ** — read capacity scales independently at the cost of the
  staleness window described above.

Both are configured per shard range in the shard map
(`datoriumctl shard-map set`).
