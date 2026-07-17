# Struct Tags And Schema JSON

This guide documents how to convert and compare Go `json` struct tags to and from an ojson schema JSON document. It is intended for tooling that keeps Go data structures and ordered JSON schema files aligned.

The conversion is intentionally conservative. Go types contain information that an ojson schema does not, and an ojson schema contains ordering and default information that ordinary Go struct tags do not. Tooling should report uncertainty instead of guessing silently.

## Goals

Struct-tag/schema tooling should help answer four questions:

- Do my Go struct fields appear in the same order as the schema?
- Do my Go `json` field names match the schema field names?
- Do my Go field types map to the schema kinds?
- Can a schema JSON document be used to suggest Go field declarations and tags?

## Methods And Procedures

The procedures in this document are intended for tooling, code generation, and project checks. They should not be required for ordinary JSON reading and writing.

These APIs should use the same failure-handling convention as the rest of ojson:

- plain methods return a useful empty result or `Void` on failure
- `*Try` methods return a result and an `error`
- comparison methods return a structured report, because mismatches are expected data rather than exceptional failures

## Shared Option Types

### `StructSchemaOption`

Configures struct/schema conversion and comparison behavior.

```go
type StructSchemaOption interface {
    applyStructSchemaOption(*structSchemaConfig)
}
```

The exact config struct should remain internal. Options should cover naming policy, required-field policy, numeric-type policy, and map handling.

### `RequireExplicitJSONTags() StructSchemaOption`

Requires every included struct field to have an explicit `json` tag name.

If this option is active, fields without explicit `json` names should produce an error in conversion procedures and a finding in comparison procedures. This is the recommended default for schema generation because it avoids guessing between Go field names and project JSON naming conventions.

### `AllowDefaultFieldNames() StructSchemaOption`

Allows fields without an explicit `json` tag name to use the Go field name.

This option should be used only when a project intentionally wants Go field names to become JSON field names.

### `RequiredFromNonOmitEmpty() StructSchemaOption`

Treats included fields without `omitempty` as `required: true` when converting from a struct to a schema.

This is a project policy, not a universal Go rule. A non-`omitempty` field in a struct does not prove that the JSON document must contain the field in every context.

### `OptionalPointers() StructSchemaOption`

Treats pointer fields as optional for comparison and generation.

Pointer optionality should not change the schema `kind`. For example, `*string` still maps to `kind: "string"`, but the field may be considered optional by policy.

### `AllowMapObjects() StructSchemaOption`

Allows `map[string]T` to map to `kind: "object"` when generating or comparing schemas.

Because Go maps do not preserve field order, this option should produce a warning unless the schema entry is intentionally unordered or has no fixed `children`.

### `DecimalType(typeName string) StructSchemaOption`

Registers a project-specific decimal type that should map to schema `kind: "number"`.

Examples might include `decimal.Decimal`, `shopspring.Decimal`, or an internal money/decimal type. The string should use the package-qualified type name expected by the tooling.

### `NumberType(typeName string) StructSchemaOption`

Registers a project-specific numeric type that should map to schema `kind: "number"`.

Use this for custom integer, float, or JSON-number wrapper types that are known to serialize as JSON numbers.

## Struct Inspection Result Types

### `StructSchemaField`

Represents one included Go struct field after `json` tag parsing and type mapping.

```go
type StructSchemaField struct {
    GoName       string
    JSONName     string
    SchemaKind   JSONKind
    Optional     bool
    OmitEmpty    bool
    GoType       string
    Path         Path
    Children     []StructSchemaField
    Item         *StructSchemaField
    Findings     []StructSchemaFinding
}
```

`Path` should use the ojson diagnostic path format. For nested structs, paths should refer to the JSON field path, such as `"pet"."name"`.

### `InspectStructTags(value any, opts ...StructSchemaOption) []StructSchemaField`

Inspects a struct value, pointer to a struct, or `reflect.Type` for a struct and returns included fields in declaration order.

If inspection fails, the plain procedure should return an empty slice. Use `InspectStructTagsTry` when tooling needs the reason.

```go
fields := ojson.InspectStructTags(Pet{})
```

### `InspectStructTagsTry(value any, opts ...StructSchemaOption) ([]StructSchemaField, error)`

Inspects a struct value, pointer to a struct, or `reflect.Type` for a struct and returns a detailed error when inspection cannot continue.

Expected failures include:

- `value` is not a struct or pointer to a struct
- required explicit `json` tags are missing
- a field type is unsupported
- an embedded field creates an ambiguous JSON name
- a recursive type is detected

