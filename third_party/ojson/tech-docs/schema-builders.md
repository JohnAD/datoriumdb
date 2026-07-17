# Schema Builders

Schema builders provide a Go-native way to create compiled ojson schemas without writing schema JSON by hand.

Builders should produce the same schema model described in [`schema-format.md`](schema-format.md). `Build` should validate the builder state and return a compiled `JSONSchema` that can be used with `ReadStringWithSchema`, `ApplySchema`, and `Validate`.

The builder API should use typed option/decorator functions. This lets the Go compiler reject incompatible options, such as using `Integer()` on a string field or `MinLength()` on a number field.

Only object and array schemas have top-level builders:

- `NewSchemaObjectBuilder()`
- `NewSchemaArrayBuilder()`

Scalar schemas are created through object field methods or array item methods. This keeps the public API focused on the schema roots that usually need composition.

## Option Type Pattern

Each schema kind should have its own option interface:

```go
type ObjectOption interface {
    applyObject(*objectSchema)
}

type ArrayOption interface {
    applyArray(*arraySchema)
}

type StringOption interface {
    applyString(*stringSchema)
}

type NumberOption interface {
    applyNumber(*numberSchema)
}

type BooleanOption interface {
    applyBoolean(*booleanSchema)
}
```

Builder methods should accept only the option type that matches the schema kind:

```go
func (b *SchemaObjectBuilder) StringField(name string, opts ...StringOption) *SchemaObjectBuilder
func (b *SchemaObjectBuilder) NumberField(name string, opts ...NumberOption) *SchemaObjectBuilder
```

This should fail at compile time:

```go
ojson.NewSchemaObjectBuilder().
    StringField("name", ojson.Integer())
```

`Integer()` returns a `NumberOption`, not a `StringOption`.

## Language Codes

Localized descriptions should use a library-defined language code type instead of raw strings:

```go
type LanguageCode string
```

The library should provide constants for valid ISO 639 language codes. The two-letter code should be preferred when it is precise enough.

```go
const (
    LangEN LanguageCode = "en"
    LangES LanguageCode = "es"
    LangFR LanguageCode = "fr"
)
```

Description options should accept `LanguageCode`:

```go
ojson.Description(ojson.LangEN, "A pet record.")
```

If the library exposes a way to create custom language codes, it should validate them before use:

```go
lang, err := ojson.ParseLanguageCode("pt-BR")
if err != nil {
    return err
}
```

## Object Builder

### `NewSchemaObjectBuilder(opts ...ObjectOption) *SchemaObjectBuilder`

Creates a builder for an object schema.

```go
schema, err := ojson.NewSchemaObjectBuilder(
    ojson.Description(ojson.LangEN, "Pet record schema."),
).
    StringField("name", ojson.Required(), ojson.MinLength(1), ojson.MaxLength(80)).
    NumberField("age", ojson.Integer(), ojson.Min("0")).
    StringField("email", ojson.Nullable(), ojson.Format(ojson.FormatEmail)).
    Build()
if err != nil {
    return err
}
```

The order in which fields are added is the schema order.

### `ObjectField(name string, configure func(*SchemaObjectBuilder), opts ...ObjectOption) *SchemaObjectBuilder`

Adds a nested object field.

```go
schema, err := ojson.NewSchemaObjectBuilder().
    ObjectField("pet", func(pet *SchemaObjectBuilder) {
        pet.StringField("name", ojson.Required())
        pet.NumberField("age", ojson.Integer(), ojson.Min("0"))
    }, ojson.Description(ojson.LangEN, "Information about one pet.")).
    Build()
```

Nested field order should follow the order of calls inside `configure`. Object field options can include `Description`, `Required`, `Nullable`, and `Default`.

### `ArrayField(name string, configure func(*SchemaArrayBuilder), opts ...ArrayOption) *SchemaObjectBuilder`

Adds an array field.

```go
schema, err := ojson.NewSchemaObjectBuilder().
    ArrayField("tags", func(tags *SchemaArrayBuilder) {
        tags.StringItems(ojson.MinLength(1))
    }, ojson.Description(ojson.LangEN, "Search tags.")).
    Build()
```

Array field options can include `Description`, `Required`, `Nullable`, and `Default`.

### `StringField(name string, opts ...StringOption) *SchemaObjectBuilder`

Adds a string field.

```go
schema, err := ojson.NewSchemaObjectBuilder().
    StringField("name", ojson.Required(), ojson.MinLength(1), ojson.MaxLength(80)).
    Build()
```

### `NumberField(name string, opts ...NumberOption) *SchemaObjectBuilder`

Adds a number field.

