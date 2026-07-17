# Examples

These examples show the intended usage style for ojson. Keep them aligned with the implementation as public APIs settle.

## Read Without A Schema

```go
package main

import (
    "fmt"

    "github.com/JohnAD/ojson"
)

func main() {
    jsonString := `{
  "user": {
    "name": "Larry",
    "customer_number": 383827
  },
  "ratings": [
    3.2,
    7.8,
    7.2,
    null,
    8.9
  ]
}`

    doc, err := ojson.ReadStringNoSchema(jsonString)
    if err != nil {
        fmt.Println(err)
        return
    }

    username := doc.Get("user").Get("name").GetString("User name is missing")
    city := doc.Get("location").Get("postal_city").GetString("Location city is missing")
    fourth := doc.Get("ratings").At(3)
    ninth := doc.Get("ratings").AtOrDefault(8, ojson.NewString("There is no ninth rating."))

    fmt.Println(username) // Larry
    fmt.Println(city)     // Location city is missing
    fmt.Println(fourth)   // null
    fmt.Println(ninth)    // There is no ninth rating.
}
```

This example demonstrates chained lookup. Missing fields produce `Void`, and accessor methods can return a fallback string.

## Write Without A Schema

```go
package main

import (
    "fmt"

    "github.com/JohnAD/ojson"
)

func main() {
    doc := ojson.NewObject()

    doc.Set("pet", ojson.NewObject())
    doc.Get("pet").Set("name", ojson.NewString("Whiffles"))

    age, err := ojson.NewNumberTry("3.2")
    if err != nil {
        fmt.Println(err)
        return
    }

    doc.Get("pet").Set("age", age)
    doc.Get("pet").Set("height", ojson.NewNumber("21.5"))
    doc.Get("pet").Set("height_units", ojson.NewString("inches"))
    doc.Get("pet").Set("safe", ojson.NewBoolean(true))

    fmt.Println(doc.ToPrettyJSON(2))
}
```

Expected pretty output:

```json
{
  "pet": {
    "name": "Whiffles",
    "age": 3.2,
    "height": 21.5,
    "height_units": "inches",
    "safe": true
  }
}
```

Without a schema, fields are emitted in insertion order.

## Missing Parent Objects

```go
doc := ojson.NewObject()
doc.Get("location").Set("city", ojson.NewString("Springfield"))

if doc.Get("location").IsMissing() {
    fmt.Println("There is no location.")
}
```

Calling `Set` on a missing parent should not create hidden intermediate objects. Create the parent explicitly:

```go
doc.Set("location", ojson.NewObject())
doc.Get("location").Set("city", ojson.NewString("Springfield"))
```

## Read With A Schema

Schema:

```json
{
  "kind": "object",
  "children": [
    {
      "name": "pet",
      "kind": "object",
      "children": [
        { "name": "name", "kind": "string", "required": true },
        { "name": "age", "kind": "number", "integer": true },
        { "name": "height", "kind": "number", "default": 0 },
        { "name": "height_units", "kind": "string", "default": "inches" },
        { "name": "safe", "kind": "boolean", "default": true }
      ]
    }
  ]
}
```

Source JSON:

```json
{
  "pet": {
    "safe": false,
    "name": "Whiffles",
    "age": 3
  }
}
```

Read procedure:

```go
schema, err := ojson.CompileSchemaJSON(schemaText)
if err != nil {
    return err
}

doc, err := ojson.ReadStringWithSchema(jsonText, schema)
if err != nil {
    return err
}

fmt.Println(doc.ToPrettyJSON(2))
```

Expected normalized output:

```json
{
  "pet": {
    "name": "Whiffles",
    "age": 3,
    "height": 0,
    "height_units": "inches",
    "safe": false
  }
}
```

The source field order did not match the schema. After reading, the document uses schema order and includes defaults.

## Build A Schema Programmatically

The same schema can be built with the typed schema builder API.

