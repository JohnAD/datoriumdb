# Conventions

This document records naming and formatting conventions used by DatoriumDB.

## Collection

Collection names are UTF-8 strings and cannot contain whitespace, using the UTF-8 definition of whitespace.

If spacing is needed, underscores are suggested as a substitute.

Collection names cannot contain two underscores in a row.

The first character of a collection name must be a letter.

Capitalization should follow the human language the name is written in. In English, a collection name is a title, so it should follow English title capitalization rules.

Collection names should usually be plural because a collection represents a set of documents. For example, prefer `Movies` over `Movie`.

## Fields

For field names in Latin-script languages such as English or Spanish, prefer `camelCase`.

Field names may contain whitespace when whitespace helps with clarity.

For field names written in other scripts or language systems, follow the natural naming and capitalization conventions of that language.

## IDs

Document IDs can be any sequence of letters and numbers, with no whitespace.

Document IDs are limited to 255 runes.

Document IDs cannot contain punctuation other than underscore, period, and dash.

When the database creates document IDs internally, it uses simple ULIDs. See the [ULID specification](https://github.com/ulid/spec).

The first period has special meaning for CRC16 sharding. If the ID has no period, the whole ID is used for sharding. If the ID has a period, only the part before the first period is used for sharding.

Periods in the first six positions are ignored for sharding prefix detection.

## Document References

Direct document references should use this string format:

```text
@__{collection}__{id}
```

Cached document summary references should use this string format:

```text
@@__{collection}__{id}
```

In document content, these references are stored as strings. The schema mechanism for declaring direct references and cached summary references is still to be determined.
