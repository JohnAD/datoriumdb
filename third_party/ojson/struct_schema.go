package ojson

import (
	"fmt"
	"go/format"
	"os"
	"reflect"
	"sort"
	"strings"
	"unicode"
)

type StructSchemaOption interface {
	applyStructSchemaOption(*structSchemaConfig)
}

type structSchemaConfig struct {
	requireExplicitJSONTags bool
	requiredFromNonOmit     bool
	optionalPointers        bool
	allowMapObjects         bool
	numberTypes             map[string]struct{}
	formatTypes             map[reflect.Type]string
	formats                 *StringFormatRegistry
}

func newStructSchemaConfig(opts ...StructSchemaOption) structSchemaConfig {
	cfg := structSchemaConfig{
		requireExplicitJSONTags: true,
		optionalPointers:        true,
		numberTypes:             map[string]struct{}{},
		formatTypes:             map[reflect.Type]string{},
	}
	for _, opt := range opts {
		opt.applyStructSchemaOption(&cfg)
	}
	return cfg
}

type requireExplicitJSONTagsOption struct{}

func RequireExplicitJSONTags() StructSchemaOption {
	return requireExplicitJSONTagsOption{}
}

func (o requireExplicitJSONTagsOption) applyStructSchemaOption(cfg *structSchemaConfig) {
	cfg.requireExplicitJSONTags = true
}

type allowDefaultFieldNamesOption struct{}

func AllowDefaultFieldNames() StructSchemaOption {
	return allowDefaultFieldNamesOption{}
}

func (o allowDefaultFieldNamesOption) applyStructSchemaOption(cfg *structSchemaConfig) {
	cfg.requireExplicitJSONTags = false
}

type requiredFromNonOmitEmptyOption struct{}

func RequiredFromNonOmitEmpty() StructSchemaOption {
	return requiredFromNonOmitEmptyOption{}
}

func (o requiredFromNonOmitEmptyOption) applyStructSchemaOption(cfg *structSchemaConfig) {
	cfg.requiredFromNonOmit = true
}

type optionalPointersOption struct{}

func OptionalPointers() StructSchemaOption {
	return optionalPointersOption{}
}

func (o optionalPointersOption) applyStructSchemaOption(cfg *structSchemaConfig) {
	cfg.optionalPointers = true
}

type allowMapObjectsOption struct{}

func AllowMapObjects() StructSchemaOption {
	return allowMapObjectsOption{}
}

func (o allowMapObjectsOption) applyStructSchemaOption(cfg *structSchemaConfig) {
	cfg.allowMapObjects = true
}

type numberTypeOption string

func DecimalType(typeName string) StructSchemaOption {
	return numberTypeOption(typeName)
}

func NumberType(typeName string) StructSchemaOption {
	return numberTypeOption(typeName)
}

func (o numberTypeOption) applyStructSchemaOption(cfg *structSchemaConfig) {
	cfg.numberTypes[string(o)] = struct{}{}
}

type stringFormatTypeOption struct {
	goType reflect.Type
	format string
}

// StringFormatType maps a Go type to a string schema format during struct inspection.
func StringFormatType(goType reflect.Type, format StringFormat) StructSchemaOption {
	return stringFormatTypeOption{goType: goType, format: string(format)}
}

func (o stringFormatTypeOption) applyStructSchemaOption(cfg *structSchemaConfig) {
	if o.goType == nil || o.format == "" {
		return
	}
	typ := o.goType
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	cfg.formatTypes[typ] = o.format
}

type structStringFormatsOption struct {
	registry *StringFormatRegistry
}

// StructStringFormats supplies a format registry when compiling schemas from structs.
func StructStringFormats(registry *StringFormatRegistry) StructSchemaOption {
	return structStringFormatsOption{registry: registry}
}

func (o structStringFormatsOption) applyStructSchemaOption(cfg *structSchemaConfig) {
	cfg.formats = o.registry
}

