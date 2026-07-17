# Array Methods And Procedures

Array values are ordered lists of JSON values. They preserve item order exactly.

See [`methods-common.md`](methods-common.md) for shared failure-handling, state, serialization, document I/O, and schema procedures.

## `NewArray() JSONValue`

Creates an empty array.

Use this when the JSON value should be an ordered list of values. Array item order is always meaningful and should be preserved.

```go
ratings := ojson.NewArray()
ratings.Append(ojson.NewNumber("3.2"))
ratings.Append(ojson.NewNull())
```

## `IsArray() bool`

Reports whether a value is `KindArray`.

Use `IsArray` when the caller needs to test the JSON kind before array-specific traversal, mutation, or native slice export.

```go
if doc.Get("ratings").IsArray() {
    fmt.Println("ratings is an array")
}
```

## Native Slice Constructors

Native slice constructors build JSON arrays from Go slice values.

Constructor procedures should use this policy:

- nil Go slices and empty Go slices become empty JSON arrays
- item order is preserved
- value-slice constructors convert every item directly
- pointer-slice constructors preserve nil item positions as explicit JSON `null`
- constructors that can fail follow the plain / `Try` / `OrItemDefault` convention

Only string, number, typed numeric, and boolean slices get native `NewArrayFrom*` constructors. Object, array, null, and void collections should be built with `NewArray` plus `Append`.

## `NewArrayFromStringArray(values []string) JSONValue`

Creates a JSON array from a Go string slice.

Each string becomes a `KindString` item.

```go
names := ojson.NewArrayFromStringArray([]string{"Ada", "Grace"})
```

## `NewArrayFromStringPointerArray(values []*string) JSONValue`

Creates a JSON array from a Go string pointer slice.

Each non-nil string pointer becomes a `KindString` item. Each nil pointer becomes explicit JSON `null` so item positions are preserved.

```go
first := "Ada"
names := ojson.NewArrayFromStringPointerArray([]*string{&first, nil})
```

## `NewArrayFromNumberArray(values []string) JSONValue`

Creates a JSON array from JSON number text values.

If any item is not valid JSON number text, `NewArrayFromNumberArray` should return `Void`.

```go
amounts := ojson.NewArrayFromNumberArray([]string{"25", "25.0", "0.25E2"})
```

## `NewArrayFromNumberArrayTry(values []string) (JSONValue, error)`

Creates a JSON array from JSON number text values, or returns an error explaining why conversion failed.

If any item is not valid JSON number text, return `Void` and an error that identifies the failing item index.

```go
amounts, err := ojson.NewArrayFromNumberArrayTry([]string{"25", "25."})
if err != nil {
    return err
}
```

## `NewArrayFromNumberArrayOrItemDefault(values []string, defaultItem string) JSONValue`

Creates a JSON array from JSON number text values, substituting the caller-provided default for invalid items.

The default item should itself be valid JSON number text. If `defaultItem` is not valid, this procedure should return `Void`.

```go
amounts := ojson.NewArrayFromNumberArrayOrItemDefault([]string{"25", "bad"}, "0")
```

## `NewArrayFromIntArray(values []int) JSONValue`

Creates a JSON array from a Go int slice.

Each int becomes a `KindNumber` item formatted as exact base-10 JSON number text.

```go
counts := ojson.NewArrayFromIntArray([]int{1, 2, 3})
```

## `NewArrayFromInt64Array(values []int64) JSONValue`

Creates a JSON array from a Go int64 slice.

Each int64 becomes a `KindNumber` item formatted as exact base-10 JSON number text.

```go
counts := ojson.NewArrayFromInt64Array([]int64{1, 2, 3})
```

## `NewArrayFromFloat64Array(values []float64) JSONValue`

Creates a JSON array from a Go float64 slice.

If any item cannot be represented as a JSON number, `NewArrayFromFloat64Array` should return `Void`. `NaN`, positive infinity, and negative infinity are not valid JSON numbers.

```go
amounts := ojson.NewArrayFromFloat64Array([]float64{1.5, 2.25})
```

## `NewArrayFromFloat64ArrayTry(values []float64) (JSONValue, error)`

Creates a JSON array from a Go float64 slice, or returns an error explaining why conversion failed.

