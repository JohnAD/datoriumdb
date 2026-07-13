# Establishment Config

This document describes the configuration returned by an establishment server.

The establishment server does not serve database reads and does not accept database writes. Its purpose is to let smart clients and DatoriumDB machines discover the current database map.

## Purpose

Every smart client needs enough information to route commands without asking a database machine to guess.

Every DatoriumDB machine also needs the same information so it can:

- know which shard slots it serves
- know which shard slots it can read from
- know where to send writes, replication patches, search patches, and cache updates
- know when its local configuration is stale

The establishment config is the shared source of routing truth.

## Security

The establishment API needs security.

Authentication is described in `AUTHENTICATION.md`.

The establishment server should only return configuration to authorized clients and machines.

For the MVP, the expected pattern is bearer-token authentication with local token validation by each DatoriumDB server.

Open questions:

- Should JWT claims restrict which collections or shard maps a client can see?
- What token lifetimes should clients and machines use?

## Config Contents

The establishment response should contain:

- the database identity
- a config version
- all current collection schemas
- default shard mapping
- server definitions
- role assignments from `general` and `shardMap`
- security metadata needed by clients

The response should be self-contained enough that a client can route reads, writes, direct reference lookups, and searches without making another discovery call.

The `GET /datoriumdb/v1/establish` endpoint returns a single combined JSON document assembled from the config files under `/db/.config`.

Like all DatoriumDB API endpoints, the establishment endpoint returns HTTP `200` with a response envelope. On success, `ok` is `true`. On failure, `ok` is `false` and the response includes an `errors` array.

Conceptually:

```text
{
  ok: true,
  general: { ... },
  servers: { ... },
  shardMap: { ... },
  schemas: { ... }
}
```

## Config Source Directory

The establishment server reads the values it serves from `/db/.config`.

This directory contains establishment-owned config files. Those files should be readable, plain JSON, and suitable for storage in a Git repository.

The config files should not be directly modified by hand during normal operation. For now, the expected update path is a command-line tool that validates requested changes and writes the config files safely. Command-line tooling is tracked in [COMMAND-LINE-TOOLS.md](COMMAND-LINE-TOOLS.md).

Individual DatoriumDB servers also store local copies of these files in `/db/.config` by default. Those local copies are not the source of truth. They are refreshed from the establishment server and give each server a local, inspectable copy of the config it is currently using.

The first concrete config files are:

```text
/db/.config/__general.json
/db/.config/__servers.json
/db/.config/__shard-map.json
/db/.config/{CollectionName}.schema.json
/db/.config/{CollectionName}.schema.{ver}.json
```

Files beginning with `__` are database-wide config files. The prefix prevents name collisions with collection-owned config files.

`__general.json` contains database-wide identity and config metadata.

`__servers.json` contains the named server definitions used by shard maps and routing logic.

`__shard-map.json` contains the `shardMap` object. For the MVP, it contains a `default` map used for all collections.

`{CollectionName}.schema.json` contains the current schema for a collection.

`{CollectionName}.schema.{ver}.json` preserves a versioned copy of a collection schema.

For the MVP, collection config files remain flat under `/db/.config`. Per-collection subdirectories can be added later if collection-level config grows enough to justify the extra structure.

## General Config

The general config is read from `/db/.config/__general.json`.

It should contain:

- a general name for the database as a whole
- an establishment server reference that matches one server entry from `__servers.json`
- a config version used by the command-line config tool
- the read-member check-in interval used for pending writes
- the cache update check-in interval used for pending cache update work
- the number of failed read-member check-ins allowed before a read server declares itself too old to read from

Conceptually:

```json
{
  "general": {
    "name": "DatoriumDB Local",
    "establishmentServer": "serverA",
    "version": 1,
    "readMemberCheckinSeconds": 10,
    "cacheUpdateCheckinSeconds": 60,
    "readMemberFailedCheckinsBeforeStale": 3
  }
}
```

The `version` field is a monotonically increasing integer. The command-line config tool increments it by 1 whenever a config change affects the combined establishment response.

`readMemberCheckinSeconds` controls how often read members contact relevant SOT-members for pending writes.

`cacheUpdateCheckinSeconds` controls how often read members contact relevant SOT-members for pending cache update work. A reasonable starting value is `60` because cached summaries are derived data and do not need the same freshness interval as replicated source-of-truth writes.

`readMemberFailedCheckinsBeforeStale` controls how many failed check-ins with the relevant SOT server a read-member tolerates before refusing all reads as too stale. With `readMemberCheckinSeconds: 10` and a stale threshold of `3`, a read-member becomes too old to read from after about 30 seconds without SOT contact.

## Schemas

