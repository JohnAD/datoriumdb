# Filesystem Storage

This document describes how DatoriumDB stores files when using filesystem-backed storage.

## Default Storage

The database configuration has a default storage location and method.

For example:

```text
location: $/db
method: plain-text-json
```

In this example, `$` means the root of the current repository, and `$/db` means the `db` directory under that root.

The `plain-text-json` method stores documents as git-trackable JSON files.

## Collection Storage

Each collection can have its own storage location and method.

If a collection does not specify its own storage location or method, it uses the database default.

The default destination for each collection is the collection/schema name under the default storage location. For example, the default destination for the `Movies` collection is `$/db/Movies`.

This allows the database to start with one simple default while still leaving room for collections to move to different storage methods or locations later.

## Collection Layout

Each document in a collection is stored as:

```text
{id}.json
```

For example:

```text
$/db/Movies/01KWD65CFQPEZS7H1WJE4MK990.json
```

When a document is patched or deleted, the previous version can be temporarily stored as:

```text
.{id}.json
```

For example:

```text
$/db/Movies/.01KWD65CFQPEZS7H1WJE4MK990.json
```

This previous-document file is temporary non-source-of-truth data used by the `change-agent`. It lets the agent compare the previous document state with the current document state so it can remove old search entries, update caches, and clean up derived data.

If a previous-document dotfile already exists when another patch occurs, it is left alone. This preserves the oldest undistributed previous version. The `change-agent` can then compare that oldest pending previous version with the latest current version and distribute the net change.

After the `change-agent` successfully distributes the change, it deletes the previous-document dotfile.

Delete operations use the previous-document dotfile plus an explicit delete queue entry. The absence of `{id}.json` alone is not used as the only signal that a delete occurred.

Change queue entries are stored in `.changeQueue` under the collection storage location. Queue filenames include the collection, document ID, and change type:

```text
{change}__{collection}__{id}.queue
```

For example:

```text
$/db/Movies/.changeQueue/delete__Movies__01KWD65CFQPEZS7H1WJE4MK990.queue
```

Collection schemas are stored in the database config directory, not under each collection directory.

The current schema for a collection is stored as:

```text
$/db/.config/{CollectionName}.schema.json
```

For example:

```text
$/db/.config/Movies.schema.json
```

Each schema version is also preserved:

```text
$/db/.config/{CollectionName}.schema.{ver}.json
```

For example:

```text
$/db/.config/Movies.schema.1.json
```

Keeping all schema files in `/db/.config` gives the establishment server one place to read the schemas it serves to clients and machines. Keeping versioned schema files preserves schema history, which is expected to be important for later migration and compatibility work.

Database-wide config files in `/db/.config` use a leading `__` prefix to avoid collisions with collection config files. For example, general database config is stored in `__general.json`, server definitions are stored in `__servers.json`, `shardMap.default` is stored in `__shard-map.json`, and public auth trust material is stored in `__auth.json`.

Source-of-truth files and directories use normal names. Non-source-of-truth files and directories are prepended with a period so users can decide whether to track them in Git.

Search definitions are stored under `/db/.config` as `{CollectionName}.search.{SearchName}.json`. See [SEARCH-DEFINITION-SCHEMA.md](SEARCH-DEFINITION-SCHEMA.md).

Precompiled search *result* trees are stored under `.search` in the collection directory:

```text
$/db/Movies/.search/{searchname}
```

For example, encoded result files live under:

```text
$/db/Movies/.search/byReleasedGenre/{encodedPath}/matches.json
```

Cached data for documents is stored under `.cache`:

```text
$/db/Movies/.cache/{SourceCollection}/{sourceDocId}.json
```

Each read server stores at most one cached summary file for a given source collection and source document ID. The cache path does not include the local field path or declaring collection; the read server uses its schemas and local documents to interpret the shared cached summary.

This cache layout also leaves room for future sharding. For example, `$/db/Movies/.cache/People/01KWD65CFQPEZS7H1WJE4MK990.json` can store an off-server cached summary for a `People` document referenced by local `Movies` documents.

Pending cache update work is stored under `.pendingCacheUpdates` on the SOT-member for the changed source collection:

```text
$/db/Movies/.pendingCacheUpdates/{readServerName}.{docId}.json
```

Read members apply these work items to their local cache files and delete the work items through the SOT-member API after durable success.

## File Update Safety

On compatible filesystems, live readers can safely continue reading a file while DatoriumDB replaces that file.

The key assumption is that files have both a universal file ID and a filename. The filename is changeable. Processes that already have a file open are attached to the file ID, not merely to the current filename.

For example, when DatoriumDB wants to update `foo.json`, it does not rewrite `foo.json` in place. Instead, it:

1. Creates a new temporary file in the same directory as the target file.
2. Writes the full new file content to that temporary file.
3. Ensures the temporary file is flushed.
4. Issues a filesystem rename operation onto the target filename, such as `foo.json`.

Same-directory rename is required so the replace is atomic on ordinary POSIX filesystems. The temporary file has a new file ID and a quasi-random filename while it is being written. Once the rename occurs, new processes opening `foo.json` attach to the new file ID.

Processes that already had the old `foo.json` open continue reading the old file ID. They do not see the file change underneath them. The old file ID is moved to the operating system's equivalent of a trash or unlinked state, but it is not removed while existing readers still have it open.

This is similar to DNS name resolution: a program does not connect to `google.com` directly; it connects to the IP address currently pointed to by that DNS name. Likewise, new readers attach to the file ID currently pointed to by `foo.json`.

## Version Verification

DatoriumDB creates a new `#` version field whenever it updates a document, search index file, cache file, or other versioned database file.

After moving the temporary file into place, DatoriumDB immediately reads the target filename again.

- If the file has the expected new `#` value, the update succeeded.
- If the file has a different `#` value, then something went wrong or another process updated the file just after the current process moved its file into place.

If a different `#` value is found, DatoriumDB checks whether the intended patch is already reflected in the file.

- If the patch is already reflected, the operation can still be considered successful.
- If the patch is not reflected, DatoriumDB waits a random number of milliseconds and retries the patch.

This conflict behavior is a major reason DatoriumDB supports patches instead of whole-document updates. Patches can be retried against the latest file state with a clear intended change. Blind whole-document updates make this much harder and risk overwriting unrelated changes.

## MVP Scope

The MVP uses filesystem-backed `plain-text-json` storage.

Later versions may support additional methods such as BSON, but those methods are outside the MVP.