```go
schema, err := ojson.NewSchemaObjectBuilder().
    NumberField("age", ojson.Integer(), ojson.Min("0"), ojson.Max("130")).
    Build()
```

### `BooleanField(name string, opts ...BooleanOption) *SchemaObjectBuilder`

Adds a boolean field.

```go
schema, err := ojson.NewSchemaObjectBuilder().
    BooleanField("safe", ojson.DefaultBool(true)).
    Build()
```

### `Build(opts ...SchemaCompileOption) (JSONSchema, error)`

Validates and compiles the schema.

`Build` should fail when the builder contains invalid field names, duplicate field names, invalid defaults, unsupported validation combinations, malformed number constraints, unsupported formats, or invalid nested schemas.

Pass `WithStringFormats(registry)` when the schema uses custom string formats.

### `MustBuild(opts ...SchemaCompileOption) JSONSchema`

Validates and compiles the schema, panicking on error.

`MustBuild` is useful for package-level schema definitions and tests where the schema is effectively static. Library and application code that accepts dynamic input should use `Build`.

## Array Builder

### `NewSchemaArrayBuilder(opts ...ArrayOption) *SchemaArrayBuilder`

Creates a builder for an array schema.

```go
schema, err := ojson.NewSchemaArrayBuilder(
    ojson.Description(ojson.LangEN, "List of tags."),
).
    StringItems(ojson.MinLength(1)).
    Build()
if err != nil {
    return err
}
```

If no item schema is defined, the array accepts mixed JSON value kinds.

### `ObjectItems(configure func(*SchemaObjectBuilder), opts ...ObjectOption) *SchemaArrayBuilder`

Defines array items as objects.

```go
schema, err := ojson.NewSchemaArrayBuilder().
    ObjectItems(func(item *SchemaObjectBuilder) {
        item.StringField("name", ojson.Required())
    }).
    Build()
```

### `ArrayItems(configure func(*SchemaArrayBuilder), opts ...ArrayOption) *SchemaArrayBuilder`

Defines array items as arrays.

### `StringItems(opts ...StringOption) *SchemaArrayBuilder`

Defines array items as strings.

```go
schema, err := ojson.NewSchemaArrayBuilder().
    StringItems(ojson.Enum("draft", "active", "archived")).
    Build()
```

### `NumberItems(opts ...NumberOption) *SchemaArrayBuilder`

Defines array items as numbers.

```go
schema, err := ojson.NewSchemaArrayBuilder().
    NumberItems(ojson.Integer(), ojson.Min("0")).
    Build()
```

### `BooleanItems(opts ...BooleanOption) *SchemaArrayBuilder`

Defines array items as booleans.

### `Build(opts ...SchemaCompileOption) (JSONSchema, error)`

Validates and compiles the schema.

For arrays, `Build` should verify that any item schema is valid. If no item schema was provided, the compiled schema should allow mixed item kinds.

Pass `WithStringFormats(registry)` when the schema uses custom string formats.

### `MustBuild(opts ...SchemaCompileOption) JSONSchema`

Validates and compiles the schema, panicking on error.

## Shared Options

### `Description(lang LanguageCode, text string)`

Adds a localized description.

This option should be available for object, array, string, number, and boolean schemas.

```go
ojson.Description(ojson.LangEN, "The pet's display name.")
```

### `Required()`

Marks a field as required.

This option should be available for fields inside object schemas. It should not be meaningful for a root schema.

### `Nullable()`

Allows explicit JSON `null`.

This option should be available for object, array, string, number, and boolean schemas.

### `Default(value JSONValue)`

Sets a default value.

This option should be available for object and array schemas. Scalar builders should prefer typed default helpers.

### `Custom(value JSONValue)`

Attaches opaque application metadata.

This option should be available for object, array, string, number, and boolean schemas. `ojson` preserves the value and does not interpret it.

```go
meta := ojson.NewObject()
meta.Set("indexed", ojson.NewBoolean(true))
ojson.Custom(meta)
```

### `CustomString(value string)`

Attaches opaque string metadata.

```go
ojson.CustomString("application-specific note")
```

## String Format Registries

Custom string formats are registered in an application-owned registry and supplied when compiling a schema:

```go
formats := ojson.NewStringFormatRegistry()
if err := formats.Register("Time", timeValidator, reflect.TypeOf(time.Time{})); err != nil {
    return err
}

schema, err := ojson.NewSchemaObjectBuilder().
    StringField("released_at", ojson.Format(ojson.StringFormat("Time"))).
    Build(ojson.WithStringFormats(formats))
```

The same registry can compile many schemas. A schema may use zero or many formats from that one registry. Built-in formats do not require registration.

