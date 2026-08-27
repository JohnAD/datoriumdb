package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/commandreq"
)

func TestCollectionEnsureViaExecute(t *testing.T) {
	eng := testEngine(t)

	res := eng.Execute(commandreq.Must("collectionEnsure", "", "", map[string]any{}))
	if res["ok"] != false || firstErrorCode(res) != "invalidRequest" {
		t.Fatalf("expected invalidRequest for empty target: %#v", res)
	}

	schema := map[string]any{
		"kind": "object",
		"children": []any{
			map[string]any{"name": "name", "kind": "string"},
		},
	}
	res = eng.Execute(commandreq.Must("collectionEnsure", "People", "", map[string]any{"schema": schema}))
	if res["ok"] != true {
		t.Fatalf("expected create ok: %#v", res)
	}
	if res["created"] != true || res["schemaVersion"] != 0 {
		t.Fatalf("unexpected ensure fields: %#v", res)
	}
	if _, err := os.Stat(filepath.Join(eng.ConfigDir, "People.schema.json")); err != nil {
		t.Fatalf("expected People.schema.json: %v", err)
	}

	// Idempotent ensure with matching schema.
	res = eng.Execute(commandreq.Must("collectionEnsure", "People", "", map[string]any{"schema": schema}))
	if res["ok"] != true || res["changed"] != false {
		t.Fatalf("expected no-op ensure: %#v", res)
	}
}

func TestCollectionEnsureRequiresEstablishment(t *testing.T) {
	eng := testEngine(t)
	eng.ServerName = "serverB"
	res := eng.Execute(commandreq.Must("collectionEnsure", "People", "", map[string]any{
		"schema": map[string]any{"kind": "object", "children": []any{}},
	}))
	if res["ok"] != false || firstErrorCode(res) != "establishmentRequired" {
		t.Fatalf("expected establishmentRequired: %#v", res)
	}
}

func TestSearchEnsureAndDeleteViaExecute(t *testing.T) {
	eng := testEngine(t)

	res := eng.Execute(commandreq.Must("searchEnsure", "Movies", "", map[string]any{}))
	if res["ok"] != false || firstErrorCode(res) != "invalidRequest" {
		t.Fatalf("expected invalidRequest: %#v", res)
	}

	def := map[string]any{
		"$":          "SearchDefinition:v1",
		"collection": "Movies",
		"name":       "byStatus",
		"version":    1,
		"v1": map[string]any{
			"clauses": []any{
				map[string]any{"field": "/status", "op": "equals", "value": "released"},
			},
		},
	}
	res = eng.Execute(commandreq.Must("searchEnsure", "Movies", "byStatus", def))
	if res["ok"] != true {
		t.Fatalf("searchEnsure failed: %#v", res)
	}

	res = eng.Execute(commandreq.Must("searchDelete", "", "byStatus", map[string]any{}))
	if res["ok"] != false || firstErrorCode(res) != "invalidRequest" {
		t.Fatalf("expected invalidRequest on delete: %#v", res)
	}

	eng.ServerName = "serverB"
	res = eng.Execute(commandreq.Must("searchDelete", "Movies", "byStatus", map[string]any{}))
	if res["ok"] != false || firstErrorCode(res) != "establishmentRequired" {
		t.Fatalf("expected establishmentRequired: %#v", res)
	}

	eng.ServerName = "serverA"
	res = eng.Execute(commandreq.Must("searchDelete", "Movies", "byStatus", map[string]any{}))
	if res["ok"] != true {
		t.Fatalf("searchDelete failed: %#v", res)
	}
}

func TestCollectionEnsureUpgradeSetsNewVerID(t *testing.T) {
	eng := testEngine(t)
	upgrade := map[string]any{
		"from":       0,
		"new_ver_id": "01KWHM7R7D3T50G0GH6XN4CRZT",
		"updates": []any{
			map[string]any{
				"op": "add", "path": "/rating", "value": 0,
				"schema": map[string]any{"kind": "number", "default": 0},
			},
		},
	}
	res := eng.Execute(commandreq.Must("collectionEnsure", "Movies", "", map[string]any{"upgrade": upgrade}))
	if res["ok"] != true {
		t.Fatalf("upgrade failed: %#v", res)
	}
	if res["upgraded"] != true || res["schemaVersion"] != 1 {
		t.Fatalf("unexpected upgrade result: %#v", res)
	}
	if res["newVerId"] != "01KWHM7R7D3T50G0GH6XN4CRZT" {
		t.Fatalf("expected newVerId, got %#v", res)
	}
}
