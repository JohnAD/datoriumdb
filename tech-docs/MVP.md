# MVP

This document defines the minimum system needed for early DatoriumDB development.

The MVP is intentionally narrow, but it is sharding-native from day one. It should prove the document model, access language, schema handling, patch behavior, file storage design, and basic shard routing before adding advanced distributed operations.

## First Version Scope

The first version will support basic sharding.

The first version can run as a single-machine deployment where all shard slots map to the same local machine.

The first version should still use the sharding path internally:

- compute the document or search shard
- resolve the target machine from establishment configuration
- route, accept, or refuse the command based on shard ownership

End-to-end MVP testing should include a Docker Compose arrangement with five or more server processes.

The first version will use local filestorage on a filesystem.

The first version will store git-trackable JSON documents.

Later versions may support BSON storage, but BSON is not part of the first version.

The filesystem must support `mv` operations that are effectively instant because the filesystem uses file IDs rather than copying file contents during moves.

The first version will not support per-document or per-collection encryption methods.

## Operations

The first version will support basic CRUD operations:

- `create`
- `read`
- `patch`
- `delete`

There will be no `update` operation. Existing documents are changed through `patch`.

The first version will support a simple version of precompiled searches.

## Dependencies

The MVP depends on OJSON for ordered JSON and schema handling.

The MVP depends on a ULID library for internally generated document IDs and document versions.

## Testing

The project should include unit tests for core library behavior and end-to-end tests for full database workflows.

End-to-end MVP tests should include a Docker Compose arrangement with five or more server processes to exercise sharding, routing, agents, and replication behavior.

## First Version Non-Goals

- No serial-line access.
- No remote access plugin system.
- No automatic shard rebalancing.
- No automatic SOT failover election.
- No dynamic cluster membership.
- No per-document encryption.
- No per-collection encryption.
- No BSON storage.

## Storage Assumption

The MVP assumes JSON document writes can be staged and then committed with filesystem move operations.

This assumption is important because it allows early development to focus on local atomicity and crash behavior before introducing storage plugins or distributed concerns.