type StructSchemaField struct {
	GoName     string
	JSONName   string
	SchemaKind JSONKind
	Format     string
	Optional   bool
	OmitEmpty  bool
	GoType     string
	Path       Path
	Children   []StructSchemaField
	Item       *StructSchemaField
	Findings   []StructSchemaFinding
}

type StructSuggestion struct {
	TypeName string
	Code     string
	Imports  []string
	Notes    []StructSchemaFinding
}

type StructSchemaFinding struct {
	Category string
	Path     Path
	Message  string
	GoType   string
	GoName   string
	JSONName string
}

type StructSchemaReport struct {
	OK       bool
	Findings []StructSchemaFinding
}

func InspectStructTags(value any, opts ...StructSchemaOption) []StructSchemaField {
	fields, err := InspectStructTagsTry(value, opts...)
	if err != nil {
		return []StructSchemaField{}
	}
	return fields
}

func InspectStructTagsTry(value any, opts ...StructSchemaOption) ([]StructSchemaField, error) {
	cfg := newStructSchemaConfig(opts...)
	structType, err := resolveStructType(value)
	if err != nil {
		return nil, err
	}
	fields, _, err := inspectStructType(structType, cfg, RootPath(), map[reflect.Type]bool{}, true)
	return fields, err
}

func NewSchemaFromStruct(value any, opts ...StructSchemaOption) JSONValue {
	schemaDoc, err := NewSchemaFromStructTry(value, opts...)
	if err != nil {
		return NewVoid()
	}
	return schemaDoc
}

func NewSchemaFromStructTry(value any, opts ...StructSchemaOption) (JSONValue, error) {
	cfg := newStructSchemaConfig(opts...)
	structType, err := resolveStructType(value)
	if err != nil {
		return NewVoid(), err
	}
	fields, _, err := inspectStructType(structType, cfg, RootPath(), map[reflect.Type]bool{}, true)
	if err != nil {
		return NewVoid(), err
	}
	return schemaFromStructFields(fields, cfg, true), nil
}

func NewSchemaFromStructOrDefault(value any, defaultValue JSONValue, opts ...StructSchemaOption) JSONValue {
	schemaDoc, err := NewSchemaFromStructTry(value, opts...)
	if err != nil {
		return defaultValue
	}
	return schemaDoc
}

func CompileSchemaFromStructTry(value any, opts ...StructSchemaOption) (JSONSchema, error) {
	cfg := newStructSchemaConfig(opts...)
	schemaDoc, err := NewSchemaFromStructTry(value, opts...)
	if err != nil {
		return JSONSchema{}, err
	}
	compileOpts := []SchemaCompileOption{}
	if cfg.formats != nil {
		compileOpts = append(compileOpts, WithStringFormats(cfg.formats))
	}
	return CompileSchemaJSON(schemaDoc.ToJSON(), compileOpts...)
}

func NewStructSuggestionFromSchema(schema JSONSchema, typeName string, opts ...StructSchemaOption) StructSuggestion {
	suggestion, err := NewStructSuggestionFromSchemaTry(schema, typeName, opts...)
	if err != nil {
		return StructSuggestion{
			TypeName: typeName,
			Notes: []StructSchemaFinding{{
				Category: "unsupported_schema_feature",
				Path:     RootPath(),
				Message:  err.Error(),
			}},
		}
	}
	return suggestion
}

func NewStructSuggestionFromSchemaTry(schema JSONSchema, typeName string, opts ...StructSchemaOption) (StructSuggestion, error) {
	if schema.root == nil {
		return StructSuggestion{}, fmt.Errorf("schema is empty")
	}
	if schema.root.Kind != KindObject {
		return StructSuggestion{}, fmt.Errorf("schema root must be object")
	}
	if typeName == "" {
		typeName = "Generated"
	}

	generator := newStructSuggestionGenerator()
	rootTypeName := generator.uniqueTypeName(exportedGoName(typeName))
	definitions := append([]string{
		generator.structDefinition(rootTypeName, schema.root.Children, RootPath()),
	}, generator.definitions...)
	code := strings.Join(definitions, "\n")
	if formatted, err := format.Source([]byte(code)); err == nil {
		code = string(formatted)
	}
	imports := make([]string, 0, len(generator.imports)+1)
	if generator.usesJSONNumber {
		imports = append(imports, "encoding/json")
	}
	for pkg := range generator.imports {
		imports = append(imports, pkg)
	}
	sort.Strings(imports)

	return StructSuggestion{
		TypeName: exportedGoName(typeName),
		Code:     code,
		Imports:  imports,
		Notes:    generator.notes,
	}, nil
}

