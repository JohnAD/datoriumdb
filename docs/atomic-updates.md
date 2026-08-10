# Atomic Updates and Batching

## What exists today

**Every single command is atomic.** A `create`, `patch`, or `delete` either
commits in full on the source-of-truth machine or fails with `ok: false` —
there are no partial documents, and a `patch` applies its whole RFC 6902
operation list or none of it. Writes are crash-safe: confirmed writes survive
a server kill, and no torn JSON is ever visible.

**There is currently no multi-document transaction command.** The README's
"Atomic Updates" item (submitting several updates together, all-or-nothing,
across collections) is a design goal that is not yet exposed in the access
API. A request body carries exactly one command, and commands execute
independently. Do not design an integration that depends on submitting a
multi-document batch in one call — it will not be accepted.

## The building block: version-checked writes

`patch` and `delete` require the exact current `#` document version. This is
optimistic concurrency control ("loose locking"):

```text
patch Movies 01KWDRHG... {$: Movies:0, #: <version-you-read>, RFC6902: [...]}
```

- If the document is unchanged since your read, the patch commits.
- If anything else wrote first, you get `versionMismatch` — re-read and
  retry.

This gives each document an atomic compare-and-swap. It is the correct
primitive for claim/queue patterns and counters: read, patch with the read
version, retry on `versionMismatch`. See
[Consistency Model](consistency.md#queue-claim-and-read-modify-write-patterns).

## Structuring multi-document changes client-side

Until a batch command exists, applications that need several documents changed
"together" (for example a session write batch or a fan-out feed write) should:

1. **Order matters: write the most important document last.** Commit the
   dependent/derived documents first, then the document whose existence
   signals "this group is complete" (an index document, a head pointer, a
   session marker). Readers that find the marker can rely on the earlier
   documents existing; readers that find partial state simply don't see the
   marker yet.
2. **Version-check every write.** Carry the `#` from your reads into each
   patch; retry individually on `versionMismatch`.
3. **Make writes idempotent where possible.** Commands already fail cleanly
   on retry (`documentExists`, `versionMismatch`, `documentNotFound`), so a
   client-side retry loop can safely resume an interrupted group of writes:
   re-read each target, skip ones already in the desired state, and re-apply
   the rest.
4. **Tolerate partial visibility.** Other clients may observe some of the
   group's documents before all are written. If that is unacceptable, gate
   visibility behind the marker document from step 1.
5. **Remember derived data lags.** Searches and cached summaries catch up
   asynchronously after each commit; a just-written group may not appear in
   `search` results immediately.

This pattern gives you atomic *visibility* (via the marker) and crash-safe
resumability (via idempotent, version-checked writes), even though the server
does not yet bundle the writes into one transaction.
