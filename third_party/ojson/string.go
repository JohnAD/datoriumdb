package ojson

import (
	"encoding"
	"fmt"
	"unicode/utf8"
)

func isValidUTF8(value string) bool {
	return utf8.ValidString(value)
}

func NewStringFromBytes(value []byte) JSONValue {
	if !utf8.Valid(value) {
		return NewVoid()
	}

	return NewString(string(value))
}

func NewStringFromStringer(value fmt.Stringer) JSONValue {
	if value == nil {
		return NewVoid()
	}

	return NewString(value.String())
}

func NewStringFromTextMarshaler(value encoding.TextMarshaler) JSONValue {
	result, err := NewStringFromTextMarshalerTry(value)
	if err != nil {
		return NewVoid()
	}

	return result
}

func NewStringFromTextMarshalerTry(value encoding.TextMarshaler) (JSONValue, error) {
	if value == nil {
		return NewVoid(), pathError(RootPath(), "text marshaler is nil")
	}

	text, err := value.MarshalText()
	if err != nil {
		return NewVoid(), pathError(RootPath(), "text marshal failed: %v", err)
	}

	if !utf8.Valid(text) {
		return NewVoid(), pathError(RootPath(), "text marshaler returned malformed UTF-8")
	}

	return NewString(string(text)), nil
}

func NewStringFromTextMarshalerOrDefault(value encoding.TextMarshaler, defaultValue JSONValue) JSONValue {
	result, err := NewStringFromTextMarshalerTry(value)
	if err != nil {
		return defaultValue
	}

	return result
}

func (v JSONValue) GetString(defaultValue string) string {
	return v.ToStringOrDefault(defaultValue)
}

func (v JSONValue) ToString() string {
	return v.ToStringOrEmpty()
}

func (v JSONValue) ToStringTry() (string, error) {
	return v.toStringTryAt(RootPath())
}

func (v JSONValue) toStringTryAt(path Path) (string, error) {
	if !v.IsString() {
		return "", pathError(path, "expected string, got %s", v.Kind())
	}

	return v.node.stringValue, nil
}

func (v JSONValue) ToStringOrDefault(defaultValue string) string {
	result, err := v.ToStringTry()
	if err != nil {
		return defaultValue
	}

	return result
}

func (v JSONValue) ToStringOrEmpty() string {
	return v.ToStringOrDefault("")
}
