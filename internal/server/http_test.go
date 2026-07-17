package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/engine"
	"github.com/JohnAD/datoriumdb/internal/establish"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const testBootstrapSecret = "test-bootstrap-secret"

func testHarness(t *testing.T) (*httptest.Server, *engine.Engine, *auth.Issuer) {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, ".config")
	if err := copyDir(t, "../../testdata/sample-config", configDir); err != nil {
		t.Fatal(err)
	}
	eng := &engine.Engine{ConfigDir: configDir, DataDir: root, ServerName: "serverA"}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	issuer, err := auth.NewIssuerFromFile(eng.Cfg.Auth, filepath.Join(configDir, "dev-signing-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &HTTPServer{Engine: eng, Issuer: issuer, BootstrapSecret: testBootstrapSecret}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, eng, issuer
}

func copyDir(t *testing.T, src, dst string) error {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		in := filepath.Join(src, e.Name())
		out := filepath.Join(dst, e.Name())
		data, err := os.ReadFile(in)
		if err != nil {
			return err
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func decodeEnvelope(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid JSON envelope: %v\nbody: %s", err, body)
	}
	return out
}

func firstErrCode(t *testing.T, env map[string]any) string {
	t.Helper()
	errs, ok := env["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected errors array, got %#v", env)
	}
	first, _ := errs[0].(map[string]any)
	code, _ := first["code"].(string)
	return code
}

func doReq(t *testing.T, method, url, contentType, body, bearer string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// --- basic contract: HTTP 200 + ok:false application errors ----------------

func TestHealthAndReady(t *testing.T) {
	ts, _, _ := testHarness(t)
	resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/health", "", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	env := decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("expected ok:true: %#v", env)
	}

	resp = doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/ready", "", "", "")
	env = decodeEnvelope(t, resp)
	if env["ok"] != true || env["ready"] != true {
		t.Fatalf("expected ready:true: %#v", env)
	}
}

func TestEverythingIsHTTP200(t *testing.T) {
	ts, _, _ := testHarness(t)
	// Even application-level failures (bad token, bad content-type, etc.)
	// must return HTTP 200 with an ok:false envelope, per
	// tech-docs conventions.
	resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/establish", "", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 even for auth failures, got %d", resp.StatusCode)
	}
	env := decodeEnvelope(t, resp)
	if env["ok"] != false {
		t.Fatalf("expected ok:false without a token: %#v", env)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}
}

// --- establishment endpoint --------------------------------------------------

func TestEstablishReturnsCombinedDocument(t *testing.T) {
	ts, _, issuer := testHarness(t)
	token, _, err := issuer.IssueClientToken("alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/establish", "", "", token)
	env := decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("expected ok:true: %#v", env)
	}
	for _, key := range []string{"general", "servers", "shardMap", "schemas", "searches", "auth"} {
		if _, ok := env[key]; !ok {
			t.Fatalf("expected establishment document to contain %q: %#v", key, env)
		}
	}
	schemas, _ := env["schemas"].(map[string]any)
	if _, ok := schemas["Movies"]; !ok {
		t.Fatalf("expected Movies schema in establishment document: %#v", schemas)
	}
}

func TestEstablishNeverLeaksPrivateKeyMaterial(t *testing.T) {
	ts, _, issuer := testHarness(t)
	token, _, err := issuer.IssueClientToken("alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/establish", "", "", token)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(raw))
	if strings.Contains(upper, "PRIVATE KEY") {
		t.Fatalf("establishment response must never include private key material: %s", raw)
	}
	if strings.Contains(string(raw), `"d":`) {
		t.Fatalf("establishment response must never include a JWK private scalar: %s", raw)
	}
}

func TestEstablishAcceptsMachineToken(t *testing.T) {
	ts, _, issuer := testHarness(t)
	token, _, err := issuer.IssueMachineToken("serverB", 0)
	if err != nil {
		t.Fatal(err)
	}
	resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/establish", "", "", token)
	env := decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("expected machine token to be accepted on /establish: %#v", env)
	}
}

// --- command endpoint: content-type / body-size checks -----------------------

func TestCommandRequiresPlainTextContentType(t *testing.T) {
	ts, _, issuer := testHarness(t)
	token, _, _ := issuer.IssueClientToken("alice", 0)

	resp := doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/command", "application/json", `create Movies null {}`, token)
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "contentTypeRequired" {
		t.Fatalf("expected contentTypeRequired, got %#v", env)
	}

	resp = doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/command", "text/plain; charset=utf-8",
		`create Movies null {$: Movies:0, title: "The Matrix"}`, token)
	env = decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("expected command to succeed: %#v", env)
	}
}

