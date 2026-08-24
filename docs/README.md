# DatoriumDB User Documentation

Documentation for IT users and application integrators who set up, run, and
write against a DatoriumDB server. This is not developer documentation; for
building DatoriumDB itself, see [DEVELOPERS.md](../DEVELOPERS.md).

## Contents

- [Getting Started](getting-started.md) — install, initial setup, starting a
  server, creating collections and searches, issuing tokens.
- [Access API](api.md) — the four-field JSON command protocol, HTTP transport,
  response envelopes, binary attachment commands, and error codes.
- [Searching](search.md) — what searches can and cannot do: operations,
  variables, sorting, result shape, and limits.
- [Consistency Model](consistency.md) — read-after-write behavior,
  source-of-truth vs. read members, `distributionComplete`, and
  `documentStale` / `readMemberStale` / `fileStale` refusals.
- [Operator Reference](operator-reference.md) — schemas, IDs and naming,
  size limits, backup/retention, token lifecycle, TLS, and versioning.
- [Atomic Updates and Batching](atomic-updates.md) — what "atomic updates"
  means today, version-checked writes, and how to structure multi-document
  changes client-side.
