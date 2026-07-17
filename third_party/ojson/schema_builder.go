package ojson

import "fmt"

type LanguageCode string

const (
	LangEN LanguageCode = "en"
	LangES LanguageCode = "es"
	LangFR LanguageCode = "fr"
	LangDE LanguageCode = "de"
	LangIT LanguageCode = "it"
	LangPT LanguageCode = "pt"
	LangZH LanguageCode = "zh"
	LangJA LanguageCode = "ja"
	LangKO LanguageCode = "ko"
)

func ParseLanguageCode(value string) (LanguageCode, error) {
	if !isLanguageCode(value) {
		return "", fmt.Errorf("invalid language code %q", value)
	}
	return LanguageCode(value), nil
}

type StringFormat string

const (
	FormatEmail StringFormat = "email"
	FormatTel   StringFormat = "tel"
	FormatURL   StringFormat = "url"
)

type ObjectOption interface {
	applyObject(*schemaBuilderEntry)
}

type ArrayOption interface {
	applyArray(*schemaBuilderEntry)
}

type StringOption interface {
	applyString(*schemaBuilderEntry)
}

type NumberOption interface {
	applyNumber(*schemaBuilderEntry)
}

type BooleanOption interface {
	applyBoolean(*schemaBuilderEntry)
}

type SchemaObjectBuilder struct {
	entry *schemaBuilderEntry
}

type SchemaArrayBuilder struct {
	entry *schemaBuilderEntry
}

type schemaBuilderEntry struct {
	name        string
	kind        JSONKind
	children    []*schemaBuilderEntry
	items       *schemaBuilderEntry
	fields      map[string]JSONValue
	buildErrors []error
}

func NewSchemaObjectBuilder(opts ...ObjectOption) *SchemaObjectBuilder {
	entry := newSchemaBuilderEntry(KindObject, "")
	for _, opt := range opts {
		opt.applyObject(entry)
	}
	return &SchemaObjectBuilder{entry: entry}
}

func NewSchemaArrayBuilder(opts ...ArrayOption) *SchemaArrayBuilder {
	entry := newSchemaBuilderEntry(KindArray, "")
	for _, opt := range opts {
		opt.applyArray(entry)
	}
	return &SchemaArrayBuilder{entry: entry}
}

func newSchemaBuilderEntry(kind JSONKind, name string) *schemaBuilderEntry {
	return &schemaBuilderEntry{
		name:   name,
		kind:   kind,
		fields: make(map[string]JSONValue),
	}
}

func (b *SchemaObjectBuilder) ObjectField(name string, configure func(*SchemaObjectBuilder), opts ...ObjectOption) *SchemaObjectBuilder {
	child := newSchemaBuilderEntry(KindObject, name)
	for _, opt := range opts {
		opt.applyObject(child)
	}
	if configure != nil {
		configure(&SchemaObjectBuilder{entry: child})
	}
	b.entry.children = append(b.entry.children, child)
	return b
}

func (b *SchemaObjectBuilder) ArrayField(name string, configure func(*SchemaArrayBuilder), opts ...ArrayOption) *SchemaObjectBuilder {
	child := newSchemaBuilderEntry(KindArray, name)
	for _, opt := range opts {
		opt.applyArray(child)
	}
	if configure != nil {
		configure(&SchemaArrayBuilder{entry: child})
	}
	b.entry.children = append(b.entry.children, child)
	return b
}

func (b *SchemaObjectBuilder) StringField(name string, opts ...StringOption) *SchemaObjectBuilder {
	child := newSchemaBuilderEntry(KindString, name)
	for _, opt := range opts {
		opt.applyString(child)
	}
	b.entry.children = append(b.entry.children, child)
	return b
}

func (b *SchemaObjectBuilder) NumberField(name string, opts ...NumberOption) *SchemaObjectBuilder {
	child := newSchemaBuilderEntry(KindNumber, name)
	for _, opt := range opts {
		opt.applyNumber(child)
	}
	b.entry.children = append(b.entry.children, child)
	return b
}

func (b *SchemaObjectBuilder) BooleanField(name string, opts ...BooleanOption) *SchemaObjectBuilder {
	child := newSchemaBuilderEntry(KindBoolean, name)
	for _, opt := range opts {
		opt.applyBoolean(child)
	}
	b.entry.children = append(b.entry.children, child)
	return b
}

