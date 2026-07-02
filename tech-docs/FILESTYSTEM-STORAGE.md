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

The collection schema is stored in a `settings` subdirectory:

```text
$/db/Movies/settings/schema.json
```

The current schema is also stored with its version number:

```text
$/db/Movies/settings/schema.{ver}.json
```

For example:

```text
$/db/Movies/settings/schema.1.json
```

Keeping versioned schema files preserves schema history, which is expected to be important for later migration and compatibility work.

Source-of-truth files and directories use normal names. Non-source-of-truth files and directories are prepended with a period so users can decide whether to track them in Git.

Precompiled search files are stored under `.search`:

```text
$/db/Movies/.search/{searchname}
```

Each search stores its settings in the search directory:

```text
$/db/Movies/.search/{searchname}/search-settings.json
```

The current search settings are also stored with their version number:

```text
$/db/Movies/.search/{searchname}/search-settings.{ver}.json
```

For example:

```text
$/db/Movies/.search/byReleasedGenre/search-settings.1.json
```

The unversioned `search-settings.json` file lets everyday use load the current search settings with a single known file read rather than searching for the latest versioned file.

Cached data for documents is stored under `.cache`:

```text
$/db/Movies/.cache/{collection}
```

This cache layout also leaves room for future sharding. For example, `$/db/Movies/.cache/Movies` can store cached off-server summaries for the `Movies` collection.

## File Update Safety

On compatible filesystems, live readers can safely continue reading a file while DatoriumDB replaces that file.

The key assumption is that files have both a universal file ID and a filename. The filename is changeable. Processes that already have a file open are attached to the file ID, not merely to the current filename.

For example, when DatoriumDB wants to update `foo.json`, it does not rewrite `foo.json` in place. Instead, it:

1. Creates a new temporary file in the operating system's `/tmp` area.
2. Writes the full new file content to that temporary file.
3. Ensures the temporary file is saved.
4. Issues a filesystem move operation to move the temporary file to the target directory and filename, such as `foo.json`.

The temporary file has a new file ID and a quasi-random filename while it is being written. Once the move occurs, new processes opening `foo.json` attach to the new file ID.

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
