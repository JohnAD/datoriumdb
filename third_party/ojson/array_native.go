package ojson

func NewArrayFromStringArray(values []string) JSONValue {
	result := NewArray()
	for _, value := range values {
		result.Append(NewString(value))
	}
	return result
}

func NewArrayFromStringPointerArray(values []*string) JSONValue {
	result := NewArray()
	for _, value := range values {
		if value == nil {
			result.Append(NewNull())
			continue
		}
		result.Append(NewString(*value))
	}
	return result
}

func NewArrayFromNumberArray(values []string) JSONValue {
	result, err := NewArrayFromNumberArrayTry(values)
	if err != nil {
		return NewVoid()
	}
	return result
}

func NewArrayFromNumberArrayTry(values []string) (JSONValue, error) {
	result := NewArray()
	for i, value := range values {
		number, err := NewNumberTry(value)
		if err != nil {
			return NewVoid(), pathError(RootPath().Index(i), "invalid JSON number %q", value)
		}
		result.Append(number)
	}
	return result, nil
}

func NewArrayFromNumberArrayOrItemDefault(values []string, defaultItem string) JSONValue {
	defaultValue, err := NewNumberTry(defaultItem)
	if err != nil {
		return NewVoid()
	}

	result := NewArray()
	for _, value := range values {
		number, err := NewNumberTry(value)
		if err != nil {
			result.Append(defaultValue)
			continue
		}
		result.Append(number)
	}
	return result
}

func NewArrayFromIntArray(values []int) JSONValue {
	result := NewArray()
	for _, value := range values {
		result.Append(NewNumberFromInt(value))
	}
	return result
}

func NewArrayFromInt64Array(values []int64) JSONValue {
	result := NewArray()
	for _, value := range values {
		result.Append(NewNumberFromInt(value))
	}
	return result
}

func NewArrayFromFloat64Array(values []float64) JSONValue {
	result, err := NewArrayFromFloat64ArrayTry(values)
	if err != nil {
		return NewVoid()
	}
	return result
}

func NewArrayFromFloat64ArrayTry(values []float64) (JSONValue, error) {
	result := NewArray()
	for i, value := range values {
		number, err := NewNumberFromFloatTry(value)
		if err != nil {
			return NewVoid(), pathError(RootPath().Index(i), "cannot convert float64 to JSON number: %v", err)
		}
		result.Append(number)
	}
	return result, nil
}

func NewArrayFromFloat64ArrayOrItemDefault(values []float64, defaultItem float64) JSONValue {
	defaultValue, err := NewNumberFromFloatTry(defaultItem)
	if err != nil {
		return NewVoid()
	}

	result := NewArray()
	for _, value := range values {
		number, err := NewNumberFromFloatTry(value)
		if err != nil {
			result.Append(defaultValue)
			continue
		}
		result.Append(number)
	}
	return result
}

func NewArrayFromBooleanArray(values []bool) JSONValue {
	result := NewArray()
	for _, value := range values {
		result.Append(NewBoolean(value))
	}
	return result
}

func NewArrayFromBooleanPointerArray(values []*bool) JSONValue {
	result := NewArray()
	for _, value := range values {
		if value == nil {
			result.Append(NewNull())
			continue
		}
		result.Append(NewBoolean(*value))
	}
	return result
}

func (v JSONValue) ToStringArray() []*string {
	if !v.IsArray() {
		return []*string{}
	}

	result := make([]*string, 0, v.Len())
	for i := range v.node.arrayValue {
		value, err := v.node.arrayValue[i].toStringTryAt(RootPath().Index(i))
		if err != nil {
			result = append(result, nil)
			continue
		}
		result = append(result, &value)
	}
	return result
}

func (v JSONValue) ToStringArrayTry() ([]string, error) {
	return arrayConvertTry(v, func(value JSONValue, path Path) (string, error) {
		return value.toStringTryAt(path)
	})
}

func (v JSONValue) ToStringArrayOrItemDefault(defaultItem string) []string {
	return arrayConvertOrItemDefault(v, defaultItem, func(value JSONValue, path Path) (string, error) {
		return value.toStringTryAt(path)
	})
}

func (v JSONValue) ToNumberArray() []*string {
	if !v.IsArray() {
		return []*string{}
	}

	result := make([]*string, 0, v.Len())
	for _, item := range v.node.arrayValue {
		if !item.IsNumber() {
			result = append(result, nil)
			continue
		}
		value := item.node.stringValue
		result = append(result, &value)
	}
	return result
}

