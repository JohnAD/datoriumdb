# Object Methods And Procedures

Object values are ordered collections of key/value pairs. They preserve insertion order unless a schema supplies a canonical order.

See [`methods-common.md`](methods-common.md) for shared failure-handling, state, serialization, document I/O, and schema procedures.

## `NewObject() JSONValue`

Creates an empty ordered object.

Use this when building a document or nested object programmatically. Fields are written in insertion order unless the object is associated with a schema.

```go
doc := ojson.NewObject()
doc.Set("name", ojson.NewString("Whiffles"))
```

## Native Object Constructors

Native object constructors build JSON objects from Go maps and structs.

Object construction should preserve order when the source has order. Struct conversion should preserve declaration order. Map conversion cannot preserve source order because Go maps are unordered, so it must use deterministic key ordering.

## `NewObjectFromMap(values map[string]interface{}) JSONValue`

Creates an object from a Go `map[string]interface{}`.

Map fields should be emitted in deterministic Go string order using ordinary `<` comparison on keys. This is not locale-aware alphabetical collation. It does not do case folding, Unicode normalization, or language-specific sorting.

If a map value cannot be converted, `NewObjectFromMap` should omit that field.

```go
doc := ojson.NewObjectFromMap(map[string]interface{}{
    "name": "Whiffles",
    "safe": true,
})
```

## `NewObjectFromMapTry(values map[string]interface{}) (JSONValue, error)`

Creates an object from a Go `map[string]interface{}`, or returns an error explaining why conversion failed.

If any map value cannot be converted, `NewObjectFromMapTry` should return `Void` and an error that identifies the failing key.

```go
doc, err := ojson.NewObjectFromMapTry(values)
if err != nil {
    return err
}
```

## `NewObjectFromMapOrDefault(values map[string]interface{}, defaultValue JSONValue) JSONValue`

Creates an object from a Go `map[string]interface{}`, or returns the caller-provided default when conversion fails.

If any map value cannot be converted, `NewObjectFromMapOrDefault` should return `defaultValue`.

```go
doc := ojson.NewObjectFromMapOrDefault(values, ojson.NewObject())
```

## `NewObjectFromMapOrItemDefault(values map[string]interface{}, defaultItem JSONValue) JSONValue`

Creates an object from a Go `map[string]interface{}`, substituting the caller-provided default item for fields whose values cannot be converted.

`NewObjectFromMapOrItemDefault` should always return an object. If a map value cannot be converted, the resulting object should include that key with `defaultItem` as its value. If `defaultItem` is `Void`, the field should be omitted because `Void` means absence.

```go
doc := ojson.NewObjectFromMapOrItemDefault(values, ojson.NewNull())
```

## `NewObjectFromStruct(value any) JSONValue`

Creates an object from a Go struct using exported fields and `json` tags.

Struct fields should be emitted in declaration order. This makes struct conversion the preferred native import path when object field order matters.

If a field cannot be converted, `NewObjectFromStruct` should omit that field.

```go
type Pet struct {
    Name string `json:"name"`
    Safe bool   `json:"safe"`
}

doc := ojson.NewObjectFromStruct(Pet{Name: "Whiffles", Safe: true})
```

## `NewObjectFromStructTry(value any) (JSONValue, error)`

Creates an object from a Go struct, or returns an error explaining why conversion failed.

If any included field cannot be converted, `NewObjectFromStructTry` should return `Void` and an error that identifies the failing field.

```go
doc, err := ojson.NewObjectFromStructTry(pet)
if err != nil {
    return err
}
```

## `NewObjectFromStructOrDefault(value any, defaultValue JSONValue) JSONValue`

Creates an object from a Go struct, or returns the caller-provided default when conversion fails.

If any included field cannot be converted, `NewObjectFromStructOrDefault` should return `defaultValue`.

```go
doc := ojson.NewObjectFromStructOrDefault(pet, ojson.NewObject())
```

## `NewObjectFromStructOrItemDefault(value any, defaultItem JSONValue) JSONValue`

Creates an object from a Go struct, substituting the caller-provided default item for fields whose values cannot be converted.

