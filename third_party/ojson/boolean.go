package ojson

func (v JSONValue) GetBoolean(defaultValue bool) bool {
	return v.ToBoolOrDefault(defaultValue)
}

func (v JSONValue) ToBool() bool {
	return v.ToBoolOrDefault(false)
}

func (v JSONValue) ToBoolTry() (bool, error) {
	return v.toBoolTryAt(RootPath())
}

func (v JSONValue) toBoolTryAt(path Path) (bool, error) {
	if !v.IsBoolean() {
		return false, pathError(path, "expected boolean, got %s", v.Kind())
	}

	return v.node.boolValue, nil
}

func (v JSONValue) ToBoolOrDefault(defaultValue bool) bool {
	result, err := v.ToBoolTry()
	if err != nil {
		return defaultValue
	}

	return result
}