func TestCommandBodyTooLarge(t *testing.T) {
	ts, _, issuer := testHarness(t)
	token, _, _ := issuer.IssueClientToken("alice", 0)
	huge := strings.Repeat("a", maxCommandBodyBytes+1)
	resp := doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/command", "text/plain; charset=utf-8", huge, token)
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "bodyTooLarge" {
		t.Fatalf("expected bodyTooLarge, got %#v", env)
	}
}

func TestCommandWithoutTokenFails(t *testing.T) {
	ts, _, _ := testHarness(t)
	resp := doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/command", "text/plain; charset=utf-8", "create Movies null {}", "")
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "unauthenticated" {
		t.Fatalf("expected unauthenticated, got %#v", env)
	}
}

// --- historic schema retrieval -----------------------------------------------

func TestSchemaHistoryEndpoint(t *testing.T) {
	ts, _, issuer := testHarness(t)
	token, _, _ := issuer.IssueClientToken("alice", 0)

	resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/schema/Movies/0", "", "", token)
	env := decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("expected historic schema lookup to succeed: %#v", env)
	}
	if env["collection"] != "Movies" {
		t.Fatalf("unexpected collection: %#v", env)
	}

	resp = doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/schema/Movies/99", "", "", token)
	env = decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "schemaVersionNotFound" {
		t.Fatalf("expected schemaVersionNotFound, got %#v", env)
	}

	resp = doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/schema/DoesNotExist/0", "", "", token)
	env = decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "collectionNotFound" {
		t.Fatalf("expected collectionNotFound, got %#v", env)
	}

	resp = doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/schema/Movies/notanumber", "", "", token)
	env = decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "invalidRequest" {
		t.Fatalf("expected invalidRequest, got %#v", env)
	}
}

// --- /sys server-to-server endpoint: machine identity binding ---------------

func TestSysPingRequiresMachineToken(t *testing.T) {
	ts, _, issuer := testHarness(t)
	clientToken, _, _ := issuer.IssueClientToken("alice", 0)
	resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/sys/ping/serverA", "", "", clientToken)
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "machineIdentityMismatch" {
		t.Fatalf("expected machineIdentityMismatch for client token, got %#v", env)
	}
}

func TestSysPingRequiresMatchingServerName(t *testing.T) {
	ts, _, issuer := testHarness(t)
	machineToken, _, _ := issuer.IssueMachineToken("serverA", 0)

	resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/sys/ping/serverA", "", "", machineToken)
	env := decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("expected matching machine identity to succeed: %#v", env)
	}

	resp = doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/sys/ping/serverB", "", "", machineToken)
	env = decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "machineIdentityMismatch" {
		t.Fatalf("expected machineIdentityMismatch for wrong server, got %#v", env)
	}
}

// --- machine-token bootstrap / renewal ---------------------------------------

func TestMachineTokenBootstrapAndRenewal(t *testing.T) {
	ts, _, _ := testHarness(t)

	// Bad bootstrap secret is rejected.
	resp := doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/auth/machine-token", "application/json",
		`{"serverName":"serverB","bootstrapSecret":"wrong"}`, "")
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "invalidBootstrapSecret" {
		t.Fatalf("expected invalidBootstrapSecret, got %#v", env)
	}

	// No credential at all is rejected.
	resp = doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/auth/machine-token", "application/json",
		`{"serverName":"serverB"}`, "")
	env = decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "unauthenticated" {
		t.Fatalf("expected unauthenticated, got %#v", env)
	}

	// Missing serverName is rejected.
	resp = doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/auth/machine-token", "application/json",
		`{"bootstrapSecret":"`+testBootstrapSecret+`"}`, "")
	env = decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "invalidRequest" {
		t.Fatalf("expected invalidRequest for missing serverName, got %#v", env)
	}

	// Correct bootstrap secret succeeds and issues a usable machine token.
	resp = doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/auth/machine-token", "application/json",
		`{"serverName":"serverB","bootstrapSecret":"`+testBootstrapSecret+`"}`, "")
	env = decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("expected bootstrap to succeed: %#v", env)
	}
	token, _ := env["token"].(string)
	if token == "" {
		t.Fatalf("expected non-empty token: %#v", env)
	}
	if lifetime, _ := env["expiresIn"].(float64); lifetime <= 0 {
		t.Fatalf("expected positive expiresIn: %#v", env)
	}

	// Renewal via bearer token (no bootstrap secret) succeeds.
	resp = doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/auth/machine-token", "application/json",
		`{"serverName":"serverB"}`, token)
	env = decodeEnvelope(t, resp)
	if env["ok"] != true {
		t.Fatalf("expected renewal via bearer token to succeed: %#v", env)
	}
	renewed, _ := env["token"].(string)
	if renewed == "" {
		t.Fatalf("expected renewed token: %#v", env)
	}

	// Renewal with a machine identity that doesn't match the requested
	// serverName is rejected.
	resp = doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/auth/machine-token", "application/json",
		`{"serverName":"serverC"}`, token)
	env = decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "machineIdentityMismatch" {
		t.Fatalf("expected machineIdentityMismatch for mismatched renewal, got %#v", env)
	}
}

