# Server-To-Server API

This document tracks API endpoints used between DatoriumDB servers.

These endpoints are separate from smart-client command routing. They are used for establishment refresh, replication catch-up, schema history access, search patch delivery, cache update catch-up, and other internal server coordination.

Server-to-server endpoints are not the mechanism for creating collections, upgrading schemas, or editing establishment config. Those administrative changes are performed through command-line tools described in [COMMAND-LINE-TOOLS.md](COMMAND-LINE-TOOLS.md).

## Goals

- Keep server-to-server behavior explicit.
- Use authenticated requests.
- Make retry and idempotency requirements clear.
- Avoid hiding distributed-system behavior inside local filesystem assumptions.

## Endpoint Conventions

Server-to-server endpoints live under:

```text
/datoriumdb/v1/sys
```

`QUERY` is used for list-style reads that need a structured request body. It does not change server state. For the MVP HTTP implementation, `QUERY` is carried as `POST` with `Content-Type: application/json` and the same path; the method name in this document describes the operation semantics.

`POST` is used for happy-path push delivery from an SOT-member to a read member.

`GET` fetches one work item.

`DELETE` confirms that a work item has been durably applied by the read member and asks the SOT-member to remove that work item.

The `{serverName}-{docId}` path component is a work item identifier. The list endpoint should return the exact identifier string the read member later passes to `GET` and `DELETE`. Clients should treat this value as opaque rather than reconstructing it by parsing or joining strings.

The authenticated server identity must match the `serverName` whose work is being requested, fetched, or deleted.

## Response Shape

All DatoriumDB API endpoints return the same basic JSON envelope for application-level results.

Successful API calls return HTTP `200` with:

```text
{
  ok: true,
  ...
}
```

Failed API calls also return HTTP `200`, but with `ok: false` and an `errors` array:

```text
{
  ok: false,
  ...,
  errors: [
    {
      code: someErrorCode,
      path: /some/path,
      message: "Human-readable explanation."
    }
  ]
}
```

Transport-level failures can still occur when the server cannot be reached or the HTTP request itself cannot be processed. Once an API endpoint is able to return a DatoriumDB response, success or failure is represented inside the envelope.

## Happy-Path Document Write Delivery

### Create, patch, and delete

For `create`, `patch`, and `delete`, after the local commit a `SHARD_SOT_MEMBER` makes one live delivery attempt to each assigned read/proxy member via:

```text
POST /datoriumdb/v1/sys/apply-document-write
```

A target that acknowledges is finished. A target that does not gets a matching `.pendingWrites` file; that member discovers the work through the pending-write list / catch-up APIs below. The SOT does not retry failed targets.

Request body:

```json
{
  "targetServer": "serverB",
  "item": {
    "collection": "Movies",
    "id": "01KWDRHGK2GXE2B0VZ85GT546T",
    "beforeVersion": "01KWDRJ2PB2MTZ2VZ9V6F6Q4FV",
    "afterVersion": "01KWDRK4X3AV9BN9MZ3EY4Y2K8",
    "operationId": "01KWDRK4X7F1M9W5K0D9S1P3QH",
    "command": "patch",
    "patch": [
      {"op": "replace", "path": "/status", "value": "released"},
      {"op": "replace", "path": "/#", "value": "01KWDRK4X3AV9BN9MZ3EY4Y2K8"}
    ]
  }
}
```

The `item` object uses the same shape as a pending document write work item. Application is idempotent by `operationId`.

On success:

```text
{
  ok: true,
  applied: true,
  operationId: 01KWDRK4X7F1M9W5K0D9S1P3QH
}
```

If the read member does not acknowledge within the SOT timeout, the SOT-member creates the matching `.pendingWrites` file and continues. See [REPLICATION-FAILURE-HANDLING.md](REPLICATION-FAILURE-HANDLING.md).

## Happy-Path Search Result Delivery

Search-result replication uses the same push-then-pending pattern.

```text
POST /datoriumdb/v1/sys/apply-search-result-write
```

Request body:

