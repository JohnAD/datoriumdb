package change

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/docjson"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/search"
)

const moviesSchema = `{
  "kind": "object",
  "children": [
    {"name": "title", "kind": "string", "required": true},
    {"name": "status", "kind": "string"}
  ]
}`

const reviewsSchema = `{
  "kind": "object",
  "children": [
    {"name": "movieSummary", "kind": "string", "format": "DatoriumCachedRef",
      "custom": {"collections": ["Movies"], "summary": ["title"]}}
  ]
}`

const byStatusSearch = `{
  "$": "SearchDefinition:v1",
  "collection": "Movies",
  "name": "byStatus",
  "version": 1,
  "v1": {
    "clauses": [{"field": "/status", "op": "equals", "value": "$status"}],
    "sort": [{"field": "/!", "dir": "asc"}]
  }
}`

func testConfig(withCache bool) *config.Config {
	cfg := &config.Config{
		General: config.General{General: config.GeneralBody{Version: 1}},
		Servers: config.ServersFile{Servers: map[string]config.ServerEntry{
			"serverA": {BaseURL: "http://serverA.local"},
		}},
		ShardMap: config.ShardMapFile{ShardMap: config.ShardMapBody{Default: map[string]config.ShardAssignment{
			"00-FF": {
				ShardSOTMember:  "serverA",
				ShardReadMember: []string{"serverA"},
			},
		}}},
		Schemas: map[string]json.RawMessage{
			"Movies": json.RawMessage(moviesSchema),
		},
		Searches: map[string]map[string]json.RawMessage{
			"Movies": {"byStatus": json.RawMessage(byStatusSearch)},
		},
	}
	if withCache {
		cfg.Schemas["Reviews"] = json.RawMessage(reviewsSchema)
	}
	return cfg
}

func newTestAgent(t *testing.T, dataDir string, cfg *config.Config) *Agent {
	t.Helper()
	return &Agent{
		DataDir:    dataDir,
		ServerName: "serverA",
		Cfg:        func() *config.Config { return cfg },
		Router:     &LocalApplier{DataDir: dataDir},
	}
}

func writeMovie(t *testing.T, dataDir, id, status string) {
	t.Helper()
	doc := map[string]any{"!": id, "$": "Movies:0", "#": "v1", "title": "T", "status": status}
	raw, encErr := docjson.EncodeMap(doc)
	if encErr != nil {
		t.Fatal(encErr)
	}
	if err := fsstore.WriteDocumentJSON(fsstore.DocumentPath(dataDir, "Movies", id), raw); err != nil {
		t.Fatalf("write document: %v", err)
	}
}

func readMatches(t *testing.T, dataDir, collection, searchName string, segments []string) *search.ResultFile {
	t.Helper()
	path := fsstore.SearchResultPath(dataDir, collection, searchName, segments)
	rf, existed, err := search.LoadResultFile(path)
	if err != nil {
		t.Fatalf("load result file: %v", err)
	}
	if !existed {
		t.Fatalf("expected result file to exist at %s", path)
	}
	return rf
}

func bucketHasID(rf *search.ResultFile, id string) bool {
	for _, it := range rf.Items {
		if it.ID == id {
			return true
		}
	}
	return false
}

func TestChangeAgentQueueClaimingAndSearchCreate(t *testing.T) {
	dataDir := t.TempDir()
	cfg := testConfig(false)
	agent := newTestAgent(t, dataDir, cfg)

	writeMovie(t, dataDir, "id1", "released")
	if err := fsstore.EnqueueChange(dataDir, "Movies", "id1", "create"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	did, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !did {
		t.Fatalf("expected RunOnce to report it found work")
	}

	// The .queue and .taken markers must both be gone once processing
	// succeeds.
	entries, err := fsstore.ListQueueEntries(dataDir, "Movies")
	if err != nil {
		t.Fatalf("list queue entries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected the change queue to be empty after successful processing, got %v", entries)
	}

	segs := []string{search.EncodeStringValue("released")}
	rf := readMatches(t, dataDir, "Movies", "byStatus", segs)
	if !bucketHasID(rf, "id1") {
		t.Fatalf("expected id1 in the released bucket, got items %v", rf.Items)
	}

	// RunOnce with nothing queued should report no work.
	did2, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if did2 {
		t.Fatalf("expected no work when the queue is empty")
	}
}

func TestChangeAgentPatchMovesBucket(t *testing.T) {
	dataDir := t.TempDir()
	cfg := testConfig(false)
	agent := newTestAgent(t, dataDir, cfg)

	// Seed as if id1 was already distributed into the "draft" bucket.
	writeMovie(t, dataDir, "id1", "draft")
	if err := fsstore.EnqueueChange(dataDir, "Movies", "id1", "create"); err != nil {
		t.Fatalf("enqueue create: %v", err)
	}
	if _, err := agent.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce (create): %v", err)
	}
	draftSegs := []string{search.EncodeStringValue("draft")}
	if rf := readMatches(t, dataDir, "Movies", "byStatus", draftSegs); !bucketHasID(rf, "id1") {
		t.Fatalf("expected id1 in the draft bucket after create")
	}

	// Now simulate a patch: preserve the previous dotfile with the old
	// value, update the live document, and enqueue a patch entry.
	if err := fsstore.PreservePreviousIfAbsent(dataDir, "Movies", "id1"); err != nil {
		t.Fatalf("preserve previous: %v", err)
	}
	writeMovie(t, dataDir, "id1", "released")
	if err := fsstore.EnqueueChange(dataDir, "Movies", "id1", "patch"); err != nil {
		t.Fatalf("enqueue patch: %v", err)
	}
	if _, err := agent.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce (patch): %v", err)
	}

	if rf := readMatches(t, dataDir, "Movies", "byStatus", draftSegs); bucketHasID(rf, "id1") {
		t.Fatalf("expected id1 to be removed from the draft bucket after patch")
	}
	releasedSegs := []string{search.EncodeStringValue("released")}
	if rf := readMatches(t, dataDir, "Movies", "byStatus", releasedSegs); !bucketHasID(rf, "id1") {
		t.Fatalf("expected id1 in the released bucket after patch")
	}

	// The previous-document dotfile must be cleaned up.
	if _, err := os.Stat(fsstore.PreviousDocumentPath(dataDir, "Movies", "id1")); !os.IsNotExist(err) {
		t.Fatalf("expected the previous-document dotfile to be removed, stat err=%v", err)
	}
}