func NewStructSuggestionFromSchemaJSONTry(schemaText string, typeName string, opts ...StructSchemaOption) (StructSuggestion, error) {
	schema, err := CompileSchemaJSON(schemaText)
	if err != nil {
		return StructSuggestion{}, err
	}
	return NewStructSuggestionFromSchemaTry(schema, typeName, opts...)
}

func CompareStructToSchema(value any, schema JSONSchema, opts ...StructSchemaOption) StructSchemaReport {
	report, err := CompareStructToSchemaTry(value, schema, opts...)
	if err != nil {
		return StructSchemaReport{
			OK: false,
			Findings: []StructSchemaFinding{{
				Category: "unsupported_go_type",
				Path:     RootPath(),
				Message:  err.Error(),
			}},
		}
	}
	return report
}

func CompareStructToSchemaTry(value any, schema JSONSchema, opts ...StructSchemaOption) (StructSchemaReport, error) {
	if schema.root == nil {
		return StructSchemaReport{}, fmt.Errorf("schema is empty")
	}

	cfg := newStructSchemaConfig(opts...)
	structType, err := resolveStructType(value)
	if err != nil {
		return StructSchemaReport{}, err
	}

	fields, findings, err := inspectStructType(structType, cfg, RootPath(), map[reflect.Type]bool{}, false)
	if err != nil {
		return StructSchemaReport{}, err
	}
	if schema.root.Kind != KindObject {
		findings = append(findings, StructSchemaFinding{
			Category: "unsupported_schema_feature",
			Path:     RootPath(),
			Message:  "schema root is not object",
		})
		return StructSchemaReport{OK: len(findings) == 0, Findings: findings}, nil
	}

	findings = append(findings, compareFieldsToSchema(fields, schema.root, RootPath())...)
	return StructSchemaReport{OK: len(findings) == 0, Findings: findings}, nil
}

func CompareStructToSchemaJSONTry(value any, schemaText string, opts ...StructSchemaOption) (StructSchemaReport, error) {
	schema, err := CompileSchemaJSON(schemaText)
	if err != nil {
		return StructSchemaReport{}, err
	}
	return CompareStructToSchemaTry(value, schema, opts...)
}

func CompareStructToSchemaFileTry(value any, schemaPath string, opts ...StructSchemaOption) (StructSchemaReport, error) {
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return StructSchemaReport{}, err
	}
	return CompareStructToSchemaJSONTry(value, string(schemaBytes), opts...)
}

func resolveStructType(value any) (reflect.Type, error) {
	if value == nil {
		return nil, fmt.Errorf("value must be a struct, pointer to struct, or reflect.Type")
	}
	if typ, ok := value.(reflect.Type); ok {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct {
			return nil, fmt.Errorf("type must be a struct")
		}
		return typ, nil
	}

	typ := reflect.TypeOf(value)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("value must be a struct or pointer to struct")
	}
	return typ, nil
}

