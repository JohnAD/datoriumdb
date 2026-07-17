package upgrade

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

type fakeIDGen struct{ n int }

func (f *fakeIDGen) New() (string, error) {
	f.n++
	return "VER" + string(rune('0'+f.n)), nil
}

func testUpgradeConfig() *config.Config {
	return &config.Config{
		SchemaVersions: map[string]int{"Movies": 1},
		SchemaUpdateHistory: map[string]map[int]json.RawMessage{
			"Movies": {1: updateSpecJSON(upgradeTestULID1, "genre", "scifi")},
		},
	}
}

func TestApplyToStoredDocumentMigratesAndEnqueues(t *testing.T) {
	dir := t.TempDir()
	doc := map[string]any{"!": "id1", "$": "Movies:0", "#": "v1", "title": "T"}
	if err := fsstore.WriteDocumentJSON(fsstore.DocumentPath(dir, "Movies", "id1"), doc); err != nil {
		t.Fatalf("seed document: %v", err)
	}
	ids := &fakeIDGen{}
	changed, err := ApplyToStoredDocument(dir, testUpgradeConfig(), "Movies", "id1", ids)
	if err != nil {
		t.Fatalf("ApplyToStoredDocument: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	got, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(dir, "Movies", "id1"))
	if err != nil {
		t.Fatalf("read migrated document: %v", err)
	}
	if got["$"] != "Movies:1" || got["genre"] != "scifi" {
		t.Fatalf("unexpected migrated document: %+v", got)
	}
	if got["#"] != "VER1" {
		t.Fatalf("expected a fresh version id, got %v", got["#"])
	}

	// A previous-document dotfile must be preserved for change-agent net
	// diffing, and a patch must be enqueued so search/cache pick this up.
	if _, err := os.Stat(fsstore.PreviousDocumentPath(dir, "Movies", "id1")); err != nil {
		t.Fatalf("expected previous-document dotfile preserved: %v", err)
	}
	entries, err := fsstore.ListQueueEntries(dir, "Movies")
	if err != nil {
		t.Fatalf("list queue: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one queued patch entry, got %v", entries)
	}
}

func TestApplyToStoredDocumentNoopWhenCurrent(t *testing.T) {
	dir := t.TempDir()
	doc := map[string]any{"!": "id1", "$": "Movies:1", "#": "v1", "title": "T"}
	if err := fsstore.WriteDocumentJSON(fsstore.DocumentPath(dir, "Movies", "id1"), doc); err != nil {
		t.Fatalf("seed document: %v", err)
	}
	changed, err := ApplyToStoredDocument(dir, testUpgradeConfig(), "Movies", "id1", &fakeIDGen{})
	if err != nil {
		t.Fatalf("ApplyToStoredDocument: %v", err)
	}
	if changed {
		t.Fatalf("expected no-op for an already-current document")
	}
}

func TestApplyToStoredDocumentNoopWhenDeleted(t *testing.T) {
	dir := t.TempDir()
	changed, err := ApplyToStoredDocument(dir, testUpgradeConfig(), "Movies", "does-not-exist", &fakeIDGen{})
	if err != nil {
		t.Fatalf("ApplyToStoredDocument: %v", err)
	}
	if changed {
		t.Fatalf("expected no-op for a document that no longer exists")
	}
}
