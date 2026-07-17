package idgen

import (
	"testing"
	"time"
)

func TestValidDocumentID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"01ARZ3NDEKTSV4RRFFQ69G5FAV", true},
		{"abc-123_X", true},
		{"", false},
		{".", false},
		{"..", false},
		{"null", false},
		{"../evil", false},
		{"a/b", false},
		{"a b", false},
	}
	for _, tc := range cases {
		if got := ValidDocumentID(tc.id); got != tc.want {
			t.Fatalf("ValidDocumentID(%q)=%v want %v", tc.id, got, tc.want)
		}
	}
}

func TestSetClockDeterministic(t *testing.T) {
	fixed := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	restore := SetClock(func() time.Time { return fixed })
	defer restore()
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if a[:10] != b[:10] {
		t.Fatalf("expected shared timestamp prefix, got %q and %q", a, b)
	}
}