func inspectStructType(typ reflect.Type, cfg structSchemaConfig, path Path, seen map[reflect.Type]bool, strict bool) ([]StructSchemaField, []StructSchemaFinding, error) {
	if seen[typ] {
		finding := StructSchemaFinding{
			Category: "recursive_type",
			Path:     path,
			Message:  "recursive struct type is not supported",
			GoType:   typ.String(),
		}
		if strict {
			return nil, nil, fmt.Errorf("%s: %s", path.visible(), finding.Message)
		}
		return nil, []StructSchemaFinding{finding}, nil
	}

	seen[typ] = true
	defer delete(seen, typ)

	fields := []StructSchemaField{}
	findings := []StructSchemaFinding{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		jsonName, skip, omitEmpty, explicit := parseStructJSONTag(field)
		if skip {
			continue
		}
		if cfg.requireExplicitJSONTags && !explicit {
			finding := StructSchemaFinding{
				Category: "ambiguous_json_name",
				Path:     path.Field(jsonName),
				Message:  "field does not have an explicit json tag name",
				GoName:   field.Name,
				JSONName: jsonName,
				GoType:   field.Type.String(),
			}
			if strict {
				return nil, nil, fmt.Errorf("%s: %s", finding.Path.visible(), finding.Message)
			}
			findings = append(findings, finding)
		}

		fieldPath := path.Field(jsonName)
		mapped, mappedFindings, err := inspectGoType(field.Type, cfg, fieldPath, seen, strict)
		if err != nil {
			return nil, nil, err
		}
		findings = append(findings, mappedFindings...)

		optional := omitEmpty || (cfg.optionalPointers && field.Type.Kind() == reflect.Pointer)
		structField := StructSchemaField{
			GoName:     field.Name,
			JSONName:   jsonName,
			SchemaKind: mapped.SchemaKind,
			Format:     mapped.Format,
			Optional:   optional,
			OmitEmpty:  omitEmpty,
			GoType:     field.Type.String(),
			Path:       fieldPath,
			Children:   mapped.Children,
			Item:       mapped.Item,
			Findings:   mapped.Findings,
		}
		fields = append(fields, structField)
	}

	return fields, findings, nil
}

func parseStructJSONTag(field reflect.StructField) (name string, skip bool, omitEmpty bool, explicit bool) {
	tag, hasTag := field.Tag.Lookup("json")
	if tag == "-" {
		return "", true, false, true
	}

	parts := strings.Split(tag, ",")
	if hasTag && parts[0] != "" {
		name = parts[0]
		explicit = true
	} else {
		name = field.Name
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			omitEmpty = true
		}
	}
	return name, false, omitEmpty, explicit
}

func inspectGoType(typ reflect.Type, cfg structSchemaConfig, path Path, seen map[reflect.Type]bool, strict bool) (StructSchemaField, []StructSchemaFinding, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	field := StructSchemaField{
		SchemaKind: KindVoid,
		GoType:     typ.String(),
		Path:       path,
	}
	if format, ok := cfg.formatTypes[typ]; ok {
		field.SchemaKind = KindString
		field.Format = format
		return field, nil, nil
	}
	if isConfiguredNumberType(typ, cfg) || isJSONNumberType(typ) {
		field.SchemaKind = KindNumber
		return field, nil, nil
	}

	switch typ.Kind() {
	case reflect.String:
		field.SchemaKind = KindString
	case reflect.Bool:
		field.SchemaKind = KindBoolean
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		field.SchemaKind = KindNumber
	case reflect.Struct:
		children, findings, err := inspectStructType(typ, cfg, path, seen, strict)
		if err != nil {
			return field, nil, err
		}
		field.SchemaKind = KindObject
		field.Children = children
		return field, findings, nil
	case reflect.Slice, reflect.Array:
		item, findings, err := inspectGoType(typ.Elem(), cfg, path.Index(0), seen, strict)
		if err != nil {
			return field, nil, err
		}
		field.SchemaKind = KindArray
		field.Item = &item
		return field, findings, nil
	case reflect.Map:
		if cfg.allowMapObjects && typ.Key().Kind() == reflect.String {
			field.SchemaKind = KindObject
			return field, []StructSchemaFinding{{
				Category: "unsupported_schema_feature",
				Path:     path,
				Message:  "map object does not preserve field order",
				GoType:   typ.String(),
			}}, nil
		}
		return unsupportedGoTypeField(field, strict)
	case reflect.Interface:
		return unsupportedGoTypeField(field, strict)
	default:
		return unsupportedGoTypeField(field, strict)
	}

	return field, nil, nil
}