If any item cannot be represented as a JSON number, return `Void` and an error that identifies the failing item index.

```go
amounts, err := ojson.NewArrayFromFloat64ArrayTry([]float64{1.5, math.NaN()})
if err != nil {
    return err
}
```

## `NewArrayFromFloat64ArrayOrItemDefault(values []float64, defaultItem float64) JSONValue`

Creates a JSON array from a Go float64 slice, substituting the caller-provided default for items that cannot be converted.

The default item must be representable as a JSON number. If `defaultItem` is not finite, this procedure should return `Void`.

```go
amounts := ojson.NewArrayFromFloat64ArrayOrItemDefault([]float64{1.5, math.NaN()}, 0)
```

## `NewArrayFromBooleanArray(values []bool) JSONValue`

Creates a JSON array from a Go bool slice.

Each bool becomes a `KindBoolean` item.

```go
flags := ojson.NewArrayFromBooleanArray([]bool{true, false})
```

## `NewArrayFromBooleanPointerArray(values []*bool) JSONValue`

Creates a JSON array from a Go bool pointer slice.

Each non-nil bool pointer becomes a `KindBoolean` item. Each nil pointer becomes explicit JSON `null` so item positions are preserved.

```go
enabled := true
flags := ojson.NewArrayFromBooleanPointerArray([]*bool{&enabled, nil})
```

## `At(index int) JSONValue`

Returns the value at an array index.

If the receiver is not an array, or if the index is out of range, `At` should return `Void`. This makes `At` the array equivalent of `Get`: it is convenient for traversal and keeps missing values inside the ojson value model.

```go
fourth := doc.Get("ratings").At(3)
```

Use `IsMissing` when the caller needs to test whether the lookup failed.

```go
if doc.Get("ratings").At(8).IsMissing() {
    fmt.Println("There is no ninth rating.")
}
```

## `AtTry(index int) (JSONValue, error)`

Returns the value at an array index, or an error explaining why lookup failed.

If the receiver is not an array, or if the index is out of range, `AtTry` should return a `Void` value and an error. If the lookup succeeds, it should return the stored value and a nil error, even when the stored value is explicit JSON `null`.

```go
rating, err := doc.Get("ratings").AtTry(3)
if err != nil {
    return err
}
```

Use `AtTry` when the caller needs to distinguish a non-array receiver from an out-of-range index or otherwise report why lookup failed.

## `AtOrDefault(index int, defaultValue JSONValue) JSONValue`

Returns the value at an array index, or the caller-provided default when lookup fails.

If the receiver is not an array, or if the index is out of range, `AtOrDefault` should return the default value. It should not use the default when the array entry exists and is explicit JSON `null`.

```go
rating := doc.Get("ratings").AtOrDefault(8, ojson.NewString("There is no ninth rating."))
```

Use `AtOrDefault` for display and reporting paths where the caller has a meaningful replacement value. Prefer `At` or `AtTry` when missing data should remain distinguishable from real document content.

## Array Iteration

`JSONValue` values with kind `KindArray` are iterable in the ordinary JSON sense: their items have a stable order and can be visited from index `0` through the last item.

Go cannot use `for range` directly on a custom `JSONValue` struct. The library exposes `Len` and `Items` so callers can use indexed traversal or ordinary Go `range`.

## `Len() int`

Returns the array length.

If the receiver is not an array, `Len` should return `0`.

Use `Len` with `At` for indexed traversal:

```go
ratings := doc.Get("ratings")
for i := 0; i < ratings.Len(); i++ {
    rating := ratings.At(i)
    // use rating
}
```

## `Items() []JSONValue`

Returns array items as a Go slice.

If the receiver is not an array, `Items` should return an empty slice. Item order must match the array order. `Items` should return a copy of the array items, not the backing slice, so callers cannot insert `Void` or otherwise mutate the array by editing the returned slice.

Use `Items` for ordinary Go `range`:

```go
for i, rating := range doc.Get("ratings").Items() {
    fmt.Println(i, rating)
}
```

`Items` should not be part of the native `To*Array` conversion family. It returns ojson `JSONValue` items for traversal, while `ToStringArray`, `ToIntArray`, and related methods convert array items into native Go values.

