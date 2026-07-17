package ojson

import "fmt"

func (v JSONValue) Get(key string) JSONValue {
	value, err := v.GetTry(key)
	if err != nil {
		return NewVoid()
	}

	return value
}

func (v JSONValue) GetTry(key string) (JSONValue, error) {
	if !v.IsObject() {
		return NewVoid(), fmt.Errorf("cannot get field %q from %s", key, v.Kind())
	}

	for _, field := range v.node.objectValue {
		if field.Key == key {
			if field.Value.IsVoid() {
				return NewVoid(), fmt.Errorf("field %q is void", key)
			}

			return field.Value, nil
		}
	}

	return NewVoid(), fmt.Errorf("field %q does not exist", key)
}

func (v JSONValue) GetOrDefault(key string, defaultValue JSONValue) JSONValue {
	value, err := v.GetTry(key)
	if err != nil {
		return defaultValue
	}

	return value
}

func (v JSONValue) HasField(key string) bool {
	if !v.IsObject() {
		return false
	}

	for _, field := range v.node.objectValue {
		if field.Key == key {
			return !field.Value.IsVoid()
		}
	}

	return false
}

func (v JSONValue) Set(key string, value JSONValue) {
	if !v.IsObject() {
		return
	}

	if value.IsVoid() {
		if child := schemaEntryForObjectField(v, key); child != nil && child.Required {
			return
		}
		v.removeField(key)
		return
	}

	child := schemaEntryForObjectField(v, key)
	if child != nil {
		normalized, err := normalizeValueForSchema(value, child, RootPath().Field(key), v.node.schema)
		if err != nil {
			return
		}
		v.setField(key, normalized)
		reorderObjectFieldsBySchema(v)
		return
	}

	v.setField(key, value)
	reorderObjectFieldsBySchema(v)
}

func (v JSONValue) setField(key string, value JSONValue) {
	if !v.IsObject() {
		return
	}

	for i := range v.node.objectValue {
		if v.node.objectValue[i].Key == key {
			v.node.objectValue[i].Value = value
			return
		}
	}

	v.node.objectValue = append(v.node.objectValue, JSONKeyValue{
		Key:   key,
		Value: value,
	})
}

func (v JSONValue) SetTry(key string, value JSONValue) error {
	if !v.IsObject() {
		return fmt.Errorf("cannot set field %q on %s", key, v.Kind())
	}
	if value.IsVoid() {
		return fmt.Errorf("cannot set field %q to void", key)
	}

	child := schemaEntryForObjectField(v, key)
	if child != nil {
		normalized, err := normalizeValueForSchema(value, child, RootPath().Field(key), v.node.schema)
		if err != nil {
			return err
		}
		v.setField(key, normalized)
		reorderObjectFieldsBySchema(v)
		return nil
	}

	v.setField(key, value)
	reorderObjectFieldsBySchema(v)
	return nil
}

func (v JSONValue) removeFieldTry(key string) (JSONValue, error) {
	if !v.IsObject() {
		return NewVoid(), fmt.Errorf("cannot remove field %q from %s", key, v.Kind())
	}

	for i, field := range v.node.objectValue {
		if field.Key != key {
			continue
		}
		if child := schemaEntryForObjectField(v, key); child != nil && child.Required {
			return NewVoid(), fmt.Errorf("field %q is required by schema", key)
		}
		if field.Value.IsVoid() {
			return NewVoid(), fmt.Errorf("field %q is void", key)
		}

		removed := field.Value
		v.node.objectValue = append(v.node.objectValue[:i], v.node.objectValue[i+1:]...)
		return removed, nil
	}

	return NewVoid(), fmt.Errorf("field %q does not exist", key)
}

func (v JSONValue) removeField(key string) {
	for i, field := range v.node.objectValue {
		if field.Key == key {
			v.node.objectValue = append(v.node.objectValue[:i], v.node.objectValue[i+1:]...)
			return
		}
	}
}