func unsupportedGoTypeField(field StructSchemaField, strict bool) (StructSchemaField, []StructSchemaFinding, error) {
	finding := StructSchemaFinding{
		Category: "unsupported_go_type",
		Path:     field.Path,
		Message:  "Go type cannot be mapped to an ojson schema kind",
		GoType:   field.GoType,
	}
	field.Findings = append(field.Findings, finding)
	if strict {
		return field, nil, fmt.Errorf("%s: %s %s", field.Path.visible(), finding.Message, field.GoType)
	}
	return field, []StructSchemaFinding{finding}, nil
}

func isConfiguredNumberType(typ reflect.Type, cfg structSchemaConfig) bool {
	names := []string{typ.String(), typ.Name()}
	if typ.PkgPath() != "" && typ.Name() != "" {
		names = append(names, typ.PkgPath()+"."+typ.Name())
	}
	for _, name := range names {
		if _, ok := cfg.numberTypes[name]; ok {
			return true
		}
	}
	return false
}

func isJSONNumberType(typ reflect.Type) bool {
	return typ.PkgPath() == "encoding/json" && typ.Name() == "Number"
}

func schemaFromStructFields(fields []StructSchemaField, cfg structSchemaConfig, root bool) JSONValue {
	schemaDoc := NewObject()
	schemaDoc.Set("kind", NewString("object"))
	children := NewArray()
	for _, field := range fields {
		children.appendRaw(schemaFromStructField(field, cfg))
	}
	schemaDoc.Set("children", children)
	return schemaDoc
}

func schemaFromStructField(field StructSchemaField, cfg structSchemaConfig) JSONValue {
	schemaDoc := NewObject()
	schemaDoc.Set("name", NewString(field.JSONName))
	schemaDoc.Set("kind", NewString(schemaKindName(field.SchemaKind)))
	if field.Format != "" {
		schemaDoc.Set("format", NewString(field.Format))
	}
	if cfg.requiredFromNonOmit && !field.Optional {
		schemaDoc.Set("required", NewBoolean(true))
	}
	if field.SchemaKind == KindObject && len(field.Children) > 0 {
		children := NewArray()
		for _, child := range field.Children {
			children.appendRaw(schemaFromStructField(child, cfg))
		}
		schemaDoc.Set("children", children)
	}
	if field.SchemaKind == KindArray && field.Item != nil && field.Item.SchemaKind != KindVoid {
		itemSchema := NewObject()
		itemSchema.Set("kind", NewString(schemaKindName(field.Item.SchemaKind)))
		if field.Item.Format != "" {
			itemSchema.Set("format", NewString(field.Item.Format))
		}
		if field.Item.SchemaKind == KindObject && len(field.Item.Children) > 0 {
			children := NewArray()
			for _, child := range field.Item.Children {
				children.appendRaw(schemaFromStructField(child, cfg))
			}
			itemSchema.Set("children", children)
		}
		schemaDoc.Set("items", itemSchema)
	}
	return schemaDoc
}

type structSuggestionGenerator struct {
	usedNames      map[string]int
	definitions    []string
	notes          []StructSchemaFinding
	usesJSONNumber bool
	imports        map[string]struct{}
}

func newStructSuggestionGenerator() *structSuggestionGenerator {
	return &structSuggestionGenerator{
		usedNames: map[string]int{},
		imports:   map[string]struct{}{},
	}
}

func (g *structSuggestionGenerator) uniqueTypeName(base string) string {
	if base == "" {
		base = "Generated"
	}
	if count := g.usedNames[base]; count > 0 {
		g.usedNames[base] = count + 1
		return fmt.Sprintf("%s%d", base, count+1)
	}
	g.usedNames[base] = 1
	return base
}

