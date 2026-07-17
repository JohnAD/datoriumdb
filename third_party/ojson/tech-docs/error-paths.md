# Error Paths

Ojson errors that refer to a location inside a JSON document or schema should include a path.

Paths use a period-separated list of segments:

- object field names are written as quoted JSON strings
- array indexes are written as base-10 integers
- segments are joined with `.`

Example:

```text
"pet"."ratings".4
```

This path means:

1. the `pet` field in the root object
2. the `ratings` field in the `pet` object
3. index `4` in the `ratings` array

## Root Path

The root value should use an empty path when no child segment exists.

When an error message needs visible root text, use `$`:

```text
$
```

## Object Fields

Object fields should always be quoted as JSON strings, even when they contain only simple identifier characters.

```text
"name"
"pet"."name"
"pet"."contact.email"
```

Quoting every field avoids ambiguity when field names contain periods, spaces, digits, quotes, or other punctuation.

For example, this path has two object fields:

```text
"pet"."contact.email"
```

The second field name is `contact.email`; it is not two nested fields.

## Array Indexes

Array indexes are zero-based base-10 integers.

```text
"pets".0
"pets".12."name"
```

Indexes are not quoted. A quoted numeric segment is an object field name, not an array index:

```text
"items"."4"
```

This means the object field named `4`, not array index `4`.

## Error Messages

Errors should include the path and a concise reason.

Examples:

```text
"pet"."age": expected integer number
"pet"."ratings".4: expected number, got string
"pet"."contact_email": invalid email format
"tags".2: string length is below min_length 1
```

Schema compilation errors should also use paths when the error refers to a schema entry:

```text
"pet"."age": min must be less than or equal to max
"pet"."height_units": enum is only supported for string schemas
```

## Missing Paths

When an error is caused by a missing object field, the path should refer to the missing field:

```text
"pet"."name": required field is missing
```

When an error is caused by an out-of-range array index, the path should include the requested index:

```text
"ratings".8: array index out of range
```

## Implementation Notes

Path rendering should reuse JSON string escaping for object field segments. This keeps paths readable while preserving exact field names.

Path strings are for diagnostics. They are not intended to be a query language and should not support wildcards, filters, or recursive descent.
