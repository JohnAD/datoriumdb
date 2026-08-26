package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/engine"
)

const booksSchema = `{
  "kind": "object",
  "children": [
    {"name": "title", "kind": "string", "required": true}
  ]
}`

func TestAdminCollectionEnsureRequiresAdminToken(t *testing.T) {
	ts, _, issuer := testHarness(t)
	clientToken, _, err := issuer.IssueClientToken("alice", 0)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"command":   "collectionEnsure",
		"target":    "Books",
		"parameter": "",
		"detail": map[string]any{
			"schema": map[string]any{
				"kind": "object",
				"children": []any{
					map[string]any{"name": "title", "kind": "string", "required": true},
				},
			},
		},
	})
	resp := doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/command", "application/json", string(body), clientToken)
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "adminRequired" {
		t.Fatalf("expected adminRequired for client token, got %#v", env)
	}
}

func TestAdminCollectionEnsureOnEstablishmentCreatesCollection(t *testing.T) {
	ts, eng, issuer := testHarness(t)
	adminToken, _, err := issuer.IssueAdminToken("admin", 0)
	if err != nil {
		t.Fatal(err)
	}

	var schemaObj map[string]any
	if err := json.Unmarshal([]byte(booksSchema), &schemaObj); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"command":   "collectionEnsure",
		"target":    "Books",
		"parameter": "",
		"detail":    map[string]any{"schema": schemaObj},
	})
	resp := doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/command", "application/json", string(body), adminToken)
	env := decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("expected collectionEnsure to succeed: %#v", env)
	}
	if env["created"] != true {
		t.Fatalf("expected created:true, got %#v", env)
	}

	if _, err := os.Stat(filepath.Join(eng.ConfigDir, "Books.schema.json")); err != nil {
		t.Fatalf("expected Books.schema.json on disk: %v", err)
	}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.Cfg.Schemas["Books"]; !ok {
		t.Fatalf("expected Books schema loaded in engine: %#v", eng.Cfg.Schemas)
	}
}

func TestAdminCollectionEnsureOnNonEstablishmentRejected(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".config")
	if err := copyDir(t, "../../testdata/sample-config", configDir); err != nil {
		t.Fatal(err)
	}
	eng := &engine.Engine{ConfigDir: configDir, DataDir: root, ServerName: "serverB"}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	issuer, err := authIssuerFromHarness(t, eng)
	if err != nil {
		t.Fatal(err)
	}
	srv := &HTTPServer{Engine: eng, Issuer: issuer, BootstrapSecret: testBootstrapSecret}
	ts := httptestNewServer(t, srv)

	adminToken, _, err := issuer.IssueAdminToken("admin", 0)
	if err != nil {
		t.Fatal(err)
	}

	var schemaObj map[string]any
	if err := json.Unmarshal([]byte(booksSchema), &schemaObj); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"command":   "collectionEnsure",
		"target":    "Books",
		"parameter": "",
		"detail":    map[string]any{"schema": schemaObj},
	})
	resp := doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/command", "application/json", string(body), adminToken)
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "establishmentRequired" {
		t.Fatalf("expected establishmentRequired on non-establishment server, got %#v", env)
	}
}

func authIssuerFromHarness(t *testing.T, eng *engine.Engine) (*auth.Issuer, error) {
	t.Helper()
	return auth.NewIssuerFromFile(eng.Cfg.Auth, filepath.Join(eng.ConfigDir, "dev-signing-key.pem"))
}

func httptestNewServer(t *testing.T, srv *HTTPServer) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}
