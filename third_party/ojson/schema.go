package ojson

import (
	"fmt"
	"math/big"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
)

// JSONSchema is the compiled runtime form of an ojson schema document.
type JSONSchema struct {
	root *schemaEntry
}

// SchemaEntry is a read-only view of a compiled schema entry.
type SchemaEntry struct {
	entry *schemaEntry
}

type schemaEntry struct {
	Name         string
	Kind         JSONKind
	Descriptions map[string]string

	Children    []*schemaEntry
	childByName map[string]*schemaEntry
	Items       *schemaEntry

	Default    JSONValue
	HasDefault bool
	Required   bool
	Nullable   bool

	Min     *big.Rat
	MinText string
	Max     *big.Rat
	MaxText string
	Integer bool

	Enum    []string
	enumSet map[string]struct{}

	MinLength *int
	MaxLength *int
	Format    string

	formatValidator StringFormatValidator
	formatGoType    reflect.Type

	Custom JSONValue
}

// Kind returns the root schema kind.
func (s JSONSchema) Kind() JSONKind {
	if s.root == nil {
		return KindVoid
	}
	return s.root.Kind
}

// Root returns a read-only view of the compiled root schema entry.
func (s JSONSchema) Root() SchemaEntry {
	return SchemaEntry{entry: s.root}
}

// Valid reports whether the schema entry view refers to a compiled entry.
func (e SchemaEntry) Valid() bool {
	return e.entry != nil
}

// Name returns the schema entry name, or an empty string for the root.
func (e SchemaEntry) Name() string {
	if e.entry == nil {
		return ""
	}
	return e.entry.Name
}

// Kind returns the schema entry kind.
func (e SchemaEntry) Kind() JSONKind {
	if e.entry == nil {
		return KindVoid
	}
	return e.entry.Kind
}

// Format returns the string format name, if any.
func (e SchemaEntry) Format() string {
	if e.entry == nil {
		return ""
	}
	return e.entry.Format
}

// Custom returns a clone of opaque custom metadata, or Void when absent.
func (e SchemaEntry) Custom() JSONValue {
	if e.entry == nil || e.entry.Custom.IsVoid() {
		return NewVoid()
	}
	return cloneJSONValue(e.entry.Custom)
}

// Child returns the named object child schema entry, if present.
func (e SchemaEntry) Child(name string) SchemaEntry {
	if e.entry == nil {
		return SchemaEntry{}
	}
	return SchemaEntry{entry: e.entry.childByName[name]}
}

// Children returns object child schema entries in schema order.
func (e SchemaEntry) Children() []SchemaEntry {
	if e.entry == nil || len(e.entry.Children) == 0 {
		return nil
	}
	children := make([]SchemaEntry, 0, len(e.entry.Children))
	for _, child := range e.entry.Children {
		children = append(children, SchemaEntry{entry: child})
	}
	return children
}

// Items returns the array item schema entry, if present.
func (e SchemaEntry) Items() SchemaEntry {
	if e.entry == nil {
		return SchemaEntry{}
	}
	return SchemaEntry{entry: e.entry.Items}
}

func CompileSchemaJSON(schemaText string, opts ...SchemaCompileOption) (JSONSchema, error) {
	return CompileSchemaBytes([]byte(schemaText), opts...)
}

func CompileSchemaBytes(schemaBytes []byte, opts ...SchemaCompileOption) (JSONSchema, error) {
	value, err := ReadBytesNoSchema(schemaBytes)
	if err != nil {
		return JSONSchema{}, err
	}

	cfg := newSchemaCompileConfig(opts...)
	root, err := compileSchemaEntry(value, RootPath(), false, cfg)
	if err != nil {
		return JSONSchema{}, err
	}

	return JSONSchema{root: root}, nil
}

func CompileSchemaFile(path string, opts ...SchemaCompileOption) (JSONSchema, error) {
	schemaBytes, err := os.ReadFile(path)
	if err != nil {
		return JSONSchema{}, err
	}

	return CompileSchemaBytes(schemaBytes, opts...)
}