## String Options

String options should implement `StringOption`.

### `DefaultString(value string) StringOption`

Sets a string default.

### `DefaultNull() StringOption`

Sets a `null` default. The field must also be nullable.

### `MinLength(value uint16) StringOption`

Sets the inclusive minimum string length in Unicode code points.

Using `uint16` prevents accidental negative integer literals from compiling.

### `MaxLength(value uint16) StringOption`

Sets the inclusive maximum string length in Unicode code points.

### `Enum(values ...string) StringOption`

Restricts the string to one of the supplied values.

### `Format(value StringFormat) StringOption`

Sets an HTML-aligned string format.

The library should provide constants for supported formats:

```go
const (
    FormatEmail StringFormat = "email"
    FormatTel   StringFormat = "tel"
    FormatURL   StringFormat = "url"
)
```

Using a custom `StringFormat` type prevents arbitrary strings unless callers explicitly opt into conversion.

## Number Options

Number options should implement `NumberOption`.

### `DefaultNumber(value string) NumberOption`

Sets a number default from JSON number text.

The string must be valid JSON number text.

### `DefaultInt(value int64) NumberOption`

Sets an integer number default.

### `DefaultNull() NumberOption`

Sets a `null` default. The field must also be nullable.

### `Integer() NumberOption`

Requires the number to represent an integer value.

### `Min(value string) NumberOption`

Sets the inclusive minimum from JSON number text.

### `Max(value string) NumberOption`

Sets the inclusive maximum from JSON number text.

Number strings are still runtime values, so malformed values must be detected by `Build`.

## Boolean Options

Boolean options should implement `BooleanOption`.

### `DefaultBool(value bool) BooleanOption`

Sets a boolean default.

### `DefaultNull() BooleanOption`

Sets a `null` default. The field must also be nullable.

## Compile-Time Guarantees

As a general rule, prefer explicit literals in builder calls whenever possible. Literal values give the Go compiler more opportunities to catch mistakes before runtime.

The typed option design should let the Go compiler catch incompatible options:

```go
ojson.NewSchemaObjectBuilder().
    StringField("name", ojson.Integer())
```

That should fail because `Integer()` is a `NumberOption`, not a `StringOption`.

The compiler can also reject negative length literals:

```go
ojson.MinLength(-20)
```

That should fail because `MinLength` accepts `uint16`.

## Runtime Validation Still Required

Some validation cannot be done by the Go compiler:

- duplicate field names
- malformed JSON number strings in `Min`, `Max`, or `DefaultNumber`
- `Min` greater than `Max`
- invalid custom language codes returned by user input
- invalid defaults supplied as `JSONValue`
- unsupported nested schema combinations

For these cases, `Build() (JSONSchema, error)` is still required.

`MustBuild()` may be used for static schemas where a panic during initialization is acceptable.

## Complete Object Builder Example

```go
schema, err := ojson.NewSchemaObjectBuilder(
    ojson.Description(ojson.LangEN, "Schema for one pet record."),
).
    ObjectField("pet", func(pet *SchemaObjectBuilder) {
        pet.StringField(
            "name",
            ojson.Description(ojson.LangEN, "The pet's display name."),
            ojson.Required(),
            ojson.MinLength(1),
            ojson.MaxLength(80),
        )
        pet.NumberField("age", ojson.Integer(), ojson.Min("0"))
        pet.NumberField("height", ojson.DefaultNumber("0"))
        pet.StringField(
            "height_units",
            ojson.Enum("inches", "centimeters"),
            ojson.DefaultString("inches"),
        )
        pet.StringField("contact_email", ojson.Nullable(), ojson.Format(ojson.FormatEmail))
        pet.BooleanField("safe", ojson.DefaultBool(true))
    }, ojson.Description(ojson.LangEN, "Information about one pet.")).
    Build()
if err != nil {
    return err
}
```

## Complete Array Builder Example

```go
schema, err := ojson.NewSchemaArrayBuilder(
    ojson.Description(ojson.LangEN, "Pets found from a search."),
).
    ObjectItems(func(pet *SchemaObjectBuilder) {
        pet.StringField("id", ojson.Required(), ojson.MinLength(1))
        pet.StringField("name", ojson.Required(), ojson.MinLength(1), ojson.MaxLength(80))
        pet.StringField("species", ojson.Required(), ojson.Enum("cat", "dog", "bird", "other"))
        pet.NumberField("age", ojson.Integer(), ojson.Min("0"))
        pet.BooleanField("adoptable", ojson.DefaultBool(false))
    }).
    Build()
if err != nil {
    return err
}
```
