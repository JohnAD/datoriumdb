package ojson

import (
	"encoding"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

func NewObjectFromMap(values map[string]interface{}) JSONValue {
	return NewObjectFromMapOrItemDefault(values, NewVoid())
}

func NewObjectFromMapTry(values map[string]interface{}) (JSONValue, error) {
	result := NewObject()
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value, err := nativeToJSON(values[key], RootPath().Field(key))
		if err != nil {
			return NewVoid(), err
		}
		result.Set(key, value)
	}
	return result, nil
}

func NewObjectFromMapOrDefault(values map[string]interface{}, defaultValue JSONValue) JSONValue {
	result, err := NewObjectFromMapTry(values)
	if err != nil {
		return defaultValue
	}
	return result
}

func NewObjectFromMapOrItemDefault(values map[string]interface{}, defaultItem JSONValue) JSONValue {
	result := NewObject()
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value, err := nativeToJSON(values[key], RootPath().Field(key))
		if err != nil {
			value = defaultItem
		}
		result.Set(key, value)
	}
	return result
}

func NewObjectFromStruct(value any) JSONValue {
	result, err := structToJSON(value, RootPath(), NewVoid(), true)
	if err != nil {
		return NewVoid()
	}
	return result
}

func NewObjectFromStructTry(value any) (JSONValue, error) {
	result, err := structToJSON(value, RootPath(), NewVoid(), false)
	if err != nil {
		return NewVoid(), err
	}
	return result, nil
}

func NewObjectFromStructOrDefault(value any, defaultValue JSONValue) JSONValue {
	result, err := NewObjectFromStructTry(value)
	if err != nil {
		return defaultValue
	}
	return result
}

func NewObjectFromStructOrItemDefault(value any, defaultItem JSONValue) JSONValue {
	result, err := structToJSON(value, RootPath(), defaultItem, true)
	if err != nil {
		return NewVoid()
	}
	return result
}

func (v JSONValue) ToMap() map[string]interface{} {
	result, err := v.ToMapTry()
	if err != nil {
		return map[string]interface{}{}
	}
	return result
}

func (v JSONValue) ToMapTry() (map[string]interface{}, error) {
	if !v.IsObject() {
		return map[string]interface{}{}, pathError(RootPath(), "expected object, got %s", v.Kind())
	}

	result := make(map[string]interface{}, len(v.node.objectValue))
	for _, field := range v.node.objectValue {
		if field.Value.IsVoid() {
			continue
		}
		value, err := jsonToNative(field.Value, RootPath().Field(field.Key))
		if err != nil {
			return nil, err
		}
		result[field.Key] = value
	}
	return result, nil
}

func (v JSONValue) ToMapOrDefault(defaultValue map[string]interface{}) map[string]interface{} {
	result, err := v.ToMapTry()
	if err != nil {
		return defaultValue
	}
	return result
}

func (v JSONValue) ToMapOrItemDefault(defaultItem interface{}) map[string]interface{} {
	if !v.IsObject() {
		return map[string]interface{}{}
	}

	result := make(map[string]interface{}, len(v.node.objectValue))
	for _, field := range v.node.objectValue {
		if field.Value.IsVoid() {
			continue
		}
		value, err := jsonToNative(field.Value, RootPath().Field(field.Key))
		if err != nil {
			value = defaultItem
		}
		result[field.Key] = value
	}
	return result
}

func (v JSONValue) ToStructTry(target any) error {
	targetValue := reflect.ValueOf(target)
	if !targetValue.IsValid() || targetValue.Kind() != reflect.Pointer || targetValue.IsNil() {
		return pathError(RootPath(), "target must be a non-nil pointer to a struct")
	}
	elem := targetValue.Elem()
	if elem.Kind() != reflect.Struct {
		return pathError(RootPath(), "target must point to a struct")
	}
	if !v.IsObject() {
		return pathError(RootPath(), "expected object, got %s", v.Kind())
	}

	for i := 0; i < elem.NumField(); i++ {
		fieldType := elem.Type().Field(i)
		if fieldType.PkgPath != "" {
			continue
		}
		name, skip, _ := jsonFieldName(fieldType)
		if skip {
			continue
		}
		source := v.Get(name)
		if source.IsVoid() {
			continue
		}
		if err := assignJSONToValue(source, elem.Field(i), RootPath().Field(name)); err != nil {
			return err
		}
	}
	return nil
}

