# DatoriumDB

This is a document-oriented database server. Document-oriented databases are often used when one of the following is a design goal of the project:

- Fast read latencies of `O(1)`, with slower write latencies of `O(n)` or more. This is often the reverse of relational databases.
- Individual records/documents that can carry large amounts of unstructured data in addition to schema-defined data.

## New JSON library

To make this library work, it should ideally handle ordered, optionally schemed JSON and JSON objects. While this support can certainly be embedded into the database, it makes sense to turn it into an independent library for others to use.

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

## Speculation

* [Can a SQL-style multi-table join work and kind-of sort-of scale in a doc DB?](tech-docs/relational-joins-in-doc-db.md).