`NewObjectFromStructOrItemDefault` should return an object when `value` is a struct. If an included field cannot be converted, the resulting object should include that field name with `defaultItem` as its value. If `defaultItem` is `Void`, the field should be omitted because `Void` means absence.

This is expected to be the common forgiving struct-import path because it preserves declaration order while still letting callers choose how unsupported field values are represented.

```go
doc := ojson.NewObjectFromStructOrItemDefault(pet, ojson.NewNull())
```

## Native Object Value Mapping

| Go value shape | OJSON value | Notes |
| --- | --- | --- |
| `nil` | `Null` | `nil` means explicit unknown when imported as data. |
| `string` | `String` | Direct conversion. |
| `bool` | `Boolean` | Direct conversion. |
| signed integers | `Number` | Formatted as exact base-10 JSON number text. |
| unsigned integers | `Number` | Formatted as exact base-10 JSON number text. |
| finite floats | `Number` | `NaN` and infinities cannot be converted. |
| `json.Number` | `Number` | Must be valid JSON number text. |
| `[]interface{}` | `Array` | Items are converted recursively. |
| `map[string]interface{}` | `Object` | Fields are sorted by deterministic Go string order. |
| struct | `Object` | Exported fields are converted in declaration order. |
| pointer | converted pointed-to value or `Null` | Nil pointers become `Null`. |
| unsupported type | omitted, error, object default, or item default | Behavior depends on plain, `Try`, `OrDefault`, or `OrItemDefault` constructor. |

## Struct Field Rules

Struct conversion should follow Go `encoding/json` field naming rules where practical:

- include exported fields
- skip fields tagged `json:"-"`
- use the `json` tag name when present
- ignore tag options such as `omitempty` for field naming
- preserve declaration order for included fields
- recursively convert nested structs

`omitempty` should skip fields whose converted ojson value is empty according to `IsEmpty`. This means `Void`, `Null`, `{}`, `[]`, `""`, `0`, and `false` are omitted when `omitempty` is present.

## Struct Import Custom Marshaling

Struct import should use existing Go marshaling interfaces when a field type provides them.

When importing a struct field into ojson, conversion priority should be:

1. `json.Marshaler`
2. `encoding.TextMarshaler`
3. built-in ojson conversion rules

If a field implements `json.Marshaler`, call `MarshalJSON()` and parse the returned JSON bytes into a `JSONValue`. If the returned JSON is invalid, the field conversion should follow the plain, `Try`, or `OrItemDefault` behavior of the enclosing constructor.

If a field implements `encoding.TextMarshaler`, call `MarshalText()` and store the result as `KindString`.

When `MarshalJSON()` returns an object, ojson should preserve the field order from the returned JSON text. That order is controlled by the custom marshaler and may not match the parent struct's declaration-order policy.

## `IsObject() bool`

Reports whether a value is `KindObject`.

Use `IsObject` when the caller needs to test the JSON kind before object-specific traversal or mutation.

```go
if doc.Get("pet").IsObject() {
    fmt.Println("pet is an object")
}
```

## `Get(key string) JSONValue`

Returns the value for an object field.

If the receiver is not an object, if the key does not exist, or if the stored field value is `Void`, `Get` should return `Void`.

```go
name := doc.Get("pet").Get("name")
```

Returning `Void` keeps chained lookups readable:

```go
city := doc.Get("location").Get("postal_city").GetString("Location city is missing")
```

Use [`methods-void.md`](methods-void.md) for the `Void` behavior returned by failed object lookups.

## `GetTry(key string) (JSONValue, error)`

Returns the value for an object field, or an error explaining why lookup failed.

If the receiver is not an object, if the key does not exist, or if the stored field value is `Void`, `GetTry` should return a `Void` value and an error. If the lookup succeeds, it should return the stored value and a nil error, even when the stored value is explicit JSON `null`.

```go
name, err := doc.Get("pet").GetTry("name")
if err != nil {
    return err
}
```

Use `GetTry` when the caller needs to distinguish a non-object receiver from a missing key or otherwise report why lookup failed.