func nativeToJSON(value any, path Path) (JSONValue, error) {
	if value == nil {
		return NewNull(), nil
	}
	if jsonValue, ok := value.(JSONValue); ok {
		return jsonValue, nil
	}

	reflectValue := reflect.ValueOf(value)
	if isNilReflectValue(reflectValue) {
		return NewNull(), nil
	}

	if marshaler, ok := value.(json.Marshaler); ok {
		bytes, err := marshaler.MarshalJSON()
		if err != nil {
			return NewVoid(), pathError(path, "json marshal failed: %v", err)
		}
		result, err := ReadBytesNoSchema(bytes)
		if err != nil {
			return NewVoid(), pathError(path, "json marshaler returned invalid JSON: %v", err)
		}
		return result, nil
	}
	if marshaler, ok := value.(encoding.TextMarshaler); ok {
		text, err := marshaler.MarshalText()
		if err != nil {
			return NewVoid(), pathError(path, "text marshal failed: %v", err)
		}
		result := NewStringFromBytes(text)
		if result.IsVoid() {
			return NewVoid(), pathError(path, "text marshaler returned malformed UTF-8")
		}
		return result, nil
	}

	return reflectNativeToJSON(reflectValue, path)
}

func reflectNativeToJSON(value reflect.Value, path Path) (JSONValue, error) {
	if !value.IsValid() {
		return NewNull(), nil
	}
	if value.CanInterface() {
		if jsonNumber, ok := value.Interface().(json.Number); ok {
			number, err := NewNumberTry(jsonNumber.String())
			if err != nil {
				return NewVoid(), pathError(path, "invalid json.Number %q", jsonNumber.String())
			}
			return number, nil
		}
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			return NewNull(), nil
		}
		return nativeToJSON(value.Elem().Interface(), path)
	case reflect.String:
		result := NewString(value.String())
		if result.IsVoid() {
			return NewVoid(), pathError(path, "string contains malformed UTF-8")
		}
		return result, nil
	case reflect.Bool:
		return NewBoolean(value.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return NewNumberFromInt(value.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return NewNumberFromInt(value.Uint()), nil
	case reflect.Float32, reflect.Float64:
		result, err := NewNumberFromFloatTry(value.Float())
		if err != nil {
			return NewVoid(), pathError(path, "cannot convert float to JSON number: %v", err)
		}
		return result, nil
	case reflect.Slice, reflect.Array:
		result := NewArray()
		for i := 0; i < value.Len(); i++ {
			item, err := nativeToJSON(value.Index(i).Interface(), path.Index(i))
			if err != nil {
				return NewVoid(), err
			}
			result.Append(item)
		}
		return result, nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return NewVoid(), pathError(path, "map keys must be strings")
		}
		result := NewObject()
		keys := make([]string, 0, value.Len())
		for _, key := range value.MapKeys() {
			keys = append(keys, key.String())
		}
		sort.Strings(keys)
		for _, key := range keys {
			item, err := nativeToJSON(value.MapIndex(reflect.ValueOf(key)).Interface(), path.Field(key))
			if err != nil {
				return NewVoid(), err
			}
			result.Set(key, item)
		}
		return result, nil
	case reflect.Struct:
		return structToJSON(value.Interface(), path, NewVoid(), false)
	default:
		return NewVoid(), pathError(path, "unsupported Go type %s", value.Type())
	}
}

func structToJSON(value any, path Path, defaultItem JSONValue, useItemDefault bool) (JSONValue, error) {
	reflectValue := reflect.ValueOf(value)
	if isNilReflectValue(reflectValue) {
		return NewVoid(), pathError(path, "struct value is nil")
	}
	if reflectValue.Kind() == reflect.Pointer {
		reflectValue = reflectValue.Elem()
	}
	if reflectValue.Kind() != reflect.Struct {
		return NewVoid(), pathError(path, "expected struct, got %s", reflectValue.Kind())
	}

	result := NewObject()
	for i := 0; i < reflectValue.NumField(); i++ {
		fieldType := reflectValue.Type().Field(i)
		if fieldType.PkgPath != "" {
			continue
		}
		name, skip, omitEmpty := jsonFieldName(fieldType)
		if skip {
			continue
		}
		fieldPath := path.Field(name)
		fieldValue, err := nativeToJSON(reflectValue.Field(i).Interface(), fieldPath)
		if err != nil {
			if useItemDefault {
				fieldValue = defaultItem
			} else {
				return NewVoid(), err
			}
		}
		if omitEmpty && fieldValue.IsEmpty() {
			continue
		}
		result.Set(name, fieldValue)
	}
	return result, nil
}

