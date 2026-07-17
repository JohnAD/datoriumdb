# DatoriumDB

This is a document-oriented database server. Document-oriented databases are often used when one of the following is a design goal:

- Fast read latencies of `O(1)`, with slower write latencies of `O(n)` or more — often the reverse of relational databases.
- Individual records/documents that can carry large amounts of unstructured data in addition to schema-defined data.

DatoriumDB works best with a "smart client" that can navigate sharding and distribution intelligently.

## Install

Linux and Docker install instructions are in [INSTALL.md](INSTALL.md).
Published binaries are on the
[GitHub Releases](https://github.com/JohnAD/datoriumdb/releases) page
([latest](https://github.com/JohnAD/datoriumdb/releases/latest)).

## Features

- **Separation of Truth** — A document can hold information that is (1) authoritative, (2) cached, or (3) untracked. Those roles are never mixed. Data is organized into **Collections**, each with a schema that enforces these roles across every document. If one collection owns an authoritative "Year" field for an object, no other collection may own that same "Year" authoritatively — though others may keep cached copies. Documents may also carry non-schema data that is untracked and visible only when that document is read.
- **Intrinsic Encryption** — Future encryption support is expected to be smart-client driven, with document-level key selection based on tenant, user, policy, or application context. Breaking one document's key should not automatically expose other documents.
- **Agentic Cache Updates** — A background update queue keeps cached copies of authoritative data current.
- **Automatic Forward Schema Migration** — Collection schemas are versioned and can be migrated forward.
- **Trackable** — With filesystem-backed JSON storage, state changes are git-trackable and human-readable.
- **Atomic Updates** — Multiple document updates, possibly across collections, can be submitted together; nothing is written until all updates are ready and confirmed.
- **Patches Preferred** — Replacing an entire document is supported but discouraged. Prefer fine-grained patches such as "add an object to an array and sort by {x}".
- **Prefix Sharding** — Sharding uses a prefix of the document ID rather than the full ID, so related documents can be steered to the same shard regardless of collection.

## Drawbacks

- **Eventually Correct** — Cached fields are not guaranteed to be current, but they become correct eventually — a common trait of document-oriented databases. DatoriumDB also supports live references to documents in other collections; a smart client fetches those documents on demand behind the scenes.
- **All Searches Must Be Planned** — You cannot ad hoc search on arbitrary fields. Searches are precomposed and involve database migration. That is one reason reads are fast and writes are slower.
- **Loose Locking** — Updates and deletes must match the document's current version. A client may be refused if the version does not match, and must be prepared to re-read the document and retry.

## For developers

Build, test, Compose, and release instructions are in [DEVELOPERS.md](DEVELOPERS.md).
