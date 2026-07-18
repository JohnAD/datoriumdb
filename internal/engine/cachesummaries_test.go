package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/agents/cache"
)

const engineReviewsSchema = `{
  "kind": "object",
  "children": [
    {"name": "movieRef", "kind": "string", "format": "DatoriumCachedRef",
      "custom": {"collections": ["Movies"], "summary": ["title"]}},
    {"name": "text", "kind": "string"}
  ]
}`

func testEngineWithReviews(t *testing.T) *Engine {
	t.Helper()
	eng := testEngine(t)
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "Reviews.schema.json"), []byte(engineReviewsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "Reviews.schema.0.json"), []byte(engineReviewsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	return eng
}

func TestReadCacheSummariesResolvesExistingCache(t *testing.T) {
	eng := testEngineWithReviews(t)
	if err := cache.WriteWorkItem(eng.DataDir, eng.ServerName, cache.WorkItem{
		SourceCollection: "Movies", SourceDocumentID: "movie1", Command: "create",
		AfterVersion: "v1", Payload: map[string]any{"!": "movie1", "#": "v1", "title": "The Matrix"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.EnsureStub(eng.DataDir, "Movies", "movie1"); err != nil {
		t.Fatal(err)
	}
	item, err := cache.ReadWorkItem(eng.DataDir, "Movies", eng.ServerName, "movie1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Apply(eng.DataDir, *item); err != nil {
		t.Fatal(err)
	}

	created := eng.Execute(`create Reviews null {$: Reviews:0, movieRef: "@@__Movies__movie1", text: "great!"}`)
	if created["ok"] != true {
		t.Fatalf("create failed: %#v", created)
	}
	id, _ := created["id"].(string)

	read := eng.Execute(`read Reviews ` + id + ` {cacheSummaries: true}`)
	if read["ok"] != true {
		t.Fatalf("read failed: %#v", read)
	}
	summaries, ok := read["cacheSummaries"].(map[string]any)
	if !ok {
		t.Fatalf("expected cacheSummaries map, got %#v", read["cacheSummaries"])
	}
	movies, ok := summaries["Movies"].(map[string]any)
	if !ok {
		t.Fatalf("expected a Movies group, got %#v", summaries)
	}
	movie1, ok := movies["movie1"].(map[string]any)
	if !ok {
		t.Fatalf("expected a movie1 summary, got %#v", movies)
	}
	if movie1["title"] != "The Matrix" {
		t.Fatalf("expected title projected, got %#v", movie1)
	}
	if movie1["!"] != "movie1" {
		t.Fatalf("expected ! to ride along, got %#v", movie1)
	}
}

func TestReadCacheSummariesCreatesLostReferenceStub(t *testing.T) {
	eng := testEngineWithReviews(t)
	created := eng.Execute(`create Reviews null {$: Reviews:0, movieRef: "@@__Movies__unknownMovie", text: "x"}`)
	if created["ok"] != true {
		t.Fatalf("create failed: %#v", created)
	}
	id, _ := created["id"].(string)

	read := eng.Execute(`read Reviews ` + id + ` {cacheSummaries: true}`)
	if read["ok"] != true {
		t.Fatalf("read failed: %#v", read)
	}
	summaries, _ := read["cacheSummaries"].(map[string]any)
	movies, _ := summaries["Movies"].(map[string]any)
	unknown, ok := movies["unknownMovie"].(map[string]any)
	if !ok {
		t.Fatalf("expected a lost-reference stub summary, got %#v", summaries)
	}
	if unknown["#"] != nil {
		t.Fatalf("expected a nil version for a lost reference, got %#v", unknown)
	}
	if _, hasTitle := unknown["title"]; hasTitle {
		t.Fatalf("expected no title for an unresolvable reference, got %#v", unknown)
	}

	// A stub cache file must now exist so a future pending cache-update
	// work item has something to update in place.
	if _, ok, err := cache.LoadCacheFile(eng.DataDir, "Movies", "unknownMovie"); err != nil || !ok {
		t.Fatalf("expected a stub cache file to have been created: ok=%v err=%v", ok, err)
	}
}

func TestReadWithoutCacheSummariesFlagOmitsField(t *testing.T) {
	eng := testEngineWithReviews(t)
	created := eng.Execute(`create Reviews null {$: Reviews:0, movieRef: "@@__Movies__movie1", text: "x"}`)
	id, _ := created["id"].(string)
	read := eng.Execute(`read Reviews ` + id + ` {}`)
	if read["ok"] != true {
		t.Fatalf("read failed: %#v", read)
	}
	if _, ok := read["cacheSummaries"]; ok {
		t.Fatalf("expected cacheSummaries to be omitted when not requested")
	}
}

const engineNestedReviewsSchema = `{
  "kind": "object",
  "children": [
    {"name": "text", "kind": "string"},
    {"name": "meta", "kind": "object", "children": [
      {"name": "movieRef", "kind": "string", "format": "DatoriumCachedRef",
        "custom": {"collections": ["Movies"], "summary": ["title"]}}
    ]}
  ]
}`

func TestReadCacheSummariesFindsNestedCachedRefs(t *testing.T) {
	eng := testEngine(t)
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "Reviews.schema.json"), []byte(engineNestedReviewsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "Reviews.schema.0.json"), []byte(engineNestedReviewsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteWorkItem(eng.DataDir, eng.ServerName, cache.WorkItem{
		SourceCollection: "Movies", SourceDocumentID: "movieNested", Command: "create",
		AfterVersion: "v1", Payload: map[string]any{"!": "movieNested", "#": "v1", "title": "Nested"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.EnsureStub(eng.DataDir, "Movies", "movieNested"); err != nil {
		t.Fatal(err)
	}
	item, err := cache.ReadWorkItem(eng.DataDir, "Movies", eng.ServerName, "movieNested")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Apply(eng.DataDir, *item); err != nil {
		t.Fatal(err)
	}

	created := eng.Execute(`create Reviews null {$: Reviews:0, text: "x", meta: {movieRef: "@@__Movies__movieNested"}}`)
	if created["ok"] != true {
		t.Fatalf("create failed: %#v", created)
	}
	id, _ := created["id"].(string)
	read := eng.Execute(`read Reviews ` + id + ` {cacheSummaries: true}`)
	if read["ok"] != true {
		t.Fatalf("read failed: %#v", read)
	}
	summaries, _ := read["cacheSummaries"].(map[string]any)
	movies, _ := summaries["Movies"].(map[string]any)
	movie, ok := movies["movieNested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested movieRef to produce a cache summary, got %#v", summaries)
	}
	if movie["title"] != "Nested" {
		t.Fatalf("expected nested ref title, got %#v", movie)
	}
}
