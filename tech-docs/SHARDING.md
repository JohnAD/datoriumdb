# SHARDING

Sharding splits database responsibility across multiple machines.

## 16-Bit Shard Hash

Unlike other DBs, there is an implied sharding even when running on one server. The sharding algorithm uses a 16-bit shard hash derived from Go's standard CRC32 implementation, allowing up to 64K shard buckets.

Conceptually:

```text
shard = crc32(prefixBytes) & 0xFFFF
```

However, by default all shards, `0000` to `FFFF`, are stored on one machine at first.

"Moving" one or more shard responsibilities is done by changing a global map to point to other machines.

If one wanted two machines to share write responsibility for the database, one might keep shards `0000` to `7FFF` on the first machine and move shards `8000` to `FFFF` to the second machine.

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
- if a client sends a write to the wrong machine, that machine refuses the write and returns the correct target

This means a shard is primarily a write-authority boundary.

Reads and searches can be served by any machine that has a sufficiently fresh copy of the data. Writes must go to the current authority for the target document's shard.

## Derived Data On Replicas

As each machine receives a fresh patch for a document, that machine can independently update its local derived files.

This includes:

- `.search` files
- `.cache` / cached summary files
- other non-source-of-truth files

This keeps search and cache maintenance local to each replica and avoids requiring a global search index as part of the first sharding design.

## Open Search Question

Searches may eventually need a more explicit sharding design.

Possible approaches include:

- each machine maintains local search files from its local full copy
- searches become a separate storage system
- searches are treated like a collection in some ways
- global searches are coordinated by a proxy or query coordinator

This needs more design work before sharding is implemented.

