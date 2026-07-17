package ojson

import "testing"

func TestArrayAccessAndMutation(t *testing.T) {
	array := NewArray()
	array.Append(NewString("middle"))
	array.Prepend(NewString("first"))
	if err := array.InsertAtTry(2, NewString("last")); err != nil {
		t.Fatalf("InsertAtTry returned error: %v", err)
	}

	if array.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", array.Len())
	}
	if got := array.At(0).String(); got != "first" {
		t.Fatalf("At(0) = %q, want first", got)
	}
	if got := array.AtOrDefault(99, NewString("default")).String(); got != "default" {
		t.Fatalf("AtOrDefault missing = %q, want default", got)
	}
	if got := array.ToJSON(); got != `["first","middle","last"]` {
		t.Fatalf("ToJSON() = %q", got)
	}
}

func TestArrayTryMethodsRejectInvalidMutation(t *testing.T) {
	array := NewArray()

	if err := array.AppendTry(NewVoid()); err == nil {
		t.Fatal("AppendTry(Void) returned nil error")
	}
	if err := array.PrependTry(NewVoid()); err == nil {
		t.Fatal("PrependTry(Void) returned nil error")
	}
	if err := array.InsertAtTry(1, NewString("x")); err == nil {
		t.Fatal("InsertAtTry out of range returned nil error")
	}
	if _, err := NewString("x").AtTry(0); err == nil {
		t.Fatal("AtTry on non-array returned nil error")
	}
}

func TestArrayRemoveAndCompress(t *testing.T) {
	array := NewArray()
	array.Append(NewString("first"))
	array.Append(NewString("second"))
	array.Append(NewString("third"))

	removed := array.Remove(1)
	if got := removed.String(); got != "second" {
		t.Fatalf("Remove(1) = %q, want second", got)
	}
	if got := array.ToJSON(); got != `["first","third"]` {
		t.Fatalf("ToJSON() after removal = %q", got)
	}

	array.node.arrayValue = append(array.node.arrayValue, NewVoid(), NewString("fourth"))
	if removedCount := array.Compress(); removedCount != 1 {
		t.Fatalf("Compress() = %d, want 1", removedCount)
	}
	if got := array.ToJSON(); got != `["first","third","fourth"]` {
		t.Fatalf("ToJSON() after compress = %q", got)
	}
}

func TestArrayItemsReturnsCopy(t *testing.T) {
	array := NewArray()
	array.Append(NewString("first"))

	items := array.Items()
	items[0] = NewString("changed")

	if got := array.At(0).String(); got != "first" {
		t.Fatalf("array changed through Items copy: %q", got)
	}
}
