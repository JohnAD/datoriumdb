# Number Methods And Procedures

Number values store valid JSON number text. They are decimal text, not binary floating-point values.

See [`concepts.md`](concepts.md) for the decimal-number model and [`methods-common.md`](methods-common.md) for shared failure-handling, state, serialization, document I/O, and schema procedures.

## `NewNumber(numberText string) JSONValue`

Creates a JSON number from decimal text.

If the text is not a valid JSON number, `NewNumber` should return `Void`.

```go
age := ojson.NewNumber("3.2")
doc.Set("age", age)
```

Use `NewNumber` when fluent construction is more important than reporting why conversion failed.

## `NewNumberTry(numberText string) (JSONValue, error)`

Creates a JSON number from decimal text and returns an error if the text is not a valid JSON number.

Use `NewNumberTry` when invalid numeric input should stop the operation or produce a caller-visible error.

```go
height, err := ojson.NewNumberTry("21.5")
if err != nil {
    return err
}
```

## `NewNumberOrDefault(numberText string, defaultValue JSONValue) JSONValue`

Creates a JSON number from decimal text, or returns the caller-provided default when the text is not valid.

```go
height := ojson.NewNumberOrDefault("21.5 inches", ojson.NewNumberFromInt(0))
```

Use `NewNumberOrDefault` for display and recovery paths where a replacement value is explicitly acceptable.

## `IsValidNumber(numberText string) bool`

Reports whether a string is valid JSON number text.

`IsValidNumber` should return `true` only when the complete string matches the JSON number grammar. It should not trim spaces, repair spelling, or accept non-JSON numeric forms.

```go
fmt.Println(ojson.IsValidNumber("25"))     // true
fmt.Println(ojson.IsValidNumber("25.0"))   // true
fmt.Println(ojson.IsValidNumber("0.25E2")) // true
fmt.Println(ojson.IsValidNumber("25."))    // false
fmt.Println(ojson.IsValidNumber(" 25 "))   // false
```

Use `IsValidNumber` when input should be checked exactly as provided.

## `PrepareNumber(numberText string) (string, error)`

Returns prepared JSON number text, or an error explaining why the input cannot be prepared.

`PrepareNumber` should make a best-effort cleanup pass without guessing at unclear values. It should:

- strip leading and trailing spaces
- convert a trailing decimal point form such as `25.` into scientific notation such as `0.25E2`
- convert lowercase `e` in scientific notation to uppercase `E`
- return an error when the prepared result still is not valid JSON number text
- include the reason in the error so callers can explain the failure

```go
prepared, err := ojson.PrepareNumber(" 25. ")
if err != nil {
    return err
}

fmt.Println(prepared) // 0.25E2
```

`PrepareNumber` should not convert arbitrary human text such as `"21.5 inches"`. That input should fail with an error because the unit makes the number ambiguous.

## `NewNumberFromInt[T integer](value T) JSONValue`

Converts an integer value to a valid JSON number value.

This constructor should accept Go integer types and format them as exact base-10 JSON number text with no decimal point.

```go
count := ojson.NewNumberFromInt(25)
fmt.Println(count.ToJSON()) // 25
```

Use `NewNumberFromInt` for counts, indexes, IDs that are intentionally numeric, and other exact integer values.

## `NewNumberFromFloat[T float](value T) JSONValue`

Converts a floating-point value to a JSON number value when possible.

If the value cannot be represented as a JSON number, `NewNumberFromFloat` should return `Void`. Some IEEE 754 values are not valid JSON numbers, including `NaN`, positive infinity, and negative infinity.

```go
value := ojson.NewNumberFromFloat(3.25)
```

Use `NewNumberFromFloat` when fluent construction is more important than reporting why conversion failed.

## `NewNumberFromFloatTry[T float](value T) (JSONValue, error)`

Converts a floating-point value to a JSON number value and returns an error when conversion is not possible.

Some IEEE 754 values are not valid JSON numbers. `NaN`, positive infinity, and negative infinity should not be converted to JSON number text. `NewNumberFromFloatTry` should return an error for those values.

```go
value, err := ojson.NewNumberFromFloatTry(3.25)
if err != nil {
    return err
}
```

Use this method when invalid floating-point values should be reported.

## `NewNumberFromFloatOrDefault[T float](value T, defaultValue JSONValue) JSONValue`

Converts a floating-point value to a JSON number value, or returns the caller-provided default when conversion is not possible.

```go
value := ojson.NewNumberFromFloatOrDefault(math.NaN(), ojson.NewNumberFromInt(0))
```

