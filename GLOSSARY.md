# Glossary

This document defines project terminology so the documentation and implementation can use consistent names.

## Sharding

### Shard Hash

The hash algorithm used to route a document or search to a shard slot.

DatoriumDB currently uses an 8-bit shard hash derived from CRC32:

```text
shard = crc32(prefixBytes) & 0xFF
```

### Shard Slot

One specific 8-bit shard value from `00` through `FF`.

This is the preferred term for an individual shard hash result.

For example, `7A` is a shard slot.

Use `shard slot` instead of `shard cluster` when referring to the individual hash bucket. The word `cluster` implies multiple machines, but a shard slot may be served by one machine or many machines.

### Shard Assignment

The mapping from a shard slot to the server or servers responsible for serving it.

A shard assignment can point to:

- one server
- multiple servers

Local storage path mapping is handled by each server and is not part of shard assignment.

### SOT-Member

Canonical role token: `SHARD_SOT_MEMBER`.

The machine currently authorized to accept writes for a shard slot.

Each shard slot has exactly one current SOT-member.

### Read-Member

Canonical role token: `SHARD_READ_MEMBER`.

A machine authorized to serve reads for a shard slot.

A shard slot can have one or more read-members.

### Establishment Server

Canonical role token: `ESTABLISHMENT_SERVER`.

The server that provides mapping configuration for the database.

It tells clients and DatoriumDB machines where shard slots are assigned. It does not serve database reads or accept database writes.

### Proxy Read-Member

Canonical role token: `PROXY_READ_MEMBER`.

A machine assigned in `shardMap` to receive replicated data for a shard slot without being the normal smart-client read target for that shard slot.

This role can be used for analysis servers, remote Git reflection, or other full-copy/proxy read use cases.

## Searching

### Constant

A search parameter fixed when a search is defined.

Constants are part of the precompiled search definition. They are not provided by a live query.

### Variable

A search parameter provided when a live query uses a precompiled search.

Variables let a precompiled search accept caller-provided values without becoming an open-ended query. Not all search clauses support variables.
