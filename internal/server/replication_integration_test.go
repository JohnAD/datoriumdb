package server

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/engine"
	"github.com/JohnAD/datoriumdb/internal/docjson"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/replication"
)

// replicationTopology is a 3-server, single-shard-slot deployment used by
// the replication integration tests: serverA is SHARD_SOT_MEMBER, serverB
// is SHARD_READ_MEMBER, and analysisA is PROXY_READ_MEMBER for the entire
// 00-FF slot range (tech-docs/SHARDING.md, REPLICATION-FAILURE-HANDLING.md).
type replicationTopology struct {
	sot   *engine.Engine
	read  *engine.Engine
	proxy *engine.Engine

	readTS  *httptest.Server
	proxyTS *httptest.Server

	issuer *auth.Issuer
}

func newReplicationEngine(t *testing.T, serverName string) *engine.Engine {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, ".config")
	if err := copyDir(t, "../../testdata/sample-config", configDir); err != nil {
		t.Fatal(err)
	}
	eng := &engine.Engine{ConfigDir: configDir, DataDir: root, ServerName: serverName}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	return eng
}

// newReplicationTopology wires up three real Engines (one SOT, one read
// member, one proxy member), starts httptest servers for the read and
// proxy members so the SOT's Coordinator can push to them over real HTTP,
// and gives the SOT a working machine TokenSource.
func newReplicationTopology(t *testing.T) *replicationTopology {
	t.Helper()
	sotEng := newReplicationEngine(t, "serverA")
	readEng := newReplicationEngine(t, "serverB")
	proxyEng := newReplicationEngine(t, "analysisA")

	readSrv := &HTTPServer{Engine: readEng}
	proxySrv := &HTTPServer{Engine: proxyEng}
	readTS := httptest.NewServer(readSrv.Handler())
	t.Cleanup(readTS.Close)
	proxyTS := httptest.NewServer(proxySrv.Handler())
	t.Cleanup(proxyTS.Close)

	issuer, err := auth.NewIssuerFromFile(sotEng.Cfg.Auth, filepath.Join(sotEng.ConfigDir, "dev-signing-key.pem"))
	if err != nil {
		t.Fatal(err)
	}

	servers := map[string]config.ServerEntry{
		"serverA":   {BaseURL: "http://unused.invalid"},
		"serverB":   {BaseURL: readTS.URL},
		"analysisA": {BaseURL: proxyTS.URL},
	}
	shardMap := map[string]config.ShardAssignment{
		"00-FF": {
			ShardSOTMember:  "serverA",
			ShardReadMember: []string{"serverB"},
			ProxyReadMember: []string{"analysisA"},
		},
	}
	for _, e := range []*engine.Engine{sotEng, readEng, proxyEng} {
		e.Cfg.Servers.Servers = servers
		e.Cfg.ShardMap.ShardMap.Default = shardMap
	}

	sotEng.Replicator = &replication.Coordinator{
		ServerName: "serverA",
		DataDir:    sotEng.DataDir,
		Cfg:        sotEng.Cfg,
		Tokens:     replication.IssuerTokenSource{Issuer: issuer, ServerName: "serverA"},
	}

	return &replicationTopology{
		sot: sotEng, read: readEng, proxy: proxyEng,
		readTS: readTS, proxyTS: proxyTS,
		issuer: issuer,
	}
}

func readLocalDoc(t *testing.T, eng *engine.Engine, collection, id string) map[string]any {
	t.Helper()
	doc, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(eng.DataDir, collection, id))
	if err != nil {
		t.Fatalf("expected %s/%s to exist locally on %s: %v", collection, id, eng.ServerName, err)
	}
	return doc
}

func localDocGone(eng *engine.Engine, collection, id string) bool {
	_, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(eng.DataDir, collection, id))
	return os.IsNotExist(err) || err != nil
}

// --- happy path: one-shot live delivery for create/patch/delete ----

