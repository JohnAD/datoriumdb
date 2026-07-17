package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/search"
)

const engineByStatusSearch = `{
  "$": "SearchDefinition:v1",
  "collection": "Movies",
  "name": "byStatus",
  "version": 1,
  "v1": {
    "clauses": [{"field": "/status", "op": "equals", "value": "$status"}],
    "sort": [{"field": "/!", "dir": "asc"}]
  }
}`

// testEngineWithSearch builds a standard test engine (testdata/sample-config,
// serverA serving the whole shard range) plus one extra search definition
// registered against the sample config's existing Movies schema.
func testEngineWithSearch(t *testing.T) *Engine {
	t.Helper()
	eng := testEngine(t)
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "Movies.search.byStatus.json"), []byte(engineByStatusSearch), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	return eng
}

func seedMatches(t *testing.T, eng *Engine, status string, idsInOrder ...string) {
	t.Helper()
	def, err := search.ParseDefinition([]byte(engineByStatusSearch))
	if err != nil {
		t.Fatal(err)
	}
	segs := []string{search.EncodeStringValue(status)}
	path := fsstore.SearchResultPath(eng.DataDir, "Movies", "byStatus", segs)
	n := 0
	next := func() (string, error) {
		n++
		return "VER" + string(rune('0'+n)), nil
	}
	_, _, err = search.ApplyMutation(path, next, func(rf *search.ResultFile, existed bool) (bool, error) {
		rf.Search = "byStatus"
		rf.Collection = "Movies"
		changed := false
		for _, id := range idsInOrder {
			sortVals := search.ComputeSortValues(def, map[string]any{"!": id})
			if rf.Upsert(def, id, sortVals) {
				changed = true
			}
		}
		return changed, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchCommandReturnsIDsInStoredOrder(t *testing.T) {
	eng := testEngineWithSearch(t)
	seedMatches(t, eng, "released", "id1", "id2")

	res := eng.Execute(`search Movies byStatus {status: released}`)
	if res["ok"] != true {
		t.Fatalf("search failed: %#v", res)
	}
	ids, ok := res["matches"].([]string)
	if !ok || len(ids) != 2 || ids[0] != "id1" || ids[1] != "id2" {
		t.Fatalf("unexpected matches: %#v", res["matches"])
	}
}

func TestSearchCommandEmptyBucketReturnsNoIDs(t *testing.T) {
	eng := testEngineWithSearch(t)
	res := eng.Execute(`search Movies byStatus {status: neverUsedStatus}`)
	if res["ok"] != true {
		t.Fatalf("search failed: %#v", res)
	}
	ids, ok := res["matches"].([]string)
	if !ok || len(ids) != 0 {
		t.Fatalf("expected zero matches for a bucket with no matches.json, got %#v", res["matches"])
	}
}

func TestSearchCommandUnknownCollection(t *testing.T) {
	eng := testEngineWithSearch(t)
	res := eng.Execute(`search NoSuchCollection byStatus {status: released}`)
	if res["ok"] != false {
		t.Fatalf("expected failure: %#v", res)
	}
	if code := firstErrorCode(res); code != "collectionNotFound" {
		t.Fatalf("expected collectionNotFound, got %q (%#v)", code, res)
	}
}

func TestSearchCommandUnknownSearchName(t *testing.T) {
	eng := testEngineWithSearch(t)
	res := eng.Execute(`search Movies noSuchSearch {status: released}`)
	if res["ok"] != false {
		t.Fatalf("expected failure: %#v", res)
	}
	if code := firstErrorCode(res); code != "searchNotFound" {
		t.Fatalf("expected searchNotFound, got %q (%#v)", code, res)
	}
}

func TestSearchCommandInvalidQueryDetail(t *testing.T) {
	eng := testEngineWithSearch(t)
	// The equals clause is bound to $status; omitting it must fail.
	res := eng.Execute(`search Movies byStatus {}`)
	if res["ok"] != false {
		t.Fatalf("expected failure for a query missing the bound variable: %#v", res)
	}
	if code := firstErrorCode(res); code != "invalidRequest" {
		t.Fatalf("expected invalidRequest, got %q (%#v)", code, res)
	}
}

// TestSearchCommandRefusesWrongShard exercises checkSearchRouting's
// wrongMachine path by pointing serverA's shard map at a different SOT
// server for the whole range.
func TestSearchCommandRefusesWrongShard(t *testing.T) {
	eng := testEngineWithSearch(t)
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "__shard-map.json"), []byte(`{
		"shardMap": {"default": {"00-FF": {
			"SHARD_SOT_MEMBER": "serverB",
			"SHARD_READ_MEMBER": [],
			"PROXY_READ_MEMBER": []
		}}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "__servers.json"), []byte(`{
		"servers": {"serverA": {"baseURL": "http://a"}, "serverB": {"baseURL": "http://b"}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	res := eng.Execute(`search Movies byStatus {status: released}`)
	if res["ok"] != false {
		t.Fatalf("expected failure: %#v", res)
	}
	if code := firstErrorCode(res); code != "wrongMachine" {
		t.Fatalf("expected wrongMachine, got %q (%#v)", code, res)
	}
	if res["correctServer"] != "serverB" {
		t.Fatalf("expected correctServer=serverB, got %#v", res["correctServer"])
	}
}
