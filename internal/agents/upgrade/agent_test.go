package upgrade

import (
	"context"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

func newTestAgent(dir string, cfg *config.Config) *Agent {
	return &Agent{DataDir: dir, Cfg: func() *config.Config { return cfg }}
}

func seedMovie(t *testing.T, dir, id, version string) {
	t.Helper()
	doc := map[string]any{"!": id, "$": "Movies:" + version, "#": "v1", "title": "T"}
	if err := fsstore.WriteDocumentJSON(fsstore.DocumentPath(dir, "Movies", id), doc); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeAgentMigratesStaleDocuments(t *testing.T) {
	dir := t.TempDir()
	seedMovie(t, dir, "id1", "0")
	seedMovie(t, dir, "id2", "1") // already current
	agent := newTestAgent(dir, testUpgradeConfig())

	did1, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !did1 {
		t.Fatalf("expected RunOnce to migrate id1")
	}
	got, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(dir, "Movies", "id1"))
	if err != nil {
		t.Fatalf("read migrated doc: %v", err)
	}
	if got["$"] != "Movies:1" {
		t.Fatalf("expected id1 migrated to Movies:1, got %v", got["$"])
	}

	// id2 was already current, so a second RunOnce finds nothing left.
	did2, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce (second): %v", err)
	}
	if did2 {
		t.Fatalf("expected no more stale documents after the first migration")
	}
}

func TestUpgradeAgentSkipsCollectionsNeverUpgraded(t *testing.T) {
	dir := t.TempDir()
	seedMovie(t, dir, "id1", "0")
	// SchemaVersions has no entry for Movies (defaults to 0): nothing has
	// ever been upgraded, so there is nothing to migrate.
	cfg := &config.Config{SchemaVersions: map[string]int{}}
	agent := newTestAgent(dir, cfg)
	did, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if did {
		t.Fatalf("expected no work when the collection has never been upgraded")
	}
}

type fakeUpgradeExcluder struct {
	held map[string]bool
}

func (f *fakeUpgradeExcluder) TryAcquire(key string) bool {
	if f.held[key] {
		return false
	}
	f.held[key] = true
	return true
}

func (f *fakeUpgradeExcluder) Release(key string) { delete(f.held, key) }

func TestUpgradeAgentExclusionSkipsHeldDocument(t *testing.T) {
	dir := t.TempDir()
	seedMovie(t, dir, "id1", "0")
	agent := newTestAgent(dir, testUpgradeConfig())
	agent.Exclusion = &fakeUpgradeExcluder{held: map[string]bool{"Movies/id1": true}}

	did, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if did {
		t.Fatalf("expected RunOnce to skip a document held by another worker")
	}
}

// TestUpgradeAgentIdempotentAcrossRepeatedRuns simulates re-running the
// upgrade-agent as if a previous run had crashed right after committing
// the migrated document (a benign race, since ApplyToStoredDocument's
// write is a single atomic operation): re-scanning must find the document
// already current and do nothing, never re-migrating or double-patching.
func TestUpgradeAgentIdempotentAcrossRepeatedRuns(t *testing.T) {
	dir := t.TempDir()
	seedMovie(t, dir, "id1", "0")
	agent := newTestAgent(dir, testUpgradeConfig())

	for i := 0; i < 3; i++ {
		if _, err := agent.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce iteration %d: %v", i, err)
		}
	}
	entries, err := fsstore.ListQueueEntries(dir, "Movies")
	if err != nil {
		t.Fatalf("list queue: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one queued patch despite repeated RunOnce calls, got %v", entries)
	}
}