func jsonToNative(value JSONValue, path Path) (interface{}, error) {
	switch value.Kind() {
	case KindVoid:
		return nil, pathError(path, "cannot convert void")
	case KindNull:
		return nil, nil
	case KindObject:
		result := make(map[string]interface{}, len(value.node.objectValue))
		for _, field := range value.node.objectValue {
			if field.Value.IsVoid() {
				continue
			}
			converted, err := jsonToNative(field.Value, path.Field(field.Key))
			if err != nil {
				return nil, err
			}
			result[field.Key] = converted
		}
		return result, nil
	case KindArray:
		result := make([]interface{}, 0, len(value.node.arrayValue))
		for i, item := range value.node.arrayValue {
			converted, err := jsonToNative(item, path.Index(i))
			if err != nil {
				return nil, err
			}
			result = append(result, converted)
		}
		return result, nil
	case KindString:
		return value.node.stringValue, nil
	case KindNumber:
		return json.Number(value.node.stringValue), nil
	case KindBoolean:
		return value.node.boolValue, nil
	default:
		return nil, pathError(path, "unsupported kind %s", value.Kind())
	}
}

func assignJSONToValue(source JSONValue, target reflect.Value, path Path) error {
	if !target.CanSet() {
		return pathError(path, "target field cannot be set")
	}
	if target.Kind() == reflect.Pointer {
		if source.IsNull() {
			target.Set(reflect.Zero(target.Type()))
			return nil
		}
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		return assignJSONToValue(source, target.Elem(), path)
	}
	if target.CanAddr() {
		if unmarshaler, ok := target.Addr().Interface().(json.Unmarshaler); ok {
			if err := unmarshaler.UnmarshalJSON(source.ToJSONBytes()); err != nil {
				return pathError(path, "json unmarshal failed: %v", err)
			}
			return nil
		}
		if unmarshaler, ok := target.Addr().Interface().(encoding.TextUnmarshaler); ok {
			if source.IsObject() || source.IsArray() {
				return pathError(path, "cannot text-unmarshal %s", source.Kind())
			}
			if err := unmarshaler.UnmarshalText([]byte(source.String())); err != nil {
				return pathError(path, "text unmarshal failed: %v", err)
			}
			return nil
		}
	}

	switch target.Kind() {
	case reflect.String:
		value, err := source.toStringTryAt(path)
		if err != nil {
			return err
		}
		target.SetString(value)
	case reflect.Bool:
		value, err := source.toBoolTryAt(path)
		if err != nil {
			return err
		}
		target.SetBool(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := toIntegerAt[int64](source, path)
		if err != nil {
			return err
		}
		if target.OverflowInt(value) {
			return pathError(path, "number overflows %s", target.Type())
		}
		target.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		value, err := toIntegerAt[uint64](source, path)
		if err != nil {
			return err
		}
		if target.OverflowUint(value) {
			return pathError(path, "number overflows %s", target.Type())
		}
		target.SetUint(value)
	case reflect.Float32, reflect.Float64:
		value, err := source.toFloat64TryAt(path)
		if err != nil {
			return err
		}
		if target.OverflowFloat(value) {
			return pathError(path, "number overflows %s", target.Type())
		}
		target.SetFloat(value)
	case reflect.Interface:
		value, err := jsonToNative(source, path)
		if err != nil {
			return err
		}
		if value == nil {
			target.Set(reflect.Zero(target.Type()))
			return nil
		}
		target.Set(reflect.ValueOf(value))
	case reflect.Slice:
		if source.IsNull() {
			target.Set(reflect.Zero(target.Type()))
			return nil
		}
		if !source.IsArray() {
			return pathError(path, "expected array, got %s", source.Kind())
		}
		slice := reflect.MakeSlice(target.Type(), 0, source.Len())
		for i, item := range source.node.arrayValue {
			elem := reflect.New(target.Type().Elem()).Elem()
			if err := assignJSONToValue(item, elem, path.Index(i)); err != nil {
				return err
			}
			slice = reflect.Append(slice, elem)
		}
		target.Set(slice)
	case reflect.Struct:
		if !source.IsObject() {
			return pathError(path, "expected object, got %s", source.Kind())
		}
		for i := 0; i < target.NumField(); i++ {
			fieldType := target.Type().Field(i)
			if fieldType.PkgPath != "" {
				continue
			}
			name, skip, _ := jsonFieldName(fieldType)
			if skip {
				continue
			}
			fieldSource := source.Get(name)
			if fieldSource.IsVoid() {
				continue
			}
			if err := assignJSONToValue(fieldSource, target.Field(i), path.Field(name)); err != nil {
				return err
			}
		}
	default:
		return pathError(path, "unsupported target type %s", target.Type())
	}
	return nil
}

func jsonFieldName(field reflect.StructField) (name string, skip bool, omitEmpty bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", true, false
	}
	parts := strings.Split(tag, ",")
	if parts[0] != "" {
		name = parts[0]
	} else {
		name = field.Name
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			omitEmpty = true
		}
	}
	return name, false, omitEmpty
}

func isNilReflectValue(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
