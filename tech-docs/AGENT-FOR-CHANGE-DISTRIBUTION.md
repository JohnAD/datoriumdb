# Agent For Change Distribution

This document describes the background `change-agent` that distributes changes through a DatoriumDB database.

The agent is responsible for turning durable source-of-truth changes into the derived changes needed elsewhere in the database. This includes schema migration, precompiled search maintenance, cache updates, and other non-source-of-truth data.

## Purpose

DatoriumDB separates source-of-truth data from cached and derived data. That separation helps separate what does and does not need to affect other systems.

The background agent handles this distribution work after any source-of-truth change exists in any document.

## Queue

The agent runs periodically by any mechanism supported by the database. For this document, assume the agent runs once per minute from a cron job. This document also assumes plain JSON text storage.

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

It looks for the collection subdirectory for `.cache/{changed-collection}/{id}.json`. If the file or subdirectories do not exist, this step is complete.

The current schema for this collection is then read and parsed for `DatoriumCachedRef` string fields. This determines which SOT fields should be cached.

If any of those references do not have a `summary` restriction, then all SOT fields are used.

Note: a well-written schema SHOULD have a summary array. But it is not technically required.

If a reference is locked down to a different collection than the changed document, it is ignored.

Then all summaries are combined to create an accumulative list of needed summary fields.

If there are no open cache references and no limited cache references looking at the changed collection, the `.cache/{changed-collection}/{id}.json` file is deleted.

Otherwise, `.cache/{changed-collection}/{id}.json` is updated with the correct fields using an internal patch call. While this is technically a patch, it is a terminal operation because this change is not automatically distributed anywhere else.

### Search Distribution

Search distribution updates precompiled search files for the changed document's own collection.

The `change-agent` reads the current search settings under the changed collection's `.search` directory. Each search definition is evaluated against both the previous document state, if present, and the current document state, if present.

For each search definition, the agent determines whether the document should appear in one or more search result files:

- If the previous document matched a search bucket, the document ID must be removed from that previous bucket.
- If the current document matches a search bucket, the document ID must be inserted into that current bucket.
- If both states map to the same bucket with the same sort values, no search file change is needed.

Search result file paths are derived from the search name and the clause values that define the result bucket. Path components derived from document values are encoded according to the search storage rules.

When inserting or updating a search result, the agent computes the item's `sort` values from the search definition. The search file stores a list of items containing both `sort` and `id`, so the agent can insert the changed document at the correct position without reading every referenced document.

The agent should treat search updates as idempotent. If the document ID is already in the correct search file with the correct sort values, no change is needed. If the ID appears in an old or incorrect search file, that entry is removed.

The previous-document dotfile is especially important for search distribution. Without it, the agent may know where the document belongs now, but not where the document used to belong.

Search file updates are written using the normal safe file update behavior and `#` version verification.

### Lost References

The `change-agent` does not remove references from source-of-truth documents when a referenced document is deleted.

Removing those references would mean derived-data maintenance is modifying source-of-truth fields, which can create loops and hidden source changes.

Instead, missing referenced documents are reported at read time. `cacheSummaries` and `live` return `null` for referenced documents that cannot be resolved, leaving the application to decide how to handle the lost reference.

