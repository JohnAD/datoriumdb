package ojson

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadStringNoSchemaPreservesObjectOrderAndNumbers(t *testing.T) {
	doc, err := ReadStringNoSchema(`{"z":0.25E2,"a":"x","nested":{"b":true,"a":null}}`)
	if err != nil {
		t.Fatalf("ReadStringNoSchema returned error: %v", err)
	}

	if got := doc.ToJSON(); got != `{"z":0.25E2,"a":"x","nested":{"b":true,"a":null}}` {
		t.Fatalf("ToJSON() = %q", got)
	}
	if got := doc.Get("z").String(); got != "0.25E2" {
		t.Fatalf("number text = %q, want 0.25E2", got)
	}
}

func TestReadBytesNoSchemaRejectsMalformedUTF8(t *testing.T) {
	if _, err := ReadBytesNoSchema([]byte{0xff}); err == nil {
		t.Fatal("ReadBytesNoSchema malformed UTF-8 returned nil error")
	}
}

func TestReadStringNoSchemaRejectsTrailingTokens(t *testing.T) {
	if _, err := ReadStringNoSchema(`{} {}`); err == nil {
		t.Fatal("ReadStringNoSchema trailing tokens returned nil error")
	}
}

func TestReadFileNoSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.json")
	if err := os.WriteFile(path, []byte(`{"name":"Whiffles"}`), 0o600); err != nil {
		t.Fatalf("WriteFile fixture returned error: %v", err)
	}

	doc, err := ReadFileNoSchema(path)
	if err != nil {
		t.Fatalf("ReadFileNoSchema returned error: %v", err)
	}
	if got := doc.Get("name").String(); got != "Whiffles" {
		t.Fatalf("name = %q, want Whiffles", got)
	}
}

func TestToPrettyJSON(t *testing.T) {
	doc := NewObject()
	doc.Set("name", NewString("Whiffles"))
	doc.Set("ratings", NewArray())
	doc.Get("ratings").Append(NewNumber("3.2"))
	doc.Get("ratings").Append(NewNull())

	want := "{\n" +
		"  \"name\": \"Whiffles\",\n" +
		"  \"ratings\": [\n" +
		"    3.2,\n" +
		"    null\n" +
		"  ]\n" +
		"}"
	if got := doc.ToPrettyJSON(2); got != want {
		t.Fatalf("ToPrettyJSON(2) =\n%s\nwant\n%s", got, want)
	}
}

func TestStringEscapingInJSON(t *testing.T) {
	value := NewString("line\nquote\"")
	if got := value.ToJSON(); got != `"line\nquote\""` {
		t.Fatalf("ToJSON() = %q", got)
	}
}
