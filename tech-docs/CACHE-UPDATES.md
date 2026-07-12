# Cache Updates

This document describes how cached document summaries are updated after source-of-truth document changes.

Cached summaries are derived data. They are allowed to be temporarily stale, but the database should eventually update them after a source document is created, patched, or deleted.

## Process

After an SOT-member receives a document create, patch, or delete, it queues cache update work for read members.

The original write does not wait for cache updates to finish. Instead, as part of the SOT write, the server creates pending cache update work items under:

```text
/db/{CollectionName}/.pendingCacheUpdates/{readServerName}.{docId}.json
```

`{CollectionName}` and `{docId}` identify the source document that changed.

The SOT-member builds these work items from the collection schemas it has received from the establishment server. It looks for `DatoriumCachedRef` fields whose `custom.collections` list includes the changed document's collection, then maps those possible cached-reference locations to the servers that serve the relevant shard slots.

This is done for SOT `create` commands also. A user may create a reference to a document that does not exist yet, especially when the application pre-selects document IDs before all related documents are written.

When a document does not exist yet but is cached, the cache file is still a full cached summary record. It identifies the referenced collection and document ID, but has a `null` revision (field `#`) because there is no source document version yet. Requested SOT summary fields are omitted from the JSON document rather than stored as `null` values.

Like the read-member catch-up process documented in [REPLICATION-FAILURE-HANDLING.md](REPLICATION-FAILURE-HANDLING.md), each read member contacts the relevant SOT-members every `general.cacheUpdateCheckinSeconds` seconds to get pending cache update work. This interval can be slower than replication catch-up because cached summaries are derived data and are allowed to lag briefly.

The SOT-member is responsible for creating pending cache update work items. `SHARD_READ_MEMBER` and `PROXY_READ_MEMBER` servers are responsible for applying those work items to their local caches and deleting the work items through the SOT-member API after durable success.

## Pending Cache Update Work Item

A pending cache update work item is similar to a pending replicated write, as described in [REPLICATION-FAILURE-HANDLING.md](REPLICATION-FAILURE-HANDLING.md).

Each work item should include:

- source collection
- source document ID
- before document version, if relevant
- after document version, except for deletes
- operation ID
- command type
- payload

For `create` and `patch`, the payload is always a copy of the current source document after the SOT write succeeds.

For `delete`, there is no current source document. The work item should instead contain enough delete metadata for the read member to write a full cached summary record for the deleted reference state.

## Cache Summary Files

Each read server stores at most one cached summary file for a given source collection and source document ID.

The cache filename does not include the local field path, the declaring collection, or the declaring schema location. The read server is responsible for using its schemas and local documents to interpret and apply the cached summary where needed.

Conceptually:

```text
/db/{LocalCollection}/.cache/{SourceCollection}/{sourceDocId}.json
```

If multiple local collections or fields can use the same source document summary, they still share this one local cache file on that read server.

When the source document is deleted, the cache file is not deleted and is not replaced by a bare `null` value. It remains a full cached summary record for that source collection and document ID, with its deleted or missing state represented inside the summary. As with not-yet-created references, requested SOT summary fields are omitted from the JSON document.

Deleting a referenced document does not patch or remove references from source-of-truth documents.

## Retry Behavior

Cache update catch-up is read-member driven.

If a read member cannot contact a relevant SOT-member, it keeps retrying on the `general.cacheUpdateCheckinSeconds` interval until the SOT-member is reachable again.

Pending cache update work remains on the SOT-member until the target read member applies the work item and deletes it through the SOT-member API.

## No Sweeping Reads

Cache update work does not require a sweeping read of every document.

The read member does not scan all documents looking for references.

Instead, it looks for existing cached summary files by source collection and source ID.

If a cached summary file exists for that source ID, the read member updates it in place. If no cached summary file exists, there is nothing to update for that location.

## Open Details

More details still need to be defined, including:

- exact cache update work item field names and envelope shape
- server-to-server endpoint details for pending cache update work, tracked in [SERVER-TO-SERVER-API.md](SERVER-TO-SERVER-API.md)