func (g *structSuggestionGenerator) structDefinition(typeName string, entries []*schemaEntry, path Path) string {
	var builder strings.Builder
	builder.WriteString("type ")
	builder.WriteString(typeName)
	builder.WriteString(" struct {\n")
	builder.WriteString(g.structFieldsCode(entries, path))
	builder.WriteString("}\n")
	return builder.String()
}

func (g *structSuggestionGenerator) structFieldsCode(entries []*schemaEntry, path Path) string {
	var builder strings.Builder
	for _, entry := range entries {
		fieldName := exportedGoName(entry.Name)
		goType := g.goTypeForSchemaEntry(entry, fieldName, path.Field(entry.Name))
		tag := entry.Name
		if !entry.Required {
			tag += ",omitempty"
		}
		builder.WriteByte('\t')
		builder.WriteString(fieldName)
		builder.WriteString(" ")
		builder.WriteString(goType)
		builder.WriteString(" `json:\"")
		builder.WriteString(tag)
		builder.WriteString("\"`")
		builder.WriteByte('\n')
		if entry.Required {
			g.notes = append(g.notes, StructSchemaFinding{Category: "required_policy_difference", Path: path.Field(entry.Name), Message: "field is required by schema", JSONName: entry.Name})
		}
		if entry.HasDefault {
			g.notes = append(g.notes, StructSchemaFinding{Category: "default_only_in_schema", Path: path.Field(entry.Name), Message: "schema default cannot be represented by json tags", JSONName: entry.Name})
		}
	}
	return builder.String()
}

func (g *structSuggestionGenerator) goTypeForSchemaEntry(entry *schemaEntry, suggestedName string, path Path) string {
	switch entry.Kind {
	case KindString:
		if entry.formatGoType != nil {
			return g.goTypeName(entry.formatGoType, entry.Nullable)
		}
		if entry.Nullable {
			return "*string"
		}
		return "string"
	case KindNumber:
		g.usesJSONNumber = true
		if entry.Nullable {
			return "*json.Number"
		}
		return "json.Number"
	case KindBoolean:
		if entry.Nullable {
			return "*bool"
		}
		return "bool"
	case KindObject:
		typeName := g.uniqueTypeName(suggestedName)
		g.definitions = append(g.definitions, g.structDefinition(typeName, entry.Children, path))
		return typeName
	case KindArray:
		if entry.Items == nil {
			g.notes = append(g.notes, StructSchemaFinding{Category: "unsupported_schema_feature", Path: path, Message: "array items are not typed by schema"})
			return "[]JSONValue"
		}
		itemName := suggestedName + "Item"
		if entry.Items.Kind == KindObject {
			itemName = singularGoName(suggestedName)
		}
		return "[]" + g.goTypeForSchemaEntry(entry.Items, itemName, path.Index(0))
	case KindNull:
		g.notes = append(g.notes, StructSchemaFinding{Category: "unsupported_schema_feature", Path: path, Message: "null schema needs human review"})
		return "any"
	default:
		g.notes = append(g.notes, StructSchemaFinding{Category: "unsupported_schema_feature", Path: path, Message: "unsupported schema kind"})
		return "any"
	}
}

func (g *structSuggestionGenerator) goTypeName(typ reflect.Type, nullable bool) string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
		nullable = true
	}
	name := typ.Name()
	if name == "" {
		name = typ.String()
	} else if pkg := typ.PkgPath(); pkg != "" {
		g.imports[pkg] = struct{}{}
		if last := strings.LastIndex(pkg, "/"); last >= 0 {
			name = pkg[last+1:] + "." + name
		} else {
			name = pkg + "." + name
		}
	}
	if nullable {
		return "*" + name
	}
	return name
}

func singularGoName(name string) string {
	switch {
	case strings.HasSuffix(name, "ies") && len(name) > 3:
		return strings.TrimSuffix(name, "ies") + "y"
	case strings.HasSuffix(name, "s") && !strings.HasSuffix(name, "ss") && len(name) > 1:
		return strings.TrimSuffix(name, "s")
	default:
		return name + "Item"
	}
}

