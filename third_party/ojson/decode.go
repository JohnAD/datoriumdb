package ojson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

func ReadStringNoSchema(jsonText string) (JSONValue, error) {
	return ReadBytesNoSchema([]byte(jsonText))
}

func ReadBytesNoSchema(jsonBytes []byte) (JSONValue, error) {
	if !utf8.Valid(jsonBytes) {
		return NewVoid(), fmt.Errorf("JSON text must be valid UTF-8")
	}

	decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
	decoder.UseNumber()

	value, err := parseJSONValue(decoder)
	if err != nil {
		return NewVoid(), err
	}

	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return NewVoid(), err
		}
		return NewVoid(), fmt.Errorf("unexpected trailing JSON token %v", token)
	}

	return value, nil
}

func ReadFileNoSchema(path string) (JSONValue, error) {
	jsonBytes, err := os.ReadFile(path)
	if err != nil {
		return NewVoid(), err
	}

	return ReadBytesNoSchema(jsonBytes)
}

func parseJSONValue(decoder *json.Decoder) (JSONValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return NewVoid(), err
	}

	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			return parseJSONObject(decoder)
		case '[':
			return parseJSONArray(decoder)
		default:
			return NewVoid(), fmt.Errorf("unexpected JSON delimiter %q", typed)
		}
	case string:
		return NewString(typed), nil
	case json.Number:
		return NewNumberTry(typed.String())
	case bool:
		return NewBoolean(typed), nil
	case nil:
		return NewNull(), nil
	default:
		return NewVoid(), fmt.Errorf("unexpected JSON token %v", token)
	}
}

func parseJSONObject(decoder *json.Decoder) (JSONValue, error) {
	value := NewObject()
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return NewVoid(), err
		}

		key, ok := token.(string)
		if !ok {
			return NewVoid(), fmt.Errorf("object key must be a string")
		}

		fieldValue, err := parseJSONValue(decoder)
		if err != nil {
			return NewVoid(), err
		}
		value.Set(key, fieldValue)
	}

	end, err := decoder.Token()
	if err != nil {
		return NewVoid(), err
	}
	if end != json.Delim('}') {
		return NewVoid(), fmt.Errorf("expected object end, got %v", end)
	}

	return value, nil
}

func parseJSONArray(decoder *json.Decoder) (JSONValue, error) {
	value := NewArray()
	for decoder.More() {
		item, err := parseJSONValue(decoder)
		if err != nil {
			return NewVoid(), err
		}
		value.Append(item)
	}

	end, err := decoder.Token()
	if err != nil {
		return NewVoid(), err
	}
	if end != json.Delim(']') {
		return NewVoid(), fmt.Errorf("expected array end, got %v", end)
	}

	return value, nil
}
