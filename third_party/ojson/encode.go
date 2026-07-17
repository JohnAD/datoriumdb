package ojson

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

func (v JSONValue) ToJSON() string {
	var buffer bytes.Buffer
	writeJSON(&buffer, v, 0, 0)
	return buffer.String()
}

func (v JSONValue) ToJSONBytes() []byte {
	return []byte(v.ToJSON())
}

func (v JSONValue) ToPrettyJSON(indent int) string {
	if indent <= 0 {
		return v.ToJSON()
	}

	var buffer bytes.Buffer
	writeJSON(&buffer, v, 0, indent)
	return buffer.String()
}

func (v JSONValue) ToPrettyJSONBytes(indent int) []byte {
	return []byte(v.ToPrettyJSON(indent))
}

func (v JSONValue) WriteFile(path string) error {
	return os.WriteFile(path, v.ToPrettyJSONBytes(2), 0o644)
}

func writeJSON(buffer *bytes.Buffer, value JSONValue, level int, indent int) {
	switch value.Kind() {
	case KindObject:
		writeJSONObject(buffer, value, level, indent)
	case KindArray:
		writeJSONArray(buffer, value, level, indent)
	case KindString:
		buffer.WriteString(quoteJSONString(value.node.stringValue))
	case KindNumber:
		buffer.WriteString(value.node.stringValue)
	case KindBoolean:
		buffer.WriteString(value.String())
	case KindNull:
		buffer.WriteString("null")
	case KindVoid:
		return
	}
}

func writeJSONObject(buffer *bytes.Buffer, value JSONValue, level int, indent int) {
	fields := serializableFields(value)
	if len(fields) == 0 {
		buffer.WriteString("{}")
		return
	}

	buffer.WriteByte('{')
	if indent > 0 {
		buffer.WriteByte('\n')
	}

	for i, field := range fields {
		if i > 0 {
			buffer.WriteByte(',')
			if indent > 0 {
				buffer.WriteByte('\n')
			}
		}
		writeIndent(buffer, level+1, indent)
		buffer.WriteString(quoteJSONString(field.Key))
		if indent > 0 {
			buffer.WriteString(": ")
		} else {
			buffer.WriteByte(':')
		}
		writeJSON(buffer, field.Value, level+1, indent)
	}

	if indent > 0 {
		buffer.WriteByte('\n')
		writeIndent(buffer, level, indent)
	}
	buffer.WriteByte('}')
}

func writeJSONArray(buffer *bytes.Buffer, value JSONValue, level int, indent int) {
	items := serializableItems(value)
	if len(items) == 0 {
		buffer.WriteString("[]")
		return
	}

	buffer.WriteByte('[')
	if indent > 0 {
		buffer.WriteByte('\n')
	}

	for i, item := range items {
		if i > 0 {
			buffer.WriteByte(',')
			if indent > 0 {
				buffer.WriteByte('\n')
			}
		}
		writeIndent(buffer, level+1, indent)
		writeJSON(buffer, item, level+1, indent)
	}

	if indent > 0 {
		buffer.WriteByte('\n')
		writeIndent(buffer, level, indent)
	}
	buffer.WriteByte(']')
}

func serializableFields(value JSONValue) []JSONKeyValue {
	fields := make([]JSONKeyValue, 0, len(value.node.objectValue))
	for _, field := range value.node.objectValue {
		if field.Value.IsVoid() {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

func serializableItems(value JSONValue) []JSONValue {
	items := make([]JSONValue, 0, len(value.node.arrayValue))
	for _, item := range value.node.arrayValue {
		if item.IsVoid() {
			continue
		}
		items = append(items, item)
	}
	return items
}

func quoteJSONString(value string) string {
	quoted, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(quoted)
}

func writeIndent(buffer *bytes.Buffer, level int, indent int) {
	if indent <= 0 {
		return
	}
	buffer.WriteString(strings.Repeat(" ", level*indent))
}
