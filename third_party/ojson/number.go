package ojson

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var jsonNumberPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

func IsValidNumber(numberText string) bool {
	return jsonNumberPattern.MatchString(numberText)
}

func PrepareNumber(numberText string) (string, error) {
	prepared := strings.TrimSpace(numberText)
	if strings.HasSuffix(prepared, ".") {
		left := strings.TrimSuffix(prepared, ".")
		if left == "" || left == "-" {
			return "", fmt.Errorf("invalid trailing decimal point number %q", numberText)
		}
		sign := ""
		if strings.HasPrefix(left, "-") {
			sign = "-"
			left = strings.TrimPrefix(left, "-")
		}
		if !regexp.MustCompile(`^(0|[1-9][0-9]*)$`).MatchString(left) {
			return "", fmt.Errorf("invalid integer part before trailing decimal point")
		}
		prepared = fmt.Sprintf("%s0.%sE%d", sign, left, len(left))
	}
	prepared = strings.ReplaceAll(prepared, "e", "E")

	if !IsValidNumber(prepared) {
		return "", fmt.Errorf("%q is not valid JSON number text", numberText)
	}

	return prepared, nil
}

func NewNumber(numberText string) JSONValue {
	value, err := NewNumberTry(numberText)
	if err != nil {
		return NewVoid()
	}

	return value
}

func NewNumberTry(numberText string) (JSONValue, error) {
	if !IsValidNumber(numberText) {
		return NewVoid(), fmt.Errorf("invalid JSON number %q", numberText)
	}

	return JSONValue{
		node: &jsonNode{
			kind:        KindNumber,
			stringValue: numberText,
		},
	}, nil
}

func NewNumberOrDefault(numberText string, defaultValue JSONValue) JSONValue {
	value, err := NewNumberTry(numberText)
	if err != nil {
		return defaultValue
	}

	return value
}

func NewNumberFromInt[T integer](value T) JSONValue {
	return JSONValue{
		node: &jsonNode{
			kind:        KindNumber,
			stringValue: fmt.Sprintf("%d", value),
		},
	}
}

func NewNumberFromFloat[T float](value T) JSONValue {
	result, err := NewNumberFromFloatTry(value)
	if err != nil {
		return NewVoid()
	}

	return result
}

func NewNumberFromFloatTry[T float](value T) (JSONValue, error) {
	floatValue := float64(value)
	if math.IsNaN(floatValue) || math.IsInf(floatValue, 0) {
		return NewVoid(), pathError(RootPath(), "floating-point value is not finite")
	}

	return NewNumberTry(strconv.FormatFloat(floatValue, 'g', -1, 64))
}

func NewNumberFromFloatOrDefault[T float](value T, defaultValue JSONValue) JSONValue {
	result, err := NewNumberFromFloatTry(value)
	if err != nil {
		return defaultValue
	}

	return result
}

func (v JSONValue) GetNumber(defaultValue string) string {
	if !v.IsNumber() {
		return defaultValue
	}

	return v.node.stringValue
}

func (v JSONValue) ToIntTry() (int, error) {
	return toIntegerAt[int](v, RootPath())
}

func (v JSONValue) ToIntOrDefault(defaultValue int) int {
	return ToIntegerOrDefault(v, defaultValue)
}

func (v JSONValue) ToInt64Try() (int64, error) {
	return toIntegerAt[int64](v, RootPath())
}

func (v JSONValue) ToInt64OrDefault(defaultValue int64) int64 {
	return ToIntegerOrDefault(v, defaultValue)
}

func (v JSONValue) ToUint64Try() (uint64, error) {
	return toIntegerAt[uint64](v, RootPath())
}

func (v JSONValue) ToUint64OrDefault(defaultValue uint64) uint64 {
	return ToIntegerOrDefault(v, defaultValue)
}

func (v JSONValue) ToFloat64Try() (float64, error) {
	return v.toFloat64TryAt(RootPath())
}

func (v JSONValue) toFloat64TryAt(path Path) (float64, error) {
	if !v.IsNumber() {
		return 0, pathError(path, "expected number, got %s", v.Kind())
	}

	result, err := strconv.ParseFloat(v.node.stringValue, 64)
	if err != nil {
		return 0, pathError(path, "cannot convert %q to float64: %v", v.node.stringValue, err)
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, pathError(path, "cannot convert %q to finite float64", v.node.stringValue)
	}

	return result, nil
}

func (v JSONValue) ToFloat64OrDefault(defaultValue float64) float64 {
	result, err := v.ToFloat64Try()
	if err != nil {
		return defaultValue
	}

	return result
}

func ToIntegerTry[T integer](value JSONValue) (T, error) {
	return toIntegerAt[T](value, RootPath())
}

func ToIntegerOrDefault[T integer](value JSONValue, defaultValue T) T {
	result, err := ToIntegerTry[T](value)
	if err != nil {
		return defaultValue
	}

	return result
}

