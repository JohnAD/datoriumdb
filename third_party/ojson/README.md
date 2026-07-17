# ojson

**Ordered JSON with Schema Support**

`ojson` is a Go library for reading, writing, and manipulating JSON while preserving object field order. It is designed for JSON documents that are regularly read by people, stored in source control, or validated against a project-specific schema where the order of fields is part of the workflow.

## When To Use It

Use `ojson` when:

- JSON files are stored in Git and should produce small, stable diffs.
- Pretty-printed JSON must keep fields in a specific order.
- A schema should define the order of object fields.
- Missing values, explicit `null` values, and absent fields must be handled differently.
- Numeric text should round-trip as base-10 decimal rather than binary floating-point values.

Avoid `ojson` when:

- Field order is not important.
- You are building ordinary API request or response handlers.
- You need the fastest possible JSON marshal or unmarshal path.
- Your data model is already well served by `map[string]interface{}`, `struct`, or `encoding/json`.

## Documentation

All other project documentation lives in [`tech-docs/`](tech-docs/).

- [`tech-docs/concepts.md`](tech-docs/concepts.md): core data model, ordered objects, `Void`, `Null`, numbers, and arrays.
- [`tech-docs/methods-and-procedures.md`](tech-docs/methods-and-procedures.md): index for method and procedure documentation, split by common behavior and value kind.
- [`tech-docs/methods-common.md`](tech-docs/methods-common.md): shared failure handling, document I/O, state methods, serialization, and schema procedures.
- [`tech-docs/methods-object.md`](tech-docs/methods-object.md), [`methods-array.md`](tech-docs/methods-array.md), [`methods-string.md`](tech-docs/methods-string.md), [`methods-number.md`](tech-docs/methods-number.md), [`methods-boolean.md`](tech-docs/methods-boolean.md), [`methods-null.md`](tech-docs/methods-null.md), and [`methods-void.md`](tech-docs/methods-void.md): kind-specific method docs.
- [`tech-docs/schema-format.md`](tech-docs/schema-format.md): schema JSON document format, supported kinds, defaults, required fields, and ordering behavior.
- [`tech-docs/schema-builders.md`](tech-docs/schema-builders.md): programmatic schema builder methods and examples.
- [`tech-docs/error-paths.md`](tech-docs/error-paths.md): diagnostic path format for parse, validation, and conversion errors.
- [`tech-docs/struct-tags-and-schema.md`](tech-docs/struct-tags-and-schema.md): converting and comparing Go `json` struct tags to and from ojson schema JSON documents.
- [`tech-docs/examples.md`](tech-docs/examples.md): practical examples for common workflows.

## Core Model

The library models JSON values with explicit kinds:

```go
type JSONKind uint8

const (
    KindVoid JSONKind = iota
    KindObject
    KindArray
    KindString
    KindNumber
    KindBoolean
    KindNull
)
```

`Void` and `Null` are intentionally different:

- `Void` means a value is missing or undefined. It is never written into JSON text.
- `Null` means the JSON document contains an explicit `null`, which this library treats as an unknown value.
- Go `nil` means no Go value is present and is not itself a JSON kind.

For example, in `{"a": 1, "c": null}`, field `a` is present, field `c` is present with `KindNull` and has an unknown value, and field `b` is `KindVoid` if requested.

Numbers are stored as decimal text. JSON numbers are human-readable base-10 values, and converting them to binary floating-point values can change their exact representation. `ojson` keeps numeric text intact so callers can decide how and when to convert it.

Arrays are heterogeneous, as JSON allows. The array `[1, "Hello", null]` is valid JSON and can contain values of different kinds.

## Basic Reading

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
  "ratings": [3.2, 7.8, 7.2, null, 8.9]
}`

    doc, err := ojson.ReadStringNoSchema(jsonString)
    if err != nil {
        fmt.Println(err)
        return
    }

    username := doc.Get("user").Get("name").GetString("User name is missing")
    city := doc.Get("location").Get("postal_city").GetString("Location city is missing")
    fourth := doc.Get("ratings").At(3)

    fmt.Println(username) // Larry
    fmt.Println(city)     // Location city is missing
    fmt.Println(fourth)   // null
}
```

Traversal methods return a `Void` value when the requested field or array entry is missing. That lets chains such as `doc.Get("location").Get("postal_city")` remain readable while still making absence testable.

## Basic Writing

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

    doc.Get("pet").Set("safe", ojson.NewBoolean(true))

    fmt.Println(doc.ToPrettyJSON(2))
}
```

Objects keep insertion order unless a schema is attached. With a schema, object fields are stored and written in schema order.

## JSON Serialization

Use `ToJSON` when you want the JSON representation of a value, and `ToPrettyJSON` when you want indented JSON for human review or Git-tracked files. Byte variants return the same serialized JSON as `[]byte`.

`String` is for the string-equivalent content of a value, not for JSON serialization. For example:

```go
value := ojson.NewString("foo")

fmt.Println(value.String()) // foo
fmt.Println(value.ToJSON()) // "foo"
```

## Schemas

An ojson schema is itself a JSON document. The root schema describes the whole JSON text, usually an object. Object schemas can contain ordered `children`, and each child describes one field.

```json
{
  "kind": "object",
  "children": [
    {
      "name": "pet",
      "kind": "object",
      "children": [
        { "name": "name", "kind": "string", "required": true },
        { "name": "age", "kind": "number" },
        { "name": "height", "kind": "number", "default": 0 },
        { "name": "height_units", "kind": "string", "default": "inches" },
        { "name": "safe", "kind": "boolean", "default": true }
      ]
    }
  ]
}
```

Supported schema kinds are `object`, `array`, `string`, `number`, `boolean`, and `null`. The schema does not define an `any` kind. Unknown fields can be preserved after schema-defined fields, but schema-defined fields control canonical order.

See [`tech-docs/schema-format.md`](tech-docs/schema-format.md) for the full schema format.

## Struct Tags And Schemas

Go `json` struct tags are useful for deriving or checking schema field names. `ojson` documentation defines a conservative workflow for comparing struct field declarations against schema JSON:

- parse exported struct fields in declaration order
- apply `json` tag names and ignore `json:"-"`
- compare field names, field order, and supported JSON kinds
- report fields only in the struct, fields only in the schema, type mismatches, and order mismatches

The reverse workflow can use schema JSON to produce suggested Go field names and `json` tags, but it cannot recover every Go type choice. For example, schema `number` does not prove whether the Go field should be `int`, `float64`, `json.Number`, or a decimal library type.

See [`tech-docs/struct-tags-and-schema.md`](tech-docs/struct-tags-and-schema.md) for the complete conversion and comparison guide.

## Design Notes

JSON object ordering is outside the normal JSON data model. `ojson` intentionally operates in that practical space because humans and Git diffs often care about the textual shape of a document even when the JSON standard treats object members as unordered.

That tradeoff has a cost: `ojson` is expected to be slower than Go's default JSON behavior. Choose it for ordered document workflows, not as a general-purpose replacement for `encoding/json`.