func TestReplicationHappyPathCreatePatchDelete(t *testing.T) {
	topo := newReplicationTopology(t)

	created := topo.sot.Execute(`create Movies 01TESTMOVIES00000000000001 {$: Movies:0, title: "The Matrix"}`)
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	if _, hasNote := created["note"]; hasNote {
		t.Fatalf("expected no note when every target acknowledges: %#v", created)
	}
	id, _ := created["id"].(string)
	ver, _ := created["#"].(string)

	readDoc := readLocalDoc(t, topo.read, "Movies", id)
	if readDoc["title"] != "The Matrix" {
		t.Fatalf("unexpected read-member copy: %#v", readDoc)
	}
	proxyDoc := readLocalDoc(t, topo.proxy, "Movies", id)
	if proxyDoc["title"] != "The Matrix" {
		t.Fatalf("unexpected proxy-member copy: %#v", proxyDoc)
	}
	if readDoc["#"] != ver || proxyDoc["#"] != ver {
		t.Fatalf("expected replicas to share the SOT's version: sot=%v read=%v proxy=%v", ver, readDoc["#"], proxyDoc["#"])
	}
	for _, target := range []string{"serverB", "analysisA"} {
		if _, err := replication.ReadPendingWrite(topo.sot.DataDir, "Movies", target, id); !os.IsNotExist(err) {
			t.Fatalf("expected no pending write for %s after successful one-shot: err=%v", target, err)
		}
	}

	readResult := topo.read.Execute(`read Movies ` + id + ` {}`)
	if readResult["ok"] != true {
		t.Fatalf("expected read-member to serve the replicated document: %#v", readResult)
	}

	patched := topo.sot.Execute(`patch Movies ` + id + ` {$: Movies:0, #: ` + ver + `, RFC6902: [{op: add, path: /status, value: released}]}`)
	if patched["ok"] != true {
		t.Fatalf("expected patch to succeed: %#v", patched)
	}
	if _, hasNote := patched["note"]; hasNote {
		t.Fatalf("expected no note when every target acknowledges patch: %#v", patched)
	}
	versions, _ := patched["versions"].(map[string]any)
	afterVer, _ := versions["after"].(string)

	readDoc = readLocalDoc(t, topo.read, "Movies", id)
	if readDoc["status"] != "released" {
		t.Fatalf("expected patch to replicate to read-member: %#v", readDoc)
	}
	if readDoc["#"] != afterVer {
		t.Fatalf("expected read-member to converge on the patched version: want %v got %v", afterVer, readDoc["#"])
	}
	proxyDoc = readLocalDoc(t, topo.proxy, "Movies", id)
	if proxyDoc["status"] != "released" {
		t.Fatalf("expected patch to replicate to proxy-member: %#v", proxyDoc)
	}

	deleted := topo.sot.Execute(`delete Movies ` + id + ` {#: ` + afterVer + `}`)
	if deleted["ok"] != true {
		t.Fatalf("expected delete to succeed: %#v", deleted)
	}
	if _, hasNote := deleted["note"]; hasNote {
		t.Fatalf("expected no note when every target acknowledges delete: %#v", deleted)
	}
	if !localDocGone(topo.read, "Movies", id) {
		t.Fatalf("expected one-shot delete on read-member")
	}
	if !localDocGone(topo.proxy, "Movies", id) {
		t.Fatalf("expected one-shot delete on proxy-member")
	}
}

// --- one-shot with a down read-member: live target acks; down target gets
// pending + note; no .operations tracking

func TestReplicationOneShotDownMemberPendingAndNote(t *testing.T) {
	topo := newReplicationTopology(t)
	topo.readTS.Close()

	created := topo.sot.Execute(`create Movies 01TESTMOVIES00000000000002 {$: Movies:0, title: "Down Member Test"}`)
	if created["ok"] != true {
		t.Fatalf("expected SOT-local success even though a read-member is down: %#v", created)
	}
	id, _ := created["id"].(string)

	note, ok := created["note"].(map[string]any)
	if !ok {
		t.Fatalf("expected a note naming the unacknowledged target: %#v", created)
	}
	if note["code"] != replication.NoteCode {
		t.Fatalf("unexpected note code: %#v", note)
	}
	unacked, _ := note["unacknowledged"].([]string)
	if len(unacked) != 1 || unacked[0] != "serverB" {
		t.Fatalf("expected serverB unacknowledged, got %#v", note)
	}
	acked, _ := note["acknowledged"].([]string)
	if len(acked) != 1 || acked[0] != "analysisA" {
		t.Fatalf("expected analysisA acknowledged, got %#v", note)
	}

	proxyDoc := readLocalDoc(t, topo.proxy, "Movies", id)
	if proxyDoc["title"] != "Down Member Test" {
		t.Fatalf("expected one-shot delivery to the live proxy: %#v", proxyDoc)
	}
	if !localDocGone(topo.read, "Movies", id) {
		t.Fatalf("down read-member must not have received a live create")
	}

	item, err := replication.ReadPendingWrite(topo.sot.DataDir, "Movies", "serverB", id)
	if err != nil {
		t.Fatalf("expected a pending write for serverB: %v", err)
	}
	if item.Command != "create" || item.ID != id {
		t.Fatalf("unexpected pending write: %#v", item)
	}
	if _, err := replication.ReadPendingWrite(topo.sot.DataDir, "Movies", "analysisA", id); !os.IsNotExist(err) {
		t.Fatalf("expected no pending write for the acknowledged proxy: err=%v", err)
	}
	if _, err := replication.Load(topo.sot.DataDir, created["operationId"].(string)); err == nil {
		t.Fatalf("expected no durable operation record for create")
	}
}

// --- catch-up: a read-member that was down comes back, checks in, and
// cleans up the SOT's pending write ---------------------------------------

