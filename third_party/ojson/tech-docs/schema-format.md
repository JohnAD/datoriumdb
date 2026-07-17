# Schema Format

An ojson schema is a JSON document that describes the expected kind and canonical order of another JSON document. It is intentionally smaller than JSON Schema. Its purpose is ordered document handling, defaults, required fields, basic kind checking, and a small set of common scalar validations.

## Root Schema

The root schema describes the entire JSON text. The root usually has `kind: "object"`, but an array or scalar root is possible when the document itself is not an object.

```json
{
  "kind": "object",
  "children": []
}
```

The root schema does not need a `name` because it is not a field inside a parent object.

## Schema Entry Fields

Ojson schemas are tree-shaped. They do not support recursion, references, or reusable object definitions. If the same object shape is needed in multiple places, define that shape inline at each location.

### `kind`

Required. Defines the JSON kind for the value.

Supported values:

- `object`
- `array`
- `string`
- `number`
- `boolean`
- `null`

There is no `any` kind. A value either has a supported kind or it is outside the schema model. The schema also does not support union types, except for nullable fields through `nullable: true`.

### `name`

Required for object children. Not used on the root schema.

The `name` is the JSON object field name:

```json
{
  "name": "height_units",
  "kind": "string"
}
```

### `description-{lang}`

Optional. Provides localized human-readable documentation for any schema entry, including the root schema.

`{lang}` is an ISO 639 language code. The two-letter form is encouraged when it is precise enough, such as `description-en`. Use a more specific language code only when further refinement is needed.

Multiple description languages can be provided on the same schema entry:

```json
{
  "kind": "object",
  "description-en": "A pet record.",
  "description-es": "Un registro de mascota."
}
```

The value must be a string. Any string is allowed, but CommonMark formatting is encouraged for longer descriptions.

### `children`

Optional. Used by object schemas to define ordered fields.

Each child is another schema entry. The order of entries in `children` is the canonical order for the object.

```json
{
  "name": "pet",
  "kind": "object",
  "children": [
    { "name": "name", "kind": "string" },
    { "name": "age", "kind": "number" }
  ]
}
```

### `default`

Optional. Provides a value to insert when a field is missing from the source JSON.

The default must match the schema entry's `kind`.

```json
{
  "name": "height_units",
  "kind": "string",
  "default": "inches"
}
```

Examples by kind:

```json
{ "name": "name", "kind": "string", "default": "unknown" }
```

```json
{ "name": "age", "kind": "number", "default": 0 }
```

```json
{ "name": "safe", "kind": "boolean", "default": true }
```

```json
{ "name": "middle_name", "kind": "string", "nullable": true, "default": null }
```

Object defaults should be used carefully. Prefer child defaults when only specific fields need canonical values.

### `required`

Optional. When set to `true`, the field must exist in the source JSON unless a valid default is available.

```json
{
  "name": "name",
  "kind": "string",
  "required": true
}
```

If a required field is missing and has no default, reading with the schema should fail.

### `nullable`

Optional. When set to `true`, the field may be explicit JSON `null` in addition to its declared `kind`.

```json
{
  "name": "email",
  "kind": "string",
  "nullable": true
}
```

`nullable` is the only supported union-like behavior. For example, `{ "kind": "string", "nullable": true }` means the value may be a string or `null`. The schema should not support broader unions such as string-or-number.

Defaults for nullable fields must still be valid for the declared `kind` or be `null`.

### `min` And `max`

Optional. Used by number schemas to define inclusive numeric bounds.

```json
{
  "name": "age",
  "kind": "number",
  "integer": true,
  "min": 0,
  "max": 130
}
```

`min` and `max` values must be valid JSON numbers. Validation should compare numeric value, not string spelling.

### `integer`

Optional. Used by number schemas. When set to `true`, the number must represent an integer value.

```json
{
  "name": "count",
  "kind": "number",
  "integer": true
}
```

Scientific notation is allowed when it resolves to an integer, such as `1E3`. Decimal spellings with fractional precision, such as `1.5`, should fail integer validation.

### `enum`

Optional. Used by string schemas to restrict the value to one of a fixed set of strings.

```json
{
  "name": "status",
  "kind": "string",
  "enum": ["draft", "active", "archived"]
}
```

`enum` is only supported for strings. It should not be used to model general union types.

### `min_length` And `max_length`

Optional. Used by string schemas to define inclusive string length bounds.

```json
{
  "name": "display_name",
  "kind": "string",
  "min_length": 1,
  "max_length": 80
}
```

Strings are UTF-8 text, not arbitrary byte arrays. A malformed string cannot be used and should fail schema validation. Length should be measured in Unicode code points, not bytes.

### `format`

Optional. Used by string schemas for HTML-aligned built-in validations and application-registered semantic formats.

Built-in values:

- `email`
- `tel`
- `url`

```json
{
  "name": "website",
  "kind": "string",
  "format": "url"
}
```

These formats should be practical validations, not full global truth tests. For example, email validation should reject clearly invalid email field values, but it should not attempt DNS lookup or mailbox verification.