func compileSchemaEntry(value JSONValue, path Path, requireName bool, cfg schemaCompileConfig) (*schemaEntry, error) {
	if !value.IsObject() {
		return nil, pathError(path, "schema entry must be an object")
	}

	if err := validateSchemaFields(value, path); err != nil {
		return nil, err
	}

	entry := &schemaEntry{
		Descriptions: make(map[string]string),
		childByName:  make(map[string]*schemaEntry),
		Custom:       NewVoid(),
	}

	if requireName {
		nameValue := value.Get("name")
		if !nameValue.IsString() {
			return nil, pathError(path, "object child schema must have a string name")
		}
		entry.Name = nameValue.String()
		if entry.Name == "" {
			return nil, pathError(path, "object child schema name must not be empty")
		}
		path = path.Field(entry.Name)
	}

	kindValue := value.Get("kind")
	if !kindValue.IsString() {
		return nil, pathError(path, "schema entry must have a string kind")
	}
	kind, err := schemaKindFromString(kindValue.String())
	if err != nil {
		return nil, pathError(path, "%s", err.Error())
	}
	entry.Kind = kind

	for _, field := range value.node.objectValue {
		switch field.Key {
		case "name", "kind":
			continue
		case "required":
			required, err := schemaBoolField(field.Value, path, "required")
			if err != nil {
				return nil, err
			}
			entry.Required = required
		case "nullable":
			nullable, err := schemaBoolField(field.Value, path, "nullable")
			if err != nil {
				return nil, err
			}
			entry.Nullable = nullable
		case "default":
			entry.Default = field.Value
			entry.HasDefault = true
		case "children":
			if entry.Kind != KindObject {
				return nil, pathError(path, "children is only supported for object schemas")
			}
			if err := compileChildren(entry, field.Value, path, cfg); err != nil {
				return nil, err
			}
		case "items":
			if entry.Kind != KindArray {
				return nil, pathError(path, "items is only supported for array schemas")
			}
			itemSchema, err := compileSchemaEntry(field.Value, path, false, cfg)
			if err != nil {
				return nil, err
			}
			entry.Items = itemSchema
		case "min":
			if entry.Kind != KindNumber {
				return nil, pathError(path, "min is only supported for number schemas")
			}
			min, err := schemaNumberField(field.Value, path, "min")
			if err != nil {
				return nil, err
			}
			entry.Min = min
			entry.MinText = field.Value.String()
		case "max":
			if entry.Kind != KindNumber {
				return nil, pathError(path, "max is only supported for number schemas")
			}
			max, err := schemaNumberField(field.Value, path, "max")
			if err != nil {
				return nil, err
			}
			entry.Max = max
			entry.MaxText = field.Value.String()
		case "integer":
			if entry.Kind != KindNumber {
				return nil, pathError(path, "integer is only supported for number schemas")
			}
			integer, err := schemaBoolField(field.Value, path, "integer")
			if err != nil {
				return nil, err
			}
			entry.Integer = integer
		case "enum":
			if entry.Kind != KindString {
				return nil, pathError(path, "enum is only supported for string schemas")
			}
			if err := compileStringEnum(entry, field.Value, path); err != nil {
				return nil, err
			}
		case "min_length":
			if entry.Kind != KindString {
				return nil, pathError(path, "min_length is only supported for string schemas")
			}
			minLength, err := schemaNonNegativeIntField(field.Value, path, "min_length")
			if err != nil {
				return nil, err
			}
			entry.MinLength = &minLength
		case "max_length":
			if entry.Kind != KindString {
				return nil, pathError(path, "max_length is only supported for string schemas")
			}
			maxLength, err := schemaNonNegativeIntField(field.Value, path, "max_length")
			if err != nil {
				return nil, err
			}
			entry.MaxLength = &maxLength
		case "format":
			if entry.Kind != KindString {
				return nil, pathError(path, "format is only supported for string schemas")
			}
			format, err := schemaStringField(field.Value, path, "format")
			if err != nil {
				return nil, err
			}
			if err := resolveStringFormat(entry, format, path, cfg); err != nil {
				return nil, err
			}
		case "custom":
			entry.Custom = cloneJSONValue(field.Value)
		default:
			if strings.HasPrefix(field.Key, "description-") {
				lang := strings.TrimPrefix(field.Key, "description-")
				if !isLanguageCode(lang) {
					return nil, pathError(path, "invalid description language code %q", lang)
				}
				description, err := schemaStringField(field.Value, path, field.Key)
				if err != nil {
					return nil, err
				}
				entry.Descriptions[lang] = description
			}
		}
	}

	if err := validateSchemaEntry(entry, path); err != nil {
		return nil, err
	}

	return entry, nil
}

