package ctl

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectionCreateDryRunDoesNotPOST(t *testing.T) {
	dir := t.TempDir()
	schemaFile := filepath.Join(dir, "Books.schema.json")
	if err := os.WriteFile(schemaFile, []byte(`{"kind":"object","children":[{"name":"title","kind":"string","required":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(dir, ".config")
	if err := copySampleConfigMinimal(t, configDir); err != nil {
		t.Fatal(err)
	}

	posts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--config-dir", configDir,
		"--establishment-url", ts.URL,
		"--admin-token", "fake-token",
		"--dry-run",
		"collection", "create", "Books", schemaFile,
	}, nil, &stdout, &stderr)
	if code != ExitDryRunComplete {
		t.Fatalf("expected dry-run exit code %d, got %d stderr=%s stdout=%s", ExitDryRunComplete, code, stderr.String(), stdout.String())
	}
	if posts != 0 {
		t.Fatalf("expected no HTTP POST on dry-run, got %d", posts)
	}
	if !strings.Contains(stdout.String(), "dryRun") {
		t.Fatalf("expected dryRun in output, got: %s", stdout.String())
	}
}

func TestCollectionCreatePOSTsToEstablishment(t *testing.T) {
	dir := t.TempDir()
	schemaFile := filepath.Join(dir, "Books.schema.json")
	if err := os.WriteFile(schemaFile, []byte(`{"kind":"object","children":[{"name":"title","kind":"string","required":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(dir, ".config")
	if err := copySampleConfigMinimal(t, configDir); err != nil {
		t.Fatal(err)
	}

	var gotAuth string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":         true,
			"command":    "collectionEnsure",
			"collection": "Books",
			"created":    true,
		})
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--config-dir", configDir,
		"--establishment-url", ts.URL,
		"--admin-token", "admin-jwt",
		"--json",
		"collection", "create", "Books", schemaFile,
	}, nil, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if gotAuth != "Bearer admin-jwt" {
		t.Fatalf("expected Bearer admin-jwt, got %q", gotAuth)
	}
	if gotBody["command"] != "collectionEnsure" || gotBody["target"] != "Books" {
		t.Fatalf("unexpected POST body: %#v", gotBody)
	}
	detail, _ := gotBody["detail"].(map[string]any)
	if detail["schema"] == nil {
		t.Fatalf("expected schema in detail: %#v", gotBody)
	}
}

func TestCollectionCreateRequiresEstablishmentURL(t *testing.T) {
	dir := t.TempDir()
	schemaFile := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaFile, []byte(`{"kind":"object","children":[{"name":"title","kind":"string","required":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(dir, ".config")
	if err := copySampleConfigMinimal(t, configDir); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	code := Run([]string{
		"--config-dir", configDir,
		"--admin-token", "tok",
		"collection", "create", "Books", schemaFile,
	}, nil, new(bytes.Buffer), &stderr)
	if code != ExitValidation {
		t.Fatalf("expected validation exit, got %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "establishmentURLRequired") {
		t.Fatalf("expected establishmentURLRequired, got: %s", stderr.String())
	}
}

func copySampleConfigMinimal(t *testing.T, configDir string) error {
	t.Helper()
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	for name, body := range map[string]string{
		"__general.json":   `{"general":{"name":"x","establishmentServer":"serverA","version":1}}`,
		"__servers.json":   `{"servers":{"serverA":{"baseURL":"http://127.0.0.1:1"}}}`,
		"__shard-map.json": `{"shardMap":{"default":{}}}`,
		"__auth.json":      `{"auth":{"issuer":"iss","audience":"aud","tokenLifetimeSeconds":{"client":3600,"machine":3600},"keys":[]}}`,
	} {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}