func (b *SchemaArrayBuilder) ObjectItems(configure func(*SchemaObjectBuilder), opts ...ObjectOption) *SchemaArrayBuilder {
	item := newSchemaBuilderEntry(KindObject, "")
	for _, opt := range opts {
		opt.applyObject(item)
	}
	if configure != nil {
		configure(&SchemaObjectBuilder{entry: item})
	}
	b.entry.items = item
	return b
}

func (b *SchemaArrayBuilder) ArrayItems(configure func(*SchemaArrayBuilder), opts ...ArrayOption) *SchemaArrayBuilder {
	item := newSchemaBuilderEntry(KindArray, "")
	for _, opt := range opts {
		opt.applyArray(item)
	}
	if configure != nil {
		configure(&SchemaArrayBuilder{entry: item})
	}
	b.entry.items = item
	return b
}

func (b *SchemaArrayBuilder) StringItems(opts ...StringOption) *SchemaArrayBuilder {
	item := newSchemaBuilderEntry(KindString, "")
	for _, opt := range opts {
		opt.applyString(item)
	}
	b.entry.items = item
	return b
}

func (b *SchemaArrayBuilder) NumberItems(opts ...NumberOption) *SchemaArrayBuilder {
	item := newSchemaBuilderEntry(KindNumber, "")
	for _, opt := range opts {
		opt.applyNumber(item)
	}
	b.entry.items = item
	return b
}

func (b *SchemaArrayBuilder) BooleanItems(opts ...BooleanOption) *SchemaArrayBuilder {
	item := newSchemaBuilderEntry(KindBoolean, "")
	for _, opt := range opts {
		opt.applyBoolean(item)
	}
	b.entry.items = item
	return b
}

func (b *SchemaObjectBuilder) Build(opts ...SchemaCompileOption) (JSONSchema, error) {
	if err := b.entry.firstBuildError(); err != nil {
		return JSONSchema{}, err
	}
	return CompileSchemaJSON(b.entry.toJSONValue(true).ToJSON(), opts...)
}

func (b *SchemaObjectBuilder) MustBuild(opts ...SchemaCompileOption) JSONSchema {
	schema, err := b.Build(opts...)
	if err != nil {
		panic(err)
	}
	return schema
}

func (b *SchemaArrayBuilder) Build(opts ...SchemaCompileOption) (JSONSchema, error) {
	if err := b.entry.firstBuildError(); err != nil {
		return JSONSchema{}, err
	}
	return CompileSchemaJSON(b.entry.toJSONValue(true).ToJSON(), opts...)
}

func (b *SchemaArrayBuilder) MustBuild(opts ...SchemaCompileOption) JSONSchema {
	schema, err := b.Build(opts...)
	if err != nil {
		panic(err)
	}
	return schema
}

func (e *schemaBuilderEntry) firstBuildError() error {
	if len(e.buildErrors) > 0 {
		return e.buildErrors[0]
	}
	for _, child := range e.children {
		if err := child.firstBuildError(); err != nil {
			return err
		}
	}
	if e.items != nil {
		return e.items.firstBuildError()
	}
	return nil
}

func (e *schemaBuilderEntry) addError(format string, args ...any) {
	e.buildErrors = append(e.buildErrors, fmt.Errorf(format, args...))
}

func (e *schemaBuilderEntry) toJSONValue(root bool) JSONValue {
	value := NewObject()
	if !root {
		value.Set("name", NewString(e.name))
	}
	value.Set("kind", NewString(schemaKindName(e.kind)))
	for key, fieldValue := range e.fields {
		value.Set(key, fieldValue)
	}
	if len(e.children) > 0 {
		children := NewArray()
		for _, child := range e.children {
			children.appendRaw(child.toJSONValue(false))
		}
		value.Set("children", children)
	}
	if e.items != nil {
		value.Set("items", e.items.toJSONValue(true))
	}
	return value
}

func schemaKindName(kind JSONKind) string {
	switch kind {
	case KindObject:
		return "object"
	case KindArray:
		return "array"
	case KindString:
		return "string"
	case KindNumber:
		return "number"
	case KindBoolean:
		return "boolean"
	case KindNull:
		return "null"
	default:
		return "void"
	}
}

