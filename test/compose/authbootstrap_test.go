//go:build compose

package compose

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/test/testutil"
)

// TestComposeAuthBootstrapSucceedsWithCorrectSecret brings up
// deploy/docker-compose.auth-bootstrap.yml and confirms serverB
// successfully bootstrapped a machine token and establishment config from
// serverA using the shared Compose secret, then can serve an
// authenticated read of data created on serverA
// (tech-docs/AUTHENTICATION.md's machine bootstrap flow).
func TestComposeAuthBootstrapSucceedsWithCorrectSecret(t *testing.T) {
	c := Up(t, "docker-compose.auth-bootstrap.yml")
	baseA := "http://127.0.0.1:8080"
	baseB := "http://127.0.0.1:8081"
	c.WaitHealthy(baseA, 60*time.Second)
	c.WaitHealthy(baseB, 60*time.Second)

	cfg, err := config.Load(FixtureConfigDir("auth-bootstrap", "serverA"))
	if err != nil {
		t.Fatalf("load fixture config: %v", err)
	}
	token := testutil.ClientToken(t, cfg, "compose-auth-bootstrap-client")
	ctx := context.Background()

	created, err := testutil.PostCommand(ctx, baseA, token, `create Movies null {$: Movies:0, title: "Auth Bootstrap Test"}`)
	if err != nil {
		t.Fatalf("create: %v", err)
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
}

// TestComposeAuthBootstrapFailsWithWrongSecret starts the
// "serverB-bad-secret" profile service, which points at the real
// establishment server but presents the wrong bootstrap secret, and
// confirms the container exits instead of ever becoming a working
// DatoriumDB server -- proving the bootstrap secret is genuinely checked,
// not just plumbed through.
func TestComposeAuthBootstrapFailsWithWrongSecret(t *testing.T) {
	c := Up(t, "docker-compose.auth-bootstrap.yml")
	c.WaitHealthy("http://127.0.0.1:8080", 60*time.Second)

	out, err := c.Run("--profile", "bad-secret", "run", "--rm", "serverB-bad-secret")
	// A non-zero exit is expected: main.go calls log.Fatalf and exits(1)
	// when DATORIUMDB_MACHINE_BOOTSTRAP_SECRET is wrong and there is no
	// usable local config cache to fall back on.
	if err == nil {
		t.Fatalf("expected serverB-bad-secret to exit non-zero, but it succeeded:\n%s", out)
	}
	if !strings.Contains(out, "bootstrap") && !strings.Contains(out, "invalidBootstrapSecret") && !strings.Contains(out, "establishment bootstrap failed") {
		t.Fatalf("expected failure output to mention the bootstrap failure, got:\n%s", out)
	}
}