```json
{
  "targetServer": "serverB",
  "item": {
    "collection": "Movies",
    "search": "byReleasedGenre",
    "path": "/status/released/genre/scifi",
    "beforeVersion": "01KWDRJ2PB2MTZ2VZ9V6F6Q4FV",
    "afterVersion": "01KWDRK4X3AV9BN9MZ3EY4Y2K8",
    "operationId": "01KWDRK4X7F1M9W5K0D9S1P3QH",
    "command": "patch",
    "patch": [
      {"op": "add", "path": "/matches/-", "value": "01KWDRHGK2GXE2B0VZ85GT546T"},
      {"op": "replace", "path": "/#", "value": "01KWDRK4X3AV9BN9MZ3EY4Y2K8"}
    ]
  }
}
```

Application is idempotent by `operationId`. Timeout fallback may use pending search-result work under the search directory in a later refinement; for MVP, the SOT may retry push delivery and rely on the change-agent's retryable nature.

## Pending Writes

When a `SHARD_SOT_MEMBER` cannot deliver a write to a read member in time, it creates a pending write file under the affected collection's `.pendingWrites` directory.

Read members later check in with the SOT-member and process pending writes targeted to themselves.

### List Pending Document Write Work

```text
QUERY /datoriumdb/v1/sys/pending-document-write-work-items
```

Returns a list of pending document write work item references targeted to the calling read member.

The request body contains the query parameters rather than placing them all in the URL:

```text
{
  serverName: analysisE,
  limit: 200
}
```

The request fields are:

- `serverName`, the read-member server asking for its work
- `limit`, the maximum number of work item references to return

The response should contain work item ID strings, not full work item payloads. The read member fetches individual work items after receiving the list.

On success, the returned envelope should include the work item references:

```text
{
  ok: true,
  totalItems: 1,
  items: [
    "serverB-01KWDRHGK2GXE2B0VZ85GT546T"
  ]
}
```

`totalItems` represents the total number of items found regardless of `limit`. Each entry in `items` is an opaque work item ID string. The read member uses that exact string as the final path component for `GET` and `DELETE`.

### Fetch Pending Document Write Work

```text
GET /datoriumdb/v1/sys/pending-document-write-work-items/{serverName}-{docId}
```

Returns one pending document write work item.

The `{serverName}-{docId}` path component identifies the pending work item for one target read member and one document. The work item should contain the same replication details described in [REPLICATION-FAILURE-HANDLING.md](REPLICATION-FAILURE-HANDLING.md), including collection, document ID, operation ID, command type, document versions, and either a full-document `payload` or a `patch` operation list.

On success, the returned envelope should include the work item:

```json
{
  "ok": true,
  "item": {
    "collection": "Movies",
    "id": "01KWDRHGK2GXE2B0VZ85GT546T",
    "beforeVersion": "01KWDRJ2PB2MTZ2VZ9V6F6Q4FV",
    "afterVersion": "01KWDRK4X3AV9BN9MZ3EY4Y2K8",
    "operationId": "01KWDRK4X7F1M9W5K0D9S1P3QH",
    "command": "patch",
    "patch": [
      {"op": "replace", "path": "/status", "value": "released"},
      {"op": "replace", "path": "/#", "value": "01KWDRK4X3AV9BN9MZ3EY4Y2K8"}
    ]
  }
}
```

For document write work items with `command: patch`, `patch` contains an RFC 6902-compatible operation list.

Unlike user-submitted access-language patches, SOT-authored replication patches may update database-owned metadata fields. In particular, the patch must carry the `/#` change so every read member stores the same document version created by the SOT-member.

For full document replication work, such as a create, the work item can use `payload` instead.

The `item` object is the JSON content stored in the matching `.pendingWrites/{readServerName}.{docId}.json` file on disk.

Fetching a work item does not delete it.

### Complete Pending Document Write Work

```text
DELETE /datoriumdb/v1/sys/pending-document-write-work-items/{serverName}-{docId}
```

Confirms that the read member has durably applied the pending document write work item and asks the SOT-member to delete the stored work item.

The read member should only call this endpoint after local durable success. If it already applied the same operation earlier, it should still call this endpoint so the SOT-member can clear the work item.

On success, the returned envelope should confirm completion:

