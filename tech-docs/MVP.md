# MVP

This document defines the minimum system needed for early DatoriumDB development.

The MVP is intentionally local and narrow. It should prove the document model, access language, schema handling, patch behavior, and file storage design before adding distribution, sharding, or network access.

## First Version Scope

The first version will not support sharding.

The first version will be used locally. It will not expose access over a network, serial line, or other remote transport.

The first version will not have an inherent server process. It will be used as a local library in a sense similar to SQLite3.

Because there is no inherent server, the local version relies on convention and usage patterns to prevent problems from simultaneous use.

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

## First Version Non-Goals

- No sharding.
- No network server.
- No inherent server process.
- No serial-line access.
- No remote access plugin system.
- No distributed coordination.
- No per-document encryption.
- No per-collection encryption.
- No BSON storage.

## Storage Assumption

The MVP assumes JSON document writes can be staged and then committed with filesystem move operations.

This assumption is important because it allows early development to focus on local atomicity and crash behavior before introducing storage plugins or distributed concerns.
