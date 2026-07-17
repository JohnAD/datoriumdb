# String Methods And Procedures

String values store JSON string content without the surrounding JSON quotes. A JSON string is UTF-8 text, not an arbitrary byte array.

See [`methods-common.md`](methods-common.md) for shared failure-handling, state, serialization, document I/O, and schema procedures.

## `NewString(value string) JSONValue`

Creates a JSON string value.

The input string must contain valid UTF-8. If the string is malformed, `NewString` should return `Void`.

```go
name := ojson.NewString("Whiffles")
```

## `NewEmptyString() JSONValue`

Creates an empty JSON string value.

`NewEmptyString` should return a new `KindString` value whose string content is `""`. It is the preferred constructor for the canonical empty string value.

Use a function rather than an exported package variable so callers receive a fresh value instead of sharing mutable state.

```go
empty := ojson.NewEmptyString()
```

## `NewStringFromBytes(value []byte) JSONValue`

Creates a JSON string value from bytes.

The bytes must be valid UTF-8 text. If the bytes are not valid UTF-8, `NewStringFromBytes` should return `Void`. This method must not treat strings as arbitrary byte arrays or silently replace malformed byte sequences.

```go
name := ojson.NewStringFromBytes([]byte("Whiffles"))
```

## `NewStringFromStringer(value fmt.Stringer) JSONValue`

Creates a JSON string value from a Go `fmt.Stringer`.

If `value` is nil, `NewStringFromStringer` should return `Void`.

```go
name := ojson.NewStringFromStringer(user)
```

## `NewStringFromTextMarshaler(value encoding.TextMarshaler) JSONValue`

Creates a JSON string value from a Go `encoding.TextMarshaler`.

If `value` is nil, or if `MarshalText` returns an error, `NewStringFromTextMarshaler` should return `Void`.

```go
name := ojson.NewStringFromTextMarshaler(userID)
```

## `NewStringFromTextMarshalerTry(value encoding.TextMarshaler) (JSONValue, error)`

Creates a JSON string value from a Go `encoding.TextMarshaler`, or returns an error explaining why conversion failed.

If `value` is nil, or if `MarshalText` returns an error, `NewStringFromTextMarshalerTry` should return `Void` and an error.

```go
name, err := ojson.NewStringFromTextMarshalerTry(userID)
if err != nil {
    return err
}
```

## `NewStringFromTextMarshalerOrDefault(value encoding.TextMarshaler, defaultValue JSONValue) JSONValue`

Creates a JSON string value from a Go `encoding.TextMarshaler`, or returns the caller-provided default when conversion fails.

```go
name := ojson.NewStringFromTextMarshalerOrDefault(userID, ojson.NewString("unknown"))
```

## `IsString() bool`

Reports whether a value is `KindString`.

Use `IsString` when the caller needs to test the JSON kind before string-specific conversion.

```go
if doc.Get("name").IsString() {
    fmt.Println("name is a string")
}
```

## `GetString(defaultValue string) string`

Returns the string content for `KindString`.

If the value is missing or not a string, return the supplied fallback.

```go
name := doc.Get("name").GetString("unknown")
```

## `ToString() string`

Converts a `KindString` value to a Go string.

If the value is missing or not a string, `ToString` should return an empty string. This makes `ToString` equivalent to `ToStringOrEmpty`.

```go
name := doc.Get("name").ToString()
```

## `ToStringTry() (string, error)`

Converts a `KindString` value to a Go string, or returns an error explaining why conversion failed.

If the value is `Void`, `Null`, or not a string, `ToStringTry` should return an empty string and an error.

```go
name, err := doc.Get("name").ToStringTry()
if err != nil {
    return err
}
```

## `ToStringOrDefault(defaultValue string) string`

Converts a `KindString` value to a Go string, or returns the caller-provided default when conversion fails.

`ToStringOrDefault` is equivalent to `GetString`.

```go
name := doc.Get("name").ToStringOrDefault("unknown")
```

## `ToStringOrEmpty() string`

Converts a `KindString` value to a Go string, or returns an empty string when conversion fails.

`ToStringOrEmpty` is a string-specific convenience alias for `ToStringOrDefault("")`.

```go
name := doc.Get("name").ToStringOrEmpty()
```

## `String() string`

Returns the string-equivalent content of the value.

For string values, `String` returns the contained text without JSON quotes.

```go
value := ojson.NewString("foo")

fmt.Println(value.String()) // foo
```

Use `ToJSON` when the caller needs a valid JSON representation:

```go
fmt.Println(value.ToJSON()) // "foo"
```

## Empty String

The empty state for a string is `""`. Empty strings are known values, but `NotEmpty` should return `false`.
