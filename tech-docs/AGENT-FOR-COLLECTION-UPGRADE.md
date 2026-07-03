# Agent For Collection Upgrade

This document describes the background `upgrade-agent` that migrates documents after a collection schema is upgraded.

The `upgrade` command validates the schema update patch before any document is changed. If validation succeeds, the collection schema is permanently advanced. The `upgrade-agent` then brings documents forward to the new schema.

## Purpose

The `upgrade-agent` exists so the database does not have to rewrite every document during the `upgrade` command itself.

After a successful `upgrade`, documents can be migrated in two ways:

- the `upgrade-agent` works through the collection in the background
- individual documents are migrated on-the-fly when they are accessed

Both paths should produce the same final document.

## Responsibilities

The `upgrade-agent` is expected to:

- find documents whose `$` schema/version marker is older than the current collection schema
- apply the validated schema update rules
- write the migrated document as a normal patch
- create normal change queue entries for the `change-agent`
- avoid changing database-owned metadata directly except through normal database write rules

## Migration Safety

After the collection schema is advanced, document migration should not fail.

The schema update patch must define all fallback and border cases before the `upgrade` command succeeds. This includes default values, failover values, nullable behavior, empty values, kind conversion, import behavior, and abandon behavior.

If an old document cannot be migrated using the already-validated rules, that is a database bug or an invalid accepted upgrade patch.

## Background Operation

The `upgrade-agent` can run periodically by any mechanism supported by the database. For MVP purposes, it can be a local routine or cron-driven process.

When it runs, it scans collections for documents whose `$` schema/version marker is older than the current collection schema.

For each outdated document, it:

1. Reads the current document.
2. Applies the schema update rules needed to reach the current schema version.
3. Writes the migrated document as a normal patch.
4. Leaves the normal previous-document dotfile and change queue entry for the `change-agent`.

## On-Access Migration

If a document is read or patched before the `upgrade-agent` reaches it, the database may migrate that document on-the-fly.

On a simple `read`, migration occurs only in memory, and the on-disk file is unchanged. This helps keep the read close to `O(1)` because in-memory conversion is far faster than a disk write.

On `patch`, migration occurs in memory before the patch is applied. The patched document is then written to disk in the new schema format.

## Change Distribution

Each migration write is a normal patch. Therefore, it can affect precompiled searches, caches, and other derived data.

The `upgrade-agent` does not update those derived files directly. It relies on the normal change queue and `change-agent` to distribute those effects.

## Completion

A collection upgrade is fully distributed when every document in the collection has the current `$` schema/version marker and all resulting change queue entries have been processed by the `change-agent`.

The collection schema itself is considered advanced as soon as the `upgrade` command succeeds.