func resolveStringFormat(entry *schemaEntry, format string, path Path, cfg schemaCompileConfig) error {
	if format == "" {
		return pathError(path, "string format must not be empty")
	}
	if isBuiltinStringFormat(format) {
		entry.Format = format
		return nil
	}
	def, ok := cfg.formats.lookup(format)
	if !ok {
		return pathError(path, "unsupported string format %q", format)
	}
	entry.Format = format
	entry.formatValidator = def.validator
	entry.formatGoType = def.goType
	return nil
}

func validateSchemaFields(value JSONValue, path Path) error {
	for _, field := range value.node.objectValue {
		if isKnownSchemaField(field.Key) || strings.HasPrefix(field.Key, "description-") {
			continue
		}
		return pathError(path, "unsupported schema field %q", field.Key)
	}
	return nil
}

func isKnownSchemaField(key string) bool {
	switch key {
	case "kind", "name", "children", "default", "required", "nullable", "min", "max", "integer", "enum", "min_length", "max_length", "format", "items", "custom":
		return true
	default:
		return false
	}
}

func schemaKindFromString(kind string) (JSONKind, error) {
	switch kind {
	case "object":
		return KindObject, nil
	case "array":
		return KindArray, nil
	case "string":
		return KindString, nil
	case "number":
		return KindNumber, nil
	case "boolean":
		return KindBoolean, nil
	case "null":
		return KindNull, nil
	default:
		return KindVoid, fmt.Errorf("unsupported schema kind %q", kind)
	}
}

func compileChildren(entry *schemaEntry, value JSONValue, path Path, cfg schemaCompileConfig) error {
	if !value.IsArray() {
		return pathError(path, "children must be an array")
	}

	for i, childValue := range value.node.arrayValue {
		childProbePath := path.Index(i)
		nameValue := childValue.Get("name")
		if nameValue.IsString() {
			childProbePath = path
		}

		child, err := compileSchemaEntry(childValue, childProbePath, true, cfg)
		if err != nil {
			return err
		}
		if _, exists := entry.childByName[child.Name]; exists {
			return pathError(path.Field(child.Name), "duplicate child schema name")
		}
		entry.Children = append(entry.Children, child)
		entry.childByName[child.Name] = child
	}
	return nil
}

func compileStringEnum(entry *schemaEntry, value JSONValue, path Path) error {
	if !value.IsArray() {
		return pathError(path, "enum must be an array")
	}

	entry.enumSet = make(map[string]struct{}, value.Len())
	for i, item := range value.node.arrayValue {
		if !item.IsString() {
			return pathError(path.Index(i), "enum values must be strings")
		}
		enumValue := item.String()
		entry.Enum = append(entry.Enum, enumValue)
		entry.enumSet[enumValue] = struct{}{}
	}
	return nil
}

func schemaBoolField(value JSONValue, path Path, field string) (bool, error) {
	if !value.IsBoolean() {
		return false, pathError(path, "%s must be a boolean", field)
	}
	return value.node.boolValue, nil
}

func schemaStringField(value JSONValue, path Path, field string) (string, error) {
	if !value.IsString() {
		return "", pathError(path, "%s must be a string", field)
	}
	return value.String(), nil
}

func schemaNumberField(value JSONValue, path Path, field string) (*big.Rat, error) {
	if !value.IsNumber() {
		return nil, pathError(path, "%s must be a number", field)
	}
	number, err := parseJSONNumberRat(value.String())
	if err != nil {
		return nil, pathError(path, "%s must be a valid number: %v", field, err)
	}
	return number, nil
}

func schemaNonNegativeIntField(value JSONValue, path Path, field string) (int, error) {
	number, err := schemaNumberField(value, path, field)
	if err != nil {
		return 0, err
	}
	if number.Denom().Cmp(big.NewInt(1)) != 0 {
		return 0, pathError(path, "%s must be an integer", field)
	}
	if number.Sign() < 0 {
		return 0, pathError(path, "%s must be non-negative", field)
	}
	if !number.Num().IsInt64() {
		return 0, pathError(path, "%s is too large", field)
	}
	return int(number.Num().Int64()), nil
}

