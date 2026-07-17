package ojson

import "fmt"

func (v JSONValue) At(index int) JSONValue {
	value, err := v.AtTry(index)
	if err != nil {
		return NewVoid()
	}

	return value
}

func (v JSONValue) AtTry(index int) (JSONValue, error) {
	if !v.IsArray() {
		return NewVoid(), fmt.Errorf("cannot access index %d from %s", index, v.Kind())
	}
	if index < 0 || index >= len(v.node.arrayValue) {
		return NewVoid(), fmt.Errorf("array index %d out of range", index)
	}

	return v.node.arrayValue[index], nil
}

func (v JSONValue) AtOrDefault(index int, defaultValue JSONValue) JSONValue {
	value, err := v.AtTry(index)
	if err != nil {
		return defaultValue
	}

	return value
}

func (v JSONValue) Len() int {
	if !v.IsArray() {
		return 0
	}

	return len(v.node.arrayValue)
}

func (v JSONValue) Items() []JSONValue {
	if !v.IsArray() {
		return []JSONValue{}
	}

	items := make([]JSONValue, len(v.node.arrayValue))
	copy(items, v.node.arrayValue)
	return items
}

func (v JSONValue) Append(value JSONValue) {
	if !v.IsArray() || value.IsVoid() {
		return
	}

	if v.node.schemaEntry != nil && v.node.schemaEntry.Items != nil {
		normalized, err := normalizeValueForSchema(value, v.node.schemaEntry.Items, RootPath().Index(v.Len()), v.node.schema)
		if err != nil {
			return
		}
		value = normalized
	}
	v.appendRaw(value)
}

func (v JSONValue) appendRaw(value JSONValue) {
	v.node.arrayValue = append(v.node.arrayValue, value)
}

func (v JSONValue) AppendTry(value JSONValue) error {
	if !v.IsArray() {
		return fmt.Errorf("cannot append to %s", v.Kind())
	}
	if value.IsVoid() {
		return fmt.Errorf("cannot append void")
	}

	if v.node.schemaEntry != nil && v.node.schemaEntry.Items != nil {
		normalized, err := normalizeValueForSchema(value, v.node.schemaEntry.Items, RootPath().Index(v.Len()), v.node.schema)
		if err != nil {
			return err
		}
		value = normalized
	}
	v.appendRaw(value)
	return nil
}

func (v JSONValue) Prepend(value JSONValue) {
	if !v.IsArray() || value.IsVoid() {
		return
	}

	if v.node.schemaEntry != nil && v.node.schemaEntry.Items != nil {
		normalized, err := normalizeValueForSchema(value, v.node.schemaEntry.Items, RootPath().Index(0), v.node.schema)
		if err != nil {
			return
		}
		value = normalized
	}
	v.prependRaw(value)
}

func (v JSONValue) prependRaw(value JSONValue) {
	v.node.arrayValue = append([]JSONValue{value}, v.node.arrayValue...)
}

func (v JSONValue) PrependTry(value JSONValue) error {
	if !v.IsArray() {
		return fmt.Errorf("cannot prepend to %s", v.Kind())
	}
	if value.IsVoid() {
		return fmt.Errorf("cannot prepend void")
	}

	if v.node.schemaEntry != nil && v.node.schemaEntry.Items != nil {
		normalized, err := normalizeValueForSchema(value, v.node.schemaEntry.Items, RootPath().Index(0), v.node.schema)
		if err != nil {
			return err
		}
		value = normalized
	}
	v.prependRaw(value)
	return nil
}

func (v JSONValue) InsertAtTry(index int, value JSONValue) error {
	if !v.IsArray() {
		return fmt.Errorf("cannot insert into %s", v.Kind())
	}
	if value.IsVoid() {
		return fmt.Errorf("cannot insert void")
	}
	if index < 0 || index > len(v.node.arrayValue) {
		return fmt.Errorf("array index %d out of range", index)
	}

	if v.node.schemaEntry != nil && v.node.schemaEntry.Items != nil {
		normalized, err := normalizeValueForSchema(value, v.node.schemaEntry.Items, RootPath().Index(index), v.node.schema)
		if err != nil {
			return err
		}
		value = normalized
	}
	v.node.arrayValue = append(v.node.arrayValue, NewVoid())
	copy(v.node.arrayValue[index+1:], v.node.arrayValue[index:])
	v.node.arrayValue[index] = value
	return nil
}

func (v JSONValue) Remove(selector any) JSONValue {
	removed, err := v.RemoveTry(selector)
	if err != nil {
		return NewVoid()
	}

	return removed
}

func (v JSONValue) RemoveTry(selector any) (JSONValue, error) {
	switch v.Kind() {
	case KindObject:
		key, ok := selector.(string)
		if !ok {
			return NewVoid(), fmt.Errorf("object removal requires a string key")
		}
		return v.removeFieldTry(key)
	case KindArray:
		index, ok := selector.(int)
		if !ok {
			return NewVoid(), fmt.Errorf("array removal requires an int index")
		}
		return v.removeIndexTry(index)
	default:
		return NewVoid(), fmt.Errorf("cannot remove from %s", v.Kind())
	}
}

func (v JSONValue) removeIndexTry(index int) (JSONValue, error) {
	if index < 0 || index >= len(v.node.arrayValue) {
		return NewVoid(), fmt.Errorf("array index %d out of range", index)
	}

	removed := v.node.arrayValue[index]
	v.node.arrayValue = append(v.node.arrayValue[:index], v.node.arrayValue[index+1:]...)
	return removed, nil
}

func (v JSONValue) RemoveOrDefault(selector any, defaultValue JSONValue) JSONValue {
	removed, err := v.RemoveTry(selector)
	if err != nil {
		return defaultValue
	}

	return removed
}

func (v JSONValue) Compress() int {
	if !v.IsArray() {
		return 0
	}

	items := v.node.arrayValue[:0]
	removed := 0
	for _, item := range v.node.arrayValue {
		if item.IsVoid() {
			removed++
			continue
		}
		items = append(items, item)
	}
	v.node.arrayValue = items
	return removed
}