Applications may also register custom formats through an explicit `StringFormatRegistry` supplied when compiling a schema. Custom format names conventionally begin with an uppercase letter, such as `Time`, but uppercase is a convention rather than a parser requirement. Built-in names `email`, `tel`, and `url` remain reserved.

When a schema uses a custom format:

1. the format must be present in the registry supplied to that schema compilation
2. the registry validator is snapshotted into the compiled schema
3. later registry mutations do not change already compiled schemas
4. string defaults and document values are validated through the registered validator

### `custom`

Optional. Provides opaque application metadata for any schema entry, including the root.

`custom` may contain any JSON value. `ojson` preserves the value when compiling schemas and does not interpret it. Applications can use `custom` for project-specific policy or tooling metadata without requiring `ojson` to understand that metadata.

```json
{
  "name": "director",
  "kind": "string",
  "format": "DatoriumDirectRef",
  "custom": {
    "collection": "people",
    "indexed": true
  }
}
```

Unknown schema fields other than `custom` and supported `description-{lang}` keys should still be rejected. A single known `custom` field keeps typo detection intact while still allowing arbitrary extension data.

### `items`

Optional. Used by array schemas to describe item type.

```json
{
  "name": "tags",
  "kind": "array",
  "items": {
    "kind": "string"
  }
}
```

Array typing is allowed but not required. If `items` is omitted, the array may contain mixed JSON value kinds. If `items` is present, every item must match the item schema. The item schema can use scalar validations such as `min`, `max`, `integer`, `enum`, `min_length`, `max_length`, `format`, and `nullable`.

## Complete Example

```json
{
  "kind": "object",
  "description-en": "A schema for pet store record of a pet and other meta data.",
  "children": [
    {
      "name": "pet",
      "kind": "object",
      "description-en": "Information about the pet.",
      "children": [
        {
          "name": "name",
          "kind": "string",
          "description-en": "The pet's display name.",
          "required": true,
          "min_length": 1,
          "max_length": 80
        },
        {
          "name": "age",
          "kind": "number",
          "integer": true,
          "min": 0
        },
        {
          "name": "height",
          "kind": "number",
          "default": 0
        },
        {
          "name": "height_units",
          "kind": "string",
          "enum": ["inches", "centimeters"],
          "default": "inches"
        },
        {
          "name": "contact_email",
          "kind": "string",
          "format": "email",
          "nullable": true
        },
        {
          "name": "safe",
          "kind": "boolean",
          "default": true
        }
      ]
    }
  ]
}
```

## Read Behavior

When JSON is read with a schema:

1. the source JSON is parsed
2. the schema is parsed
3. known fields are matched by name
4. known fields are validated against their schema kind
5. nullable fields are allowed to contain explicit JSON `null`
6. scalar validations are applied
7. missing fields with defaults are inserted
8. missing required fields without defaults cause an error
9. known fields are stored in schema order
10. unknown fields are preserved after known fields

Source field order is accepted even when it differs from schema order. The stored document uses schema order after normalization.

Validation errors should include diagnostic paths using the format described in [`error-paths.md`](error-paths.md).

## Write Behavior

When JSON is written after schema-backed editing, serialization should preserve the normalized order already present in memory. The writer should not need to rediscover schema ordering if document mutation already enforces it.

Pretty printing is recommended for schema-backed files because it makes field ordering visible and reviewable.

## Unknown Fields

Fields that appear in the JSON document but not in the schema should be retained after schema-defined fields.

For example, with schema fields `name` and `age`, this source:

```json
{
  "nickname": "Whiff",
  "age": 3.2,
  "name": "Whiffles"
}
```

should normalize to:

```json
{
  "name": "Whiffles",
  "age": 3.2,
  "nickname": "Whiff"
}
```

This keeps the schema authoritative without discarding data the application may not yet understand.

## Unsupported Validation

The ojson schema format does not attempt to cover every validation feature from JSON Schema.

Supported validation is intentionally limited to:

- number `min`, `max`, and `integer`
- string `enum`
- string `min_length`, `max_length`, and `format` values of `email`, `tel`, `url`, or an application-registered format
- optional opaque metadata through `custom`
- optional array item schemas through `items`
- nullable values through `nullable: true`

Keep more advanced concerns in application validation unless the schema format is intentionally expanded:

- regular expressions
- custom string formats beyond `email`, `tel`, and `url`
- union types other than nullable
- arbitrary `any` values
- object property patterns
- recursive schemas, references, or reusable definitions
- enum values for non-string kinds
- cross-field validation

The library should never perform cross-field validation. It should also never support general union types beyond `nullable`.

## Schema Evolution

Schema changes can create large diffs if they reorder existing fields. To keep Git history readable:

- append new fields when possible
- avoid reordering established fields without a migration reason
- add defaults for fields that should appear in every normalized document
- avoid marking newly added fields as required until existing documents can satisfy them
- normalize documents in a dedicated change so schema migration diffs are easy to review
