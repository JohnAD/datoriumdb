//go:build integration

// Package integration runs DatoriumDB as real `datoriumdb` subprocesses
// speaking real HTTP, exercising the same startup path
// (cmd/datoriumdb/main.go) that production deployments use. Contrast with
// test/contract (in-process engine, no HTTP/subprocess) and
// internal/server's own *_integration_test.go (in-process httptest
// servers, no subprocess). Build with
// `go test -tags integration ./test/integration/...`.
package integration

import (
	"context"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/test/testutil"
)

// TestSingleNodeCRUDLifecycle boots one real datoriumdb subprocess (acting
// as its own establishment server, per testdata/sample-config) and drives
// a full create/read/patch/delete lifecycle over real HTTP.
func TestSingleNodeCRUDLifecycle(t *testing.T) {
	bin := testutil.BuildBinary(t, "datoriumdb")
	configDir := testutil.TempConfigDir(t)
	dataDir := testutil.TempDataDir(t)

	srv := testutil.StartServer(t, testutil.ServerOptions{
		BinPath:          bin,
		ServerName:       "serverA",
		EstablishmentURL: "http://ignored.invalid",
		ConfigDir:        configDir,
		DataDir:          dataDir,
	})

	cfg, err := config.Load(configDir)
	if err != nil {
		t.Fatalf("load config for token issuance: %v", err)
	}
	token := testutil.ClientToken(t, cfg, "integration-test-client")
	ctx := context.Background()

	created, err := testutil.PostCommand(ctx, srv.BaseURL, token, `create Movies null {$: Movies:0, title: "The Matrix", releaseYear: 1999}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created["ok"] != true {
		t.Fatalf("expected create to succeed: %#v", created)
	}
	id, _ := created["id"].(string)
	ver, _ := created["#"].(string)
	if id == "" || ver == "" {
		t.Fatalf("expected id/version in create response: %#v", created)
	}

	read, err := testutil.PostCommand(ctx, srv.BaseURL, token, `read Movies `+id+` {}`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if read["ok"] != true {
		t.Fatalf("expected read to succeed: %#v", read)
	}
	sot, _ := read["sot"].(map[string]any)
	if sot["title"] != "The Matrix" {
		t.Fatalf("unexpected read payload: %#v", read)
	}

	patched, err := testutil.PostCommand(ctx, srv.BaseURL, token, `patch Movies `+id+` {$: Movies:0, #: `+ver+`, RFC6902: [{op: add, path: /status, value: released}]}`)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if patched["ok"] != true {
		t.Fatalf("expected patch to succeed: %#v", patched)
	}
	versions, _ := patched["versions"].(map[string]any)
	afterVer, _ := versions["after"].(string)
	if afterVer == "" {
		t.Fatalf("expected an after version: %#v", patched)
	}

	testutil.AssertFileExists(t, dataDirDocPath(dataDir, "Movies", id))

	deleted, err := testutil.PostCommand(ctx, srv.BaseURL, token, `delete Movies `+id+` {#: `+afterVer+`}`)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted["ok"] != true {
		t.Fatalf("expected delete to succeed: %#v", deleted)
	}
	testutil.AssertFileMissing(t, dataDirDocPath(dataDir, "Movies", id))
}

// TestSingleNodeUnauthenticatedCommandRefused ensures the HTTP transport
// still enforces bearer-token auth for a real subprocess (not just the
// in-process handler tests).
func TestSingleNodeUnauthenticatedCommandRefused(t *testing.T) {
	bin := testutil.BuildBinary(t, "datoriumdb")
	srv := testutil.StartServer(t, testutil.ServerOptions{
		BinPath:          bin,
		ServerName:       "serverA",
		EstablishmentURL: "http://ignored.invalid",
		ConfigDir:        testutil.TempConfigDir(t),
		DataDir:          testutil.TempDataDir(t),
	})

	res, err := testutil.PostCommand(context.Background(), srv.BaseURL, "", `create Movies null {$: Movies:0, title: "The Matrix"}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res["ok"] != false {
		t.Fatalf("expected unauthenticated command to be refused: %#v", res)
	}
}

func dataDirDocPath(dataDir, collection, id string) string {
	return dataDir + "/" + collection + "/" + id + ".json"
}
