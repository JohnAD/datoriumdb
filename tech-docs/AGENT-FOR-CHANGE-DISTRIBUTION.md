# Agent For Change Distribution

This document describes the background `change-agent` that distributes changes through a DatoriumDB database.

The agent is responsible for turning durable source-of-truth changes into the derived changes needed elsewhere in the database. This includes schema migration, precompiled search maintenance, cache updates, and other non-source-of-truth data.

## Purpose

DatoriumDB separates source-of-truth data from cached and derived data. That separation helps separate what does and does not need to affect other systems.

The background agent handles this distribution work after any source-of-truth change exists in any document.

## Queue

The agent runs inside the `datoriumdb` process through the in-process scheduler described in [LOCAL-ARCHITECTURE.md](LOCAL-ARCHITECTURE.md). Wake channels and periodic safety scans both drive work. This document assumes plain JSON text storage.

When the agent runs, it reads the `.changeQueue` directory under the collection storage location. This directory contains empty files with crafted names.

Each of the following creates a queue entry after its operation:

- a `create` command
- a `patch` command
- a `delete` command
- any patch action run by the `upgrade-agent` background agent

The format of the filename is `{change}__{collection}__{id}.queue`. The `{change}` part is one of `create`, `patch`, or `delete`. The file contains no data. If a queue file for the same collection, ID, and change already exists, it is left alone.

For example:

```text
patch__Movies__01KWD65CFQPEZS7H1WJE4MK990.queue
delete__Movies__01KWD65CFQPEZS7H1WJE4MK990.queue
```

The explicit `{change}` marker prevents the agent from having to infer deletion only from filesystem state. For deletes, the previous-document dotfile contains the old document and the live `{id}.json` file is absent.

The `change-agent` operates on one queue item at a time in whatever order the OS directory system provides. There is no guarantee of order. After the first free queue directory entry is parsed, the file is immediately renamed to `{change}__{collection}__{id}.taken`. The `change-agent` is not concerned with concurrency as duplicated effort should not cause problems.

The `change-agent` then reads the target document file and any previous-document dotfile in a non-binding manner. The previous-document dotfile has the form `.{id}.json`.

The `change-agent` is not concerned with each individual change that happened. It is concerned with the net difference between the oldest undistributed previous document and the current document.

After the `change-agent` is finished distributing the changes to other systems, it deletes the previous-document dotfile. After the dotfile cleanup is complete, it removes the `.taken` queue file.

If either file has already been deleted, the agent ignores that fact.

## Changes Made

To distribute changes, the agent considers every collection in the database, including the changed document's own collection.

It does the following for every collection:

### Cache Distribution

Cache distribution is handled through pending cache update work items, as described in [CACHE-UPDATES.md](CACHE-UPDATES.md).

When a source-of-truth document is created, patched, or deleted, the SOT-member uses the schemas from the establishment server to find `DatoriumCachedRef` fields that may point at the changed source collection.

The SOT-member then queues pending cache update work for the read members that may store affected cached summaries.

Read members apply those work items to existing local cache files. They do not scan source-of-truth documents looking for references, and they do not patch source-of-truth documents to remove lost references.

### Search Distribution

Search distribution computes precompiled search file changes for the changed document's own collection.

The `change-agent` reads current search definitions from establishment config under `/db/.config/{CollectionName}.search.{SearchName}.json`. Search result trees under the collection's `.search` directory are updated from those definitions. Each search definition is evaluated against both the previous document state, if present, and the current document state, if present.

For each search definition, the agent determines whether the document should appear in one or more search result files:

- If the previous document matched a search bucket, the document ID must be removed from that previous bucket.
- If the current document matches a search bucket, the document ID must be inserted into that current bucket.
- If both states map to the same bucket with the same sort values, no search file change is needed.

Search result file paths are derived from the search name and the clause values that define the result bucket. Path components derived from document values are encoded according to the search storage rules.

When inserting or updating a search result, the agent computes the item's `sort` values from the search definition. The search file stores a list of items containing both `sort` and `id`, so the search SOT can insert the changed document at the correct position without reading every referenced document.

The agent should treat search updates as idempotent. If the document ID is already in the correct search file with the correct sort values, no change is needed. If the ID appears in an old or incorrect search file, that entry is removed.

The previous-document dotfile is especially important for search distribution. Without it, the agent may know where the document belongs now, but not where the document used to belong.

The agent sends each affected search result update to the SOT machine for that search shard as a patch to the search result file.

The search SOT applies the patch, distributes the updated search result to that search shard's read-members, and then returns success to the agent.

Search file updates are written using the normal safe file update behavior and `#` version verification.

### Lost References

The `change-agent` does not remove references from source-of-truth documents when a referenced document is deleted.

Removing those references would mean derived-data maintenance is modifying source-of-truth fields, which can create loops and hidden source changes.

Instead, missing cached referenced documents are reported at read time. `cacheSummaries` returns a full summary record with a `null` revision (`#`) for referenced documents that cannot be resolved. Requested SOT summary fields are omitted from that record, leaving the application to decide how to handle the lost reference.

Direct references are not resolved by the database during reads. Smart clients are responsible for following direct references and reading those documents from the correct machines.