## `Append(value JSONValue)`

Adds a value to the end of an array.

If the receiver is not an array, `Append` should leave the value unchanged. If `value` is `Void`, `Append` should leave the array unchanged. Use `AppendTry` when the caller needs an explicit mutation error.

```go
doc.Get("ratings").Append(ojson.NewNumber("8.9"))
```

## `AppendTry(value JSONValue) error`

Adds a value to the end of an array, or returns an error explaining why append failed.

If the receiver is not an array, `AppendTry` should return an error. If `value` is `Void`, `AppendTry` should return an error. On success, it should append the value and return nil.

```go
if err := doc.Get("ratings").AppendTry(ojson.NewNumber("8.9")); err != nil {
    return err
}
```

## `Prepend(value JSONValue)`

Adds a value to the beginning of an array.

If the receiver is not an array, `Prepend` should leave the value unchanged. If `value` is `Void`, `Prepend` should leave the array unchanged. Use `PrependTry` when the caller needs an explicit mutation error. Existing items should retain their relative order after the prepended value.

```go
doc.Get("ratings").Prepend(ojson.NewNumber("3.2"))
```

## `PrependTry(value JSONValue) error`

Adds a value to the beginning of an array, or returns an error explaining why prepend failed.

If the receiver is not an array, `PrependTry` should return an error. If `value` is `Void`, `PrependTry` should return an error. On success, it should prepend the value and return nil. Existing items should retain their relative order after the prepended value.

```go
if err := doc.Get("ratings").PrependTry(ojson.NewNumber("3.2")); err != nil {
    return err
}
```

## `InsertAtTry(index int, value JSONValue) error`

Inserts a value at an array index, or returns an error explaining why insertion failed.

If the receiver is not an array, `InsertAtTry` should return an error. If `value` is `Void`, `InsertAtTry` should return an error. If the index is out of range, it should return an error. Valid insertion indexes are from `0` through `Len()`, inclusive. Inserting at `0` is equivalent to `Prepend`; inserting at `Len()` is equivalent to `Append`.

```go
if err := doc.Get("ratings").InsertAtTry(2, ojson.NewNumber("7.4")); err != nil {
    return err
}
```

## `Remove(selector any) JSONValue`

Removes an array item and returns the removed value.

For arrays, `selector` must be an int index. This keeps the call site simple, such as `ratings.Remove(3)`, while allowing objects to use the same Go method name with a string selector.

If the receiver is not an array, if the selector is not an int, or if the index is out of range, `Remove` should return `Void`. On success, the item should be removed from the array and the removed value should be returned.

```go
removed := doc.Get("ratings").Remove(3)
if removed.IsMissing() {
    fmt.Println("There is no fourth rating to remove.")
}
```

## `RemoveTry(selector any) (JSONValue, error)`

Removes an array item and returns the removed value, or an error explaining why removal failed.

For arrays, `selector` must be an int index. If the receiver is not an array, if the selector is not an int, or if the index is out of range, `RemoveTry` should return a `Void` value and an error. On success, it should remove the item and return the removed value with a nil error.

```go
removed, err := doc.Get("ratings").RemoveTry(3)
if err != nil {
    return err
}
```

## `RemoveOrDefault(selector any, defaultValue JSONValue) JSONValue`

Removes an array item and returns the removed value, or returns the caller-provided default when removal fails.

For arrays, `selector` must be an int index. If the receiver is not an array, if the selector is not an int, or if the index is out of range, `RemoveOrDefault` should return the default value. On success, it should remove the item and return the removed value.

```go
removed := doc.Get("ratings").RemoveOrDefault(8, ojson.NewString("There is no ninth rating."))
```

## `Compress() int`

Removes all `Void` items from an array and returns the number of removed items.

If the receiver is not an array, `Compress` should return `0`. `Compress` should preserve the relative order of all remaining items. It should not remove `Null` values, because `Null` is explicit JSON `null`, meaning an unknown value in ojson.

`Compress` is a cleanup tool for arrays that somehow contain `Void` values. Public mutation methods should avoid adding `Void`, but `Compress` provides a way to normalize arrays that were built through lower-level or internal paths.

