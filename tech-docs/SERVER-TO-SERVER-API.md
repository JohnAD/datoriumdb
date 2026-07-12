# Server-To-Server API

This document tracks API endpoints used between DatoriumDB servers.

These endpoints are separate from smart-client command routing. They are used for establishment refresh, replication catch-up, schema history access, search patch delivery, cache update catch-up, and other internal server coordination.

Server-to-server endpoints are not the mechanism for creating collections, upgrading schemas, or editing establishment config. Those administrative changes are performed through command-line tools described in [COMMAND-LINE-TOOLS.md](COMMAND-LINE-TOOLS.md).

## Goals

- Keep server-to-server behavior explicit.
- Use authenticated requests.
- Make retry and idempotency requirements clear.
- Avoid hiding distributed-system behavior inside local filesystem assumptions.

## Pending Writes

When a `SHARD_SOT_MEMBER` cannot deliver a write to a read member in time, it creates a pending write file under the affected collection's `.pendingWrites` directory.

Read members later check in with the SOT-member and process pending writes targeted to themselves.

The exact endpoint shape is still to be defined.

## Pending Cache Updates

When a source document is created, patched, or deleted, the SOT-member creates pending cache update work items for read members that may store cached summaries pointing at that source document.

Pending cache update work is tracked separately from pending replicated writes. Replicated writes keep read-member source-of-truth copies current. Pending cache updates keep derived cached summaries current.

The work item shape is similar to a pending replicated write. For creates and patches, the payload is a copy of the current source document. For deletes, the work item carries delete metadata instead of a current document payload.

Read members later check in with the relevant SOT-members, apply pending cache update work to their local cache files, and delete the work item through the SOT-member API after durable success.

The exact endpoint shape is still to be defined.

## TODO

- Define the endpoint a read-member uses to list up to N pending write document references targeted to itself.
- Define the endpoint a read-member uses to fetch one pending write document.
- Define the endpoint a read-member uses to delete one pending write document after durable local application.
- Define whether the apply-and-delete step should be one API call or two API calls.
- Define the endpoint a read-member uses to list up to N pending cache update work items targeted to itself.
- Define the endpoint a read-member uses to fetch one pending cache update work item.
- Define whether applying pending cache update work is purely local to the read member or acknowledged through a dedicated SOT-member endpoint.
- Define the endpoint a read-member uses to delete one pending cache update work item after durable local application.
- Define whether pending writes and pending cache updates use parallel endpoint shapes.
- Define authentication and authorization requirements for pending write endpoints.
- Define authentication and authorization requirements for pending cache update endpoints.
- Define the retry behavior when a delete acknowledgement fails after the read-member has applied the pending write.
- Define the retry behavior when a delete acknowledgement fails after the read-member has applied a pending cache update.
- Define response envelopes for server-to-server success and failure.
- Define whether server-to-server endpoints use the same `/datoriumdb/v1` route prefix.