```text
{
  ok: true,
  completed: true
}
```

If the SOT-member no longer has the work item, presumably because a previous `DELETE` already cleared it, the endpoint still returns success with `existing: false`:

```text
{
  ok: true,
  completed: true,
  existing: false
}
```

The read member may log this as an unusual retry result, but should otherwise treat it as successful completion.

A true failure, such as an expected filesystem error would return a typical error shape.

## Pending Cache Updates

When a source document is created, patched, or deleted, the SOT-member creates pending cache update work items for read members that may store cached summaries pointing at that source document.

Pending cache update work is tracked separately from pending replicated writes. Replicated writes keep read-member source-of-truth copies current. Pending cache updates keep derived cached summaries current.

The work item shape is similar to a pending replicated write. For creates and patches, the payload is a copy of the current source document. For deletes, the work item carries delete metadata instead of a current document payload.

Read members later check in with the relevant SOT-members, apply pending cache update work to their local cache files, and delete the work item through the SOT-member API after durable success.

### List Pending Cache Update Work

```text
QUERY /datoriumdb/v1/sys/pending-cache-update-work-items
```

Returns a list of pending cache update work item references targeted to the calling read member.

The request body contains the query parameters rather than placing them all in the URL:

```text
{
  serverName: serverA,
  limit: 200
}
```

The request fields are:

- `serverName`, the read-member server asking for its work
- `limit`, the maximum number of work item references to return

The response should contain work item ID strings, not full work item payloads. The read member fetches individual work items after receiving the list.

On success, the returned envelope should include the work item references:

```text
{
  ok: true,
  totalItems: 1,
  items: [
    "serverB-01KWDRHGK2GXE2B0VZ85GT546T"
  ]
}
```

`totalItems` represents the total number of items found regardless of `limit`. Each entry in `items` is an opaque work item ID string. The read member uses that exact string as the final path component for `GET` and `DELETE`.

### Fetch Pending Cache Update Work

```text
GET /datoriumdb/v1/sys/pending-cache-update-work-items/{serverName}-{docId}
```

Returns one pending cache update work item.

The `{serverName}-{docId}` path component identifies the pending work item for one target read member and one source document. For creates and patches, the work item payload is a copy of the current source document. For deletes, the work item carries delete metadata instead of a current document payload.

On success, the returned envelope should include the work item:

```json
{
  "ok": true,
  "item": {
    "sourceCollection": "People",
    "sourceDocumentId": "01KWDRHGK2GXE2B0VZ85GT546T",
    "beforeVersion": "01KWDRJ2PB2MTZ2VZ9V6F6Q4FV",
    "afterVersion": "01KWDRK4X3AV9BN9MZ3EY4Y2K8",
    "operationId": "01KWDRK4X7F1M9W5K0D9S1P3QH",
    "command": "patch",
    "payload": {
      "!": "01KWDRHGK2GXE2B0VZ85GT546T",
      "$": "People:1",
      "#": "01KWDRK4X3AV9BN9MZ3EY4Y2K8",
      "name": "Joe",
      "avatar": "joe.png"
    }
  }
}
```

The `item` object is the JSON content stored in the matching `.pendingCacheUpdates/{readServerName}.{docId}.json` file on disk. For `create` and `patch`, the payload is the current source document. The read member decides which fields belong in its cached summary.

Fetching a work item does not delete it.

### Complete Pending Cache Update Work

```text
DELETE /datoriumdb/v1/sys/pending-cache-update-work-items/{serverName}-{docId}
```

Confirms that the read member has durably applied the pending cache update work item to its local cache files and asks the SOT-member to delete the stored work item.

Applying pending cache update work is local to the read member. The SOT-member does not need a separate apply endpoint; durable local application followed by this `DELETE` is the acknowledgement.

On success, the returned envelope should confirm completion:

```text
{
  ok: true,
  completed: true
}
```

If the SOT-member no longer has the work item, presumably because a previous `DELETE` already cleared it, the endpoint still returns success with `existing: false`:

```text
{
  ok: true,
  completed: true,
  existing: false
}
```

The read member may log this as an unusual retry result, but should otherwise treat it as successful completion.