```go
removedCount := doc.Get("ratings").Compress()
fmt.Println(removedCount)
```

## Native Slice Export Methods

Native slice export methods convert JSON array items into Go slice values.

Array export methods should use this policy:

- if the receiver is not an array, return an empty slice
- if the receiver is an empty array, return an empty slice
- plain `To*Array` methods preserve item positions and use nil item pointers when an item cannot be converted
- `To*ArrayTry` methods return an error when any item cannot be converted
- `To*ArrayOrItemDefault` methods preserve item positions and substitute the caller-provided item default when an item cannot be converted

This is intentionally different from `AtTry`: native array export treats a non-array receiver as an empty result, while `Try` variants report per-item conversion failures.

Only string, number, and boolean arrays get native `To*Array` methods. Object, array, null, and void values should be handled through `At`, `AtTry`, `AtOrDefault`, or direct `JSONValue` traversal instead of a separate native slice export family.

## `ToStringArray() []*string`

Converts an array to a slice of string pointers.

If the receiver is not an array, return an empty slice. For each array item, return a pointer to the string content when the item is `KindString`; insert `nil` when the item is not a string.

`[]string` cannot contain `nil`, so the plain method returns `[]*string` to preserve the requested "nil for bad item" behavior without losing item positions. `KindNull` is also treated as a bad item in this context and should produce `nil`.

```go
names := doc.Get("names").ToStringArray()
for _, name := range names {
    if name == nil {
        fmt.Println("not a string")
        continue
    }
    fmt.Println(*name)
}
```

## `ToStringArrayTry() ([]string, error)`

Converts an array to a slice of strings.

If the receiver is not an array, return an empty slice and a nil error. If any item is not a string, return an error that identifies the failing item index.

```go
names, err := doc.Get("names").ToStringArrayTry()
if err != nil {
    return err
}
```

## `ToStringArrayOrItemDefault(defaultItem string) []string`

Converts an array to a slice of strings, substituting the caller-provided default for items that are not strings.

If the receiver is not an array, return an empty slice. If an item is not a string, place `defaultItem` at that item position.

```go
names := doc.Get("names").ToStringArrayOrItemDefault("unknown")
```

## `ToNumberArray() []*string`

Converts an array to a slice of number-text pointers.

If the receiver is not an array, return an empty slice. For each array item, return a pointer to the decimal number text when the item is `KindNumber`; insert `nil` when the item is not a number.

Numbers are returned as strings because ojson stores JSON numbers as decimal text. Callers that want integer or floating-point conversion should use the number export methods documented in [`methods-number.md`](methods-number.md).

```go
amounts := doc.Get("amounts").ToNumberArray()
for _, amount := range amounts {
    if amount == nil {
        fmt.Println("not a number")
        continue
    }
    fmt.Println(*amount)
}
```

## `ToNumberArrayTry() ([]string, error)`

Converts an array to a slice of number strings.

If the receiver is not an array, return an empty slice and a nil error. If any item is not a number, return an error that identifies the failing item index.

```go
amounts, err := doc.Get("amounts").ToNumberArrayTry()
if err != nil {
    return err
}
```

## `ToNumberArrayOrItemDefault(defaultItem string) []string`

Converts an array to a slice of number strings, substituting the caller-provided default for items that are not numbers.

If the receiver is not an array, return an empty slice. If an item is not a number, place `defaultItem` at that item position.

```go
amounts := doc.Get("amounts").ToNumberArrayOrItemDefault("0")
```

## `ToIntArray() []*int`

Converts an array to a slice of int pointers.

If the receiver is not an array, return an empty slice. For each array item, return a pointer to the converted `int` when the item is a number that can be represented as an `int`; insert `nil` when the item cannot be converted.

Number conversion should follow the same rules as [`ToIntTry`](methods-number.md): fractional values, values that do not resolve to integers, and values outside the target range cannot be converted.

```go
counts := doc.Get("counts").ToIntArray()
for _, count := range counts {
    if count == nil {
        fmt.Println("not an int")
        continue
    }
    fmt.Println(*count)
}
```

## `ToIntArrayTry() ([]int, error)`

Converts an array to a slice of ints.

If the receiver is not an array, return an empty slice and a nil error. If any item cannot be converted to `int`, return an error that identifies the failing item index.