```go
schema, err := ojson.NewSchemaObjectBuilder(
    ojson.Description(ojson.LangEN, "Schema for pet records."),
).
    ObjectField("pet", func(pet *ojson.SchemaObjectBuilder) {
        pet.StringField("name", ojson.Required(), ojson.MinLength(1), ojson.MaxLength(80))
        pet.NumberField("age", ojson.Integer(), ojson.Min("0"))
        pet.NumberField("height", ojson.DefaultNumber("0"))
        pet.StringField(
            "height_units",
            ojson.Enum("inches", "centimeters"),
            ojson.DefaultString("inches"),
        )
        pet.BooleanField("safe", ojson.DefaultBool(true))
    }, ojson.Description(ojson.LangEN, "Information about one pet.")).
    Build()
if err != nil {
    return err
}

doc, err := ojson.ReadStringWithSchema(jsonText, schema)
if err != nil {
    return err
}

fmt.Println(doc.ToPrettyJSON(2))
```

Typed builder options help the Go compiler catch incompatible schema options. For example, `StringField("name", ojson.Integer())` should not compile because `Integer` is a number option, not a string option.

## Required Field Failure

Schema:

```json
{
  "kind": "object",
  "children": [
    { "name": "name", "kind": "string", "required": true }
  ]
}
```

Source JSON:

```json
{}
```

Reading this document should fail because `name` is required and has no default.

## Unknown Field Preservation

Schema:

```json
{
  "kind": "object",
  "children": [
    { "name": "name", "kind": "string" },
    { "name": "age", "kind": "number" }
  ]
}
```

Source JSON:

```json
{
  "nickname": "Whiff",
  "age": 3.2,
  "name": "Whiffles"
}
```

Expected normalized output:

```json
{
  "name": "Whiffles",
  "age": 3.2,
  "nickname": "Whiff"
}
```

Unknown fields are kept after schema-defined fields.

## Compare Struct Tags To Schema

Struct:

```go
type Pet struct {
    Name        string      `json:"name"`
    Age         json.Number `json:"age,omitempty"`
    Height      json.Number `json:"height,omitempty"`
    HeightUnits string      `json:"height_units,omitempty"`
    Safe        bool        `json:"safe"`
}
```

Schema:

```json
{
  "kind": "object",
  "children": [
    { "name": "name", "kind": "string" },
    { "name": "age", "kind": "number" },
    { "name": "height", "kind": "number" },
    { "name": "height_units", "kind": "string" },
    { "name": "safe", "kind": "boolean" }
  ]
}
```

Expected comparison result:

```text
OK: field order matches
OK: field names match
OK: schema kinds match supported Go type mappings
NOTE: required/default metadata cannot be fully represented by json tags
```

## Compare With Mismatches

Struct:

```go
type Pet struct {
    Name string `json:"name"`
    Safe bool   `json:"safe"`
    Age  string `json:"age,omitempty"`
}
```

Schema:

```json
{
  "kind": "object",
  "children": [
    { "name": "name", "kind": "string" },
    { "name": "age", "kind": "number" },
    { "name": "safe", "kind": "boolean" }
  ]
}
```

Expected comparison result:

```text
ORDER MISMATCH:
- struct order: name, safe, age
- schema order: name, age, safe

KIND MISMATCH:
- age: struct maps to string, schema expects number
```

## Convert Schema To Struct Tags

Schema:

```json
{
  "kind": "object",
  "children": [
    { "name": "name", "kind": "string", "required": true },
    { "name": "age", "kind": "number" },
    { "name": "safe", "kind": "boolean", "default": true }
  ]
}
```

Suggested struct:

```go
type Pet struct {
    Name string      `json:"name"`
    Age  json.Number `json:"age,omitempty"`
    Safe bool        `json:"safe,omitempty"`
}
```

Generated code should include review notes:

```text
name: required by schema
safe: schema default is true
age: json.Number is suggested; choose a project numeric type if needed
```

Schema-to-struct generation is a starting point, not a perfect reconstruction of the intended Go model.
