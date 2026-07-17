//go:build compose

package compose

import (
	"context"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/test/testutil"
)

// TestComposeSingleNodeCRUD brings up deploy/docker-compose.single-node.yml
// and drives a full create/read/patch/delete lifecycle against the
// published host port, proving the built container image actually boots,
// serves HTTP, and persists to its /db volume.
func TestComposeSingleNodeCRUD(t *testing.T) {
	c := Up(t, "docker-compose.single-node.yml")
	baseURL := "http://127.0.0.1:8080"
	c.WaitHealthy(baseURL, 60*time.Second)

	cfg, err := config.Load(FixtureConfigDir("single-node", "serverA"))
	if err != nil {
		t.Fatalf("load fixture config for token issuance: %v", err)
	}
	token := testutil.ClientToken(t, cfg, "compose-single-node-client")
	ctx := context.Background()

	created, err := testutil.PostCommand(ctx, baseURL, token, `create Movies null {$: Movies:0, title: "Compose Smoke Test"}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	id, _ := created["id"].(string)
	ver, _ := created["#"].(string)

	read, err := testutil.PostCommand(ctx, baseURL, token, `read Movies `+id+` {}`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if read["ok"] != true {
		t.Fatalf("expected read to succeed: %#v", read)
	}

	patched, err := testutil.PostCommand(ctx, baseURL, token, `patch Movies `+id+` {$: Movies:0, #: `+ver+`, RFC6902: [{op: add, path: /status, value: released}]}`)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if patched["ok"] != true {
		t.Fatalf("expected patch to succeed: %#v", patched)
	}
	versions, _ := patched["versions"].(map[string]any)
	afterVer, _ := versions["after"].(string)

	deleted, err := testutil.PostCommand(ctx, baseURL, token, `delete Movies `+id+` {#: `+afterVer+`}`)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted["ok"] != true {
		t.Fatalf("expected delete to succeed: %#v", deleted)
	}
}