func validateSchemaEntry(entry *schemaEntry, path Path) error {
	if entry.Min != nil && entry.Max != nil && entry.Min.Cmp(entry.Max) > 0 {
		return pathError(path, "min must be less than or equal to max")
	}
	if entry.MinLength != nil && entry.MaxLength != nil && *entry.MinLength > *entry.MaxLength {
		return pathError(path, "min_length must be less than or equal to max_length")
	}
	if entry.HasDefault {
		if err := validateValueAgainstSchemaEntry(entry.Default, entry, path); err != nil {
			return err
		}
	}
	return nil
}

func validateValueAgainstSchemaEntry(value JSONValue, entry *schemaEntry, path Path) error {
	if value.IsNull() {
		if entry.Nullable || entry.Kind == KindNull {
			return nil
		}
		return pathError(path, "default null requires nullable true")
	}
	if value.Kind() != entry.Kind {
		return pathError(path, "default must match schema kind %s, got %s", entry.Kind, value.Kind())
	}

	switch entry.Kind {
	case KindObject:
		for _, field := range value.node.objectValue {
			child, ok := entry.childByName[field.Key]
			if !ok {
				continue
			}
			if err := validateValueAgainstSchemaEntry(field.Value, child, path.Field(field.Key)); err != nil {
				return err
			}
		}
	case KindArray:
		if entry.Items == nil {
			return nil
		}
		for i, item := range value.node.arrayValue {
			if err := validateValueAgainstSchemaEntry(item, entry.Items, path.Index(i)); err != nil {
				return err
			}
		}
	case KindString:
		return validateStringValueAgainstSchema(value.String(), entry, path)
	case KindNumber:
		return validateNumberValueAgainstSchema(value, entry, path)
	}
	return nil
}

func validateStringValueAgainstSchema(value string, entry *schemaEntry, path Path) error {
	length := len([]rune(value))
	if entry.MinLength != nil && length < *entry.MinLength {
		return pathError(path, "string length is below min_length %d", *entry.MinLength)
	}
	if entry.MaxLength != nil && length > *entry.MaxLength {
		return pathError(path, "string length is above max_length %d", *entry.MaxLength)
	}
	if entry.enumSet != nil {
		if _, ok := entry.enumSet[value]; !ok {
			return pathError(path, "string is not in enum")
		}
	}
	if entry.Format == "" {
		return nil
	}
	if entry.formatValidator != nil {
		if err := entry.formatValidator.ValidateString(value); err != nil {
			return pathError(path, "invalid %s format: %v", entry.Format, err)
		}
		return nil
	}
	if !matchesStringFormat(value, entry.Format) {
		return pathError(path, "invalid %s format", entry.Format)
	}
	return nil
}

func validateNumberValueAgainstSchema(value JSONValue, entry *schemaEntry, path Path) error {
	number, err := parseJSONNumberRat(value.String())
	if err != nil {
		return pathError(path, "invalid number: %v", err)
	}
	if entry.Integer && number.Denom().Cmp(big.NewInt(1)) != 0 {
		return pathError(path, "expected integer number")
	}
	if entry.Min != nil && number.Cmp(entry.Min) < 0 {
		return pathError(path, "number is below min %s", entry.MinText)
	}
	if entry.Max != nil && number.Cmp(entry.Max) > 0 {
		return pathError(path, "number is above max %s", entry.MaxText)
	}
	return nil
}

func matchesStringFormat(value string, format string) bool {
	switch format {
	case "email":
		return strings.Count(value, "@") == 1 && !strings.HasPrefix(value, "@") && !strings.HasSuffix(value, "@")
	case "tel":
		return value != "" && regexp.MustCompile(`^[0-9+(). -]+$`).MatchString(value)
	case "url":
		parsed, err := url.ParseRequestURI(value)
		return err == nil && parsed.Scheme != "" && parsed.Host != ""
	default:
		return false
	}
}

var languageCodePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]+)*$`)

func isLanguageCode(value string) bool {
	return languageCodePattern.MatchString(value)
}