## `GetOrDefault(key string, defaultValue JSONValue) JSONValue`

Returns the value for an object field, or the caller-provided default when lookup fails.

If the receiver is not an object, if the key does not exist, or if the stored field value is `Void`, `GetOrDefault` should return the default value. It should not use the default when the field exists and is explicit JSON `null`.

```go
city := doc.Get("location").GetOrDefault("postal_city", ojson.NewString("Location city is missing"))
```

## `HasField(key string) bool`

Reports whether an object has a field.

If the receiver is not an object, `HasField` should return `false`. If the key does not exist, `HasField` should return `false`. If the key exists but the stored value is `Void`, `HasField` should return `false` because `Void` means absence.

`HasField` should return `true` for fields whose value is explicit JSON `null`, because `Null` means the field exists with an unknown value.

```go
if doc.Get("pet").HasField("name") {
    fmt.Println("pet has a name field")
}
```

## `Set(key string, value JSONValue)`

Sets an object field.

For unordered, no-schema objects, `Set` should preserve existing field position when replacing a value and append new fields when adding a value. For schema-backed objects, `Set` should place schema-defined fields in schema order.

If `value` is `Void`, `Set` should remove the field when it exists or leave it absent when it does not exist. This keeps `Void` as absence rather than object content.

```go
doc.Set("pet", ojson.NewObject())
doc.Get("pet").Set("name", ojson.NewString("Whiffles"))
```

Calling `Set` on a `Void` value should not create an implicit parent object. For example, `doc.Get("location").Set("city", ...)` should not mutate the document if `location` is missing.

## `SetTry(key string, value JSONValue) error`

Sets an object field, or returns an error explaining why mutation failed.

If the receiver is not an object, `SetTry` should return an error. If `value` is `Void`, `SetTry` should return an error. Use `Remove` to delete fields explicitly.

```go
if err := doc.Get("pet").SetTry("name", ojson.NewString("Whiffles")); err != nil {
    return err
}
```

## `Remove(selector any) JSONValue`

Removes an object field and returns the removed value.

For objects, `selector` must be a string key. This keeps the call site simple, such as `doc.Remove("name")`, while allowing arrays to use the same Go method name with an integer selector.

If the receiver is not an object, if the selector is not a string, if the key does not exist, or if the stored field value is `Void`, `Remove` should return `Void`. On success, the field should be removed from the object and the removed value should be returned.

Removal should produce absence, not JSON `null`. Use `Set(key, NewNull())` when the field should remain present with an unknown value.

```go
removed := doc.Get("pet").Remove("name")
if removed.IsMissing() {
    fmt.Println("there was no name field to remove")
}
```

## `RemoveTry(selector any) (JSONValue, error)`

Removes an object field and returns the removed value, or an error explaining why removal failed.

For objects, `selector` must be a string key. If the receiver is not an object, if the selector is not a string, if the key does not exist, or if the stored field value is `Void`, `RemoveTry` should return a `Void` value and an error. On success, it should remove the field and return the removed value with a nil error.

```go
removed, err := doc.Get("pet").RemoveTry("name")
if err != nil {
    return err
}
```

## Native Object Export Methods

Native object export methods convert `KindObject` values into Go maps or structs.

These methods should not be called unmarshal methods because they do not parse serialized JSON text. They convert an existing `JSONValue` tree into Go-native data.

## `ToMap() map[string]interface{}`

Converts an object to a Go `map[string]interface{}`.

If the receiver is not an object, `ToMap` should return an empty map. Fields whose value is `Void` should be omitted. Fields whose value is `Null` should be included with a nil Go value.

```go
values := doc.Get("pet").ToMap()
```

## `ToMapTry() (map[string]interface{}, error)`

Converts an object to a Go `map[string]interface{}`, or returns an error explaining why conversion failed.

If the receiver is not an object, `ToMapTry` should return an empty map and an error. If any field cannot be converted, `ToMapTry` should return an error that identifies the failing field.

```go
values, err := doc.Get("pet").ToMapTry()
if err != nil {
    return err
}
```

