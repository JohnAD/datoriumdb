package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

const httpSearchTestByStatusDef = `{
  "$": "SearchDefinition:v1",
  "collection": "Movies",
  "name": "byStatus",
  "version": 1,
  "v1": {
    "clauses": [{"field": "/status", "op": "equals", "value": "$status"}],
    "sort": [{"field": "/!", "dir": "asc"}]
  }
}`

func TestApplySearchResultWriteAuthSemantics(t *testing.T) {
	ts, eng, issuer := testHarness(t)
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "Movies.search.byStatus.json"), []byte(httpSearchTestByStatusDef), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}

	body := func(target string) string {
		b, _ := json.Marshal(map[string]any{
			"targetServer": target,
			"item": map[string]any{
				"collection": "Movies",
				"search":     "byStatus",
				"segments":   []string{"72656C6561736564"},
				"operation":  "upsert",
				"id":         "id1",
			},
		})
		return string(b)
	}

	// A client (non-machine) token must be rejected.
	clientTok, _, _ := issuer.IssueClientToken("alice", 0)
	resp := doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/sys/apply-search-result-write", "application/json", body("serverA"), clientTok)
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "machineIdentityMismatch" {
		t.Fatalf("expected machineIdentityMismatch for a client token, got %#v", env)
	}

	// A machine token authenticating as a *different* server than the
	// caller's own identity must still be accepted (push semantics: any
	// machine can push, targetServer just names the intended recipient).
	machineTokAsServerB, _, _ := issuer.IssueMachineToken("serverB", 0)
	resp = doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/sys/apply-search-result-write", "application/json", body("serverA"), machineTokAsServerB)
	env = decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("expected a push from a different machine identity to succeed, got %#v", env)
	}

	// A delivery addressed to a server other than the one actually
	// receiving it must fail with targetServerMismatch.
	resp = doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/sys/apply-search-result-write", "application/json", body("serverC"), machineTokAsServerB)
	env = decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "targetServerMismatch" {
		t.Fatalf("expected targetServerMismatch, got %#v", env)
	}
}

func TestApplySearchResultWriteUpsertAndRemove(t *testing.T) {
	ts, eng, issuer := testHarness(t)
	if err := os.WriteFile(filepath.Join(eng.ConfigDir, "Movies.search.byStatus.json"), []byte(httpSearchTestByStatusDef), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	machineTok, _, _ := issuer.IssueMachineToken("serverB", 0)

	upsertBody, _ := json.Marshal(map[string]any{
		"targetServer": "serverA",
		"item": map[string]any{
			"collection": "Movies",
			"search":     "byStatus",
			"segments":   []string{"72656C6561736564"},
			"operation":  "upsert",
			"id":         "id1",
			"sort":       []any{"id1"},
		},
	})
	resp := doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/sys/apply-search-result-write", "application/json", string(upsertBody), machineTok)
	env := decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("upsert failed: %#v", env)
	}
	path := fsstore.SearchResultPath(eng.DataDir, "Movies", "byStatus", []string{"72656C6561736564"})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected matches.json to be created: %v", err)
	}

	removeBody, _ := json.Marshal(map[string]any{
		"targetServer": "serverA",
		"item": map[string]any{
			"collection": "Movies",
			"search":     "byStatus",
			"segments":   []string{"72656C6561736564"},
			"operation":  "remove",
			"id":         "id1",
		},
	})
	resp = doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/sys/apply-search-result-write", "application/json", string(removeBody), machineTok)
	env = decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("remove failed: %#v", env)
	}
}

func TestApplySearchResultWriteUnknownCollectionOrSearch(t *testing.T) {
	ts, _, issuer := testHarness(t)
	machineTok, _, _ := issuer.IssueMachineToken("serverB", 0)
	body, _ := json.Marshal(map[string]any{
		"targetServer": "serverA",
		"item": map[string]any{
			"collection": "NoSuchCollection",
			"search":     "byStatus",
			"segments":   []string{"x"},
			"operation":  "upsert",
			"id":         "id1",
		},
	})
	resp := doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/sys/apply-search-result-write", "application/json", string(body), machineTok)
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "collectionNotFound" {
		t.Fatalf("expected collectionNotFound, got %#v", env)
	}
}
