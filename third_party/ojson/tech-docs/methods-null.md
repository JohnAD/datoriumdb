# Null Methods And Procedures

Null values represent explicit JSON `null`. In ojson, `null` means the field exists but its value is unknown.

See [`methods-common.md`](methods-common.md) for shared failure-handling, state, serialization, document I/O, and schema procedures.

## `NewNull() JSONValue`

Creates an explicit JSON `null` value.

Use `NewNull` when the document should contain an explicit unknown value. Do not use it to represent a missing field; missing fields are represented by `Void`.

```go
doc.Set("middle_name", ojson.NewNull())
```

## `IsNull() bool`

Reports whether a value is explicit JSON `null`, meaning an unknown value in ojson.

This should be separate from `IsMissing` so callers can distinguish absent data from known unknown data.

## Null And Known Values

`Null` is not considered known:

- `IsKnown` should return `false`
- `NotEmpty` should return `false`
- `IsEmpty` should return `true`

## Serialization

`Null` serializes as JSON `null`.

```go
fmt.Println(ojson.NewNull().ToJSON()) // null
```
