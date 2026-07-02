# Updating a Schema

After a collection is created with its initial schema, all later schema versions are created with an update list similar to JSON Patch.

For example:

```text
{
  from: 0,
  to: 1,
  new_ver_id: 01KWHM7R7D3T50G0GH6XN4CRZT,
  updates: [
    {op: add, path: /rating, value: 0, schema: {kind: number, default: 0}}
  ]
}
```

This update list is not exactly RFC 6902. A schema update manipulates both the schema and the underlying documents at the same time.

A collection is created for the first time with the `establish` command. After that, the collection is advanced with the `upgrade` command.

## Upgrade Atomicity

The database is intentionally strict about schema update patches.

The `upgrade` command validates the update patch before any document is changed. If the update patch is invalid, the `upgrade` command fails and no documents are modified.

If the update patch succeeds, the collection schema is permanently advanced. From that point forward, documents are migrated to the new schema by one of two paths:

- the background agent updates documents as it works through the collection
- individual documents are updated on-the-fly when the system responds to access requests for those documents

After the schema is advanced, document conversion should not fail. Every fallback and border case must be defined so each document can be brought forward safely.

## Database-Owned Metadata

Database-owned metadata fields are not patchable by schema update operations.

This includes:

- `!`, the document ID
- `$`, the schema/version marker
- `#`, the document version

The document ID is immutable. To use a different ID, create a new document with the desired ID and remove the old document.

## Background Migration

After a schema is upgraded, the database's background agent works through the collection and updates documents to the new schema.

Each document change made by the background agent is a normal patch. Like any other patch, it can affect indexes, precompiled searches, and cached data.

The strict separation between source-of-truth fields and all other data helps prevent loops during this process.

## Add

The `add` operation adds a field to the schema and applies a value to the affected documents.

```text
{op: add, path: /a/b/c, value: ["foo", "bar"], schema: {kind: array, items: {kind: string}}}
```

If `value` is present, that value is added to affected documents.

If `value` is not present and the new field is required, use the default, then the failover value, then `null` if nullable, then the empty value.

If `value` is not present and the new field is not required, the new field is not added to existing documents.

If a conflicting non-schemed extra field already exists at the added path, the preexisting extra field is discarded from the document.

## Import

The `import` operation behaves like `add` and adds a source-of-truth schema field.

```text
{op: import, path: /a/b/c, value: ["foo", "bar"], schema: {kind: array, items: {kind: string}}}
```

If the added field matches a preexisting non-schemed extra field, and that extra field's value matches the new schema, the extra field's value is imported and used instead of the supplied `value`.

If the matching extra field's value does not match the new schema, the extra field is removed and its value is ignored. The operation then follows the same value selection rules as `add`.

## Remove

The `remove` operation removes a field from the schema and all affected documents.

The path can point to an array field, but it cannot reference an array index. Paths such as `/reviews/4` or `/msgs/6/username` are not valid remove paths.

```text
{op: remove, path: /a/b/c}
```

## Abandon

The `abandon` operation behaves like `remove`, except the field is recategorized as a non-schemed extra field instead of being removed from the document.

```text
{op: abandon, path: /a/b/c}
```

After `abandon`, the field is no longer source-of-truth. It is not searchable or cacheable.

The field may move in the object's visible order because schema-defined fields and extra fields are ordered separately, but the field still exists in the document as extra data.

## Replace

The `replace` operation applies the same replacement value to all affected documents. The replacement value must match the resulting schema.

```text
{op: replace, path: /a/b/c, value: 42}
```

## Move

The `move` operation moves a value from one path to another. It can also be used to rename a field.

```text
{op: move, from: /a/b/c, path: /a/b/d}
```

If a conflicting non-schemed extra field already exists at the destination path, the preexisting extra field is discarded from the document.

## Copy

The `copy` operation copies a value from one path to another.

```text
{op: copy, from: /a/b/c, path: /a/b/e}
```

## Convert

The `convert` operation has no direct RFC 6902 equivalent.

It converts the target value's schema.

```text
{op: convert, path: /a/b/c, schema: {kind: number}, failover: 0}
```

If `value` is provided, the current value is ignored and replaced wholesale under the new schema.

```text
{op: convert, path: /a/b/c, schema: {kind: object, children: []}, value: {}}
```

Some conversions force a value to be removed or replaced in some documents. When that happens, the following **removal logic** is used:

- If there is a default, replace the value with the default.
- Otherwise, if the `convert` operation has a failover value, use that value.
- Otherwise, if the field is nullable, use `null`.
- Otherwise, if the value is not required, remove the key/value pair.
- Otherwise, replace the value with the empty value for its kind.

Empty values by kind:

- `string`: `""`
- `number`: `0`
- `boolean`: `false`
- `array`: `[]`
- `object`: `{}`

### Schema Change Notes

- **Adds nullable**: existing document values do not change, but future values can be `null`.
- **Removes nullable**: current non-null values do not change. Current null values follow removal logic.
- **Adds required**: if the value does not exist yet, use the default, then the failover value, then `null` if nullable, then the empty value.
- **Removes required**: existing document values do not change, but future values can be omitted.
- **Adds format**: if the current value does not conform, removal logic is followed. If the schema change adds `format`, the `failover` parameter is required.
- **Removes format**: current values do not change.
- **Adds array item definition**: each individual item is converted by the rules in this document.
- **Removes array item definition**: current values do not change.

### Kind Changes

If a schema update changes a field's `kind`, the current value is converted according to the new kind.

- New `boolean` from any old kind: `false` if the value is empty, null, or missing; otherwise `true`.
- New `string` from any old kind: the string equivalent of the JSON text. For example, `true` becomes `"true"` and `[]` becomes `"[]"`.
- New `number` from `boolean`: `false` becomes `0`; `true` becomes `1`.
- New `number` from `string`: characters are converted to their numeric equivalent until an error is detected. If no number results, use the number empty value. `"24.3blah"` becomes `24.3`, `"foobar"` becomes `0`.
- New `number` from `array`: the array length is stored.
- New `number` from `object`: the count of existing keys is stored, including valued keys and null keys. Missing keys are skipped.
- New `array` from any old kind: the current value is placed as the first item in the array if allowed. Otherwise, use an empty array.
- New `object` from any old kind: the current key/value is embedded into the object if allowed by the schema. Otherwise, use an empty object. For example, `{foo: bar}` becomes `{foo: {foo: bar}}` unless `foo` or `bar` are not allowed by the new root `foo` schema. If they are not allowed, the result is `{foo: {}}`.

If you want the new field to have a different or empty value and ignore the previous value, provide `value` on the `convert` operation or consider using the `replace` command instead.
