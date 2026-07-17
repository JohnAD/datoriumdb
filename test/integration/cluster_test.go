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
// ESTABLISHMENT-CONFIG.md), writes replicate synchronously from the
// SOT-member to the read-member (REPLICATION-FAILURE-HANDLING.md), reads
// route only to the assigned SHARD_READ_MEMBER, and writes sent to a
// non-SOT server are refused with a wrongMachine hint (SHARDING.md).
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

	created, err := testutil.PostCommand(ctx, srvA.BaseURL, token, `create Movies null {$: Movies:0, title: "Arrival", releaseYear: 2016}`)
	if err != nil {
		t.Fatalf("create on SOT: %v", err)
	}
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	if _, hasNote := created["note"]; hasNote {
		t.Fatalf("expected synchronous replication with no note: %#v", created)
	}
	id, _ := created["id"].(string)

	// The write landed on disk on serverB (the read-member), not just
	// serverA, proving real cross-process replication happened.
	testutil.PollUntilErr(t, 5*time.Second, 50*time.Millisecond, func() error {
		if !testutil.FileExists(topo.DataDirB + "/Movies/" + id + ".json") {
			return errNotYetReplicated
		}
		return nil
	})

	// Reads route to the assigned SHARD_READ_MEMBER (serverB).
	readFromB, err := testutil.PostCommand(ctx, srvB.BaseURL, token, `read Movies `+id+` {}`)
	if err != nil {
		t.Fatalf("read from serverB: %v", err)
	}
	if readFromB["ok"] != true {
		t.Fatalf("expected read from serverB to succeed: %#v", readFromB)
	}

	// Writes sent to serverB (not the SOT) are refused with a
	// wrongMachine hint pointing back at serverA.
	wrongMachine, err := testutil.PostCommand(ctx, srvB.BaseURL, token, `create Movies null {$: Movies:0, title: "Should Not Land Here"}`)
	if err != nil {
		t.Fatalf("create on read-member: %v", err)
	}
	if wrongMachine["ok"] != false {
		t.Fatalf("expected write on read-member to be refused: %#v", wrongMachine)
	}
	if wrongMachine["correctServer"] != "serverA" {
		t.Fatalf("expected wrongMachine hint to name serverA: %#v", wrongMachine)
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

	created, err := testutil.PostCommand(ctx, "http://"+topo.ServerAAddr, token, `create Movies null {$: Movies:0, title: "Restart Recovery"}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	id, _ := created["id"].(string)
	testutil.PollUntilErr(t, 5*time.Second, 50*time.Millisecond, func() error {
		if !testutil.FileExists(topo.DataDirB + "/Movies/" + id + ".json") {
			return errNotYetReplicated
		}
		return nil
	})

	srvB.Stop()
	srvB.Restart(t)

	readAfterRestart, err := testutil.PostCommand(ctx, srvB.BaseURL, token, `read Movies `+id+` {}`)
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
