package ojson

import "os"

func ReadStringWithSchema(jsonText string, schema JSONSchema) (JSONValue, error) {
	value, err := ReadStringNoSchema(jsonText)
	if err != nil {
		return NewVoid(), err
	}
	return value.ApplySchema(schema)
}

func ReadBytesWithSchema(jsonBytes []byte, schema JSONSchema) (JSONValue, error) {
	value, err := ReadBytesNoSchema(jsonBytes)
	if err != nil {
		return NewVoid(), err
	}
	return value.ApplySchema(schema)
}

func ReadFileWithSchema(path string, schema JSONSchema) (JSONValue, error) {
	jsonBytes, err := os.ReadFile(path)
	if err != nil {
		return NewVoid(), err
	}
	return ReadBytesWithSchema(jsonBytes, schema)
}

func (v JSONValue) ApplySchema(schema JSONSchema) (JSONValue, error) {
	if schema.root == nil {
		return NewVoid(), pathError(RootPath(), "schema is empty")
	}

	schemaCopy := schema
	normalized, err := normalizeValueForSchema(v, schemaCopy.root, RootPath(), &schemaCopy)
	if err != nil {
		return NewVoid(), err
	}
	return normalized, nil
}

func (s JSONSchema) Validate(value JSONValue) error {
	if s.root == nil {
		return pathError(RootPath(), "schema is empty")
	}

	_, err := normalizeValueForSchema(value, s.root, RootPath(), nil)
	return err
}

func normalizeValueForSchema(value JSONValue, entry *schemaEntry, path Path, schema *JSONSchema) (JSONValue, error) {
	if entry == nil {
		return cloneJSONValue(value), nil
	}

	if value.IsNull() {
		if entry.Kind == KindNull || entry.Nullable {
			normalized := NewNull()
			attachSchema(normalized, schema, entry)
			return normalized, nil
		}
		return NewVoid(), pathError(path, "expected %s, got null", entry.Kind)
	}

	if value.Kind() != entry.Kind {
		return NewVoid(), pathError(path, "expected %s, got %s", entry.Kind, value.Kind())
	}

	var normalized JSONValue
	var err error
	switch entry.Kind {
	case KindObject:
		normalized, err = normalizeObjectForSchema(value, entry, path, schema)
	case KindArray:
		normalized, err = normalizeArrayForSchema(value, entry, path, schema)
	case KindString:
		err = validateStringValueAgainstSchema(value.String(), entry, path)
		normalized = cloneJSONValue(value)
	case KindNumber:
		err = validateNumberValueAgainstSchema(value, entry, path)
		normalized = cloneJSONValue(value)
	case KindBoolean, KindNull:
		normalized = cloneJSONValue(value)
	default:
		err = pathError(path, "unsupported schema kind %s", entry.Kind)
	}
	if err != nil {
		return NewVoid(), err
	}

	attachSchema(normalized, schema, entry)
	return normalized, nil
}

func normalizeObjectForSchema(value JSONValue, entry *schemaEntry, path Path, schema *JSONSchema) (JSONValue, error) {
	result := NewObject()
	source := make(map[string]JSONValue, len(value.node.objectValue))
	for _, field := range value.node.objectValue {
		if field.Value.IsVoid() {
			continue
		}
		source[field.Key] = field.Value
	}

	for _, child := range entry.Children {
		fieldPath := path.Field(child.Name)
		fieldValue, exists := source[child.Name]
		if exists {
			normalized, err := normalizeValueForSchema(fieldValue, child, fieldPath, schema)
			if err != nil {
				return NewVoid(), err
			}
			result.node.objectValue = append(result.node.objectValue, JSONKeyValue{Key: child.Name, Value: normalized})
			continue
		}
		if child.HasDefault {
			normalized, err := normalizeValueForSchema(child.Default, child, fieldPath, schema)
			if err != nil {
				return NewVoid(), err
			}
			result.node.objectValue = append(result.node.objectValue, JSONKeyValue{Key: child.Name, Value: normalized})
			continue
		}
		if child.Required {
			return NewVoid(), pathError(fieldPath, "required field is missing")
		}
	}

	for _, field := range value.node.objectValue {
		if field.Value.IsVoid() {
			continue
		}
		if _, known := entry.childByName[field.Key]; known {
			continue
		}
		result.node.objectValue = append(result.node.objectValue, JSONKeyValue{
			Key:   field.Key,
			Value: cloneJSONValue(field.Value),
		})
	}
	return result, nil
}

func normalizeArrayForSchema(value JSONValue, entry *schemaEntry, path Path, schema *JSONSchema) (JSONValue, error) {
	result := NewArray()
	for i, item := range value.node.arrayValue {
		if item.IsVoid() {
			continue
		}
		if entry.Items == nil {
			result.node.arrayValue = append(result.node.arrayValue, cloneJSONValue(item))
			continue
		}
		normalized, err := normalizeValueForSchema(item, entry.Items, path.Index(i), schema)
		if err != nil {
			return NewVoid(), err
		}
		result.node.arrayValue = append(result.node.arrayValue, normalized)
	}
	return result, nil
}

func schemaEntryForObjectField(object JSONValue, key string) *schemaEntry {
	if object.node == nil || object.node.schemaEntry == nil || object.node.schemaEntry.Kind != KindObject {
		return nil
	}
	return object.node.schemaEntry.childByName[key]
}

func reorderObjectFieldsBySchema(object JSONValue) {
	if !object.IsObject() || object.node.schemaEntry == nil || object.node.schemaEntry.Kind != KindObject {
		return
	}

	current := make(map[string]JSONValue, len(object.node.objectValue))
	unknown := make([]JSONKeyValue, 0)
	for _, field := range object.node.objectValue {
		if field.Value.IsVoid() {
			continue
		}
		if _, ok := object.node.schemaEntry.childByName[field.Key]; ok {
			current[field.Key] = field.Value
			continue
		}
		unknown = append(unknown, field)
	}

	ordered := make([]JSONKeyValue, 0, len(object.node.objectValue))
	for _, child := range object.node.schemaEntry.Children {
		if value, ok := current[child.Name]; ok {
			ordered = append(ordered, JSONKeyValue{Key: child.Name, Value: value})
		}
	}
	ordered = append(ordered, unknown...)
	object.node.objectValue = ordered
}