type descriptionOption struct {
	lang LanguageCode
	text string
}

func Description(lang LanguageCode, text string) descriptionOption {
	return descriptionOption{lang: lang, text: text}
}

func (o descriptionOption) applyObject(entry *schemaBuilderEntry)  { o.apply(entry) }
func (o descriptionOption) applyArray(entry *schemaBuilderEntry)   { o.apply(entry) }
func (o descriptionOption) applyString(entry *schemaBuilderEntry)  { o.apply(entry) }
func (o descriptionOption) applyNumber(entry *schemaBuilderEntry)  { o.apply(entry) }
func (o descriptionOption) applyBoolean(entry *schemaBuilderEntry) { o.apply(entry) }
func (o descriptionOption) apply(entry *schemaBuilderEntry) {
	if !isLanguageCode(string(o.lang)) {
		entry.addError("invalid language code %q", o.lang)
		return
	}
	entry.fields["description-"+string(o.lang)] = NewString(o.text)
}

type requiredOption struct{}

func Required() requiredOption {
	return requiredOption{}
}

func (o requiredOption) applyObject(entry *schemaBuilderEntry) {
	entry.fields["required"] = NewBoolean(true)
}
func (o requiredOption) applyArray(entry *schemaBuilderEntry) {
	entry.fields["required"] = NewBoolean(true)
}
func (o requiredOption) applyString(entry *schemaBuilderEntry) {
	entry.fields["required"] = NewBoolean(true)
}
func (o requiredOption) applyNumber(entry *schemaBuilderEntry) {
	entry.fields["required"] = NewBoolean(true)
}
func (o requiredOption) applyBoolean(entry *schemaBuilderEntry) {
	entry.fields["required"] = NewBoolean(true)
}

type nullableOption struct{}

func Nullable() nullableOption {
	return nullableOption{}
}

func (o nullableOption) applyObject(entry *schemaBuilderEntry) {
	entry.fields["nullable"] = NewBoolean(true)
}
func (o nullableOption) applyArray(entry *schemaBuilderEntry) {
	entry.fields["nullable"] = NewBoolean(true)
}
func (o nullableOption) applyString(entry *schemaBuilderEntry) {
	entry.fields["nullable"] = NewBoolean(true)
}
func (o nullableOption) applyNumber(entry *schemaBuilderEntry) {
	entry.fields["nullable"] = NewBoolean(true)
}
func (o nullableOption) applyBoolean(entry *schemaBuilderEntry) {
	entry.fields["nullable"] = NewBoolean(true)
}

type defaultOption struct {
	value JSONValue
}

func Default(value JSONValue) defaultOption {
	return defaultOption{value: value}
}

func (o defaultOption) applyObject(entry *schemaBuilderEntry) { entry.fields["default"] = o.value }
func (o defaultOption) applyArray(entry *schemaBuilderEntry)  { entry.fields["default"] = o.value }

type customOption struct {
	value JSONValue
}

func Custom(value JSONValue) customOption {
	return customOption{value: value}
}

func (o customOption) applyObject(entry *schemaBuilderEntry)  { entry.fields["custom"] = o.value }
func (o customOption) applyArray(entry *schemaBuilderEntry)   { entry.fields["custom"] = o.value }
func (o customOption) applyString(entry *schemaBuilderEntry)  { entry.fields["custom"] = o.value }
func (o customOption) applyNumber(entry *schemaBuilderEntry)  { entry.fields["custom"] = o.value }
func (o customOption) applyBoolean(entry *schemaBuilderEntry) { entry.fields["custom"] = o.value }

type customStringOption struct {
	value string
}

func CustomString(value string) customStringOption {
	return customStringOption{value: value}
}

func (o customStringOption) applyObject(entry *schemaBuilderEntry) {
	entry.fields["custom"] = NewString(o.value)
}
func (o customStringOption) applyArray(entry *schemaBuilderEntry) {
	entry.fields["custom"] = NewString(o.value)
}
func (o customStringOption) applyString(entry *schemaBuilderEntry) {
	entry.fields["custom"] = NewString(o.value)
}
func (o customStringOption) applyNumber(entry *schemaBuilderEntry) {
	entry.fields["custom"] = NewString(o.value)
}
func (o customStringOption) applyBoolean(entry *schemaBuilderEntry) {
	entry.fields["custom"] = NewString(o.value)
}