```go
fields, err := ojson.InspectStructTagsTry(Pet{}, ojson.RequireExplicitJSONTags())
if err != nil {
    return err
}
```

## Struct To Schema Procedures

### `NewSchemaFromStruct(value any, opts ...StructSchemaOption) JSONValue`

Creates a schema JSON document from a Go struct value, pointer to a struct, or `reflect.Type` for a struct.

The plain procedure should return `Void` when conversion fails. On success it should return a `KindObject` value representing the schema JSON document, not a compiled `JSONSchema`.

```go
schemaDoc := ojson.NewSchemaFromStruct(Pet{})
fmt.Println(schemaDoc.ToPrettyJSON(2))
```

Returning `JSONValue` keeps this procedure focused on schema document generation. Callers that need a compiled schema can call `CompileSchemaJSON(schemaDoc.ToJSON(), opts...)` or use `CompileSchemaFromStructTry` with `StructStringFormats` when custom formats are present.

### `NewSchemaFromStructTry(value any, opts ...StructSchemaOption) (JSONValue, error)`

Creates a schema JSON document from a Go struct value, pointer to a struct, or `reflect.Type` for a struct, or returns an error explaining why conversion failed.

The generated schema should:

- use root `kind: "object"`
- preserve struct declaration order as `children` order
- use parsed `json` tag names as schema `name` values
- map supported Go types to schema `kind`
- recurse into nested structs
- use `items` for slices and arrays when the item kind can be inferred
- omit defaults unless supplied by explicit tooling configuration
- include `required: true` only when enabled by policy

```go
schemaDoc, err := ojson.NewSchemaFromStructTry(Pet{}, ojson.RequiredFromNonOmitEmpty())
if err != nil {
    return err
}
```

### `NewSchemaFromStructOrDefault(value any, defaultValue JSONValue, opts ...StructSchemaOption) JSONValue`

Creates a schema JSON document from a Go struct value, pointer to a struct, or `reflect.Type` for a struct, or returns `defaultValue` when conversion fails.

Use this only for display or recovery paths. Build tooling should prefer `NewSchemaFromStructTry`.

### `CompileSchemaFromStructTry(value any, opts ...StructSchemaOption) (JSONSchema, error)`

Creates and compiles a schema from a Go struct value, pointer to a struct, or `reflect.Type` for a struct.

This is a convenience procedure equivalent to:

```go
schemaDoc, err := ojson.NewSchemaFromStructTry(value, opts...)
if err != nil {
    return ojson.JSONSchema{}, err
}

return ojson.CompileSchemaJSON(schemaDoc.ToJSON())
```

Use this when the schema will immediately be used with `ReadStringWithSchema`, `ApplySchema`, or `Validate`.

## Schema To Struct Procedures

### `StructSuggestion`

Represents generated Go struct code plus review notes.

```go
type StructSuggestion struct {
    TypeName string
    Code     string
    Imports  []string
    Notes    []StructSchemaFinding
}
```

The generated `Code` should contain the suggested type declaration only. Required imports should be listed separately in `Imports` so caller tools can merge them into an existing file or render a complete standalone source file.

The suggested code should be a starting point, not a claim that all project-specific type decisions are solved.

### `NewStructSuggestionFromSchema(schema JSONSchema, typeName string, opts ...StructSchemaOption) StructSuggestion`

Creates a suggested Go struct definition from a compiled schema.

If generation fails, the plain procedure should return an empty `StructSuggestion` with findings when possible. Use `NewStructSuggestionFromSchemaTry` for strict tooling.

```go
suggestion := ojson.NewStructSuggestionFromSchema(schema, "Pet")
fmt.Println(suggestion.Code)
```

### `NewStructSuggestionFromSchemaTry(schema JSONSchema, typeName string, opts ...StructSchemaOption) (StructSuggestion, error)`

Creates a suggested Go struct definition from a compiled schema, or returns an error when the schema cannot be converted.

Expected behavior:

- require an object root schema
- preserve schema child order as struct field order
- generate exported Go field names from schema names
- emit `json` tags for every generated field
- map schema `number` to `json.Number` by default
- map nullable scalar fields to pointers when configured by policy
- recurse into object children
- generate slice types from array `items` when possible
- add review notes for defaults, required fields, nullable fields, and unsupported schema metadata

```go
suggestion, err := ojson.NewStructSuggestionFromSchemaTry(schema, "Pet")
if err != nil {
    return err
}
```

For this schema:

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

The returned `suggestion.Code` should contain the full Go source string for the suggested type:

```go
type Pet struct {
    Name string      `json:"name"`
    Age  json.Number `json:"age,omitempty"`
    Safe bool        `json:"safe,omitempty"`
}
```

The source string should include struct tags. It should not include imports.

For this example, the returned `suggestion.Imports` should contain:

```go
[]string{"encoding/json"}
```

Separating imports from code keeps the API useful for utilities that insert generated types into existing source files and manage imports themselves.

For a schema with nested objects and arrays of objects:

```json
{
  "kind": "object",
  "children": [
    {
      "name": "pet",
      "kind": "object",
      "children": [
        { "name": "name", "kind": "string", "required": true },
        { "name": "age", "kind": "number" }
      ]
    },
    {
      "name": "search_results",
      "kind": "array",
      "items": {
        "kind": "object",
        "children": [
          { "name": "id", "kind": "string", "required": true },
          { "name": "score", "kind": "number" }
        ]
      }
    }
  ]
}
```

Calling `NewStructSuggestionFromSchemaTry(schema, "SearchResponse")` should return `suggestion.Code` with multiple struct declarations:

```go
type SearchResponse struct {
    Pet           Pet            `json:"pet,omitempty"`
    SearchResults []SearchResult `json:"search_results,omitempty"`
}

type Pet struct {
    Name string      `json:"name"`
    Age  json.Number `json:"age,omitempty"`
}

type SearchResult struct {
    Id    string      `json:"id"`
    Score json.Number `json:"score,omitempty"`
}
```

The returned `suggestion.Imports` should still contain `[]string{"encoding/json"}` because both `Pet.Age` and `SearchResult.Score` use `json.Number`.

### `NewStructSuggestionFromSchemaJSONTry(schemaText string, typeName string, opts ...StructSchemaOption) (StructSuggestion, error)`

Parses schema JSON text and returns a suggested Go struct definition.

This is a convenience procedure for command-line tools and code generators that operate directly on schema files.

## Comparison Procedures

### `StructSchemaFinding`

Represents one comparison finding or review note.

```go
type StructSchemaFinding struct {
    Category string
    Path     Path
    Message  string
    GoType   string
    GoName   string
    JSONName string
}
```

`Category` should use stable strings so tools can filter and fail builds by finding type.

Recommended categories:

- `missing_in_schema`
- `missing_in_struct`
- `order_mismatch`
- `kind_mismatch`
- `unsupported_go_type`
- `unsupported_schema_feature`
- `default_only_in_schema`
- `required_policy_difference`
- `ambiguous_json_name`
- `recursive_type`

### `StructSchemaReport`

Represents the full comparison result.

```go
type StructSchemaReport struct {
    OK       bool
    Findings []StructSchemaFinding
}
```

`OK` should be `true` only when there are no findings that the selected policy treats as failures. Informational notes may still be present if the report has severity levels in a later version.

### `CompareStructToSchema(value any, schema JSONSchema, opts ...StructSchemaOption) StructSchemaReport`

Compares a Go struct value, pointer to a struct, or `reflect.Type` for a struct to a compiled ojson schema.

Comparison should not return an error for ordinary mismatches. Mismatches are the report's purpose. If the struct cannot be inspected or the schema root is incompatible, the report should contain findings that describe the problem.

```go
report := ojson.CompareStructToSchema(Pet{}, schema)
if !report.OK {
    for _, finding := range report.Findings {
        fmt.Println(finding.Path, finding.Category, finding.Message)
    }
}
```

### `CompareStructToSchemaTry(value any, schema JSONSchema, opts ...StructSchemaOption) (StructSchemaReport, error)`

Compares a Go struct value, pointer to a struct, or `reflect.Type` for a struct to a compiled schema, returning an error only when comparison cannot run at all.

Use errors for invalid inputs such as a non-struct `value` or an empty `JSONSchema`. Use report findings for semantic mismatches such as order, kind, missing fields, or unsupported metadata.

### `CompareStructToSchemaJSONTry(value any, schemaText string, opts ...StructSchemaOption) (StructSchemaReport, error)`

Compiles schema JSON text and compares it to a Go struct value, pointer to a struct, or `reflect.Type` for a struct.

This is the preferred entry point for command-line tools that check a struct against a schema file.

### `CompareStructToSchemaFileTry(value any, schemaPath string, opts ...StructSchemaOption) (StructSchemaReport, error)`

Compiles a schema JSON file and compares it to a Go struct value, pointer to a struct, or `reflect.Type` for a struct.