## `ToMapOrDefault(defaultValue map[string]interface{}) map[string]interface{}`

Converts an object to a Go `map[string]interface{}`, or returns the caller-provided default when conversion fails.

If the receiver is not an object, or if any field cannot be converted, `ToMapOrDefault` should return `defaultValue`.

```go
values := doc.Get("pet").ToMapOrDefault(map[string]interface{}{})
```

## `ToMapOrItemDefault(defaultItem interface{}) map[string]interface{}`

Converts an object to a Go `map[string]interface{}`, substituting the caller-provided default item for fields that cannot be converted.

If the receiver is not an object, `ToMapOrItemDefault` should return an empty map. Fields whose value is `Void` should be omitted. If a field cannot be converted, the output map should include that key with `defaultItem` as its value.

```go
values := doc.Get("pet").ToMapOrItemDefault(nil)
```

## Native Object Export Mapping

| OJSON value | Go value | Notes |
| --- | --- | --- |
| `KindVoid` | omitted | `Void` means absence. |
| `KindNull` | `nil` | `Null` means unknown. |
| `KindObject` | `map[string]interface{}` | Fields are converted recursively. |
| `KindArray` | `[]interface{}` | Items are converted recursively. |
| `KindString` | `string` | Direct conversion. |
| `KindNumber` | `json.Number` | Preserves exact JSON number text. |
| `KindBoolean` | `bool` | Direct conversion. |

## `ToStructTry(target any) error`

Converts an object into a Go struct pointer.

`target` must be a non-nil pointer to a struct. `ToStructTry` should fill exported fields using Go `json` tags where practical. It should return an error when the receiver is not an object, when `target` is not a suitable struct pointer, or when a field cannot be converted.

```go
type Pet struct {
    Name string `json:"name"`
    Safe bool   `json:"safe"`
}

var pet Pet
if err := doc.Get("pet").ToStructTry(&pet); err != nil {
    return err
}
```

## Struct Export Rules

Struct export should follow Go `encoding/json` field matching rules where practical:

- exported fields only
- `json:"name"` maps object field `name` to the struct field
- `json:"-"` skips the field
- missing fields leave the existing Go field value unchanged
- `Void` fields behave as missing
- `Null` maps to nil for pointer, slice, map, and interface fields
- `Null` should produce an error for non-nullable scalar fields
- object fields can fill nested structs
- array fields can fill supported slice types
- number fields should use the number export conversion rules from [`methods-number.md`](methods-number.md)

## Struct Export Custom Unmarshaling

Struct export should use existing Go unmarshaling interfaces when a target field type provides them.

When exporting an ojson field into a struct field, conversion priority should be:

1. `json.Unmarshaler`
2. `encoding.TextUnmarshaler`
3. built-in ojson conversion rules

If the target field implements `json.Unmarshaler`, convert the source `JSONValue` to JSON bytes with `ToJSONBytes()` and call `UnmarshalJSON(bytes)`.

If the target field implements `encoding.TextUnmarshaler`, pass text content to `UnmarshalText()`. For `KindString`, use the string content without JSON quotes. For other scalar kinds, use their string-equivalent content. Object and array values should not use `encoding.TextUnmarshaler` unless the implementation explicitly defines a text representation.

Errors returned by custom unmarshalers should be returned by `ToStructTry`.

Callers can get defaulting behavior by pre-populating the target struct before calling `ToStructTry`.

```go
pet := Pet{Name: "unknown"}
if err := doc.Get("pet").ToStructTry(&pet); err != nil {
    return err
}
```

## Ordering Rules

Objects should keep a stable field order:

- replacing an existing field keeps the existing field position
- adding a new field without a schema appends the field
- adding a schema-defined field places it according to schema order
- unknown fields read through a schema are retained after schema-defined fields

## Void Fields And Serialization

`Void` is absence, not object content. If an object somehow contains a field whose value is `Void`, that field should behave as missing for lookup and should be omitted from JSON serialization.

## Empty Object

The empty state for an object is `{}`. Empty objects are known values, but `NotEmpty` should return `false`.