type defaultNullOption struct{}

func DefaultNull() defaultNullOption {
	return defaultNullOption{}
}

func (o defaultNullOption) applyObject(entry *schemaBuilderEntry) {
	entry.fields["default"] = NewNull()
}
func (o defaultNullOption) applyArray(entry *schemaBuilderEntry) { entry.fields["default"] = NewNull() }
func (o defaultNullOption) applyString(entry *schemaBuilderEntry) {
	entry.fields["default"] = NewNull()
}
func (o defaultNullOption) applyNumber(entry *schemaBuilderEntry) {
	entry.fields["default"] = NewNull()
}
func (o defaultNullOption) applyBoolean(entry *schemaBuilderEntry) {
	entry.fields["default"] = NewNull()
}

type defaultStringOption struct {
	value string
}

func DefaultString(value string) StringOption {
	return defaultStringOption{value: value}
}

func (o defaultStringOption) applyString(entry *schemaBuilderEntry) {
	entry.fields["default"] = NewString(o.value)
}

type minLengthOption uint16

func MinLength(value uint16) StringOption {
	return minLengthOption(value)
}

func (o minLengthOption) applyString(entry *schemaBuilderEntry) {
	entry.fields["min_length"] = NewNumberFromInt(uint16(o))
}

type maxLengthOption uint16

func MaxLength(value uint16) StringOption {
	return maxLengthOption(value)
}

func (o maxLengthOption) applyString(entry *schemaBuilderEntry) {
	entry.fields["max_length"] = NewNumberFromInt(uint16(o))
}

type enumOption []string

func Enum(values ...string) StringOption {
	return enumOption(values)
}

func (o enumOption) applyString(entry *schemaBuilderEntry) {
	values := NewArray()
	for _, value := range o {
		values.appendRaw(NewString(value))
	}
	entry.fields["enum"] = values
}

type formatOption StringFormat

func Format(value StringFormat) StringOption {
	return formatOption(value)
}

func (o formatOption) applyString(entry *schemaBuilderEntry) {
	entry.fields["format"] = NewString(string(o))
}

type defaultNumberOption struct {
	value string
}

func DefaultNumber(value string) NumberOption {
	return defaultNumberOption{value: value}
}

func (o defaultNumberOption) applyNumber(entry *schemaBuilderEntry) {
	value, err := NewNumberTry(o.value)
	if err != nil {
		entry.addError("invalid default number %q", o.value)
		return
	}
	entry.fields["default"] = value
}

type defaultIntOption int64

func DefaultInt(value int64) NumberOption {
	return defaultIntOption(value)
}

func (o defaultIntOption) applyNumber(entry *schemaBuilderEntry) {
	entry.fields["default"] = NewNumberFromInt(int64(o))
}

type integerOption struct{}

func Integer() NumberOption {
	return integerOption{}
}

func (o integerOption) applyNumber(entry *schemaBuilderEntry) {
	entry.fields["integer"] = NewBoolean(true)
}

type minOption struct {
	value string
}

func Min(value string) NumberOption {
	return minOption{value: value}
}

func (o minOption) applyNumber(entry *schemaBuilderEntry) {
	value, err := NewNumberTry(o.value)
	if err != nil {
		entry.addError("invalid min number %q", o.value)
		return
	}
	entry.fields["min"] = value
}

type maxOption struct {
	value string
}

func Max(value string) NumberOption {
	return maxOption{value: value}
}

func (o maxOption) applyNumber(entry *schemaBuilderEntry) {
	value, err := NewNumberTry(o.value)
	if err != nil {
		entry.addError("invalid max number %q", o.value)
		return
	}
	entry.fields["max"] = value
}

type defaultBoolOption bool

func DefaultBool(value bool) BooleanOption {
	return defaultBoolOption(value)
}

func (o defaultBoolOption) applyBoolean(entry *schemaBuilderEntry) {
	entry.fields["default"] = NewBoolean(bool(o))
}
