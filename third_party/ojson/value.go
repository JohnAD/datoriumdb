package ojson

// JSONValue is an ordered JSON value plus the ojson-specific Void marker.
type JSONValue struct {
	node *jsonNode
}

type jsonNode struct {
	kind        JSONKind
	stringValue string
	boolValue   bool
	objectValue []JSONKeyValue
	arrayValue  []JSONValue
	schema      *JSONSchema
	schemaEntry *schemaEntry
}

// JSONKeyValue stores one ordered object field.
type JSONKeyValue struct {
	Key   string
	Value JSONValue
}

func NewVoid() JSONValue {
	return JSONValue{}
}

func NewObject() JSONValue {
	return JSONValue{
		node: &jsonNode{
			kind:        KindObject,
			objectValue: make([]JSONKeyValue, 0),
		},
	}
}

func NewArray() JSONValue {
	return JSONValue{
		node: &jsonNode{
			kind:       KindArray,
			arrayValue: make([]JSONValue, 0),
		},
	}
}

func NewString(value string) JSONValue {
	if !isValidUTF8(value) {
		return NewVoid()
	}

	return JSONValue{
		node: &jsonNode{
			kind:        KindString,
			stringValue: value,
		},
	}
}

func NewEmptyString() JSONValue {
	return NewString("")
}

func NewBoolean(value bool) JSONValue {
	return JSONValue{
		node: &jsonNode{
			kind:      KindBoolean,
			boolValue: value,
		},
	}
}

func NewNull() JSONValue {
	return JSONValue{
		node: &jsonNode{kind: KindNull},
	}
}

func (v JSONValue) Kind() JSONKind {
	if v.node == nil {
		return KindVoid
	}

	return v.node.kind
}

func (v JSONValue) IsVoid() bool {
	return v.Kind() == KindVoid
}

func (v JSONValue) IsMissing() bool {
	return v.IsVoid()
}

func (v JSONValue) IsObject() bool {
	return v.Kind() == KindObject
}

func (v JSONValue) IsArray() bool {
	return v.Kind() == KindArray
}

func (v JSONValue) IsString() bool {
	return v.Kind() == KindString
}

func (v JSONValue) IsNumber() bool {
	return v.Kind() == KindNumber
}

func (v JSONValue) IsBoolean() bool {
	return v.Kind() == KindBoolean
}

func (v JSONValue) IsNull() bool {
	return v.Kind() == KindNull
}

func (v JSONValue) IsKnown() bool {
	switch v.Kind() {
	case KindObject, KindArray, KindString, KindNumber, KindBoolean:
		return true
	default:
		return false
	}
}

func (v JSONValue) IsEmpty() bool {
	switch v.Kind() {
	case KindVoid, KindNull:
		return true
	case KindObject:
		return len(v.node.objectValue) == 0
	case KindArray:
		return len(v.node.arrayValue) == 0
	case KindString:
		return v.node.stringValue == ""
	case KindNumber:
		return v.node.stringValue == "0"
	case KindBoolean:
		return !v.node.boolValue
	default:
		return true
	}
}

func (v JSONValue) NotEmpty() bool {
	return !v.IsEmpty()
}

func (v JSONValue) String() string {
	switch v.Kind() {
	case KindString, KindNumber:
		return v.node.stringValue
	case KindBoolean:
		if v.node.boolValue {
			return "true"
		}
		return "false"
	case KindNull:
		return "null"
	case KindVoid:
		return ""
	case KindObject:
		return "[object]"
	case KindArray:
		return "[array]"
	default:
		return ""
	}
}

func (v JSONValue) Schema() *JSONSchema {
	if v.node == nil {
		return nil
	}
	return v.node.schema
}

func (v JSONValue) HasSchema() bool {
	return v.Schema() != nil
}

func (v JSONValue) WithoutSchema() JSONValue {
	clone := cloneJSONValue(v)
	clearSchema(clone)
	return clone
}

func cloneJSONValue(value JSONValue) JSONValue {
	switch value.Kind() {
	case KindObject:
		clone := NewObject()
		for _, field := range value.node.objectValue {
			clone.node.objectValue = append(clone.node.objectValue, JSONKeyValue{
				Key:   field.Key,
				Value: cloneJSONValue(field.Value),
			})
		}
		return clone
	case KindArray:
		clone := NewArray()
		for _, item := range value.node.arrayValue {
			clone.node.arrayValue = append(clone.node.arrayValue, cloneJSONValue(item))
		}
		return clone
	case KindString:
		return NewString(value.node.stringValue)
	case KindNumber:
		return NewNumber(value.node.stringValue)
	case KindBoolean:
		return NewBoolean(value.node.boolValue)
	case KindNull:
		return NewNull()
	default:
		return NewVoid()
	}
}

func clearSchema(value JSONValue) {
	if value.node == nil {
		return
	}
	value.node.schema = nil
	value.node.schemaEntry = nil
	for _, field := range value.node.objectValue {
		clearSchema(field.Value)
	}
	for _, item := range value.node.arrayValue {
		clearSchema(item)
	}
}

func attachSchema(value JSONValue, schema *JSONSchema, entry *schemaEntry) {
	if value.node == nil {
		return
	}
	value.node.schema = schema
	value.node.schemaEntry = entry

	switch value.Kind() {
	case KindObject:
		for _, field := range value.node.objectValue {
			var childEntry *schemaEntry
			if entry != nil {
				childEntry = entry.childByName[field.Key]
			}
			attachSchema(field.Value, schema, childEntry)
		}
	case KindArray:
		for _, item := range value.node.arrayValue {
			var itemEntry *schemaEntry
			if entry != nil {
				itemEntry = entry.Items
			}
			attachSchema(item, schema, itemEntry)
		}
	}
}