func exportedGoName(name string) string {
	parts := splitNameParts(name)
	if len(parts) == 0 {
		return "Field"
	}
	for i, part := range parts {
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	result := strings.Join(parts, "")
	if isGoKeyword(result) {
		return result + "Value"
	}
	return result
}

func splitNameParts(name string) []string {
	parts := []string{}
	current := strings.Builder{}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func isGoKeyword(name string) bool {
	keywords := map[string]struct{}{
		"Break": {}, "Default": {}, "Func": {}, "Interface": {}, "Select": {},
		"Case": {}, "Defer": {}, "Go": {}, "Map": {}, "Struct": {},
		"Chan": {}, "Else": {}, "Goto": {}, "Package": {}, "Switch": {},
		"Const": {}, "Fallthrough": {}, "If": {}, "Range": {}, "Type": {},
		"Continue": {}, "For": {}, "Import": {}, "Return": {}, "Var": {},
	}
	_, ok := keywords[name]
	return ok
}

func compareFieldsToSchema(fields []StructSchemaField, entry *schemaEntry, path Path) []StructSchemaFinding {
	findings := []StructSchemaFinding{}
	fieldByName := make(map[string]StructSchemaField, len(fields))
	fieldOrder := make([]string, 0, len(fields))
	for _, field := range fields {
		fieldByName[field.JSONName] = field
		fieldOrder = append(fieldOrder, field.JSONName)
		findings = append(findings, field.Findings...)
	}

	schemaOrder := make([]string, 0, len(entry.Children))
	for _, child := range entry.Children {
		schemaOrder = append(schemaOrder, child.Name)
		field, ok := fieldByName[child.Name]
		if !ok {
			findings = append(findings, StructSchemaFinding{
				Category: "missing_in_struct",
				Path:     path.Field(child.Name),
				Message:  "schema field is missing in struct",
				JSONName: child.Name,
			})
			continue
		}
		if field.SchemaKind != child.Kind {
			findings = append(findings, StructSchemaFinding{
				Category: "kind_mismatch",
				Path:     path.Field(child.Name),
				Message:  fmt.Sprintf("struct maps to %s, schema expects %s", field.SchemaKind, child.Kind),
				GoType:   field.GoType,
				GoName:   field.GoName,
				JSONName: child.Name,
			})
		}
		if child.HasDefault {
			findings = append(findings, StructSchemaFinding{
				Category: "default_only_in_schema",
				Path:     path.Field(child.Name),
				Message:  "schema default cannot be represented by json tags",
				GoName:   field.GoName,
				JSONName: child.Name,
			})
		}
		if child.Required && field.Optional {
			findings = append(findings, StructSchemaFinding{
				Category: "required_policy_difference",
				Path:     path.Field(child.Name),
				Message:  "schema requires a field that struct marks optional",
				GoName:   field.GoName,
				JSONName: child.Name,
			})
		}
		if child.Kind == KindObject && len(field.Children) > 0 {
			findings = append(findings, compareFieldsToSchema(field.Children, child, path.Field(child.Name))...)
		}
	}

	for _, field := range fields {
		if _, ok := entry.childByName[field.JSONName]; !ok {
			findings = append(findings, StructSchemaFinding{
				Category: "missing_in_schema",
				Path:     path.Field(field.JSONName),
				Message:  "struct field is missing in schema",
				GoType:   field.GoType,
				GoName:   field.GoName,
				JSONName: field.JSONName,
			})
		}
	}

	if sameStringSet(fieldOrder, schemaOrder) && !sameStringSlice(fieldOrder, schemaOrder) {
		findings = append(findings, StructSchemaFinding{
			Category: "order_mismatch",
			Path:     path,
			Message:  "struct field order differs from schema order",
		})
	}

	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].Path.String() < findings[j].Path.String()
	})
	return findings
}

func sameStringSet(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := map[string]int{}
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func sameStringSlice(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
