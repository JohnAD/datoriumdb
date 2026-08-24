//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/test/testutil"
)

// TestTwoNodeBootstrapReplicationAndRouting exercises the whole
// non-establishment startup path end-to-end over real subprocesses and
// real HTTP: serverB bootstraps against serverA (AUTHENTICATION.md +
// ESTABLISHMENT-CONFIG.md), creates stage .pendingWrites on the SOT and
// the read-member catch-up applies them (REPLICATION-FAILURE-HANDLING.md),
// reads route only to the assigned SHARD_READ_MEMBER, and writes sent to a
// non-SOT server are refused with wrongMachine (SHARDING.md).
func TestTwoNodeBootstrapReplicationAndRouting(t *testing.T) {
	bin := testutil.BuildBinary(t, "datoriumdb")
	topo := newTwoNodeTopology(t)

	srvA := topo.StartA(t, bin)
	srvB := topo.StartB(t, bin)

	// serverB's bootstrap must have produced a usable local config cache.
	testutil.AssertFileExists(t, topo.ConfigDirB+"/__general.json")
	testutil.AssertFileExists(t, topo.ConfigDirB+"/Movies.schema.json")

	cfg, err := config.Load(topo.ConfigDirA)
	if err != nil {
		t.Fatalf("load serverA config for token issuance: %v", err)
	}
	token := testutil.ClientToken(t, cfg, "integration-test-client")
	ctx := context.Background()

	created, err := testutil.PostCommand(ctx, srvA.BaseURL, token, "create", "Movies", "01TESTMOVIES00000000000001", map[string]any{"$": "Movies:0", "title": "Arrival", "releaseYear": 2016})
	if err != nil {
		t.Fatalf("create on SOT: %v", err)
	}
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	if _, hasNote := created["note"]; hasNote {
		t.Fatalf("expected one-shot delivery with no note when the read-member is up: %#v", created)
	}
	if created["distributionComplete"] != true {
		t.Fatalf("expected distributionComplete true when read-member acknowledges: %#v", created)
	}
	id, _ := created["id"].(string)

	// Read-member catch-up pulls the staged pending write onto serverB.
	testutil.PollUntilErr(t, 10*time.Second, 50*time.Millisecond, func() error {
		if !testutil.FileExists(topo.DataDirB + "/Movies/" + id + ".json") {
			return errNotYetReplicated
		}
		return nil
	})

	// Reads route to the assigned SHARD_READ_MEMBER (serverB).
	readFromB, err := testutil.PostCommand(ctx, srvB.BaseURL, token, "read", "Movies", id, map[string]any{})
	if err != nil {
		t.Fatalf("read from serverB: %v", err)
	}
	if readFromB["ok"] != true {
		t.Fatalf("expected read from serverB to succeed: %#v", readFromB)
	}

	// Writes sent to serverB (not the SOT) are refused with wrongMachine.
	// Clients must refresh establishment and re-route locally; the bounce
	// does not include retry-target hints.
	wrongMachine, err := testutil.PostCommand(ctx, srvB.BaseURL, token, "create", "Movies", "01TESTMOVIES00000000000002", map[string]any{"$": "Movies:0", "title": "Should Not Land Here"})
	if err != nil {
		t.Fatalf("create on read-member: %v", err)
	}
	if wrongMachine["ok"] != false {
		t.Fatalf("expected write on read-member to be refused: %#v", wrongMachine)
	}
	errs, _ := wrongMachine["errors"].([]any)
	if len(errs) == 0 {
		t.Fatalf("expected wrongMachine errors: %#v", wrongMachine)
	}
	err0, _ := errs[0].(map[string]any)
	if err0["code"] != "wrongMachine" {
		t.Fatalf("expected wrongMachine error code, got %#v", wrongMachine)
	}
	for _, banned := range []string{"correctServer", "baseURL", "shardSlot"} {
		if _, ok := wrongMachine[banned]; ok {
			t.Fatalf("wrongMachine must not include %q: %#v", banned, wrongMachine)
		}
	}
	if _, ok := wrongMachine["configVersion"]; !ok {
		t.Fatalf("expected diagnostic configVersion on wrongMachine: %#v", wrongMachine)
	}

	_ = srvA
}

// TestReadMemberRestartRecoversReplicatedData stops the read-member
// process, restarts it against the same config/data directories, and
// confirms previously replicated data is still servable -- the
// filesystem-backed persistence model requires no special recovery step
// beyond the process coming back up and re-bootstrapping.
func TestReadMemberRestartRecoversReplicatedData(t *testing.T) {
	bin := testutil.BuildBinary(t, "datoriumdb")
	topo := newTwoNodeTopology(t)

	topo.StartA(t, bin)
	srvB := topo.StartB(t, bin)

	cfg, err := config.Load(topo.ConfigDirA)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	token := testutil.ClientToken(t, cfg, "integration-test-client")
	ctx := context.Background()

	created, err := testutil.PostCommand(ctx, "http://"+topo.ServerAAddr, token, "create", "Movies", "01TESTMOVIES00000000000003", map[string]any{"$": "Movies:0", "title": "Restart Recovery"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	id, _ := created["id"].(string)
	testutil.PollUntilErr(t, 10*time.Second, 50*time.Millisecond, func() error {
		if !testutil.FileExists(topo.DataDirB + "/Movies/" + id + ".json") {
			return errNotYetReplicated
		}
		return nil
	})

	srvB.Stop()
	srvB.Restart(t)

	readAfterRestart, err := testutil.PostCommand(ctx, srvB.BaseURL, token, "read", "Movies", id, map[string]any{})
	if err != nil {
		t.Fatalf("read after restart: %v", err)
	}
	if readAfterRestart["ok"] != true {
		t.Fatalf("expected read after restart to succeed: %#v", readAfterRestart)
	}
}

type sentinelError string

func (e sentinelError) Error() string { return string(e) }

const errNotYetReplicated = sentinelError("document not yet replicated to read-member")
