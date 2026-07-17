package ojson

// JSONKind identifies the kind of value stored in a JSONValue.
type JSONKind uint8

const (
	KindVoid JSONKind = iota
	KindObject
	KindArray
	KindString
	KindNumber
	KindBoolean
	KindNull
)

func (k JSONKind) String() string {
	switch k {
	case KindVoid:
		return "void"
	case KindObject:
		return "object"
	case KindArray:
		return "array"
	case KindString:
		return "string"
	case KindNumber:
		return "number"
	case KindBoolean:
		return "boolean"
	case KindNull:
		return "null"
	default:
		return "unknown"
	}
}
