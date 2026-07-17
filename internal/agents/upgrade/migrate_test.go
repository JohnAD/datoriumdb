package upgrade

import (
	"encoding/json"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/config"
)

const upgradeTestULID1 = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
const upgradeTestULID2 = "01ARZ3NDEKTSV4RRFFQ69G5FAW"

func updateSpecJSON(ulid, fieldName, value string) json.RawMessage {
	return json.RawMessage(`{"from":0,"to":1,"new_ver_id":"` + ulid + `","updates":[
		{"op":"add","path":"/` + fieldName + `","value":"` + value + `","schema":{"kind":"string"}}
	]}`)
}

func TestDocVersion(t *testing.T) {
	v, ok := DocVersion(map[string]any{"$": "Movies:3"})
	if !ok || v != 3 {
		t.Fatalf("expected version 3, got %d ok=%v", v, ok)
	}
	if _, ok := DocVersion(map[string]any{"$": "Movies"}); ok {
		t.Fatalf("expected no version parsed from a marker with no colon")
	}
	if _, ok := DocVersion(map[string]any{}); ok {
		t.Fatalf("expected no version parsed from a document with no $ marker")
	}
	if _, ok := DocVersion(map[string]any{"$": "Movies:abc"}); ok {
		t.Fatalf("expected no version parsed from a non-numeric suffix")
	}
}

func TestNeedsMigration(t *testing.T) {
	cfg := &config.Config{SchemaVersions: map[string]int{"Movies": 2}}
	if !NeedsMigration(cfg, "Movies", map[string]any{"$": "Movies:0"}) {
		t.Fatalf("expected a version-0 document to need migration to version 2")
	}
	if NeedsMigration(cfg, "Movies", map[string]any{"$": "Movies:2"}) {
		t.Fatalf("expected a current document to not need migration")
	}
	if NeedsMigration(nil, "Movies", map[string]any{"$": "Movies:0"}) {
		t.Fatalf("expected a nil config to report no migration needed")
	}
	if NeedsMigration(cfg, "Movies", nil) {
		t.Fatalf("expected a nil (deleted) document to report no migration needed")
	}
}

func TestMigrateDocumentSingleStep(t *testing.T) {
	cfg := &config.Config{
		SchemaVersions: map[string]int{"Movies": 1},
		SchemaUpdateHistory: map[string]map[int]json.RawMessage{
			"Movies": {1: updateSpecJSON(upgradeTestULID1, "genre", "scifi")},
		},
	}
	doc := map[string]any{"!": "id1", "$": "Movies:0", "#": "v1", "title": "T"}
	changed, err := MigrateDocument(cfg, "Movies", doc, 1)
	if err != nil {
		t.Fatalf("MigrateDocument: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if doc["genre"] != "scifi" {
		t.Fatalf("expected genre added, got %+v", doc)
	}
	if doc["$"] != "Movies:1" {
		t.Fatalf("expected $ marker advanced to Movies:1, got %v", doc["$"])
	}
	// "!" and "#" must be left untouched.
	if doc["!"] != "id1" || doc["#"] != "v1" {
		t.Fatalf("expected !/# untouched, got %+v", doc)
	}
}

func TestMigrateDocumentMultiStepChain(t *testing.T) {
	cfg := &config.Config{
		SchemaUpdateHistory: map[string]map[int]json.RawMessage{
			"Movies": {
				1: updateSpecJSON(upgradeTestULID1, "genre", "scifi"),
				2: updateSpecJSON(upgradeTestULID2, "rating", "PG"),
			},
		},
	}
	doc := map[string]any{"$": "Movies:0"}
	changed, err := MigrateDocument(cfg, "Movies", doc, 2)
	if err != nil {
		t.Fatalf("MigrateDocument: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if doc["genre"] != "scifi" || doc["rating"] != "PG" {
		t.Fatalf("expected both migration steps applied in order, got %+v", doc)
	}
	if doc["$"] != "Movies:2" {
		t.Fatalf("expected $ marker at Movies:2, got %v", doc["$"])
	}
}

func TestMigrateDocumentAlreadyCurrentIsNoop(t *testing.T) {
	cfg := &config.Config{SchemaUpdateHistory: map[string]map[int]json.RawMessage{}}
	doc := map[string]any{"$": "Movies:2"}
	changed, err := MigrateDocument(cfg, "Movies", doc, 2)
	if err != nil {
		t.Fatalf("MigrateDocument: %v", err)
	}
	if changed {
		t.Fatalf("expected no-op for an already-current document")
	}
}

func TestMigrateDocumentMissingHistoryErrors(t *testing.T) {
	cfg := &config.Config{SchemaUpdateHistory: map[string]map[int]json.RawMessage{"Movies": {}}}
	doc := map[string]any{"$": "Movies:0"}
	if _, err := MigrateDocument(cfg, "Movies", doc, 1); err == nil {
		t.Fatalf("expected an error when the persisted update list for a required step is missing")
	}
}

func TestMigrateDocumentNoVersionMarkerErrors(t *testing.T) {
	cfg := &config.Config{}
	doc := map[string]any{}
	if _, err := MigrateDocument(cfg, "Movies", doc, 1); err == nil {
		t.Fatalf("expected an error for a document with no valid $ marker")
	}
}
