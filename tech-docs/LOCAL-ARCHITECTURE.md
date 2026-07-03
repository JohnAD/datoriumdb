# Local Single-Machine Architecture

The local single-machine version of DatoriumDB will run as a local Go webserver bound to loopback.

The server will not use a web framework. It should use Go's standard HTTP server tools unless a specific need appears later.

## Shape

The local server owns command execution and agent scheduling.

Applications talk to the database through the local server. This makes access from non-Go languages easier and gives the database one local process responsible for coordinating writes and background work.

The server listens only on loopback, such as:

```text
127.0.0.1
localhost
```

It is not intended to expose a network service in the MVP.

## Command Handling

The local server accepts DatoriumDB access-language commands and returns command result envelopes.

The command layer should remain separate from the transport layer. This keeps the access language reusable if later versions add other transports.

## Scheduler

The local server includes an internal scheduler for agents.

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

The local server runs the database agents internally.

Initial agents:

- `change-agent`, which distributes source-of-truth changes to searches, caches, and other derived files.
- `upgrade-agent`, which migrates documents after collection schema upgrades.

Agent work should be safe to retry.

The scheduler must prevent unsafe overlap:

- The same document should not be processed by two workers at the same time.
- The `upgrade-agent` should run at most once per collection at a time.
- Search and cache file conflicts should rely on normal `#` version checks and retry behavior.

## Benefits

This architecture gives DatoriumDB:

- a single local coordinator for writes
- no required external scheduler
- easy access from other programming languages
- a natural path to Docker Compose setups
- a cleaner path toward a future clustered service
- fewer OS-specific details than a custom local service or IPC daemon

## MVP Limits

The MVP local server is still local-only.

It does not provide:

- remote network access
- clustering
- sharding
- authentication or authorization
- encryption

Those concerns belong to later versions.
