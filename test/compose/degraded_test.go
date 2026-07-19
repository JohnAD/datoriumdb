//go:build compose

package compose

import (
	"context"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/test/testutil"
)

// TestComposeDegradedReplicationRecovers brings up
// deploy/docker-compose.degraded-replication.yml, stops the read-member
// container to simulate an outage, confirms the SOT still accepts creates
// (ok:true after one-shot delivery + pending for the down member), restarts
// the read-member, and confirms its catch-up loop repairs the gap without
// any operator action beyond bringing the container back up.
func TestComposeDegradedReplicationRecovers(t *testing.T) {
	c := Up(t, "docker-compose.degraded-replication.yml")
	baseA := "http://127.0.0.1:8080"
	baseB := "http://127.0.0.1:8081"
	c.WaitHealthy(baseA, 60*time.Second)
	c.WaitHealthy(baseB, 60*time.Second)

	cfg, err := config.Load(FixtureConfigDir("degraded-replication", "serverA"))
	if err != nil {
		t.Fatalf("load fixture config: %v", err)
	}
	token := testutil.ClientToken(t, cfg, "compose-degraded-replication-client")
	ctx := context.Background()

	if out, err := c.Run("stop", "serverB"); err != nil {
		t.Fatalf("stop serverB: %v\n%s", err, out)
	}

	created, err := testutil.PostCommand(ctx, baseA, token, `create Movies 01TESTMOVIES00000000000001 {$: Movies:0, title: "Degraded Replication Test"}`)
	if err != nil {
		t.Fatalf("create while serverB is down: %v", err)
	}
	if created["ok"] != true {
		t.Fatalf("expected SOT-local success even with the read-member down: %#v", created)
	}
	note, hasNote := created["note"].(map[string]any)
	if !hasNote {
		t.Fatalf("expected a note naming the unacknowledged read-member: %#v", created)
	}
	var unacked []string
	if raw, ok := note["unacknowledged"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				unacked = append(unacked, s)
			}
		}
	}
	if len(unacked) != 1 || unacked[0] != "serverB" {
		t.Fatalf("expected serverB unacknowledged: %#v", note)
	}
	id, _ := created["id"].(string)

	if out, err := c.Run("start", "serverB"); err != nil {
		t.Fatalf("start serverB: %v\n%s", err, out)
	}
	c.WaitHealthy(baseB, 60*time.Second)

	// This topology's fixture sets readMemberCheckinSeconds: 3, so
	// catch-up should complete within a handful of seconds.
	testutil.PollUntilErr(t, 30*time.Second, 500*time.Millisecond, func() error {
		res, err := testutil.PostCommand(ctx, baseB, token, `read Movies `+id+` {}`)
		if err != nil {
			return err
		}
		if res["ok"] != true {
			return errNotReady
		}
		return nil
	})
}