This procedure should include file read and schema compilation errors directly. Struct/schema mismatches should still be returned as `StructSchemaReport` findings.

## Struct Field Selection

When converting a Go struct to an ojson schema:

1. inspect fields in declaration order
2. include exported fields
3. skip unexported fields unless they are embedded fields that expose exported fields through normal Go JSON behavior
4. skip fields tagged `json:"-"`
5. use the `json` tag name when present
6. otherwise use the Go field name according to the project's naming policy

Recommended default naming policy: require explicit `json` tags for schema generation. This avoids ambiguous conversions such as `CustomerNumber` to `CustomerNumber` versus `customer_number`.

## Parsing `json` Tags

A Go JSON tag has a field name followed by comma-separated options:

```go
Name string `json:"name,omitempty"`
```

The field name is `name`. The option is `omitempty`.

Rules:

- `json:"name"` maps the field to schema name `name`.
- `json:"name,omitempty"` maps the field to schema name `name` and marks it as optional for comparison purposes.
- `json:",omitempty"` uses the default Go JSON field name and marks it optional.
- `json:"-"` excludes the field.
- tag options do not affect schema `kind`.

## Mapping Go Types To Schema Kinds

Use this mapping as the default conversion policy:

| Go type shape | Schema kind | Notes |
| --- | --- | --- |
| `string` | `string` | Direct mapping. |
| `bool` | `boolean` | Direct mapping. |
| signed or unsigned integers | `number` | JSON has one numeric kind. |
| floating-point numbers | `number` | Decimal round-trip concerns still apply. |
| `json.Number` | `number` | Good fit for decimal text preservation. |
| decimal package types | `number` | Require project-specific type allowlist. |
| struct | `object` | Recurse through exported fields. |
| pointer to struct | `object` | Optionality differs from kind. |
| slice or array | `array` | Item validation is limited by current schema model. |
| pointer to scalar | scalar kind | Optionality differs from kind. |
| `interface{}` or `any` | unsupported | No ojson `any` kind. |
| map | unsupported or `object` by policy | Maps do not preserve field order. |

Because JSON has only one `number` kind, converting from Go to schema loses whether the source was `int`, `float64`, `json.Number`, or a decimal type.

## Required And Optional Fields

`omitempty` should not automatically produce `required: true`.

Recommended policy:

- fields without `omitempty` may be treated as required by strict tooling
- fields with `omitempty` should be treated as optional
- pointer fields should be treated as optional unless project rules say otherwise
- defaults should come from schema metadata or explicit tooling configuration, not from struct tags

Example:

```go
type Pet struct {
    Name        string  `json:"name"`
    Age         string  `json:"age,omitempty"`
    Height      string  `json:"height,omitempty"`
    HeightUnits string  `json:"height_units,omitempty"`
    Safe        bool    `json:"safe"`
}
```

Conservative generated schema:

```json
{
  "kind": "object",
  "children": [
    { "name": "name", "kind": "string" },
    { "name": "age", "kind": "string" },
    { "name": "height", "kind": "string" },
    { "name": "height_units", "kind": "string" },
    { "name": "safe", "kind": "boolean" }
  ]
}
```

A stricter project policy could mark `name` and `safe` as required, but that should be explicit.

## Converting Struct Tags To Schema

Procedure:

1. Parse the Go package with `go/parser` and `go/ast`.
2. Find the target struct type.
3. Walk fields in declaration order.
4. Skip fields excluded by visibility or `json:"-"`.
5. Resolve the JSON field name from the tag.
6. Map the Go type to an ojson schema kind.
7. Recurse into nested structs for object children.
8. Emit schema children in struct declaration order.
9. Record warnings for unsupported fields.
10. Attach defaults only from explicit configuration.

Example input:

```go
type Pet struct {
    Name        string `json:"name"`
    Age         string `json:"age,omitempty"`
    Height      string `json:"height,omitempty"`
    HeightUnits string `json:"height_units,omitempty"`
    Safe        bool   `json:"safe"`
}
```

Example schema output:

```json
{
  "kind": "object",
  "children": [
    { "name": "name", "kind": "string" },
    { "name": "age", "kind": "string" },
    { "name": "height", "kind": "string" },
    { "name": "height_units", "kind": "string" },
    { "name": "safe", "kind": "boolean" }
  ]
}
```

If numeric fields are stored as strings in Go to preserve decimal text, the generated schema will say `string`. If the JSON document should contain JSON numbers, use a numeric Go type, `json.Number`, or a project decimal type that the converter maps to `number`.

## Comparing Struct Tags To Schema

