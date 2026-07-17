package ojson

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestPathRendering(t *testing.T) {
	path := RootPath().Field("pet").Field("ratings").Index(4)
	if got := path.String(); got != `"pet"."ratings".4` {
		t.Fatalf("Path.String() = %q", got)
	}
}

func TestScalarConversions(t *testing.T) {
	if got := NewString("Whiffles").ToStringOrDefault("unknown"); got != "Whiffles" {
		t.Fatalf("ToStringOrDefault() = %q", got)
	}
	if _, err := NewNull().ToStringTry(); err == nil || !strings.Contains(err.Error(), "$: expected string, got null") {
		t.Fatalf("ToStringTry null error = %v", err)
	}

	if got := NewBoolean(true).ToBool(); !got {
		t.Fatal("ToBool() = false, want true")
	}

	prepared, err := PrepareNumber(" 25. ")
	if err != nil {
		t.Fatalf("PrepareNumber returned error: %v", err)
	}
	if prepared != "0.25E2" {
		t.Fatalf("PrepareNumber() = %q, want 0.25E2", prepared)
	}

	value := NewNumber("0.25E2")
	if got, err := value.ToIntTry(); err != nil || got != 25 {
		t.Fatalf("ToIntTry() = %d, %v; want 25, nil", got, err)
	}
	if _, err := NewNumber("1.5").ToIntTry(); err == nil {
		t.Fatal("ToIntTry fractional number returned nil error")
	}
	if _, err := NewNumberFromFloatTry(math.NaN()); err == nil {
		t.Fatal("NewNumberFromFloatTry(NaN) returned nil error")
	}
}

func TestNativeArrayConversions(t *testing.T) {
	array := NewArray()
	array.Append(NewString("Ada"))
	array.Append(NewNumberFromInt(7))
	array.Append(NewString("Grace"))

	names := array.ToStringArray()
	if names[0] == nil || *names[0] != "Ada" {
		t.Fatalf("ToStringArray()[0] = %v", names[0])
	}
	if names[1] != nil {
		t.Fatalf("ToStringArray()[1] = %v, want nil", *names[1])
	}

	if _, err := array.ToStringArrayTry(); err == nil || !strings.Contains(err.Error(), "1: expected string, got number") {
		t.Fatalf("ToStringArrayTry error = %v", err)
	}
	if got := array.ToStringArrayOrItemDefault("unknown"); got[1] != "unknown" {
		t.Fatalf("ToStringArrayOrItemDefault()[1] = %q", got[1])
	}

	numbers := NewArrayFromNumberArray([]string{"1", "0.25E2"})
	ints, err := numbers.ToIntArrayTry()
	if err != nil {
		t.Fatalf("ToIntArrayTry returned error: %v", err)
	}
	if ints[0] != 1 || ints[1] != 25 {
		t.Fatalf("ToIntArrayTry() = %#v", ints)
	}
}

func TestNativeArrayConstructors(t *testing.T) {
	if got := NewArrayFromStringArray([]string{"Ada", "Grace"}).ToJSON(); got != `["Ada","Grace"]` {
		t.Fatalf("NewArrayFromStringArray JSON = %q", got)
	}

	first := "Ada"
	if got := NewArrayFromStringPointerArray([]*string{&first, nil}).ToJSON(); got != `["Ada",null]` {
		t.Fatalf("NewArrayFromStringPointerArray JSON = %q", got)
	}

	if got := NewArrayFromNumberArray([]string{"1", "bad"}); !got.IsVoid() {
		t.Fatalf("NewArrayFromNumberArray invalid kind = %v, want void", got.Kind())
	}
	if got := NewArrayFromNumberArrayOrItemDefault([]string{"1", "bad"}, "0").ToJSON(); got != `[1,0]` {
		t.Fatalf("NewArrayFromNumberArrayOrItemDefault JSON = %q", got)
	}
}

func TestObjectMapConversion(t *testing.T) {
	doc := NewObjectFromMap(map[string]interface{}{
		"z": true,
		"a": json.Number("0.25E2"),
		"m": []interface{}{"x", nil},
	})

	if got := doc.ToJSON(); got != `{"a":0.25E2,"m":["x",null],"z":true}` {
		t.Fatalf("NewObjectFromMap JSON = %q", got)
	}

	values, err := doc.ToMapTry()
	if err != nil {
		t.Fatalf("ToMapTry returned error: %v", err)
	}
	if values["a"] != json.Number("0.25E2") {
		t.Fatalf("ToMapTry a = %#v", values["a"])
	}
}

func TestStructConversion(t *testing.T) {
	type Pet struct {
		Name  string `json:"name"`
		Age   int    `json:"age"`
		Skip  string `json:"skip,omitempty"`
		Safe  bool   `json:"safe"`
		Notes []int  `json:"notes"`
	}

	doc := NewObjectFromStruct(Pet{
		Name:  "Whiffles",
		Age:   3,
		Safe:  true,
		Notes: []int{1, 2},
	})
	if got := doc.ToJSON(); got != `{"name":"Whiffles","age":3,"safe":true,"notes":[1,2]}` {
		t.Fatalf("NewObjectFromStruct JSON = %q", got)
	}

	var pet Pet
	if err := doc.ToStructTry(&pet); err != nil {
		t.Fatalf("ToStructTry returned error: %v", err)
	}
	if pet.Name != "Whiffles" || pet.Age != 3 || !pet.Safe || len(pet.Notes) != 2 || pet.Notes[1] != 2 {
		t.Fatalf("ToStructTry target = %#v", pet)
	}
}

func TestStructExportPathError(t *testing.T) {
	type Pet struct {
		Ratings []int `json:"ratings"`
	}

	doc := NewObject()
	doc.Set("ratings", NewArray())
	doc.Get("ratings").Append(NewNumberFromInt(1))
	doc.Get("ratings").Append(NewString("bad"))

	var pet Pet
	err := doc.ToStructTry(&pet)
	if err == nil {
		t.Fatal("ToStructTry invalid array item returned nil error")
	}
	if !strings.Contains(err.Error(), `"ratings".1: expected number, got string`) {
		t.Fatalf("ToStructTry error = %v", err)
	}
}