The default should be used for IEEE 754 values that cannot be represented as JSON numbers, such as `NaN` and infinities.

## `GetNumber(defaultValue string) string`

Returns the decimal text for `KindNumber`.

Do not automatically convert to binary floating-point unless the method name makes that conversion explicit.

```go
amountText := doc.Get("amount").GetNumber("0")
```

## `IsNumber() bool`

Reports whether a value is `KindNumber`.

Use `IsNumber` when the caller needs to test the JSON kind before number-specific conversion.

```go
if doc.Get("amount").IsNumber() {
    fmt.Println("amount is a number")
}
```

## Number Export Methods

Number export methods convert a `KindNumber` value into Go numeric types.

These methods should use only `Try` and `OrDefault` forms. A plain conversion method such as `ToInt` would need to return a Go `int`, so it cannot return `Void` on failure. To keep the failure-handling convention clear, primitive conversions should either return an error or return the caller-provided default.

## `ToIntTry() (int, error)`

Converts a `KindNumber` value to `int`.

Returns an error when the value is `Void`, `Null`, not a number, has a fractional component, cannot be represented as an integer, or overflows the platform `int` range.

```go
count, err := doc.Get("count").ToIntTry()
if err != nil {
    return err
}
```

## `ToIntOrDefault(defaultValue int) int`

Converts a `KindNumber` value to `int`, or returns the caller-provided default on failure.

```go
count := doc.Get("count").ToIntOrDefault(0)
```

## `ToInt64Try() (int64, error)`

Converts a `KindNumber` value to `int64`.

Returns an error when the value is not a number, has a fractional component, or overflows `int64`.

```go
count, err := doc.Get("count").ToInt64Try()
if err != nil {
    return err
}
```

## `ToInt64OrDefault(defaultValue int64) int64`

Converts a `KindNumber` value to `int64`, or returns the caller-provided default on failure.

```go
count := doc.Get("count").ToInt64OrDefault(0)
```

## `ToUint64Try() (uint64, error)`

Converts a `KindNumber` value to `uint64`.

Returns an error when the value is not a number, is negative, has a fractional component, or overflows `uint64`.

```go
count, err := doc.Get("count").ToUint64Try()
if err != nil {
    return err
}
```

## `ToUint64OrDefault(defaultValue uint64) uint64`

Converts a `KindNumber` value to `uint64`, or returns the caller-provided default on failure.

```go
count := doc.Get("count").ToUint64OrDefault(0)
```

## `ToFloat64Try() (float64, error)`

Converts a `KindNumber` value to `float64`.

Returns an error when the value is not a number or cannot be represented as a finite `float64`. This conversion may lose decimal precision, so callers should use it only when binary floating-point behavior is acceptable.

```go
amount, err := doc.Get("amount").ToFloat64Try()
if err != nil {
    return err
}
```

## `ToFloat64OrDefault(defaultValue float64) float64`

Converts a `KindNumber` value to `float64`, or returns the caller-provided default on failure.

```go
amount := doc.Get("amount").ToFloat64OrDefault(0)
```

## `ToIntegerTry[T integer](value JSONValue) (T, error)`

Package-level generic helper for converting a `KindNumber` value to a specific integer type.

`ToIntegerTry` should be range-checked for the target type. It should return an error when the value is not a number, has a fractional component, is negative for an unsigned target type, or overflows the target type.

```go
small, err := ojson.ToIntegerTry[int16](doc.Get("small_count"))
if err != nil {
    return err
}
```

Supported target types should include Go's built-in signed and unsigned integer types: `int`, `int8`, `int16`, `int32`, `int64`, `uint`, `uint8`, `uint16`, `uint32`, `uint64`, and `uintptr`.

## `ToIntegerOrDefault[T integer](value JSONValue, defaultValue T) T`

Package-level generic helper for converting a `KindNumber` value to a specific integer type, or returning the caller-provided default on failure.

```go
small := ojson.ToIntegerOrDefault[int16](doc.Get("small_count"), 0)
```

## Number Export Failure Rules

Number export methods should fail when:

- the value is `Void`
- the value is explicit JSON `null`
- the value is not `KindNumber`
- integer conversion sees a fractional component
- integer conversion sees an exponent value that does not resolve to an integer
- unsigned integer conversion sees a negative value
- conversion would overflow or underflow the target type
- float conversion cannot produce a finite `float64`

## Empty Number

The empty state for a number is `0` with no decimal part. Zero is a known value, but `NotEmpty` should return `false`.
