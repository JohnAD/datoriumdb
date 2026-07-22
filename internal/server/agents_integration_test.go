package server

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/agents/cache"
	"github.com/JohnAD/datoriumdb/internal/agents/change"
	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/engine"
	"github.com/JohnAD/datoriumdb/internal/docjson"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/replication"
	"github.com/JohnAD/datoriumdb/internal/search"
)

const integrationMoviesSchema = `{
  "kind": "object",
  "children": [
    {"name": "title", "kind": "string", "required": true},
    {"name": "status", "kind": "string"}
  ]
}`

const integrationReviewsSchema = `{
  "kind": "object",
  "children": [
    {"name": "movieRef", "kind": "string", "format": "DatoriumCachedRef",
      "custom": {"collections": ["Movies"], "summary": ["title"]}}
  ]
}`

const integrationByStatusSearch = `{
  "$": "SearchDefinition:v1",
  "collection": "Movies",
  "name": "byStatus",
  "version": 1,
  "v1": {
    "clauses": [{"field": "/status", "op": "equals", "value": "$status"}],
    "sort": [{"field": "/!", "dir": "asc"}]
  }
}`

// twoServerHarness sets up serverA as the real HTTP+engine SOT-member
// (shard SOT for the whole keyspace, over an httptest.Server), plus a
// second server name "serverB" configured as that shard's read member.
// It returns serverA's engine/data dir plus a config accessor usable from
// either side (AllSOTMembers/ServerBaseURL only depend on shard map +
// servers, which are shared here for test simplicity).
func twoServerHarness(t *testing.T) (eng *engine.Engine, ts *httptest.Server, issuer *auth.Issuer, cfgFn func() *config.Config) {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, ".config")
	if err := copyDir(t, "../../testdata/sample-config", configDir); err != nil {
		t.Fatal(err)
	}
	// Overwrite the sample shard map/schemas with this test's two-server,
	// cache+search-ready fixtures.
	writeFile(t, filepath.Join(configDir, "__shard-map.json"), `{
		"shardMap": {"default": {"00-FF": {
			"SHARD_SOT_MEMBER": "serverA",
			"SHARD_READ_MEMBER": ["serverB"],
			"PROXY_READ_MEMBER": []
		}}}
	}`)
	writeFile(t, filepath.Join(configDir, "__servers.json"), `{
		"servers": {
			"serverA": {"baseURL": "http://127.0.0.1:8080"},
			"serverB": {"baseURL": "http://127.0.0.1:8081"}
		}
	}`)
	writeFile(t, filepath.Join(configDir, "Movies.schema.json"), integrationMoviesSchema)
	writeFile(t, filepath.Join(configDir, "Movies.schema.0.json"), integrationMoviesSchema)
	writeFile(t, filepath.Join(configDir, "Reviews.schema.json"), integrationReviewsSchema)
	writeFile(t, filepath.Join(configDir, "Reviews.schema.0.json"), integrationReviewsSchema)
	writeFile(t, filepath.Join(configDir, "Movies.search.byStatus.json"), integrationByStatusSearch)

	eng = &engine.Engine{ConfigDir: configDir, DataDir: root, ServerName: "serverA"}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	issuer, err := auth.NewIssuerFromFile(eng.Cfg.Auth, filepath.Join(configDir, "dev-signing-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &HTTPServer{Engine: eng, Issuer: issuer, BootstrapSecret: testBootstrapSecret}
	ts = httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// Now that we know the httptest URL, make it resolvable as serverA's
	// baseURL for outbound server-to-server calls.
	eng.Cfg.Servers.Servers["serverA"] = config.ServerEntry{BaseURL: ts.URL}
	cfgFn = func() *config.Config { return eng.Cfg }
	return eng, ts, issuer, cfgFn
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCacheAgentConvergesAfterInterruption exercises the full pull-model
// path end to end: change-agent on the SOT (serverA) fans out a pending
// cache-update work item for the read member (serverB); cache.Agent
// (running as serverB against the real HTTP server) fetches, applies, and
// completes it; a second check-in is a no-op, and re-running from an
// already-applied state (simulating a crash after apply but before
// completion) safely re-converges without duplicating effects.
func TestCacheAgentConvergesAfterInterruption(t *testing.T) {
	eng, ts, issuer, cfgFn := twoServerHarness(t)
	ctx := context.Background()

	// Seed and distribute a Movies document on the SOT.
	writeMovieDoc(t, eng.DataDir, "id1", "released")
	if err := fsstore.EnqueueChange(eng.DataDir, "Movies", "id1", "create"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	changeAgent := &change.Agent{
		DataDir: eng.DataDir, ServerName: "serverA", Cfg: cfgFn,
		Router: &change.LocalApplier{DataDir: eng.DataDir},
	}
	if _, err := changeAgent.RunOnce(ctx); err != nil {
		t.Fatalf("change-agent RunOnce: %v", err)
	}
	pendingPath := fsstore.PendingCacheUpdatePath(eng.DataDir, "Movies", "serverB", "id1")
	if _, err := os.Stat(pendingPath); err != nil {
		t.Fatalf("expected a pending cache update for serverB: %v", err)
	}

	// serverB already has a stub cache file for id1 (it previously read a
	// DatoriumCachedRef pointing at it).
	serverBDataDir := t.TempDir()
	if _, err := cache.EnsureStub(serverBDataDir, "Movies", "id1"); err != nil {
		t.Fatalf("EnsureStub: %v", err)
	}

	cacheAgent := &cache.Agent{
		ServerName: "serverB",
		DataDir:    serverBDataDir,
		Cfg:        cfgFn,
		Tokens:     replication.IssuerTokenSource{Issuer: issuer, ServerName: "serverB"},
		HTTPClient: ts.Client(),
	}
	did, err := cacheAgent.RunOnce(ctx)
	if err != nil {
		t.Fatalf("cache-agent RunOnce: %v", err)
	}
	if !did {
		t.Fatalf("expected the first check-in to report work applied")
	}

	loaded, ok, err := cache.LoadCacheFile(serverBDataDir, "Movies", "id1")
	if err != nil || !ok {
		t.Fatalf("expected serverB's local cache to be populated: ok=%v err=%v", ok, err)
	}
	if loaded["title"] != "T" {
		t.Fatalf("unexpected cached content: %+v", loaded)
	}

	// The work item must be gone from the SOT after completion.
	if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
		t.Fatalf("expected the pending cache update to be completed/removed, stat err=%v", err)
	}

	// A second check-in must be a no-op (nothing left to pull).
	did2, err := cacheAgent.RunOnce(ctx)
	if err != nil {
		t.Fatalf("second cache-agent RunOnce: %v", err)
	}
	if did2 {
		t.Fatalf("expected the second check-in to find no pending work")
	}
}

// TestCacheAgentRetriesAfterCrashBeforeCompletion simulates a read member
// that applied a work item locally but crashed before telling the SOT to
// delete it: the SOT still serves the (now-redundant) work item, and a
// fresh check-in must re-apply it idempotently rather than erroring or
// duplicating any state.
func TestCacheAgentRetriesAfterCrashBeforeCompletion(t *testing.T) {
	eng, ts, issuer, cfgFn := twoServerHarness(t)
	ctx := context.Background()

	writeMovieDoc(t, eng.DataDir, "id1", "released")
	if err := fsstore.EnqueueChange(eng.DataDir, "Movies", "id1", "create"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	changeAgent := &change.Agent{
		DataDir: eng.DataDir, ServerName: "serverA", Cfg: cfgFn,
		Router: &change.LocalApplier{DataDir: eng.DataDir},
	}
	if _, err := changeAgent.RunOnce(ctx); err != nil {
		t.Fatalf("change-agent RunOnce: %v", err)
	}

	serverBDataDir := t.TempDir()
	if _, err := cache.EnsureStub(serverBDataDir, "Movies", "id1"); err != nil {
		t.Fatalf("EnsureStub: %v", err)
	}
	// Apply the effect directly (as if a first attempt had already
	// updated the local cache) without going through the agent, so the
	// SOT's work item is still outstanding when we retry via the agent.
	item, err := cache.ReadWorkItem(eng.DataDir, "Movies", "serverB", "id1")
	if err != nil {
		t.Fatalf("ReadWorkItem: %v", err)
	}
	if _, err := cache.Apply(serverBDataDir, *item); err != nil {
		t.Fatalf("pre-apply: %v", err)
	}

	cacheAgent := &cache.Agent{
		ServerName: "serverB",
		DataDir:    serverBDataDir,
		Cfg:        cfgFn,
		Tokens:     replication.IssuerTokenSource{Issuer: issuer, ServerName: "serverB"},
		HTTPClient: ts.Client(),
	}
	if _, err := cacheAgent.RunOnce(ctx); err != nil {
		t.Fatalf("cache-agent RunOnce (retry): %v", err)
	}
	loaded, ok, err := cache.LoadCacheFile(serverBDataDir, "Movies", "id1")
	if err != nil || !ok || loaded["title"] != "T" {
		t.Fatalf("unexpected cache state after retry: ok=%v err=%v loaded=%+v", ok, err, loaded)
	}
	pendingPath := fsstore.PendingCacheUpdatePath(eng.DataDir, "Movies", "serverB", "id1")
	if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
		t.Fatalf("expected the outstanding work item to be completed on retry, stat err=%v", err)
	}
}

func writeMovieDoc(t *testing.T, dataDir, id, status string) {
	t.Helper()
	doc := map[string]any{"!": id, "$": "Movies:0", "#": "v1", "title": "T", "status": status}
	raw, encErr := docjson.EncodeMap(doc)
	if encErr != nil {
		t.Fatal(encErr)
	}
	if err := fsstore.WriteDocumentJSON(fsstore.DocumentPath(dataDir, "Movies", id), raw); err != nil {
		t.Fatal(err)
	}
}

// TestSearchConvergesSingleNode is a single-node end-to-end check that a
// change-agent run leaves matches.json holding exactly the sorted IDs an
// equivalent `search` command execution should return.
func TestSearchConvergesSingleNode(t *testing.T) {
	eng, _, _, cfgFn := twoServerHarness(t)
	ctx := context.Background()
	changeAgent := &change.Agent{
		DataDir: eng.DataDir, ServerName: "serverA", Cfg: cfgFn,
		Router: &change.LocalApplier{DataDir: eng.DataDir},
	}
	for _, seed := range []struct{ id, status string }{
		{"id1", "released"}, {"id2", "released"}, {"id3", "draft"},
	} {
		writeMovieDoc(t, eng.DataDir, seed.id, seed.status)
		if err := fsstore.EnqueueChange(eng.DataDir, "Movies", seed.id, "create"); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	for {
		did, err := changeAgent.RunOnce(ctx)
		if err != nil {
			t.Fatalf("change-agent RunOnce: %v", err)
		}
		if !did {
			break
		}
	}

	segs := []string{search.EncodeStringValue("released")}
	rf, existed, err := search.LoadResultFile(fsstore.SearchResultPath(eng.DataDir, "Movies", "byStatus", segs))
	if err != nil || !existed {
		t.Fatalf("expected matches.json to exist: existed=%v err=%v", existed, err)
	}
	if len(rf.Items) != 2 {
		t.Fatalf("expected exactly id1 and id2 in the released bucket, got %+v", rf.Items)
	}
	ids := []string{rf.Items[0].ID, rf.Items[1].ID}
	if ids[0] != "id1" || ids[1] != "id2" {
		t.Fatalf("expected deterministic sort order id1,id2; got %v", ids)
	}
}

// TestSearchResultsReplicateToReadMember runs serverA (the search shard's
// SOT) and serverB (its read member) as two independent, real HTTP
// servers with their own data directories, and checks that a
// change.ShardRouter-driven create/patch/delete on serverA replicates
// each search bucket mutation to serverB's own local matches.json via
// /datoriumdb/v1/sys/apply-search-result-write, per
// SERVER-TO-SERVER-API.md's "Happy-Path Search Result Delivery".
func TestSearchResultsReplicateToReadMember(t *testing.T) {
	eng, tsA, issuer, cfgFn := twoServerHarness(t)
	ctx := context.Background()

	serverBDataDir := t.TempDir()
	engB := &engine.Engine{ConfigDir: eng.ConfigDir, DataDir: serverBDataDir, ServerName: "serverB"}
	if err := engB.Reload(); err != nil {
		t.Fatal(err)
	}
	srvB := &HTTPServer{Engine: engB, Issuer: issuer, BootstrapSecret: testBootstrapSecret}
	tsB := httptest.NewServer(srvB.Handler())
	t.Cleanup(tsB.Close)
	eng.Cfg.Servers.Servers["serverB"] = config.ServerEntry{BaseURL: tsB.URL}

	changeAgent := &change.Agent{
		DataDir: eng.DataDir, ServerName: "serverA", Cfg: cfgFn,
		Router: &change.ShardRouter{
			ServerName: "serverA",
			Cfg:        cfgFn,
			Local:      &change.LocalApplier{DataDir: eng.DataDir},
			Remote: &change.RemoteApplier{
				Cfg:        cfgFn,
				Tokens:     replication.IssuerTokenSource{Issuer: issuer, ServerName: "serverA"},
				HTTPClient: tsA.Client(),
			},
		},
	}

	writeMovieDoc(t, eng.DataDir, "id1", "released")
	if err := fsstore.EnqueueChange(eng.DataDir, "Movies", "id1", "create"); err != nil {
		t.Fatalf("enqueue create: %v", err)
	}
	if _, err := changeAgent.RunOnce(ctx); err != nil {
		t.Fatalf("change-agent RunOnce (create): %v", err)
	}

	releasedSegs := []string{search.EncodeStringValue("released")}
	remoteRF, existed, err := search.LoadResultFile(fsstore.SearchResultPath(serverBDataDir, "Movies", "byStatus", releasedSegs))
	if err != nil || !existed {
		t.Fatalf("expected the released bucket to be replicated onto serverB: existed=%v err=%v", existed, err)
	}
	if len(remoteRF.Items) != 1 || remoteRF.Items[0].ID != "id1" {
		t.Fatalf("unexpected replicated bucket contents: %+v", remoteRF.Items)
	}

	// Patch: move id1 from the released bucket to the draft bucket. Both
	// the removal and the new bucket's upsert must replicate to serverB.
	if err := fsstore.PreservePreviousIfAbsent(eng.DataDir, "Movies", "id1"); err != nil {
		t.Fatalf("preserve previous: %v", err)
	}
	writeMovieDoc(t, eng.DataDir, "id1", "draft")
	if err := fsstore.EnqueueChange(eng.DataDir, "Movies", "id1", "patch"); err != nil {
		t.Fatalf("enqueue patch: %v", err)
	}
	if _, err := changeAgent.RunOnce(ctx); err != nil {
		t.Fatalf("change-agent RunOnce (patch): %v", err)
	}
	remoteReleased, _, err := search.LoadResultFile(fsstore.SearchResultPath(serverBDataDir, "Movies", "byStatus", releasedSegs))
	if err != nil {
		t.Fatalf("load remote released bucket: %v", err)
	}
	if len(remoteReleased.Items) != 0 {
		t.Fatalf("expected id1 removed from the replicated released bucket, got %+v", remoteReleased.Items)
	}
	draftSegs := []string{search.EncodeStringValue("draft")}
	remoteDraft, existed, err := search.LoadResultFile(fsstore.SearchResultPath(serverBDataDir, "Movies", "byStatus", draftSegs))
	if err != nil || !existed || len(remoteDraft.Items) != 1 || remoteDraft.Items[0].ID != "id1" {
		t.Fatalf("expected id1 replicated into the draft bucket: existed=%v items=%+v err=%v", existed, remoteDraft.Items, err)
	}

	// Delete: id1 must be replicated out of the draft bucket entirely.
	if err := fsstore.SoftDeleteDocument(eng.DataDir, "Movies", "id1"); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if err := fsstore.EnqueueChange(eng.DataDir, "Movies", "id1", "delete"); err != nil {
		t.Fatalf("enqueue delete: %v", err)
	}
	if _, err := changeAgent.RunOnce(ctx); err != nil {
		t.Fatalf("change-agent RunOnce (delete): %v", err)
	}
	remoteDraftAfterDelete, _, err := search.LoadResultFile(fsstore.SearchResultPath(serverBDataDir, "Movies", "byStatus", draftSegs))
	if err != nil {
		t.Fatalf("load remote draft bucket: %v", err)
	}
	if len(remoteDraftAfterDelete.Items) != 0 {
		t.Fatalf("expected id1 removed from the replicated draft bucket after delete, got %+v", remoteDraftAfterDelete.Items)
	}
}
