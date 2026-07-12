# Local Single-Machine Architecture

The local and MVP versions of DatoriumDB will run as Go webservers.

The server will not use a web framework. It should use Go's standard HTTP server tools unless a specific need appears later.

## Shape

Each server owns command execution and agent scheduling for the shard slots assigned to it.

Applications talk to DatoriumDB servers over HTTP. This makes access from non-Go languages easier and gives each database process responsibility for coordinating writes and background work.

The server listens only on loopback, such as:

```text
127.0.0.1
localhost
```

Single-machine development can bind to loopback. End-to-end MVP testing should use multiple server processes, likely through Docker Compose.

## Startup Parameters

Every DatoriumDB server process starts with two required parameters:

1. the server's own name
2. the establishment server base URL

If either parameter is missing, the process shuts back down.

The server uses its own name to find its entry in establishment config. It uses the establishment server base URL to fetch that config and learn its roles.

The default route prefix for DatoriumDB servers is `/datoriumdb/v1`. After authentication, if authentication is needed, the server fetches establishment config with `GET /datoriumdb/v1/establish`.

## Command Handling

The server accepts DatoriumDB access-language commands and returns command result envelopes.

The command layer should remain separate from the transport layer. This keeps the access language reusable if later versions add other transports.

Commands should follow the sharding path even when every shard maps to the same machine:

1. Compute the document or search shard.
2. Resolve the target server from establishment configuration.
3. Accept the command locally or refuse/redirect it to the correct server.

## Scheduler

Each server includes an internal scheduler for agents.

The scheduler should not require external systems such as `cron`.

The scheduler can use:

- Go goroutines
- `time.Ticker`
- `context.Context` for shutdown
- wake channels so writes can notify agents immediately
- periodic scans as a safety net

The default MVP scheduler can be conservative, with one worker per agent type.

Later configuration may allow multiple workers:

```text
agents: {
  change: {
    workers: 4,
    interval: "30s"
  },
  upgrade: {
    workers: 1,
    interval: "1m"
  }
}
```

## Agents

Each server runs the database agents internally.

Initial agents:

- `change-agent`, which distributes source-of-truth changes to searches, caches, and other derived files.
- `upgrade-agent`, which migrates documents after collection schema upgrades.

Agent work should be safe to retry.

The scheduler must prevent unsafe overlap:

- The same document should not be processed by two workers at the same time.
- The `upgrade-agent` should run at most once per collection at a time.
- Search and cache file conflicts should rely on normal `#` version checks and retry behavior.

## Read-Member Memory Cache

Any machine that is a read-member of a shard slot will have an internal memory cache.

This cache keeps a limited number of documents in memory based on recency and the quantity of recent queries.

The exact cache algorithm is still to be determined.

## Benefits

This architecture gives DatoriumDB:

- a single local coordinator for writes
- no required external scheduler
- easy access from other programming languages
- a natural path to Docker Compose setups
- a cleaner path toward a future clustered service
- fewer OS-specific details than a custom local service or IPC daemon

## MVP Limits

The MVP includes sharding, but avoids advanced distributed operations.

It does not provide:

- authentication or authorization
- encryption
- automatic shard rebalancing
- automatic SOT failover election
- dynamic cluster membership

Those concerns belong to later versions.
