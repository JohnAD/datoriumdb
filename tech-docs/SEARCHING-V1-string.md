# Searching V1: String

This document describes v1 search behavior for schema fields with `kind: string`.

Only schema-defined source-of-truth fields are searchable.

## Supported Clauses

- `equals` with a field-path constant and value constant
- `equals` with a field-path constant and value variable
- `hashEquals` with a field-path constant and value variable
- `in` with a field-path constant, value constants, and truth variable
- `startsWith` with a field-path constant, value constant, and truth variable
- `containsText` with a field-path constant, value constant, and truth variable
- `exists` with a field-path constant and truth variable

## String Comparison

String comparison should be byte-for-byte unless a later version explicitly defines normalization or collation behavior.

## `equals` With A Constant Value

```text
{field: {field_path_constant}, op: equals, value: {value_constant}, truth: {truth_variable}}
```

`equals` matches one exact string value.

For example:

```text
{field: /status, op: equals, value: released, truth: $isReleased}
```

The truth value is a live query variable:

- `true` means the string must equal the constant value.
- `false` means the string must not equal the constant value.

## `equals` With A Variable Value

```text
{field: {field_path_constant}, op: equals, value: {value_variable}}
```

For example:

```text
{field: /status, op: equals, value: $status}
```

This `equals` form matches documents where the string equals a value provided by the live query.

For v1, variable-value `equals` is limited to 63 runes because matching values are encoded into filesystem path components.

When a document is evaluated for this clause, if the field value is larger than 63 runes, the field is skipped and no match of any kind is recorded for that clause.

A direct search for a larger string will therefore fail to match.

Variable string values are encoded as uppercase hex from the string's UTF-8 bytes. Use `A` through `F`, not `a` through `f`.

An empty string encodes as the literal `empty`.

For longer live string values, use `hashEquals`.

There is currently no variable-value "does not equal" clause.

## `hashEquals` With A Variable Value

```text
{field: {field_path_constant}, op: hashEquals, value: {value_variable}}
```

For example:

```text
{field: /description, op: hashEquals, value: $description}
```

`hashEquals` matches documents by comparing a CRC32 hash of the stored string with a CRC32 hash of the live query value.

The live query value can be any length.

CRC32 hash values are encoded as uppercase hex with a `0x` prefix.

Only the hash is stored. No extra collision-check metadata is stored for `hashEquals`.

Because this is a 32-bit hash comparison, there is a small false-positive risk. The chance of an accidental hash collision is approximately 1 in 4,294,967,296 for a single unrelated comparison.

There is currently no variable-value "does not hash equal" clause.

## `in` With Constant Values

```text
{field: {field_path_constant}, op: in, value: [{value_constants}], truth: {truth_variable}}
```

`in` matches one of several exact string values for the same field.

This is the v1 way to express same-field alternatives without supporting arbitrary OR clauses.

For example:

```text
{field: /status, op: in, value: [released, archived], truth: $isVisibleStatus}
```

The truth value is a live query variable:

- `true` means the string must equal one of the constant values.
- `false` means the string must not equal any of the constant values.

There is currently no variable-value `in` clause for strings.

## `startsWith`

```text
{field: {field_path_constant}, op: startsWith, value: {value_constant}, truth: {truth_variable}}
```

`startsWith` matches strings that start with a constant prefix.

For example:

```text
{field: /title, op: startsWith, value: The, truth: $startsWithThe}
```

The truth value is a live query variable:

- `true` means the string must start with the constant value.
- `false` means the string must not start with the constant value.

## `containsText`

```text
{field: {field_path_constant}, op: containsText, value: {value_constant}, truth: {truth_variable}}
```

`containsText` matches strings that contain a constant substring.

This is not full-text search. It is a precompiled constant substring check.

For example:

```text
{field: /title, op: containsText, value: Matrix, truth: $mentionsMatrix}
```

The truth value is a live query variable:

- `true` means the string must contain the constant value.
- `false` means the string must not contain the constant value.

## `exists`

```text
{field: {field_path_constant}, op: exists, value: {truth_variable}}
```

For example:

```text
{field: /title, op: exists, value: $hasTitle}
```

`exists` matches documents based on whether the string field is present.

An `exists` clause may include the constant option `hideNulls: true` to treat `null` as absent for that clause.

The truth value is a live query variable:

- `true` means the field must exist.
- `false` means the field must not exist.

Because the truth variable handles both cases, v1 string search does not need a separate `missing` clause.

## Encoding Note

When a truth variable determines an encoded clause directory, the directory name is the literal `true` or `false`.

## Not Supported In V1

V1 string searching does not support:

- `regex`
- locale-aware collation
- case-insensitive matching
- normalized Unicode matching