func TestMachineTokenRequiresJSONContentType(t *testing.T) {
	ts, _, _ := testHarness(t)
	resp := doReq(t, http.MethodPost, ts.URL+"/datoriumdb/v1/auth/machine-token", "text/plain",
		`{"serverName":"serverB","bootstrapSecret":"`+testBootstrapSecret+`"}`, "")
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "contentTypeRequired" {
		t.Fatalf("expected contentTypeRequired, got %#v", env)
	}
}

func TestMachineTokenIssuanceUnavailableWithoutSigningKey(t *testing.T) {
	ts, eng, _ := testHarness(t)
	srv := &HTTPServer{Engine: eng, Issuer: nil, BootstrapSecret: testBootstrapSecret}
	noKeyTS := httptest.NewServer(srv.Handler())
	defer noKeyTS.Close()
	_ = ts

	resp := doReq(t, http.MethodPost, noKeyTS.URL+"/datoriumdb/v1/auth/machine-token", "application/json",
		`{"serverName":"serverB","bootstrapSecret":"`+testBootstrapSecret+`"}`, "")
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "machineTokenIssuanceUnavailable" {
		t.Fatalf("expected machineTokenIssuanceUnavailable, got %#v", env)
	}
}

// --- auth error matrix --------------------------------------------------------

func TestAuthErrorMatrix(t *testing.T) {
	ts, eng, issuer := testHarness(t)

	t.Run("missing token", func(t *testing.T) {
		resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/establish", "", "", "")
		env := decodeEnvelope(t, resp)
		if firstErrCode(t, env) != "unauthenticated" {
			t.Fatalf("got %#v", env)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		expiredIssuer, err := auth.NewIssuerFromFile(eng.Cfg.Auth, "../../testdata/sample-config/dev-signing-key.pem")
		if err != nil {
			t.Fatal(err)
		}
		expiredIssuer.Now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
		token, _, err := expiredIssuer.IssueClientToken("alice", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/establish", "", "", token)
		env := decodeEnvelope(t, resp)
		if firstErrCode(t, env) != "tokenExpired" {
			t.Fatalf("got %#v", env)
		}
	})

	t.Run("wrong issuer", func(t *testing.T) {
		badCfg := eng.Cfg.Auth
		badCfg.Auth.Issuer = "https://not-trusted.test"
		priv := testSigningPrivateKey(t)
		badIssuer, err := auth.NewIssuer(badCfg, priv)
		if err != nil {
			t.Fatal(err)
		}
		token, _, err := badIssuer.IssueClientToken("alice", 0)
		if err != nil {
			t.Fatal(err)
		}
		resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/establish", "", "", token)
		env := decodeEnvelope(t, resp)
		if firstErrCode(t, env) != "invalidToken" {
			t.Fatalf("got %#v", env)
		}
	})

	t.Run("wrong audience", func(t *testing.T) {
		badCfg := eng.Cfg.Auth
		badCfg.Auth.Audience = "someone-else"
		priv := testSigningPrivateKey(t)
		badIssuer, err := auth.NewIssuer(badCfg, priv)
		if err != nil {
			t.Fatal(err)
		}
		token, _, err := badIssuer.IssueClientToken("alice", 0)
		if err != nil {
			t.Fatal(err)
		}
		resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/establish", "", "", token)
		env := decodeEnvelope(t, resp)
		if firstErrCode(t, env) != "invalidToken" {
			t.Fatalf("got %#v", env)
		}
	})

	t.Run("unknown kid", func(t *testing.T) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		token := craftRawToken(t, priv, "unknown-kid", eng.Cfg.Auth.Auth.Issuer, eng.Cfg.Auth.Auth.Audience,
			map[string]any{auth.ClaimKind: string(auth.KindClient)}, "alice", time.Hour)
		resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/establish", "", "", token)
		env := decodeEnvelope(t, resp)
		if firstErrCode(t, env) != "invalidToken" {
			t.Fatalf("got %#v", env)
		}
	})

	t.Run("client token on sys endpoint", func(t *testing.T) {
		token, _, err := issuer.IssueClientToken("alice", 0)
		if err != nil {
			t.Fatal(err)
		}
		resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/sys/ping/serverA", "", "", token)
		env := decodeEnvelope(t, resp)
		if firstErrCode(t, env) != "machineIdentityMismatch" {
			t.Fatalf("got %#v", env)
		}
	})

	t.Run("machine name mismatch", func(t *testing.T) {
		token, _, err := issuer.IssueMachineToken("serverA", 0)
		if err != nil {
			t.Fatal(err)
		}
		resp := doReq(t, http.MethodGet, ts.URL+"/datoriumdb/v1/sys/ping/serverZ", "", "", token)
		env := decodeEnvelope(t, resp)
		if firstErrCode(t, env) != "machineIdentityMismatch" {
			t.Fatalf("got %#v", env)
		}
	})
}

func testSigningPrivateKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/sample-config/dev-signing-key.pem")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("dev-signing-key.pem is not PEM encoded")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		t.Fatal("dev-signing-key.pem does not contain an Ed25519 private key")
	}
	return priv
}

