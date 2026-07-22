package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
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

// TestCreatePersistsEnforcedSOTFieldOrderAndPreservesExtras locks the
// SCHEMAS.md field-order contract on the create→disk path: database-owned
// metadata (!, $, #) is enforced first, schema children (including nested
// array item fields) follow schema order even when the client scrambled
// them, and non-schema extras keep their relative input order after SOT
// fields. Storage must go through OJSON — not encoding/json + map[string]any.
func TestCreatePersistsEnforcedSOTFieldOrderAndPreservesExtras(t *testing.T) {
	eng := testEngine(t)
	for _, name := range []string{"IPv4Lookups.schema.json", "IPv4Lookups.schema.0.json"} {
		if err := os.WriteFile(filepath.Join(eng.ConfigDir, name), []byte(ipv4LookupsSchema), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}

	// Scramble meta, top-level schema field position, nested item fields,
	// and include two extras in a deliberate relative order.
	cmd := `create IPv4Lookups 1-24 {
		ranges: [{location: null, jurisdiction: cn, asn: 4837, to: 8191, from: 0}],
		extraZ: 1,
		extraA: 2,
		$: IPv4Lookups:0
	}`
	res := eng.Execute(cmd)
	if res["ok"] != true {
		t.Fatalf("create failed: %#v", res)
	}

	raw, err := os.ReadFile(fsstore.DocumentPath(eng.DataDir, "IPv4Lookups", "1-24"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)

	metaKeys := []string{`"!"`, `"$"`, `"#"`}
	last := -1
	for _, key := range metaKeys {
		idx := strings.Index(got, key)
		if idx < 0 {
			t.Fatalf("missing %s in on-disk document:\n%s", key, got)
		}
		if idx < last {
			t.Fatalf("database-owned metadata order not enforced around %s:\n%s", key, got)
		}
		last = idx
	}
	rangesIdx := strings.Index(got, `"ranges"`)
	if rangesIdx < last {
		t.Fatalf("schema field ranges must follow metadata:\n%s", got)
	}

	fromIdx := strings.Index(got, `"from"`)
	toIdx := strings.Index(got, `"to"`)
	asnIdx := strings.Index(got, `"asn"`)
	jurIdx := strings.Index(got, `"jurisdiction"`)
	locIdx := strings.Index(got, `"location"`)
	if !(fromIdx >= 0 && fromIdx < toIdx && toIdx < asnIdx && asnIdx < jurIdx && jurIdx < locIdx) {
		t.Fatalf("nested SOT field order not enforced (client sent location/jurisdiction/asn/to/from):\n%s", got)
	}

	extraZ := strings.Index(got, `"extraZ"`)
	extraA := strings.Index(got, `"extraA"`)
	if extraZ < rangesIdx || extraA < rangesIdx {
		t.Fatalf("extra fields must follow SOT fields:\n%s", got)
	}
	if extraZ > extraA {
		t.Fatalf("extra field input order not preserved (extraZ before extraA):\n%s", got)
	}
}
