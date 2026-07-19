//go:build compose

package compose

import (
	"context"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/test/testutil"
)

// TestComposeProxyReadReceivesReplicatedWritesButIsNotAReadTarget brings
// up deploy/docker-compose.proxy-read.yml (serverA=SOT, serverB=read,
// analysisA=proxy) and confirms: a write on serverA replicates to both
// serverB and analysisA, but analysisA -- a PROXY_READ_MEMBER -- refuses
// normal smart-client reads (tech-docs/SHARDING.md: "PROXY_READ_MEMBER
// servers are not normal smart-client read targets").
func TestComposeProxyReadReceivesReplicatedWritesButIsNotAReadTarget(t *testing.T) {
	c := Up(t, "docker-compose.proxy-read.yml")
	baseA := "http://127.0.0.1:8080"
	baseB := "http://127.0.0.1:8081"
	baseProxy := "http://127.0.0.1:8082"
	c.WaitHealthy(baseA, 60*time.Second)
	c.WaitHealthy(baseB, 60*time.Second)
	c.WaitHealthy(baseProxy, 60*time.Second)

	cfg, err := config.Load(FixtureConfigDir("proxy-read", "serverA"))
	if err != nil {
		t.Fatalf("load fixture config: %v", err)
	}
	token := testutil.ClientToken(t, cfg, "compose-proxy-read-client")
	ctx := context.Background()

	created, err := testutil.PostCommand(ctx, baseA, token, `create Movies 01TESTMOVIES00000000000001 {$: Movies:0, title: "Proxy Read Test"}`)
	if err != nil {
		t.Fatalf("create on SOT: %v", err)
	}
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	id, _ := created["id"].(string)

	testutil.PollUntilErr(t, 15*time.Second, 250*time.Millisecond, func() error {
		res, err := testutil.PostCommand(ctx, baseB, token, `read Movies `+id+` {}`)
		if err != nil {
			return err
		}
		if res["ok"] != true {
			return errNotReady
		}
		return nil
	})

	// analysisA is not a normal read target even though it now holds a
	// replicated copy of the document.
	proxyRead, err := testutil.PostCommand(ctx, baseProxy, token, `read Movies `+id+` {}`)
	if err != nil {
		t.Fatalf("read on proxy: %v", err)
	}
	if proxyRead["ok"] != false {
		t.Fatalf("expected proxy read-member to refuse a normal smart-client read: %#v", proxyRead)
	}
}