The establishment config includes a copy of all current collection schemas.

This lets smart clients:

- validate commands before sending them
- understand source-of-truth fields
- understand cached summary reference definitions
- understand search definitions
- detect stale local assumptions

Schemas are read from `/db/.config/{CollectionName}.schema.json`.

Versioned schema history is stored as `/db/.config/{CollectionName}.schema.{ver}.json`.

In the combined establishment response, each collection object should include the current schema version and schema document.

Conceptually:

```text
schemas: {
  Movies: {
    version: 1,
    schema: {
      kind: object,
      children: [...]
    }
  },
  People: {
    version: 3,
    schema: {
      kind: object,
      children: [...]
    }
  }
}
```

The combined establishment response only includes current schemas. If an SOT-member needs a historic schema version for migration support, it can request that version from the establishment server:

```text
GET /datoriumdb/v1/schema/{collectionName}/{ver}
```

On success, the endpoint returns an `ok: true` envelope containing the requested historic schema. The establishment server reads historic schema versions from `/db/.config/{CollectionName}.schema.{ver}.json`.

## Shard Maps

DatoriumDB uses an 8-bit shard hash, producing shard slots `00` through `FF`.

The establishment config provides mapping information for those shard slots.

The `shardMap.default` map applies to all collections for the MVP.

Collection-specific shard maps may be added later as `shardMap.collections`.

## Default Shard Map

The default shard map defines where shard slots live for collections without a more specific map.

The default shard map is read from `/db/.config/__shard-map.json`.

Conceptually:

```json
{
  "shardMap": {
    "default": {
      "00-7F": {
        "SHARD_SOT_MEMBER": "serverA",
        "SHARD_READ_MEMBER": ["serverB"],
        "PROXY_READ_MEMBER": ["analysisA"]
      },
      "80-FF": {
        "SHARD_SOT_MEMBER": "serverC",
        "SHARD_READ_MEMBER": ["serverD"],
        "PROXY_READ_MEMBER": ["analysisA"]
      }
    }
  }
}
```

The read-members can be different from the SOT-member. This lets a shard slot have one server responsible for writes while one or more other servers handle reads.

Proxy read-members are also assigned in `shardMap`. A `PROXY_READ_MEMBER` receives replicated data for the shard slot but is not the normal smart-client read target. This can be used for analysis servers, remote Git reflection, or other servers that should keep a copy of all documents.

When a shard write is replicated, delivery goes to both `SHARD_READ_MEMBER` and `PROXY_READ_MEMBER` servers for that shard slot.

Replication failure handling is described in `REPLICATION-FAILURE-HANDLING.md`.

For the MVP, the combined establishment response uses `shardMap.default`. Future collection-specific shard maps can be added under `shardMap.collections`.

## Future Service: Collection Shard Overrides

A future version may allow a collection to define a shard map that overrides `shardMap.default`.

For example:

```json
{
  "shardMap": {
    "default": {
      "00-FF": {
        "SHARD_SOT_MEMBER": "serverA",
        "SHARD_READ_MEMBER": ["serverB"],
        "PROXY_READ_MEMBER": ["analysisA"]
      }
    },
    "collections": {
      "AuditEvents": {
        "00-FF": {
          "SHARD_SOT_MEMBER": "serverE",
          "SHARD_READ_MEMBER": ["serverE"],
          "PROXY_READ_MEMBER": ["analysisA"]
        }
      }
    }
  }
}
```

In this example, `AuditEvents` would have its own sharding behavior, while other collections would continue to use `shardMap.default`.

This is not part of the MVP.

## Servers

The establishment config names the DatoriumDB servers that provide service for one or more shard slots.

Server definitions are read from `/db/.config/__servers.json`.

Each server entry identifies a network service endpoint. For now, `baseURL` is the only required server-entry field. The endpoint should be a full service URL, including scheme, host, and port when needed.

For example:

```json
{
  "servers": {
    "serverA": {
      "baseURL": "https://shard00.mytest.local"
    },
    "serverB": {
      "baseURL": "https://s32.datoriumdb.com"
    },
    "analysisA": {
      "baseURL": "https://analysis.datoriumdb.com"
    },
    "localServer": {
      "baseURL": "http://127.0.0.1:9001"
    }
  }
}
```

DNS names are preferred for production because they work better with TLS certificates, service migration, and operational changes. IP addresses are acceptable for local deployments, private networks, or controlled environments where DNS is unavailable or intentionally avoided.

A server entry should identify a specific DatoriumDB server endpoint. It should not point to a generic load-balanced pool unless that pool is intentionally acting as one logical server.

