# Command-Line Tools

This document tracks administrative command-line tools for DatoriumDB.

These tools are separate from the access language. The access language is for document operations such as `create`, `read`, `patch`, and `delete`. Administrative tasks such as creating collections, changing schemas, and updating establishment config files are performed by command-line tools.

## Purpose

Command-line tools should make config and schema changes explicit, validated, and Git-friendly.

They should:

- validate requested changes before writing files
- write plain JSON config files under `/db/.config`
- preserve readable, stable formatting for Git diffs
- use safe file replacement so readers do not observe partially written config
- preserve versioned schema history
- increment `general.version` when the served establishment config changes

## Schema Management

Collection creation and schema upgrades are administrative operations.

Creating a collection should write the initial current schema file and any required versioned schema history under `/db/.config`.

Upgrading a collection schema should validate the schema update rules before the current schema is advanced. After the schema is advanced, documents are migrated by the normal upgrade process described in [AGENT-FOR-COLLECTION-UPGRADE.md](AGENT-FOR-COLLECTION-UPGRADE.md).

## Establishment Config Management

The command-line tools are also responsible for updating establishment config files such as:

```text
/db/.config/__general.json
/db/.config/__servers.json
/db/.config/__shard-map.json
/db/.config/{CollectionName}.schema.json
/db/.config/{CollectionName}.schema.{ver}.json
```

The establishment server serves the resulting files; it does not provide a general write API for editing them.

## TODO

- Define the command-line tool name and invocation pattern.
- Define the command for creating a collection with its initial schema.
- Define the command for upgrading a collection schema.
- Define the exact schema update input file format.
- Define dry-run behavior for schema and config changes.
- Define validation rules for OJSON collection schemas.
- Define validation rules for schema upgrade operations.
- Define how command-line tools preserve previous schema versions.
- Define how command-line tools safely update `/db/.config` files.
- Define how command-line tools increment `general.version`.
- Define commands for server config changes in `__servers.json`.
- Define commands for shard map changes in `__shard-map.json`.
- Define how command-line tools report errors.
