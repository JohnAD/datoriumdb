package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

const engineMoviesSchemaV1 = `{
  "kind": "object",
  "children": [
    {"name": "title", "kind": "string", "required": true},
    {"name": "releaseYear", "kind": "number", "integer": true},
    {"name": "status", "kind": "string"},
    {"name": "highRated", "kind": "boolean", "default": false},
    {"name": "genre", "kind": "string", "required": true, "default": "unknown"}
  ]
}`

const engineMoviesUpdateSpecV1 = `{"from":0,"to":1,"new_ver_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","updates":[
  {"op":"add","path":"/genre","schema":{"kind":"string","required":true,"default":"unknown"}}
]}`

// testEngineWithSchemaUpgrade builds a standard test engine (Movies at
// version 0) plus a persisted version-1 schema/update-list pair, so
// on-access migration has somewhere to advance a stale document to.
func testEngineWithSchemaUpgrade(t *testing.T) *Engine {
	t.Helper()
	eng := testEngine(t)
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "Movies.schema.json"), []byte(engineMoviesSchemaV1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "Movies.schema.1.json"), []byte(engineMoviesSchemaV1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "Movies.schema.1.update.json"), []byte(engineMoviesUpdateSpecV1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	return eng
}

func TestReadMigratesStaleDocumentOnAccess(t *testing.T) {
	eng := testEngineWithSchemaUpgrade(t)
	doc := map[string]any{"!": "id1", "$": "Movies:0", "#": "v1", "title": "T"}
	if err := fsstore.WriteDocumentJSON(fsstore.DocumentPath(eng.DataDir, "Movies", "id1"), doc); err != nil {
		t.Fatal(err)
	}

	read := eng.Execute(`read Movies id1 {}`)
	if read["ok"] != true {
		t.Fatalf("read failed: %#v", read)
	}
	sot, ok := read["sot"].(map[string]any)
	if !ok {
		t.Fatalf("expected sot map, got %#v", read["sot"])
	}
	if sot["genre"] != "unknown" {
		t.Fatalf("expected genre filled in by migration, got %#v", sot)
	}

	// The migration must have been durably persisted, not just returned
	// in-memory for this one read.
	onDisk, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(eng.DataDir, "Movies", "id1"))
	if err != nil {
		t.Fatal(err)
	}
	if onDisk["$"] != "Movies:1" || onDisk["genre"] != "unknown" {
		t.Fatalf("expected migration persisted to disk, got %#v", onDisk)
	}
	if onDisk["#"] == "v1" {
		t.Fatalf("expected a fresh version id after migration, still v1")
	}

	// A repeat read must be a no-op (idempotent, no double-migration).
	read2 := eng.Execute(`read Movies id1 {}`)
	if read2["ok"] != true {
		t.Fatalf("second read failed: %#v", read2)
	}
	onDisk2, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(eng.DataDir, "Movies", "id1"))
	if err != nil {
		t.Fatal(err)
	}
	if onDisk2["#"] != onDisk["#"] {
		t.Fatalf("expected no further version bump on a repeat read of an already-current document")
	}
}

func TestReadDoesNotMigrateAlreadyCurrentDocument(t *testing.T) {
	eng := testEngineWithSchemaUpgrade(t)
	doc := map[string]any{"!": "id1", "$": "Movies:1", "#": "v1", "title": "T", "genre": "scifi"}
	if err := fsstore.WriteDocumentJSON(fsstore.DocumentPath(eng.DataDir, "Movies", "id1"), doc); err != nil {
		t.Fatal(err)
	}
	read := eng.Execute(`read Movies id1 {}`)
	if read["ok"] != true {
		t.Fatalf("read failed: %#v", read)
	}
	onDisk, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(eng.DataDir, "Movies", "id1"))
	if err != nil {
		t.Fatal(err)
	}
	if onDisk["#"] != "v1" {
		t.Fatalf("expected no migration/version bump for an already-current document, got %#v", onDisk)
	}
}
