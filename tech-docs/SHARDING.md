# SHARDING

Sharding splits database responsibility across multiple machines.

Sharding is part of the MVP. Single-machine mode is treated as the degenerate case where every shard slot maps to one machine.

End-to-end MVP testing should include a Docker Compose arrangement with five or more server processes.

## 8-Bit Shard Hash

Unlike other DBs, there is an implied sharding even when running on one server. The sharding algorithm uses an 8-bit shard hash derived from Go's standard CRC32 implementation, allowing up to 256 shard slots.

Conceptually:

```text
shard = crc32(prefixBytes) & 0xFF
```

However, by default all shard slots, `00` to `FF`, are stored on one machine at first.

"Moving" one or more shard responsibilities is done by changing a global map to point to other machines.

If one wanted two machines to share write responsibility for the database, one might keep shard slots `00` to `7F` on the first machine and move shard slots `80` to `FF` to the second machine.

A DB machine can even have no write-authority shards assigned. That machine can act as a live proxy to other machines for reads. Writes, on the other hand, receive a redirect response to the correct shard authority. Writes MUST go to a machine authorized for that shard.

The shard hash is applied to each document's ID prefix. This is the part of the ID before the first dot. If there is no dot, the entire ID is used.

By assigning shards this way, documents that are likely to be accessed together can be purposely routed to the same shard. For example, if the same system that accesses user document `01KWJYMCTDNTF4MKNHD92FWPGW` is also likely to access that user's settings and mailbox documents, those related documents can use IDs like `01KWJYMCTDNTF4MKNHD92FWPGW.01KWJYMCTDNTF4MKNHD92FWPGW`. The shared prefix routes those documents to the same shard.

## Tentative Full-Replica Model

The current tentative plan is that sharding controls write authority more than storage availability.

In this model:

- every machine may keep a full copy of the database
- each shard has a small set of machines allowed to accept writes
- exactly one machine is the current source-of-truth write authority for a shard
- other machines can receive replicated writes and update their local derived data
- clients can compute the shard from the document ID prefix
- if a client sends a write to the wrong machine, that machine refuses the write with an `ok: false` response that includes the correct target

This means a shard is primarily a write-authority boundary.

Document reads can be served by any machine that has a sufficiently fresh copy of the data and is assigned to the document's shard. Search reads are routed by the search result shard. Writes must go to the current authority for the target document's shard.

## Machine Roles

### Establishment Server

Canonical role token: `ESTABLISHMENT_SERVER`.

The establishment server provides clients with the mapping configuration for the entire database.

Most importantly, it tells clients where each shard slot is located.

The establishment server does not provide database data for reads and does not accept database writes. Its sole purpose is configuration discovery.

### Shard Slot Members

Each shard slot has two kinds of member roles:

- `SHARD_READ_MEMBER` for reads
- one `SHARD_SOT_MEMBER` for writes

If only one machine is assigned to a shard slot, that machine serves both roles.

If two or more machines are assigned to a shard slot, reads and writes are split by role:

- reads go to `SHARD_READ_MEMBER` machines
- writes go to the `SHARD_SOT_MEMBER`

A read-member refuses to return a document that is not assigned to one of its shard slots, even if the machine has a full replica of the database.

If this happens, the smart client should request the latest mapping data from the establishment server and retry against the correct machine.

A read-member also refuses writes unless it is serving both roles for the shard slot.

Similarly, unless it is serving both roles, an SOT-member only accepts writes for the shard slots assigned to it.

When an SOT-member receives a write, it performs the slow and expensive write operation. It returns success only after it has finished distributing the update to the read-members for that shard slot.

Replication failure handling is described in `REPLICATION-FAILURE-HANDLING.md`.

Direct document references are resolved by smart clients, not by the database during a read response. The client uses the referenced document ID, computes the shard, and reads the referenced document from the correct machine.

Searches are also sharded. The client computes the search shard from the field path used to encode the search parameters and queries the machine assigned to that search shard. Querying the wrong machine returns an `ok: false` error envelope.

Search result updates are routed to the SOT-member for the search shard. The search SOT applies the patch and distributes the updated search result to the search shard's read-members before returning success to the agent.

Because search updates are distributed later by agents, searches are eventually correct.

## Derived Data On Replicas

As each machine receives a fresh patch for a document, that machine can independently update its local derived files.

This includes:

- `.search` files
- `.cache` / cached summary files
- other non-source-of-truth files

This keeps search and cache maintenance local to each replica.

## Open Search Storage Question

Searches may eventually need a more explicit storage design.

Possible approaches include:

- searches become a separate storage system
- searches are treated like a collection in some ways

This needs more design work before search storage is finalized.

