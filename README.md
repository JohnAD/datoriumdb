# DatoriumDB

This is a document-oriented database server. Document-oriented databases are often used when one of the following is a design goal of the project:

- Fast read latencies of `O(1)` but slow write latencies of `O(n)` or more. (compared to relational databases which are often the reverse of this.)
- Individual records/documents that can carry large amounts of non-structured data in addition to schema'd data

## Features

- **Separation of Truth** - A document in any document-oriented database can contains information that is either (1) authoritative, (2) cached, or (3) untracked. Those roles are never mixed. Data in this service is separated by **Collections** and each collection has a schema enforced on all of it's documents that separates these datum roles. For example, if a document has an authoritative "Year" field for a particular object, then not other collection has that same "Year" data authoritatively. However, other Collections can have cached "copies" of the "Year". Documents can also have non-schema'd data that is untracked and is only visible when the individual document is pulled.
- **Intrinsic Encryption** - A collection and sub-collections can have doc-separated encryption keys. Breaking the key on one document does not let you read any other documents. Each must be broken individually; vastly improving security.
- **Agentic Update of Cache** - This service keeps an "update queue" of work needing to update the cached copies of authoritative data. 
- **Automatic Forward Schema Migration** - Collection schemas are versioned and are updatable live; supporting LUA (or TBD).
- **Storage Plugins** - The method of storing the data is independent of this service by using executable plugins. "JSON filestore" and "BSON filestore" are available at first.
- **Trackable** - If the "JSON filestore" plugin is used, the change of state in the database is git-trackable and human readable.
- **Access Plugins** - How instructions reach the server is based on the executable plugin chosen. HTTPS and FiberSerial are available at first.
- **Atomic Update** - Multiple document updates, possibly crossing collections, can be submitted and nothing is written until all updates are ready/confirmed.
- **Patches Stressed** - While clients can do a simple "replace this whole document" request, this is frowned upon and documented as bad practice. Instead the service supports much more finely tuned arguments such as "add object to array and sort by {x}".
- **Prefix Sharding** - Sharding is done by the prefix of a document's ID rather than the full ID. That way multiple documents can be shepharded to individual shards in a server farm (regardless of collection).

## Drawbacks

- **Eventually Correct** - A document's cached fields are not guaranteed to be true; but will be "eventually" correct. This is a common trait of most document-oriented databases. ^1
- **All Searches Must Be Planned** - The database services does NOT let you just openly search on any field. Searches are pre-composed and involve database migration. This is one of the reasons why reading is fast but writing is slow.
- **Loose Locking** - Updates/Deletes must match doc version references. A client can send an update request and simply be told "no" if any discrepancy is found. So, the calling software must be written to handle this (possibly forcing it to re-examine the updated documents.)

[1] A client could, of course, simply read multiple documents to get around caching. But that should be a fairly rare circumstance. 
[2] A cllint could, of course, simply open every document in the collection to do a search. But that should be a fairly rare circumstance.
