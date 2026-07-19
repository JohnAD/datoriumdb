package replication

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
)

func TestTargetsForAssignmentExcludesSelfAndDedups(t *testing.T) {
	c := &Coordinator{ServerName: "serverA"}
	assignment := config.ShardAssignment{
		ShardSOTMember:  "serverA",
		ShardReadMember: []string{"serverA", "serverB"},
		ProxyReadMember: []string{"serverB", "analysisA"},
	}
	targets := c.TargetsForAssignment(assignment)
	want := map[string]bool{"serverB": true, "analysisA": true}
	if len(targets) != len(want) {
		t.Fatalf("unexpected targets: %#v", targets)
	}
	for _, tgt := range targets {
		if !want[tgt] {
			t.Fatalf("unexpected target %q in %#v", tgt, targets)
		}
	}
}

func TestPushOutcomeComplete(t *testing.T) {
	complete := PushOutcome{Required: []string{"serverB"}, Acknowledged: []string{"serverB"}}
	if !complete.Complete() {
		t.Fatalf("expected complete outcome")
	}
	incomplete := PushOutcome{Required: []string{"serverB"}, Unacknowledged: []string{"serverB"}}
	if incomplete.Complete() {
		t.Fatalf("expected incomplete outcome")
	}
}

func TestBuildNoteShape(t *testing.T) {
	outcome := PushOutcome{
		Required:       []string{"serverB", "serverD"},
		Acknowledged:   []string{"serverB"},
		Unacknowledged: []string{"serverD"},
		TimeoutMs:      10000,
	}
	note := BuildNote(outcome)
	if note["code"] != NoteCode {
		t.Fatalf("unexpected note code: %#v", note["code"])
	}
	if note["timeoutMs"] != 10000 {
		t.Fatalf("unexpected timeoutMs: %#v", note["timeoutMs"])
	}
	req, _ := note["required"].([]string)
	if len(req) != 2 {
		t.Fatalf("unexpected required list: %#v", note["required"])
	}
	unacked, _ := note["unacknowledged"].([]string)
	if len(unacked) != 1 || unacked[0] != "serverD" {
		t.Fatalf("unexpected unacknowledged list: %#v", note["unacknowledged"])
	}
}

// fakeApplyServer is a minimal stand-in for another DatoriumDB server's
// apply-document-write endpoint, used to test Coordinator push behavior
// without a full HTTPServer.
func fakeApplyServer(t *testing.T, accept bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if accept {
			_, _ = w.Write([]byte(`{"ok":true,"applied":true,"operationId":"op1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":false,"errors":[{"code":"applyFailed","message":"nope"}]}`))
	}))
}

func testCfgWithServers(servers map[string]string) *config.Config {
	entries := map[string]config.ServerEntry{}
	for name, base := range servers {
		entries[name] = config.ServerEntry{BaseURL: base}
	}
	return &config.Config{Servers: config.ServersFile{Servers: entries}}
}

func TestDeliverOnceAcknowledgedTargetLeavesNoPending(t *testing.T) {
	up := fakeApplyServer(t, true)
	defer up.Close()

	dir := t.TempDir()
	c := &Coordinator{
		ServerName: "serverA",
		DataDir:    dir,
		Cfg:        testCfgWithServers(map[string]string{"serverB": up.URL}),
		Tokens:     StaticTokenSource("tok"),
		Timeout:    2 * time.Second,
	}
	item := DocumentWorkItem{Collection: "Movies", ID: "doc1", OperationID: "op1", Command: "create", Payload: map[string]any{"!": "doc1"}}
	outcome := c.DeliverOnce(context.Background(), item, []string{"serverB"})
	if !outcome.Complete() {
		t.Fatalf("expected complete outcome, got %#v", outcome)
	}
	if _, err := os.Stat(PendingWritePath(dir, "Movies", "serverB", "doc1")); !os.IsNotExist(err) {
		t.Fatalf("expected no pending write after acknowledged one-shot")
	}
}