func TestChangeAgentDeleteRemovesFromSearch(t *testing.T) {
	dataDir := t.TempDir()
	cfg := testConfig(false)
	agent := newTestAgent(t, dataDir, cfg)

	writeMovie(t, dataDir, "id1", "released")
	if err := fsstore.EnqueueChange(dataDir, "Movies", "id1", "create"); err != nil {
		t.Fatalf("enqueue create: %v", err)
	}
	if _, err := agent.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce (create): %v", err)
	}

	// Soft-delete: move live doc to the dotfile path, then enqueue delete.
	if err := fsstore.SoftDeleteDocument(dataDir, "Movies", "id1"); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if err := fsstore.EnqueueChange(dataDir, "Movies", "id1", "delete"); err != nil {
		t.Fatalf("enqueue delete: %v", err)
	}
	if _, err := agent.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce (delete): %v", err)
	}

	segs := []string{search.EncodeStringValue("released")}
	if rf := readMatches(t, dataDir, "Movies", "byStatus", segs); bucketHasID(rf, "id1") {
		t.Fatalf("expected id1 to be removed from the released bucket after delete")
	}
}

func TestChangeAgentCrashRecoveryResumesTakenEntry(t *testing.T) {
	dataDir := t.TempDir()
	cfg := testConfig(false)
	agent := newTestAgent(t, dataDir, cfg)

	writeMovie(t, dataDir, "id1", "released")
	// Simulate a worker that crashed after claiming (.queue -> .taken)
	// but before completing distribution/cleanup: write the .taken
	// marker directly instead of a .queue file.
	takenPath := fsstore.TakenQueuePath(dataDir, "Movies", "create", "id1")
	if err := fsstore.WriteFileAtomic(takenPath, []byte(""), 0o644); err != nil {
		t.Fatalf("seed taken marker: %v", err)
	}

	did, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !did {
		t.Fatalf("expected RunOnce to resume the orphaned .taken entry")
	}
	if _, err := os.Stat(takenPath); !os.IsNotExist(err) {
		t.Fatalf("expected the .taken marker to be cleaned up after resuming, stat err=%v", err)
	}
	segs := []string{search.EncodeStringValue("released")}
	if rf := readMatches(t, dataDir, "Movies", "byStatus", segs); !bucketHasID(rf, "id1") {
		t.Fatalf("expected the resumed entry to have completed search distribution")
	}
}

func TestChangeAgentCacheDistributionFanOut(t *testing.T) {
	dataDir := t.TempDir()
	cfg := testConfig(true) // includes a Reviews schema with a DatoriumCachedRef into Movies
	agent := newTestAgent(t, dataDir, cfg)

	writeMovie(t, dataDir, "id1", "released")
	if err := fsstore.EnqueueChange(dataDir, "Movies", "id1", "create"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := agent.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	path := fsstore.PendingCacheUpdatePath(dataDir, "Movies", "serverA", "id1")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected a pending cache update work item at %s: %v", path, err)
	}
	var item struct {
		SourceCollection string         `json:"sourceCollection"`
		SourceDocumentID string         `json:"sourceDocumentId"`
		Command          string         `json:"command"`
		Payload          map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(data, &item); err != nil {
		t.Fatalf("decode pending cache update: %v", err)
	}
	if item.SourceCollection != "Movies" || item.SourceDocumentID != "id1" || item.Command != "create" {
		t.Fatalf("unexpected pending cache update item: %+v", item)
	}
	if item.Payload["title"] != "T" {
		t.Fatalf("expected the full source document as payload, got %+v", item.Payload)
	}
}

func TestChangeAgentCacheDistributionSkippedWithoutReferencingSchema(t *testing.T) {
	dataDir := t.TempDir()
	cfg := testConfig(false) // no Reviews schema referencing Movies
	agent := newTestAgent(t, dataDir, cfg)

	writeMovie(t, dataDir, "id1", "released")
	if err := fsstore.EnqueueChange(dataDir, "Movies", "id1", "create"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := agent.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	dir := fsstore.PendingCacheUpdatesDir(dataDir, "Movies")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected no pending cache updates directory when no schema references the collection")
	}
}

func TestChangeAgentExclusionPreventsOverlap(t *testing.T) {
	dataDir := t.TempDir()
	cfg := testConfig(false)
	agent := newTestAgent(t, dataDir, cfg)
	locked := &fakeExcluder{held: map[string]bool{"Movies/id1": true}}
	agent.Exclusion = locked

	writeMovie(t, dataDir, "id1", "released")
	if err := fsstore.EnqueueChange(dataDir, "Movies", "id1", "create"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	did, err := agent.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if did {
		t.Fatalf("expected RunOnce to skip work already excluded by another worker")
	}
}

type fakeExcluder struct {
	held map[string]bool
}

func (f *fakeExcluder) TryAcquire(key string) bool {
	if f.held[key] {
		return false
	}
	f.held[key] = true
	return true
}

func (f *fakeExcluder) Release(key string) {
	delete(f.held, key)
}
