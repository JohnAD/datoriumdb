package ojson

import "testing"

func TestNewStringRejectsMalformedUTF8(t *testing.T) {
	malformed := string([]byte{0xff})

	if got := NewString(malformed); !got.IsVoid() {
		t.Fatalf("NewString malformed UTF-8 kind = %v, want void", got.Kind())
	}
}

func TestNewStringFromBytesRejectsMalformedUTF8(t *testing.T) {
	if got := NewStringFromBytes([]byte{0xff}); !got.IsVoid() {
		t.Fatalf("NewStringFromBytes malformed UTF-8 kind = %v, want void", got.Kind())
	}
}

func TestNewStringFromBytesAcceptsUTF8(t *testing.T) {
	if got := NewStringFromBytes([]byte("Whiffles")); got.String() != "Whiffles" {
		t.Fatalf("NewStringFromBytes = %q, want %q", got.String(), "Whiffles")
	}
}