func TestStagePendingWritesWritesAllTargets(t *testing.T) {
	dir := t.TempDir()
	c := &Coordinator{ServerName: "serverA", DataDir: dir, Cfg: testCfgWithServers(nil)}
	item := DocumentWorkItem{
		Collection:  "Movies",
		ID:          "doc1",
		OperationID: "op1",
		Command:     "create",
		Payload:     map[string]any{"!": "doc1", "title": "x"},
	}
	if err := c.StagePendingWrites(item, []string{"serverB", "analysisA"}); err != nil {
		t.Fatalf("StagePendingWrites: %v", err)
	}
	for _, target := range []string{"serverB", "analysisA"} {
		pending, err := ReadPendingWrite(dir, "Movies", target, "doc1")
		if err != nil {
			t.Fatalf("expected pending write for %s: %v", target, err)
		}
		if pending.OperationID != "op1" || pending.Command != "create" {
			t.Fatalf("unexpected pending write for %s: %#v", target, pending)
		}
	}
}

func TestReplicateDocumentWriteHappyPath(t *testing.T) {
	up := fakeApplyServer(t, true)
	defer up.Close()

	dir := t.TempDir()
	c := &Coordinator{
		ServerName: "serverA",
		DataDir:    dir,
		Cfg:        testCfgWithServers(map[string]string{"serverB": up.URL}),
		Tokens:     StaticTokenSource("tok"),
		Timeout:    2 * time.Second,
	}
	item := DocumentWorkItem{Collection: "Movies", ID: "doc1", OperationID: "op1", Command: "create", Payload: map[string]any{"!": "doc1"}}
	outcome := c.ReplicateDocumentWrite(context.Background(), item, []string{"serverB"})
	if !outcome.Complete() {
		t.Fatalf("expected complete outcome, got %#v", outcome)
	}
	if len(outcome.Acknowledged) != 1 || outcome.Acknowledged[0] != "serverB" {
		t.Fatalf("expected serverB acknowledged, got %#v", outcome)
	}
	if _, err := os.Stat(PendingWritePath(dir, "Movies", "serverB", "doc1")); !os.IsNotExist(err) {
		t.Fatalf("expected no pending write file on the happy path")
	}
}

func TestReplicateDocumentWriteDownMemberRecordsPendingWrite(t *testing.T) {
	down := fakeApplyServer(t, false)
	defer down.Close()

	dir := t.TempDir()
	c := &Coordinator{
		ServerName: "serverA",
		DataDir:    dir,
		Cfg:        testCfgWithServers(map[string]string{"serverB": down.URL}),
		Tokens:     StaticTokenSource("tok"),
		Timeout:    2 * time.Second,
	}
	item := DocumentWorkItem{Collection: "Movies", ID: "doc1", OperationID: "op1", Command: "create", Payload: map[string]any{"!": "doc1"}}
	outcome := c.ReplicateDocumentWrite(context.Background(), item, []string{"serverB"})
	if outcome.Complete() {
		t.Fatalf("expected incomplete outcome for a rejecting member, got %#v", outcome)
	}
	if len(outcome.Unacknowledged) != 1 || outcome.Unacknowledged[0] != "serverB" {
		t.Fatalf("expected serverB unacknowledged, got %#v", outcome)
	}
	pending, err := ReadPendingWrite(dir, "Movies", "serverB", "doc1")
	if err != nil {
		t.Fatalf("expected a pending write file to be recorded: %v", err)
	}
	if pending.OperationID != "op1" {
		t.Fatalf("unexpected pending write content: %#v", pending)
	}
}

func TestReplicateDocumentWriteUnknownServerHasNoBaseURL(t *testing.T) {
	dir := t.TempDir()
	c := &Coordinator{
		ServerName: "serverA",
		DataDir:    dir,
		Cfg:        testCfgWithServers(map[string]string{}),
		Tokens:     StaticTokenSource("tok"),
		Timeout:    2 * time.Second,
	}
	item := DocumentWorkItem{Collection: "Movies", ID: "doc1", OperationID: "op1", Command: "create"}
	outcome := c.ReplicateDocumentWrite(context.Background(), item, []string{"serverGhost"})
	if outcome.Complete() {
		t.Fatalf("expected incomplete outcome for an unknown server")
	}
}

func TestReplicateDocumentWriteNoTargetsIsComplete(t *testing.T) {
	dir := t.TempDir()
	c := &Coordinator{ServerName: "serverA", DataDir: dir, Cfg: testCfgWithServers(nil), Tokens: StaticTokenSource("tok")}
	outcome := c.ReplicateDocumentWrite(context.Background(), DocumentWorkItem{Collection: "Movies", ID: "doc1"}, nil)
	if !outcome.Complete() {
		t.Fatalf("expected an empty target list to be trivially complete")
	}
}
