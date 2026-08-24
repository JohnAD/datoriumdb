//go:build compose

package compose

import (
	"context"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/test/testutil"
)

// TestComposeSplitSOTReadRouting brings up
// deploy/docker-compose.split-sot-read.yml (serverA=SOT, serverB=bootstrapped
// read-member) and confirms: serverB successfully bootstrapped against
// serverA, writes on serverA replicate to serverB, reads succeed on
// serverB, and writes sent to serverB are refused as wrongMachine.
func TestComposeSplitSOTReadRouting(t *testing.T) {
	c := Up(t, "docker-compose.split-sot-read.yml")
	baseA := "http://127.0.0.1:8080"
	baseB := "http://127.0.0.1:8081"
	c.WaitHealthy(baseA, 60*time.Second)
	c.WaitHealthy(baseB, 60*time.Second)

	cfg, err := config.Load(FixtureConfigDir("split-sot-read", "serverA"))
	if err != nil {
		t.Fatalf("load fixture config: %v", err)
	}
	token := testutil.ClientToken(t, cfg, "compose-split-sot-read-client")
	ctx := context.Background()

	created, err := testutil.PostCommand(ctx, baseA, token, "create", "Movies", "01TESTMOVIES00000000000001", map[string]any{"$": "Movies:0", "title": "Split SOT Read Test"})
	if err != nil {
		t.Fatalf("create on SOT: %v", err)
	}
	if created["ok"] != true {
		t.Fatalf("expected create on serverA to succeed: %#v", created)
	}
	id, _ := created["id"].(string)

	testutil.PollUntilErr(t, 15*time.Second, 250*time.Millisecond, func() error {
		res, err := testutil.PostCommand(ctx, baseB, token, "read", "Movies", id, map[string]any{})
		if err != nil {
			return err
		}
		if res["ok"] != true {
			return errNotReady
		}
		return nil
	})

	refused, err := testutil.PostCommand(ctx, baseB, token, "create", "Movies", "01TESTMOVIES00000000000002", map[string]any{"$": "Movies:0", "title": "Should Be Refused"})
	if err != nil {
		t.Fatalf("create on read-member: %v", err)
	}
	if refused["ok"] != false {
		t.Fatalf("expected write on read-member to be refused: %#v", refused)
	}
	errs, _ := refused["errors"].([]any)
	if len(errs) == 0 {
		t.Fatalf("expected wrongMachine errors: %#v", refused)
	}
	err0, _ := errs[0].(map[string]any)
	if err0["code"] != "wrongMachine" {
		t.Fatalf("expected wrongMachine error code, got %#v", refused)
	}
	for _, banned := range []string{"correctServer", "baseURL", "shardSlot"} {
		if _, ok := refused[banned]; ok {
			t.Fatalf("wrongMachine must not include %q: %#v", banned, refused)
		}
	}
	if _, ok := refused["configVersion"]; !ok {
		t.Fatalf("expected diagnostic configVersion on wrongMachine: %#v", refused)
	}
}

type sentinelErr string

func (e sentinelErr) Error() string { return string(e) }

const errNotReady = sentinelErr("document not yet replicated/ready")
