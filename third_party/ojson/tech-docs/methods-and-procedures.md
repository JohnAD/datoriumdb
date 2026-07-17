# Methods And Procedures

This is the index for ojson method and procedure documentation. The API documentation is split by value kind so each document can grow without making one large reference file difficult to read.

## Common Behavior

- [`methods-common.md`](methods-common.md): failure handling convention, document reading, schema compilation, schema builders, schema-backed document procedures, state methods, serialization, and schema procedures.

## Kind-Specific Methods

- [`methods-object.md`](methods-object.md): ordered objects, native map/struct constructors and exports, item-default imports, `IsObject`, `Get`, `HasField`, mutation/removal methods, and object ordering.
- [`methods-array.md`](methods-array.md): arrays, `NewArray`, `IsArray`, native slice constructors, `At`, iteration, append/prepend/insert/remove methods, `Compress`, and native slice export methods for strings, numbers, typed numeric values, and booleans.
- [`methods-string.md`](methods-string.md): strings, `NewString`, `NewEmptyString`, native string constructors, `IsString`, `GetString`, `ToString` methods, and string content behavior.
- [`methods-number.md`](methods-number.md): numbers, `IsNumber`, validation, preparation, constructors, integer/float constructors, `GetNumber`, and numeric export methods.
- [`methods-boolean.md`](methods-boolean.md): booleans, `NewBoolean`, `GetBoolean`, `IsBoolean`, `ToBool` methods, and boolean empty-state behavior.
- [`methods-null.md`](methods-null.md): explicit JSON `null`, `NewNull`, `IsNull`, and the ojson convention that null means unknown.
- [`methods-void.md`](methods-void.md): absence, `NewVoid`, `IsMissing`, `IsVoid`, failed traversal behavior, and serialization rules for `Void`.

## Related Documentation

- [`concepts.md`](concepts.md): core data model, ordered objects, `Void`, `Null`, numbers, and arrays.
- [`schema-format.md`](schema-format.md): schema JSON document structure and behavior.
- [`schema-builders.md`](schema-builders.md): programmatic schema builder methods and examples.
- [`error-paths.md`](error-paths.md): diagnostic path format for parse, validation, and conversion errors.
- [`struct-tags-and-schema.md`](struct-tags-and-schema.md): converting and comparing Go `json` struct tags with ojson schema documents.
- [`examples.md`](examples.md): practical examples for common workflows.
