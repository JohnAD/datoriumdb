# Boolean Methods And Procedures

Boolean values store JSON `true` or `false`.

See [`methods-common.md`](methods-common.md) for shared failure-handling, state, serialization, document I/O, and schema procedures.

## `NewBoolean(value bool) JSONValue`

Creates a JSON boolean value.

```go
safe := ojson.NewBoolean(true)
```

## `GetBoolean(defaultValue bool) bool`

Returns the bool content for `KindBoolean`.

If the value is missing or not a boolean, return the supplied fallback.

```go
enabled := doc.Get("enabled").GetBoolean(false)
```

## `IsBoolean() bool`

Reports whether a value is `KindBoolean`.

Use `IsBoolean` when the caller needs to test the JSON kind without converting the value to a Go bool.

```go
if doc.Get("enabled").IsBoolean() {
    fmt.Println("enabled is a JSON boolean")
}
```

## `ToBool() bool`

Converts a `KindBoolean` value to a Go bool.

If the value is missing or not a boolean, `ToBool` should return `false`. This makes `ToBool` equivalent to `ToBoolOrDefault(false)`.

```go
enabled := doc.Get("enabled").ToBool()
```

## `ToBoolTry() (bool, error)`

Converts a `KindBoolean` value to a Go bool, or returns an error explaining why conversion failed.

If the value is `Void`, `Null`, or not a boolean, `ToBoolTry` should return `false` and an error.

```go
enabled, err := doc.Get("enabled").ToBoolTry()
if err != nil {
    return err
}
```

## `ToBoolOrDefault(defaultValue bool) bool`

Converts a `KindBoolean` value to a Go bool, or returns the caller-provided default when conversion fails.

`ToBoolOrDefault` is an alias for `GetBoolean`.

```go
enabled := doc.Get("enabled").ToBoolOrDefault(true)
```

## Empty Boolean

The empty state for a boolean is `false`, following common convention. False is a known value, but `NotEmpty` should return `false` for `false`.
