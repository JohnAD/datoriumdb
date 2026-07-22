package docjson

import (
	"strings"
	"testing"

	"github.com/JohnAD/ojson"
)

const ipv4LookupsSchema = `{
  "kind": "object",
  "children": [
    {
      "name": "ranges",
      "kind": "array",
      "required": true,
      "items": {
        "kind": "object",
        "children": [
          {"name": "from", "kind": "number", "integer": true, "required": true},
          {"name": "to", "kind": "number", "integer": true, "required": true},
          {"name": "asn", "kind": "number", "integer": true, "nullable": true},
          {"name": "jurisdiction", "kind": "string", "nullable": true},
          {"name": "location", "kind": "string", "nullable": true}
        ]
      }
    }
  ]
}`

func TestCanonicalizeEnforcesMetaAndSchemaOrderPreservesExtras(t *testing.T) {
	// Deliberately scramble meta, schema, nested item, and extra field order.
	raw := []byte(`{
  "ranges": [
    {
      "location": null,
      "jurisdiction": "cn",
      "asn": 4837,
      "to": 8191,
      "from": 0
    }
  ],
  "extraZ": 1,
  "extraA": 2,
  "#": "01VERTEST0000000000000001",
  "$": "IPv4Lookups:0",
  "!": "1-24"
}`)
	out, err := CanonicalizeBytes([]byte(ipv4LookupsSchema), raw)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	// Database-owned metadata must lead in enforced order.
	metaIdx := []int{
		strings.Index(got, `"!"`),
		strings.Index(got, `"$"`),
		strings.Index(got, `"#"`),
	}
	for i, idx := range metaIdx {
		if idx < 0 {
			t.Fatalf("missing meta field at position %d in:\n%s", i, got)
		}
		if i > 0 && idx < metaIdx[i-1] {
			t.Fatalf("meta field order wrong in:\n%s", got)
		}
	}
	rangesIdx := strings.Index(got, `"ranges"`)
	if rangesIdx < 0 || rangesIdx < metaIdx[2] {
		t.Fatalf("schema field ranges must follow metadata in:\n%s", got)
	}

	// Nested SOT item fields must follow schema children order.
	fromIdx := strings.Index(got, `"from"`)
	toIdx := strings.Index(got, `"to"`)
	asnIdx := strings.Index(got, `"asn"`)
	jurIdx := strings.Index(got, `"jurisdiction"`)
	locIdx := strings.Index(got, `"location"`)
	if !(fromIdx < toIdx && toIdx < asnIdx && asnIdx < jurIdx && jurIdx < locIdx) {
		t.Fatalf("nested SOT field order not enforced in:\n%s", got)
	}

	// Non-schema extras keep relative input order (extraZ before extraA)
	// and appear after schema fields.
	extraZ := strings.Index(got, `"extraZ"`)
	extraA := strings.Index(got, `"extraA"`)
	if extraZ < rangesIdx || extraA < rangesIdx {
		t.Fatalf("extras must follow SOT fields in:\n%s", got)
	}
	if extraZ > extraA {
		t.Fatalf("extra field input order not preserved in:\n%s", got)
	}
}

func TestCanonicalizeUsesOJSONNotGoMaps(t *testing.T) {
	doc := ojson.NewObject()
	_ = doc.SetTry("title", ojson.NewString("x"))
	_ = doc.SetTry("!", ojson.NewString("id1"))
	_ = doc.SetTry("$", ojson.NewString("Movies:0"))
	_ = doc.SetTry("#", ojson.NewString("v1"))
	_ = doc.SetTry("status", ojson.NewString("released"))
	schema := []byte(`{
	  "kind":"object",
	  "children":[
	    {"name":"title","kind":"string"},
	    {"name":"status","kind":"string"}
	  ]
	}`)
	out, err := Canonicalize(schema, doc)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	wantOrder := []string{`"!"`, `"$"`, `"#"`, `"title"`, `"status"`}
	last := -1
	for _, key := range wantOrder {
		idx := strings.Index(got, key)
		if idx < 0 {
			t.Fatalf("missing %s in:\n%s", key, got)
		}
		if idx < last {
			t.Fatalf("order broken around %s in:\n%s", key, got)
		}
		last = idx
	}
}