```go
counts, err := doc.Get("counts").ToIntArrayTry()
if err != nil {
    return err
}
```

## `ToIntArrayOrItemDefault(defaultItem int) []int`

Converts an array to a slice of ints, substituting the caller-provided default for items that cannot be converted to `int`.

If the receiver is not an array, return an empty slice. If an item cannot be converted to `int`, place `defaultItem` at that item position.

```go
counts := doc.Get("counts").ToIntArrayOrItemDefault(0)
```

## `ToInt64Array() []*int64`

Converts an array to a slice of int64 pointers.

If the receiver is not an array, return an empty slice. For each array item, return a pointer to the converted `int64` when the item is a number that can be represented as an `int64`; insert `nil` when the item cannot be converted.

Number conversion should follow the same rules as [`ToInt64Try`](methods-number.md): fractional values, values that do not resolve to integers, and values outside the `int64` range cannot be converted.

```go
counts := doc.Get("counts").ToInt64Array()
```

## `ToInt64ArrayTry() ([]int64, error)`

Converts an array to a slice of int64 values.

If the receiver is not an array, return an empty slice and a nil error. If any item cannot be converted to `int64`, return an error that identifies the failing item index.

```go
counts, err := doc.Get("counts").ToInt64ArrayTry()
if err != nil {
    return err
}
```

## `ToInt64ArrayOrItemDefault(defaultItem int64) []int64`

Converts an array to a slice of int64 values, substituting the caller-provided default for items that cannot be converted to `int64`.

If the receiver is not an array, return an empty slice. If an item cannot be converted to `int64`, place `defaultItem` at that item position.

```go
counts := doc.Get("counts").ToInt64ArrayOrItemDefault(0)
```

## `ToFloat64Array() []*float64`

Converts an array to a slice of float64 pointers.

If the receiver is not an array, return an empty slice. For each array item, return a pointer to the converted `float64` when the item is a number that can be represented as a finite `float64`; insert `nil` when the item cannot be converted.

This conversion may lose decimal precision, so callers should use it only when binary floating-point behavior is acceptable.

```go
amounts := doc.Get("amounts").ToFloat64Array()
```

## `ToFloat64ArrayTry() ([]float64, error)`

Converts an array to a slice of float64 values.

If the receiver is not an array, return an empty slice and a nil error. If any item cannot be converted to a finite `float64`, return an error that identifies the failing item index.

```go
amounts, err := doc.Get("amounts").ToFloat64ArrayTry()
if err != nil {
    return err
}
```

## `ToFloat64ArrayOrItemDefault(defaultItem float64) []float64`

Converts an array to a slice of float64 values, substituting the caller-provided default for items that cannot be converted to a finite `float64`.

If the receiver is not an array, return an empty slice. If an item cannot be converted to `float64`, place `defaultItem` at that item position.

```go
amounts := doc.Get("amounts").ToFloat64ArrayOrItemDefault(0)
```

## `ToBoolArray() []*bool`

Converts an array to a slice of bool pointers.

If the receiver is not an array, return an empty slice. For each array item, return a pointer to the bool value when the item is `KindBoolean`; insert `nil` when the item is not a boolean.

```go
flags := doc.Get("flags").ToBoolArray()
for _, flag := range flags {
    if flag == nil {
        fmt.Println("not a boolean")
        continue
    }
    fmt.Println(*flag)
}
```

## `ToBoolArrayTry() ([]bool, error)`

Converts an array to a slice of bools.

If the receiver is not an array, return an empty slice and a nil error. If any item is not a boolean, return an error that identifies the failing item index.

```go
flags, err := doc.Get("flags").ToBoolArrayTry()
if err != nil {
    return err
}
```

## `ToBoolArrayOrItemDefault(defaultItem bool) []bool`

Converts an array to a slice of bools, substituting the caller-provided default for items that are not booleans.

If the receiver is not an array, return an empty slice. If an item is not a boolean, place `defaultItem` at that item position.

```go
flags := doc.Get("flags").ToBoolArrayOrItemDefault(false)
```

## Empty Array

The empty state for an array is `[]`. Empty arrays are known values, but `NotEmpty` should return `false`.