func craftRawToken(t *testing.T, priv ed25519.PrivateKey, kid, issuer, audience string, extra map[string]any, subject string, lifetime time.Duration) string {
	t.Helper()
	now := time.Now()
	b := jwt.NewBuilder().
		Issuer(issuer).
		Audience([]string{audience}).
		Subject(subject).
		IssuedAt(now).
		Expiration(now.Add(lifetime))
	for k, v := range extra {
		b = b.Claim(k, v)
	}
	tok, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	key, err := jwk.Import(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := key.Set(jwk.KeyIDKey, kid); err != nil {
		t.Fatal(err)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.EdDSA(), key))
	if err != nil {
		t.Fatal(err)
	}
	return string(signed)
}

// --- gate: isolated worker bootstraps with no local config ------------------

func TestGateWorkerBootstrapsWithoutLocalConfigAndRenews(t *testing.T) {
	ts, _, _ := testHarness(t)

	workerRoot := t.TempDir()
	configDir := filepath.Join(workerRoot, ".config")
	dataDir := filepath.Join(workerRoot, "data")

	if establish.HasLocalConfig(configDir) {
		t.Fatalf("expected no local config to exist yet in a fresh temp dir")
	}

	worker := &establish.Worker{
		ServerName:       "serverB",
		EstablishmentURL: ts.URL,
		BootstrapSecret:  testBootstrapSecret,
		ConfigDir:        configDir,
		DataDir:          dataDir,
		RefreshInterval:  10 * time.Millisecond,
		RenewSkew:        50 * time.Millisecond,
	}

	if err := worker.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if !establish.HasLocalConfig(configDir) {
		t.Fatalf("expected bootstrap to cache a usable local config")
	}
	cfg, err := config.Load(configDir)
	if err != nil {
		t.Fatalf("cached config failed full validation: %v", err)
	}
	if cfg.General.General.EstablishmentServer != "serverA" {
		t.Fatalf("unexpected cached general config: %#v", cfg.General)
	}
	if _, ok := cfg.Schemas["Movies"]; !ok {
		t.Fatalf("expected Movies schema to be cached: %#v", cfg.Schemas)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "Movies")); err != nil {
		t.Fatalf("expected Movies collection directory to be created: %v", err)
	}

	// RefreshOnce again exercises the bearer-token renewal path (a
	// still-valid token stands in for the bootstrap secret).
	if err := worker.RefreshOnce(context.Background()); err != nil {
		t.Fatalf("second refresh (renewal) failed: %v", err)
	}
}

func TestGateWorkerFailsCleanlyWithBadSecretAndNoCache(t *testing.T) {
	ts, _, _ := testHarness(t)
	workerRoot := t.TempDir()
	worker := &establish.Worker{
		ServerName:       "serverB",
		EstablishmentURL: ts.URL,
		BootstrapSecret:  "totally-wrong",
		ConfigDir:        filepath.Join(workerRoot, ".config"),
		DataDir:          filepath.Join(workerRoot, "data"),
	}
	if err := worker.Bootstrap(context.Background()); err == nil {
		t.Fatalf("expected bootstrap to fail with a bad secret and no cached config")
	}
}