Comparison should produce a structured report rather than a single boolean.

Procedure:

1. Convert the target struct into an intermediate ordered field list.
2. Parse the schema JSON into an intermediate ordered schema list.
3. Compare field names in order.
4. Compare field names as sets.
5. Compare schema kinds for matching fields.
6. Recurse into object fields.
7. Report unsupported Go types.
8. Report unsupported schema kinds.
9. Report required/default differences that cannot be represented in struct tags.

Recommended report categories:

- `missing_in_schema`: field appears in the struct but not the schema
- `missing_in_struct`: field appears in the schema but not the struct
- `order_mismatch`: same fields exist but order differs
- `kind_mismatch`: field names match but kinds differ
- `unsupported_go_type`: Go type cannot map to an ojson schema kind
- `unsupported_schema_feature`: schema metadata has no struct-tag equivalent
- `default_only_in_schema`: schema default exists and cannot be represented by `json` tags
- `required_policy_difference`: required status differs from the selected project policy

Example mismatch:

```go
type Pet struct {
    Name string `json:"name"`
    Safe bool   `json:"safe"`
    Age  string `json:"age,omitempty"`
}
```

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

Expected comparison findings:

- `order_mismatch`: `safe` appears before `age` in the struct, but after `age` in the schema.
- `kind_mismatch`: struct field `age` maps to `string`, but schema field `age` is `number`.

## Converting Schema To Struct Tags

Schema-to-struct conversion can generate a useful starting point, but it cannot know every desired Go type.

Procedure:

1. Parse the schema JSON.
2. Confirm the root schema kind is `object`.
3. Convert each child into an exported Go field name.
4. Preserve schema order as struct field order.
5. Add a `json` tag using the schema `name`.
6. Map schema kinds to default Go types.
7. Recurse into object children.
8. Add comments or warnings for defaults and required fields.

Default reverse mapping:

| Schema kind | Suggested Go type | Notes |
| --- | --- | --- |
| `string` | `string` | Direct mapping, unless a registered format associates a Go type. |
| `number` | `json.Number` | Preserves decimal text better than `float64`. |
| `boolean` | `bool` | Direct mapping. |
| `object` | nested struct | Generate a named or anonymous struct by policy. |
| `array` | `[]T` | Item type follows the item schema when present. |
| `null` | `*struct{}` or `any` by policy | Usually needs human review. |

### String Format Type Mapping

Applications can associate Go types with string formats for both struct-to-schema generation and schema-to-struct suggestions.

```go
formats := ojson.NewStringFormatRegistry()
_ = formats.Register("Time", timeValidator, reflect.TypeOf(time.Time{}))

schema, err := ojson.NewSchemaFromStructTry(
    Movie{},
    ojson.StringFormatType(reflect.TypeOf(time.Time{}), "Time"),
)
```

When suggesting structs from a compiled schema, use the format's associated Go type when available:

```text
kind: string, format: Time -> time.Time
kind: string, format: email -> string
```

If the associated type comes from another package, add its import path to `StructSuggestion.Imports`. Runtime conversion continues to use named-string handling and Go's `json.Marshaler`, `json.Unmarshaler`, `encoding.TextMarshaler`, and `encoding.TextUnmarshaler` interfaces.

Example schema:

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

Suggested Go struct:

```go
type Pet struct {
    Name string      `json:"name"`
    Age  json.Number `json:"age,omitempty"`
    Safe bool        `json:"safe,omitempty"`
}
```

The generated struct should include review notes:

- `name` is required by schema
- `safe` has schema default `true`
- `age` uses `json.Number` by default but may need a project-specific numeric type

## Field Name Generation

When generating Go names from schema field names:

- split on underscores, hyphens, and spaces
- capitalize each word
- preserve common initialisms by project policy
- avoid Go keywords
- make duplicate names unique with a clear suffix

Examples:

| Schema name | Go field name |
| --- | --- |
| `name` | `Name` |
| `customer_number` | `CustomerNumber` |
| `height_units` | `HeightUnits` |
| `type` | `TypeValue` |

## Limitations

Struct tags and ojson schemas do not contain the same information.

Struct tags cannot represent:

- schema field order independently of struct declaration order
- default values
- all required-field policies
- unknown field handling
- schema migration guidance

Ojson schemas cannot fully represent:

- exact Go numeric type choice
- custom marshal/unmarshal behavior
- embedded field promotion rules
- map key/value types
- interface implementations
- validation beyond basic JSON kind

Tooling should preserve these limitations in its output. A warning is better than a generated schema or struct that looks precise but is not.
