package ojson

import "testing"

func TestIsValidNumber(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{value: "0", valid: true},
		{value: "-0", valid: true},
		{value: "25", valid: true},
		{value: "25.0", valid: true},
		{value: "0.25E2", valid: true},
		{value: "1e-9", valid: true},
		{value: "", valid: false},
		{value: "01", valid: false},
		{value: "25.", valid: false},
		{value: ".25", valid: false},
		{value: " 25 ", valid: false},
		{value: "NaN", valid: false},
		{value: "Infinity", valid: false},
	}

	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			if got := IsValidNumber(test.value); got != test.valid {
				t.Fatalf("IsValidNumber(%q) = %v, want %v", test.value, got, test.valid)
			}
		})
	}
}

func TestNewNumberFailureModes(t *testing.T) {
	if got := NewNumber("25."); !got.IsVoid() {
		t.Fatalf("NewNumber invalid result kind = %v, want void", got.Kind())
	}

	if _, err := NewNumberTry("25."); err == nil {
		t.Fatal("NewNumberTry invalid number returned nil error")
	}

	defaultValue := NewNumberFromInt(7)
	if got := NewNumberOrDefault("25.", defaultValue); got.String() != "7" {
		t.Fatalf("NewNumberOrDefault invalid = %q, want %q", got.String(), "7")
	}
}

func TestNewNumberFromInt(t *testing.T) {
	if got := NewNumberFromInt(-12).String(); got != "-12" {
		t.Fatalf("NewNumberFromInt(-12) = %q, want %q", got, "-12")
	}
	if got := NewNumberFromInt(uint64(12)).String(); got != "12" {
		t.Fatalf("NewNumberFromInt(uint64(12)) = %q, want %q", got, "12")
	}
}
