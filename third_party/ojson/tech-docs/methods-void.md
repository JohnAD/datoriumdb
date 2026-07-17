# Void Methods And Procedures

Void values represent absence. `Void` is not JSON `null`, and it is never serialized into JSON text.

See [`methods-common.md`](methods-common.md) for shared failure-handling, state, serialization, document I/O, and schema procedures.

## `NewVoid() JSONValue`

Creates or returns the ojson absence marker.

`Void` is useful internally and for traversal results. It should not be serialized into JSON text.

`NewVoid` is not a data constructor in the same sense as `NewString`, `NewNumber`, or `NewNull`. Use it when code needs an explicit ojson absence value. Do not use it when the JSON document should contain a field or item; use `NewNull` for an explicit unknown value.

## `IsMissing() bool`

Reports whether a value is `KindVoid`.

```go
if doc.Get("location").IsMissing() {
    fmt.Println("There is no location.")
}
```

## `IsVoid() bool`

Reports whether a value is `KindVoid`.

`IsVoid` is an alias for `IsMissing`. Use `IsVoid` when code is explicitly testing the value kind, and `IsMissing` when code is describing traversal or lookup absence.

```go
if doc.Get("location").IsVoid() {
    fmt.Println("location is absent")
}
```

## Failed Traversal

Plain traversal methods should return `Void` on failure:

- object `Get` returns `Void` when the receiver is not an object, the key is missing, or the stored field value is `Void`
- array `At` returns `Void` when the receiver is not an array or the index is out of range
- plain number constructors return `Void` when the input cannot become a valid number

`Void` makes chained traversal readable while still allowing absence checks.

```go
city := doc.Get("location").Get("postal_city").GetString("Location city is missing")
```

## Void In Mutation APIs

Public mutation APIs should avoid storing `Void` as content.

- object `Set(key, NewVoid())` should remove the field or leave it absent
- object `SetTry(key, NewVoid())` should return an error
- array `Append(NewVoid())` and `Prepend(NewVoid())` should leave the array unchanged
- array `AppendTry(NewVoid())`, `PrependTry(NewVoid())`, and `InsertAtTry(index, NewVoid())` should return errors
- array `Compress()` should remove any `Void` items that already exist

These rules keep `Void` as absence even if lower-level code can still create a `Void` value.

## Void And Known Values

`Void` is not considered known:

- `IsKnown` should return `false`
- `NotEmpty` should return `false`
- `IsEmpty` should return `true`

## Serialization

`Void` values are never expressed into JSON text. If an object field is `Void`, it should be omitted rather than serialized as `null`.

If an array somehow contains `Void`, normal serialization should not write a JSON placeholder for it. Arrays should be compressed before serialization so remaining item order is preserved without serializing absence as data.

## No Native Conversion Methods

`Void` should not have `To*` or `From*` native conversion methods. It is not JSON data and has no Go-native value equivalent. Use `IsMissing` or `IsVoid` to test for it, and use the relevant `OrDefault` method when a caller needs a replacement value.

Technically, Go `nil` is neither `Null` nor `Void`. It means a Go pointer, slice, map, interface, channel, or function is not set, which is not itself a JSON value. This library may interpret Go `nil` as ojson `Null` when importing native data, but it should never interpret `nil` as `Void`.
