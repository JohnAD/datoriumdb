package ojson

import "testing"

func TestObjectGetSetRemoveAndOrder(t *testing.T) {
	doc := NewObject()
	doc.Set("name", NewString("Whiffles"))
	doc.Set("safe", NewBoolean(true))
	doc.Set("name", NewString("Patches"))

	if got := doc.Get("name").String(); got != "Patches" {
		t.Fatalf("Get(name) = %q, want %q", got, "Patches")
	}
	if !doc.HasField("safe") {
		t.Fatal("HasField(safe) = false")
	}
	if got := doc.ToJSON(); got != `{"name":"Patches","safe":true}` {
		t.Fatalf("ToJSON() = %q", got)
	}

	removed := doc.Remove("name")
	if got := removed.String(); got != "Patches" {
		t.Fatalf("removed value = %q, want %q", got, "Patches")
	}
	if doc.HasField("name") {
		t.Fatal("HasField(name) = true after removal")
	}
	if got := doc.ToJSON(); got != `{"safe":true}` {
		t.Fatalf("ToJSON() after removal = %q", got)
	}
}

func TestObjectVoidSetRemovesField(t *testing.T) {
	doc := NewObject()
	doc.Set("name", NewString("Whiffles"))
	doc.Set("name", NewVoid())

	if !doc.Get("name").IsVoid() {
		t.Fatal("Get(name) after Set(Void) did not return void")
	}
	if got := doc.ToJSON(); got != `{}` {
		t.Fatalf("ToJSON() = %q, want {}", got)
	}
}

func TestObjectTryMethods(t *testing.T) {
	doc := NewObject()
	if err := doc.SetTry("name", NewString("Whiffles")); err != nil {
		t.Fatalf("SetTry returned error: %v", err)
	}
	if err := doc.SetTry("missing", NewVoid()); err == nil {
		t.Fatal("SetTry with void returned nil error")
	}

	if got, err := doc.GetTry("name"); err != nil || got.String() != "Whiffles" {
		t.Fatalf("GetTry(name) = %q, %v", got.String(), err)
	}
	if _, err := doc.GetTry("missing"); err == nil {
		t.Fatal("GetTry(missing) returned nil error")
	}
	if _, err := NewString("x").RemoveTry("name"); err == nil {
		t.Fatal("RemoveTry on non-object returned nil error")
	}
}

func TestNestedObjectMutationThroughGet(t *testing.T) {
	doc := NewObject()
	doc.Set("pet", NewObject())
	doc.Get("pet").Set("name", NewString("Whiffles"))

	if got := doc.ToJSON(); got != `{"pet":{"name":"Whiffles"}}` {
		t.Fatalf("nested mutation ToJSON() = %q", got)
	}
}