func (v JSONValue) ToNumberArrayTry() ([]string, error) {
	return arrayConvertTry(v, func(value JSONValue, path Path) (string, error) {
		if !value.IsNumber() {
			return "", pathError(path, "expected number, got %s", value.Kind())
		}
		return value.node.stringValue, nil
	})
}

func (v JSONValue) ToNumberArrayOrItemDefault(defaultItem string) []string {
	return arrayConvertOrItemDefault(v, defaultItem, func(value JSONValue, path Path) (string, error) {
		if !value.IsNumber() {
			return "", pathError(path, "expected number, got %s", value.Kind())
		}
		return value.node.stringValue, nil
	})
}

func (v JSONValue) ToIntArray() []*int {
	return arrayConvertPointers(v, func(value JSONValue, path Path) (int, error) {
		return toIntegerAt[int](value, path)
	})
}

func (v JSONValue) ToIntArrayTry() ([]int, error) {
	return arrayConvertTry(v, func(value JSONValue, path Path) (int, error) {
		return toIntegerAt[int](value, path)
	})
}

func (v JSONValue) ToIntArrayOrItemDefault(defaultItem int) []int {
	return arrayConvertOrItemDefault(v, defaultItem, func(value JSONValue, path Path) (int, error) {
		return toIntegerAt[int](value, path)
	})
}

func (v JSONValue) ToInt64Array() []*int64 {
	return arrayConvertPointers(v, func(value JSONValue, path Path) (int64, error) {
		return toIntegerAt[int64](value, path)
	})
}

func (v JSONValue) ToInt64ArrayTry() ([]int64, error) {
	return arrayConvertTry(v, func(value JSONValue, path Path) (int64, error) {
		return toIntegerAt[int64](value, path)
	})
}

func (v JSONValue) ToInt64ArrayOrItemDefault(defaultItem int64) []int64 {
	return arrayConvertOrItemDefault(v, defaultItem, func(value JSONValue, path Path) (int64, error) {
		return toIntegerAt[int64](value, path)
	})
}

func (v JSONValue) ToFloat64Array() []*float64 {
	return arrayConvertPointers(v, func(value JSONValue, path Path) (float64, error) {
		return value.toFloat64TryAt(path)
	})
}

func (v JSONValue) ToFloat64ArrayTry() ([]float64, error) {
	return arrayConvertTry(v, func(value JSONValue, path Path) (float64, error) {
		return value.toFloat64TryAt(path)
	})
}

func (v JSONValue) ToFloat64ArrayOrItemDefault(defaultItem float64) []float64 {
	return arrayConvertOrItemDefault(v, defaultItem, func(value JSONValue, path Path) (float64, error) {
		return value.toFloat64TryAt(path)
	})
}

func (v JSONValue) ToBoolArray() []*bool {
	return arrayConvertPointers(v, func(value JSONValue, path Path) (bool, error) {
		return value.toBoolTryAt(path)
	})
}

func (v JSONValue) ToBoolArrayTry() ([]bool, error) {
	return arrayConvertTry(v, func(value JSONValue, path Path) (bool, error) {
		return value.toBoolTryAt(path)
	})
}

func (v JSONValue) ToBoolArrayOrItemDefault(defaultItem bool) []bool {
	return arrayConvertOrItemDefault(v, defaultItem, func(value JSONValue, path Path) (bool, error) {
		return value.toBoolTryAt(path)
	})
}

func arrayConvertPointers[T any](value JSONValue, convert func(JSONValue, Path) (T, error)) []*T {
	if !value.IsArray() {
		return []*T{}
	}

	result := make([]*T, 0, value.Len())
	for i, item := range value.node.arrayValue {
		converted, err := convert(item, RootPath().Index(i))
		if err != nil {
			result = append(result, nil)
			continue
		}
		result = append(result, &converted)
	}
	return result
}

func arrayConvertTry[T any](value JSONValue, convert func(JSONValue, Path) (T, error)) ([]T, error) {
	if !value.IsArray() {
		return []T{}, nil
	}

	result := make([]T, 0, value.Len())
	for i, item := range value.node.arrayValue {
		converted, err := convert(item, RootPath().Index(i))
		if err != nil {
			return nil, err
		}
		result = append(result, converted)
	}
	return result, nil
}

func arrayConvertOrItemDefault[T any](value JSONValue, defaultItem T, convert func(JSONValue, Path) (T, error)) []T {
	if !value.IsArray() {
		return []T{}
	}

	result := make([]T, 0, value.Len())
	for i, item := range value.node.arrayValue {
		converted, err := convert(item, RootPath().Index(i))
		if err != nil {
			result = append(result, defaultItem)
			continue
		}
		result = append(result, converted)
	}
	return result
}