func toIntegerAt[T integer](value JSONValue, path Path) (T, error) {
	var zero T
	if !value.IsNumber() {
		return zero, pathError(path, "expected number, got %s", value.Kind())
	}

	rat, err := parseJSONNumberRat(value.node.stringValue)
	if err != nil {
		return zero, pathError(path, "cannot parse number %q: %v", value.node.stringValue, err)
	}
	if rat.Denom().Cmp(big.NewInt(1)) != 0 {
		return zero, pathError(path, "expected integer number, got %q", value.node.stringValue)
	}

	intValue := rat.Num()
	result, err := convertBigInt[T](intValue)
	if err != nil {
		return zero, pathError(path, "%v", err)
	}

	return result, nil
}

func parseJSONNumberRat(numberText string) (*big.Rat, error) {
	if !IsValidNumber(numberText) {
		return nil, fmt.Errorf("invalid JSON number")
	}

	base := numberText
	exponent := 0
	if index := strings.IndexAny(base, "eE"); index >= 0 {
		parsedExponent, err := strconv.Atoi(base[index+1:])
		if err != nil {
			return nil, err
		}
		exponent = parsedExponent
		base = base[:index]
	}

	negative := strings.HasPrefix(base, "-")
	if negative {
		base = strings.TrimPrefix(base, "-")
	}

	fractionDigits := 0
	if index := strings.IndexByte(base, '.'); index >= 0 {
		fractionDigits = len(base) - index - 1
		base = base[:index] + base[index+1:]
	}

	numerator := new(big.Int)
	numerator.SetString(base, 10)
	if negative {
		numerator.Neg(numerator)
	}

	scale := fractionDigits - exponent
	if scale < 0 {
		numerator.Mul(numerator, pow10(-scale))
		return new(big.Rat).SetInt(numerator), nil
	}

	return new(big.Rat).SetFrac(numerator, pow10(scale)), nil
}

func pow10(power int) *big.Int {
	result := big.NewInt(1)
	ten := big.NewInt(10)
	for i := 0; i < power; i++ {
		result.Mul(result, ten)
	}
	return result
}

func convertBigInt[T integer](value *big.Int) (T, error) {
	var zero T
	switch any(zero).(type) {
	case int:
		if !value.IsInt64() {
			return zero, fmt.Errorf("number overflows int")
		}
		i := value.Int64()
		if strconv.IntSize == 32 && (i < math.MinInt32 || i > math.MaxInt32) {
			return zero, fmt.Errorf("number overflows int")
		}
		return T(int(i)), nil
	case int8:
		if !value.IsInt64() || value.Int64() < math.MinInt8 || value.Int64() > math.MaxInt8 {
			return zero, fmt.Errorf("number overflows int8")
		}
		return T(int8(value.Int64())), nil
	case int16:
		if !value.IsInt64() || value.Int64() < math.MinInt16 || value.Int64() > math.MaxInt16 {
			return zero, fmt.Errorf("number overflows int16")
		}
		return T(int16(value.Int64())), nil
	case int32:
		if !value.IsInt64() || value.Int64() < math.MinInt32 || value.Int64() > math.MaxInt32 {
			return zero, fmt.Errorf("number overflows int32")
		}
		return T(int32(value.Int64())), nil
	case int64:
		if !value.IsInt64() {
			return zero, fmt.Errorf("number overflows int64")
		}
		return T(value.Int64()), nil
	case uint, uintptr:
		if value.Sign() < 0 || !value.IsUint64() {
			return zero, fmt.Errorf("number overflows unsigned integer")
		}
		u := value.Uint64()
		if strconv.IntSize == 32 && u > math.MaxUint32 {
			return zero, fmt.Errorf("number overflows unsigned integer")
		}
		return T(uint(u)), nil
	case uint8:
		if value.Sign() < 0 || !value.IsUint64() || value.Uint64() > math.MaxUint8 {
			return zero, fmt.Errorf("number overflows uint8")
		}
		return T(uint8(value.Uint64())), nil
	case uint16:
		if value.Sign() < 0 || !value.IsUint64() || value.Uint64() > math.MaxUint16 {
			return zero, fmt.Errorf("number overflows uint16")
		}
		return T(uint16(value.Uint64())), nil
	case uint32:
		if value.Sign() < 0 || !value.IsUint64() || value.Uint64() > math.MaxUint32 {
			return zero, fmt.Errorf("number overflows uint32")
		}
		return T(uint32(value.Uint64())), nil
	case uint64:
		if value.Sign() < 0 || !value.IsUint64() {
			return zero, fmt.Errorf("number overflows uint64")
		}
		return T(value.Uint64()), nil
	default:
		return zero, fmt.Errorf("unsupported integer type")
	}
}

type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type float interface {
	~float32 | ~float64
}