func TestCatchUpAppliesPendingWriteAndCleansUpAfterRestart(t *testing.T) {
	topo := newReplicationTopology(t)
	topo.readTS.Close()

	created := topo.sot.Execute(`create Movies 01TESTMOVIES00000000000003 {$: Movies:0, title: "Catch Up Test"}`)
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	id, _ := created["id"].(string)

	if _, err := replication.ReadPendingWrite(topo.sot.DataDir, "Movies", "serverB", id); err != nil {
		t.Fatalf("expected a pending write to exist before catch-up: %v", err)
	}

	// The read-member "restarts": bring its HTTP server back up, pointed
	// at the SOT-member's data (which owns the pending-write bookkeeping).
	sotHandlerSrv := &HTTPServer{Engine: topo.sot}
	sotTS := httptest.NewServer(sotHandlerSrv.Handler())
	t.Cleanup(sotTS.Close)
	topo.sot.Cfg.Servers.Servers["serverA"] = config.ServerEntry{BaseURL: sotTS.URL}
	topo.read.Cfg.Servers.Servers["serverA"] = config.ServerEntry{BaseURL: sotTS.URL}

	readState := &replication.ReadMemberState{}
	agent := &replication.CatchUpAgent{
		ServerName: "serverB",
		DataDir:    topo.read.DataDir,
		Cfg:        topo.read.Cfg,
		Tokens:     replication.IssuerTokenSource{Issuer: topo.issuer, ServerName: "serverB"},
		State:      readState,
	}
	if err := agent.CheckIn(context.Background(), "serverA"); err != nil {
		t.Fatalf("catch-up check-in failed: %v", err)
	}

	// The read-member now has the document locally.
	readDoc := readLocalDoc(t, topo.read, "Movies", id)
	if readDoc["title"] != "Catch Up Test" {
		t.Fatalf("unexpected read-member copy after catch-up: %#v", readDoc)
	}

	// The pending write on the SOT-member has been cleaned up.
	if _, err := replication.ReadPendingWrite(topo.sot.DataDir, "Movies", "serverB", id); !os.IsNotExist(err) {
		t.Fatalf("expected pending write to be deleted after successful catch-up, err=%v", err)
	}

	// The check-in succeeded, so failure counters / staleness are clear.
	if readState.IsStaleForSOT("serverA") {
		t.Fatalf("expected a successful check-in to clear staleness")
	}
	if readState.IsPending("Movies", id) {
		t.Fatalf("expected the per-document pending flag to clear after a successful apply+complete")
	}
}

// --- SOT restart recovery: resume replication for a non-terminal
// operation left behind by a simulated crash ------------------------------

func TestSOTResumeIncompleteReplicatesLeftoverOperations(t *testing.T) {
	topo := newReplicationTopology(t)

	// Simulate a crash: commit locally and durably record the operation,
	// but never call ReplicateOperation (as if the process died right
	// after StateCommittedLocal).
	doc := map[string]any{"!": "01CRASHTESTDOC00000000001", "$": "Movies:0", "#": "01CRASHTESTVER00000000001", "title": "Crash Recovery"}
	if err := fsstore.EnsureCollectionDir(topo.sot.DataDir, "Movies"); err != nil {
		t.Fatal(err)
	}
	raw, encErr := docjson.EncodeMap(doc)
	if encErr != nil {
		t.Fatal(encErr)
	}
	if err := fsstore.WriteDocumentJSONVerified(fsstore.DocumentPath(topo.sot.DataDir, "Movies", "01CRASHTESTDOC00000000001"), raw); err != nil {
		t.Fatal(err)
	}
	op, err := replication.Begin(topo.sot.DataDir, replication.DocumentWorkItem{
		Collection:   "Movies",
		ID:           "01CRASHTESTDOC00000000001",
		AfterVersion: "01CRASHTESTVER00000000001",
		OperationID:  "01CRASHTESTOPID0000000001",
		Command:      "create",
		Payload:      raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := op.SetState(topo.sot.DataDir, replication.StateCommittedLocal); err != nil {
		t.Fatal(err)
	}

	incomplete, err := replication.ListIncomplete(topo.sot.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(incomplete) != 1 {
		t.Fatalf("expected exactly one incomplete operation before resume, got %d", len(incomplete))
	}

	resumed, err := topo.sot.Replicator.ResumeIncomplete(context.Background())
	if err != nil {
		t.Fatalf("ResumeIncomplete failed: %v", err)
	}
	if len(resumed) != 1 {
		t.Fatalf("expected exactly one resumed operation, got %d", len(resumed))
	}

	readDoc := readLocalDoc(t, topo.read, "Movies", "01CRASHTESTDOC00000000001")
	if readDoc["title"] != "Crash Recovery" {
		t.Fatalf("expected the resumed operation to replicate to the read-member: %#v", readDoc)
	}
	proxyDoc := readLocalDoc(t, topo.proxy, "Movies", "01CRASHTESTDOC00000000001")
	if proxyDoc["title"] != "Crash Recovery" {
		t.Fatalf("expected the resumed operation to replicate to the proxy-member: %#v", proxyDoc)
	}

	reloaded, err := replication.Load(topo.sot.DataDir, "01CRASHTESTOPID0000000001")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.State != replication.StateReplicated {
		t.Fatalf("expected the operation to reach StateReplicated after resume, got %q", reloaded.State)
	}

	incomplete, err = replication.ListIncomplete(topo.sot.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(incomplete) != 0 {
		t.Fatalf("expected no incomplete operations after resume, got %d", len(incomplete))
	}
}