The default collection storage path on each server is `/db/{CollectionName}`. A server can map that local path to a different mounted drive, directory, or storage device, but that is local server administration. The establishment config should not describe per-server storage paths.

## Server Startup

When any DatoriumDB server starts, regardless of role, it must receive two startup parameters:

1. its own server name
2. the establishment server base URL

For example:

```text
datoriumdb serverA https://db.mydomain.com
```

If either parameter is missing, the server must shut back down.

The server's own name is used to find its entry in the establishment config. The establishment server base URL tells it where to fetch the config. This is critical because a server learns its roles from the establishment server rather than assuming them from local startup flags.

The default route prefix for DatoriumDB servers is:

```text
/datoriumdb/v1
```

After authentication, if authentication is needed, a server fetches establishment config with:

```text
GET /datoriumdb/v1/establish
```

That endpoint returns an `ok: true` envelope containing the combined JSON establishment document with general config, server definitions, the shard map, schemas, and other establishment config needed by the caller.

## Machine Roles

The config should identify machine roles using hardened enum-like tokens.

Known roles include:

- `ESTABLISHMENT_SERVER`
- `SHARD_SOT_MEMBER`
- `SHARD_READ_MEMBER`
- `PROXY_READ_MEMBER`

The establishment server itself does not need to be `SHARD_READ_MEMBER` or `SHARD_SOT_MEMBER`.

The `ESTABLISHMENT_SERVER` role is learned from `general.establishmentServer`.

Shard role assignment is stored in `shardMap`, not duplicated into each server entry. For the MVP, a server reads `shardMap.default` and learns its shard roles by finding its own server name in the `SHARD_SOT_MEMBER`, `SHARD_READ_MEMBER`, and `PROXY_READ_MEMBER` entries for each shard range.

This avoids maintaining a reverse mapping from server to shard roles.

Every machine role should call the establishment server for configuration updates. Non-establishment servers update their local `/db/.config` files from the establishment response.

## Client Routing

A smart client uses the establishment config to route commands.

For document reads and writes:

1. Take the document ID prefix.
2. Compute the 8-bit shard hash.
3. Find the shard slot in `shardMap.default`.
4. Route normal reads to a `SHARD_READ_MEMBER`.
5. Route writes to the SOT-member.

For direct document references, the smart client follows the same process using the referenced collection and ID.

For searches, the client computes the search shard from the search parameter path and routes the search request to the assigned machine.

`PROXY_READ_MEMBER` servers are not normal smart-client read targets unless a specific client or workflow intentionally chooses them.

## Wrong-Machine Behavior

If a client sends a command to the wrong machine, that machine should refuse the command.

For writes, the refusal should include enough information for the client to route the command correctly.

For reads or searches, the refusal may tell the client to refresh establishment config and retry.

The establishment config should include a version so stale clients can detect that their routing data may be outdated.

## Config Updates

Clients and machines should periodically refresh establishment config.

They should also refresh when:

- a machine refuses a command because it is not assigned to the shard
- a machine reports that the client's config version is stale
- a connection to an expected target fails repeatedly
- an establishment config expiration time is reached

The exact refresh interval is still to be determined.

Establishment config file updates should be made through a command-line tool, not by directly editing files in `/db/.config`.

On the establishment server, `/db/.config` is the source directory for the served config. On other DatoriumDB servers, `/db/.config` is a local cache of the establishment config returned by the establishment server.

Collection creation and schema upgrades are also command-line tool responsibilities. They are not access-language commands and are not general server-to-server API operations.

The command-line tool should:

- validate the requested config change
- write plain JSON config files
- preserve Git-friendly formatting
- use safe file replacement so readers do not observe partially written config
- increment `general.version` when the served config changes
- verify that `shardMap.default` covers all 256 shard slots
- verify that shard slot ranges do not overlap
- verify that `PROXY_READ_MEMBER` entries refer to known servers

## MVP Scope

For the MVP, establishment config can be static or mostly static.

The MVP should support:

- one establishment server
- authenticated config reads
- all current collection schemas in the config response
- config files under `/db/.config`
- server definitions in `/db/.config/__servers.json`
- `shardMap.default` in `/db/.config/__shard-map.json`
- collection schemas in `/db/.config/{CollectionName}.schema.json`
- versioned collection schemas in `/db/.config/{CollectionName}.schema.{ver}.json`
- config versioning
- command-line config updates

The MVP does not need:

- automatic shard rebalancing
- automatic SOT failover election
- dynamic cluster membership
- collection-specific shard map overrides
- partial schema visibility by authorization scope

## Open Questions

- What authentication method should be used first?
- What is the exact serialized config format?
- Should config be signed so clients can verify it independently?
- Should the establishment server be replicated?
- How do clients discover the establishment server itself?
