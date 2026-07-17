package ojson

import "testing"

func TestConstructorsSetKinds(t *testing.T) {
	tests := []struct {
		name  string
		value JSONValue
		kind  JSONKind
	}{
		{name: "void", value: NewVoid(), kind: KindVoid},
		{name: "object", value: NewObject(), kind: KindObject},
		{name: "array", value: NewArray(), kind: KindArray},
		{name: "string", value: NewString("hello"), kind: KindString},
		{name: "number", value: NewNumber("12.5"), kind: KindNumber},
		{name: "boolean", value: NewBoolean(true), kind: KindBoolean},
		{name: "null", value: NewNull(), kind: KindNull},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.value.Kind() != test.kind {
				t.Fatalf("Kind() = %v, want %v", test.value.Kind(), test.kind)
			}
		})
	}
}

func TestKindChecks(t *testing.T) {
	if !NewObject().IsObject() {
		t.Fatal("NewObject().IsObject() = false")
	}
	if !NewArray().IsArray() {
		t.Fatal("NewArray().IsArray() = false")
	}
	if !NewString("x").IsString() {
		t.Fatal("NewString().IsString() = false")
	}
	if !NewNumber("1").IsNumber() {
		t.Fatal("NewNumber().IsNumber() = false")
	}
	if !NewBoolean(false).IsBoolean() {
		t.Fatal("NewBoolean().IsBoolean() = false")
	}
	if !NewNull().IsNull() {
		t.Fatal("NewNull().IsNull() = false")
	}
	if !NewVoid().IsVoid() {
		t.Fatal("NewVoid().IsVoid() = false")
	}
	if !NewVoid().IsMissing() {
		t.Fatal("NewVoid().IsMissing() = false")
	}
}

func TestKnownAndEmptySemantics(t *testing.T) {
	tests := []struct {
		name     string
		value    JSONValue
		known    bool
		empty    bool
		notEmpty bool
	}{
		{name: "void", value: NewVoid(), known: false, empty: true, notEmpty: false},
		{name: "null", value: NewNull(), known: false, empty: true, notEmpty: false},
		{name: "empty object", value: NewObject(), known: true, empty: true, notEmpty: false},
		{name: "empty array", value: NewArray(), known: true, empty: true, notEmpty: false},
		{name: "empty string", value: NewEmptyString(), known: true, empty: true, notEmpty: false},
		{name: "zero number", value: NewNumber("0"), known: true, empty: true, notEmpty: false},
		{name: "false boolean", value: NewBoolean(false), known: true, empty: true, notEmpty: false},
		{name: "non-empty string", value: NewString("x"), known: true, empty: false, notEmpty: true},
		{name: "non-zero number", value: NewNumber("1"), known: true, empty: false, notEmpty: true},
		{name: "true boolean", value: NewBoolean(true), known: true, empty: false, notEmpty: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.value.IsKnown() != test.known {
				t.Fatalf("IsKnown() = %v, want %v", test.value.IsKnown(), test.known)
			}
			if test.value.IsEmpty() != test.empty {
				t.Fatalf("IsEmpty() = %v, want %v", test.value.IsEmpty(), test.empty)
			}
			if test.value.NotEmpty() != test.notEmpty {
				t.Fatalf("NotEmpty() = %v, want %v", test.value.NotEmpty(), test.notEmpty)
			}
		})
	}
}

func TestStringReturnsContentNotJSON(t *testing.T) {
	if got := NewString("foo").String(); got != "foo" {
		t.Fatalf("String() = %q, want %q", got, "foo")
	}
	if got := NewNumber("1.25").String(); got != "1.25" {
		t.Fatalf("String() = %q, want %q", got, "1.25")
	}
	if got := NewBoolean(true).String(); got != "true" {
		t.Fatalf("String() = %q, want %q", got, "true")
	}
}
