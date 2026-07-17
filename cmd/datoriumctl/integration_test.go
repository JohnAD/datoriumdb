package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// binPath is the path to the datoriumctl binary built once in TestMain.
var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "datoriumctl-bin-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	binPath = filepath.Join(dir, "datoriumctl")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = mustWD()
	if out, err := build.CombinedOutput(); err != nil {
		panic("failed to build datoriumctl: " + err.Error() + "\n" + string(out))
	}

	os.Exit(m.Run())
}

func mustWD() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
}

// repoRoot locates the repository root (two levels up from cmd/datoriumctl).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(filepath.Dir(wd))
}

// freshConfigDir copies testdata/sample-config into a new temp directory so
// each test can mutate it without affecting other tests.
func freshConfigDir(t *testing.T) string {
	t.Helper()
	src := filepath.Join(repoRoot(t), "testdata", "sample-config")
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

type cliResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// envelope decodes Stdout as a JSON application envelope. Callers must have
// passed --json.
func (r cliResult) envelope(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &m); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s\nstderr: %s", err, r.Stdout, r.Stderr)
	}
	return m
}

func runCLI(t *testing.T, configDir string, extraEnv []string, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	if configDir != "" {
		hasConfigDir := false
		for _, a := range args {
			if a == "--config-dir" {
				hasConfigDir = true
				break
			}
		}
		if !hasConfigDir {
			cmd.Args = append(cmd.Args, "--config-dir", configDir)
		}
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader("")
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run CLI: %v", err)
		}
	}
	return cliResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code}
}

func listConfigFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// --- config ---

func TestCLIConfigValidateSuccess(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "config", "validate")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	env := res.envelope(t)
	if env["ok"] != true {
		t.Fatalf("expected ok:true, got %#v", env)
	}
	if _, ok := env["generalVersion"]; !ok {
		t.Fatalf("expected generalVersion in envelope, got %#v", env)
	}
}

func TestCLIConfigValidateFailureUnknownEstablishmentServer(t *testing.T) {
	dir := freshConfigDir(t)
	generalPath := filepath.Join(dir, "__general.json")
	data, err := os.ReadFile(generalPath)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(data), "serverA", "serverGhost", 1)
	if err := os.WriteFile(generalPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "config", "validate")
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if env["ok"] != false {
		t.Fatalf("expected ok:false, got %#v", env)
	}
	if !envelopeHasErrorCode(t, env, "serverNotFound") {
		t.Fatalf("expected serverNotFound error, got %#v", env)
	}
}

func TestCLIConfigShow(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "config", "show")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if env["name"] != "DatoriumDB Local" {
		t.Fatalf("expected database name, got %#v", env)
	}
	if env["shardMapComplete"] != true {
		t.Fatalf("expected shardMapComplete true, got %#v", env)
	}
}

// --- collection ---

func TestCLICollectionCreateSuccess(t *testing.T) {
	dir := freshConfigDir(t)
	dataDir := t.TempDir()
	schemaFile := filepath.Join(t.TempDir(), "People.schema.json")
	if err := os.WriteFile(schemaFile, []byte(`{"kind":"object","children":[{"name":"name","kind":"string"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "--data-dir", dataDir, "collection", "create", "People", schemaFile)
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	env := res.envelope(t)
	if env["schemaVersion"] != float64(0) {
		t.Fatalf("expected schemaVersion 0, got %#v", env)
	}
	if !fileExists(filepath.Join(dir, "People.schema.json")) {
		t.Fatal("expected People.schema.json to be written")
	}
	if !fileExists(filepath.Join(dir, "People.schema.0.json")) {
		t.Fatal("expected People.schema.0.json to be written")
	}
	if !fileExists(filepath.Join(dataDir, "People")) {
		t.Fatal("expected data directory People to be created")
	}
	generalRaw, err := os.ReadFile(filepath.Join(dir, "__general.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generalRaw), `"version": 2`) {
		t.Fatalf("expected general.version bumped to 2, got: %s", generalRaw)
	}
}

func TestCLICollectionCreateAlreadyExists(t *testing.T) {
	dir := freshConfigDir(t)
	schemaFile := filepath.Join(t.TempDir(), "Movies.schema.json")
	if err := os.WriteFile(schemaFile, []byte(`{"kind":"object","children":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "collection", "create", "Movies", schemaFile)
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if !envelopeHasErrorCode(t, env, "collectionAlreadyExists") {
		t.Fatalf("expected collectionAlreadyExists, got %#v", env)
	}
}

func TestCLICollectionCreateInvalidSchemaRootKind(t *testing.T) {
	dir := freshConfigDir(t)
	schemaFile := filepath.Join(t.TempDir(), "Bad.schema.json")
	if err := os.WriteFile(schemaFile, []byte(`{"kind":"string"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "collection", "create", "Bad", schemaFile)
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	if fileExists(filepath.Join(dir, "Bad.schema.json")) {
		t.Fatal("expected no schema file written on failure")
	}
}

func TestCLICollectionCreateDryRunWritesNothing(t *testing.T) {
	dir := freshConfigDir(t)
	before := listConfigFiles(t, dir)
	schemaFile := filepath.Join(t.TempDir(), "Books.schema.json")
	if err := os.WriteFile(schemaFile, []byte(`{"kind":"object","children":[{"name":"title","kind":"string"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "--dry-run", "collection", "create", "Books", schemaFile)
	if res.ExitCode != 3 {
		t.Fatalf("expected exit 3 for dry-run success, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	env := res.envelope(t)
	if env["dryRun"] != true {
		t.Fatalf("expected dryRun:true, got %#v", env)
	}
	after := listConfigFiles(t, dir)
	if len(after) != len(before) {
		t.Fatalf("expected no new files after dry-run, before=%v after=%v", before, after)
	}
	if fileExists(filepath.Join(dir, "Books.schema.json")) {
		t.Fatal("dry-run must not write Books.schema.json")
	}
}

func TestCLICollectionUpgradeSuccess(t *testing.T) {
	dir := freshConfigDir(t)
	upgradeFile := filepath.Join(t.TempDir(), "upgrade.json")
	upgrade := `{
		"from": 0,
		"new_ver_id": "01KWHM7R7D3T50G0GH6XN4CRZT",
		"updates": [
			{"op": "add", "path": "/rating", "value": 0, "schema": {"kind": "number", "default": 0}}
		]
	}`
	if err := os.WriteFile(upgradeFile, []byte(upgrade), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "collection", "upgrade", "Movies", upgradeFile)
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	env := res.envelope(t)
	if env["toVersion"] != float64(1) {
		t.Fatalf("expected toVersion 1, got %#v", env)
	}
	if !fileExists(filepath.Join(dir, "Movies.schema.1.json")) {
		t.Fatal("expected Movies.schema.1.json to be written")
	}
	if !fileExists(filepath.Join(dir, "Movies.schema.0.json")) {
		t.Fatal("expected old Movies.schema.0.json history file to remain")
	}
	current, err := os.ReadFile(filepath.Join(dir, "Movies.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "rating") {
		t.Fatalf("expected current schema to include new field, got: %s", current)
	}
}

func TestCLICollectionUpgradeStaleVersion(t *testing.T) {
	dir := freshConfigDir(t)
	upgradeFile := filepath.Join(t.TempDir(), "upgrade.json")
	upgrade := `{
		"from": 5,
		"new_ver_id": "01KWHM7R7D3T50G0GH6XN4CRZT",
		"updates": [{"op": "remove", "path": "/status"}]
	}`
	if err := os.WriteFile(upgradeFile, []byte(upgrade), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "collection", "upgrade", "Movies", upgradeFile)
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if !envelopeHasErrorCode(t, env, "staleSchemaVersion") {
		t.Fatalf("expected staleSchemaVersion, got %#v", env)
	}
}

func TestCLICollectionList(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "collection", "list")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	collections, ok := env["collections"].([]any)
	if !ok || len(collections) == 0 {
		t.Fatalf("expected non-empty collections list, got %#v", env)
	}
}

func TestCLICollectionShow(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "collection", "show", "Movies")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if env["version"] != float64(0) {
		t.Fatalf("expected version 0, got %#v", env)
	}
}

func TestCLICollectionShowUnknownCollection(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "collection", "show", "Ghosts")
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if !envelopeHasErrorCode(t, env, "collectionNotFound") {
		t.Fatalf("expected collectionNotFound, got %#v", env)
	}
}

// --- server ---

func TestCLIServerListAndSet(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "server", "list")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}

	res = runCLI(t, dir, nil, "--json", "server", "set", "serverB", "--base-url", "https://serverb.example.com")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	serversRaw, err := os.ReadFile(filepath.Join(dir, "__servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(serversRaw), "serverb.example.com") {
		t.Fatalf("expected serverB to be written, got: %s", serversRaw)
	}
}

func TestCLIServerSetInvalidBaseURL(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "server", "set", "serverB", "--base-url", "not-a-url")
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if !envelopeHasErrorCode(t, env, "invalidBaseURL") {
		t.Fatalf("expected invalidBaseURL, got %#v", env)
	}
}

func TestCLIServerRemoveStillReferenced(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "--yes", "server", "remove", "serverA")
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if !envelopeHasErrorCode(t, env, "serverStillReferenced") {
		t.Fatalf("expected serverStillReferenced, got %#v", env)
	}
}

func TestCLIServerRemoveSuccess(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "server", "set", "serverB", "--base-url", "https://serverb.example.com")
	if res.ExitCode != 0 {
		t.Fatalf("failed to add serverB: %d %s", res.ExitCode, res.Stdout)
	}
	res = runCLI(t, dir, nil, "--json", "--yes", "server", "remove", "serverB")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	serversRaw, err := os.ReadFile(filepath.Join(dir, "__servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serversRaw), "serverb.example.com") {
		t.Fatalf("expected serverB removed, got: %s", serversRaw)
	}
}

func TestCLIServerRemoveWithoutYesIsCancelled(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "server", "set", "serverB", "--base-url", "https://serverb.example.com")
	if res.ExitCode != 0 {
		t.Fatalf("failed to add serverB: %d %s", res.ExitCode, res.Stdout)
	}
	res = runCLI(t, dir, nil, "--json", "server", "remove", "serverB")
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1 (cancelled without --yes), got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	serversRaw, err := os.ReadFile(filepath.Join(dir, "__servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(serversRaw), "serverb.example.com") {
		t.Fatal("expected serverB to remain since removal was cancelled")
	}
}

// --- shard-map ---

func TestCLIShardMapShow(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "shard-map", "show")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
}

func TestCLIShardMapSetIncompleteCoverage(t *testing.T) {
	dir := freshConfigDir(t)
	shardMapFile := filepath.Join(t.TempDir(), "shard-map.json")
	body := `{
		"shardMap": {
			"default": {
				"00-7E": {"SHARD_SOT_MEMBER": "serverA", "SHARD_READ_MEMBER": [], "PROXY_READ_MEMBER": []}
			}
		}
	}`
	if err := os.WriteFile(shardMapFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "shard-map", "set", shardMapFile)
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if !envelopeHasErrorCode(t, env, "incompleteShardMap") {
		t.Fatalf("expected incompleteShardMap, got %#v", env)
	}
}

func TestCLIShardMapSetSuccess(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "server", "set", "serverB", "--base-url", "https://serverb.example.com")
	if res.ExitCode != 0 {
		t.Fatalf("failed to add serverB: %d %s", res.ExitCode, res.Stdout)
	}
	shardMapFile := filepath.Join(t.TempDir(), "shard-map.json")
	body := `{
		"shardMap": {
			"default": {
				"00-7F": {"SHARD_SOT_MEMBER": "serverA", "SHARD_READ_MEMBER": ["serverA"], "PROXY_READ_MEMBER": []},
				"80-FF": {"SHARD_SOT_MEMBER": "serverB", "SHARD_READ_MEMBER": ["serverB"], "PROXY_READ_MEMBER": []}
			}
		}
	}`
	if err := os.WriteFile(shardMapFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res = runCLI(t, dir, nil, "--json", "shard-map", "set", shardMapFile)
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	shardMapRaw, err := os.ReadFile(filepath.Join(dir, "__shard-map.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shardMapRaw), "80-FF") {
		t.Fatalf("expected new shard map written, got: %s", shardMapRaw)
	}
}

func TestCLIShardMapSetUnsupportedCollectionsFeature(t *testing.T) {
	dir := freshConfigDir(t)
	shardMapFile := filepath.Join(t.TempDir(), "shard-map.json")
	body := `{
		"shardMap": {
			"default": {"00-FF": {"SHARD_SOT_MEMBER": "serverA", "SHARD_READ_MEMBER": [], "PROXY_READ_MEMBER": []}},
			"collections": {}
		}
	}`
	if err := os.WriteFile(shardMapFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "shard-map", "set", shardMapFile)
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if !envelopeHasErrorCode(t, env, "unsupportedShardMapFeature") {
		t.Fatalf("expected unsupportedShardMapFeature, got %#v", env)
	}
}

// --- general ---

func TestCLIGeneralSetSuccess(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "general", "set", "--name", "New Name", "--read-member-checkin-seconds", "20")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	generalRaw, err := os.ReadFile(filepath.Join(dir, "__general.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generalRaw), "New Name") {
		t.Fatalf("expected updated name, got: %s", generalRaw)
	}
	if !strings.Contains(string(generalRaw), `"version": 2`) {
		t.Fatalf("expected version bumped to 2, got: %s", generalRaw)
	}
}

func TestCLIGeneralSetInvalidNumeric(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "general", "set", "--read-member-checkin-seconds", "-5")
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
}

func TestCLIGeneralSetDryRunWritesNothing(t *testing.T) {
	dir := freshConfigDir(t)
	before, err := os.ReadFile(filepath.Join(dir, "__general.json"))
	if err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "--dry-run", "general", "set", "--name", "Should Not Persist")
	if res.ExitCode != 3 {
		t.Fatalf("expected exit 3, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	after, err := os.ReadFile(filepath.Join(dir, "__general.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("expected __general.json unchanged after dry-run:\nbefore: %s\nafter: %s", before, after)
	}
}

// --- auth ---

func TestCLIAuthShow(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "auth", "show")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if env["issuer"] == "" || env["issuer"] == nil {
		t.Fatalf("expected issuer present, got %#v", env)
	}
	if strings.Contains(res.Stdout, "PRIVATE KEY") {
		t.Fatal("auth show must never print private key material")
	}
}

func TestCLIAuthKeyAddAndRetire(t *testing.T) {
	dir := freshConfigDir(t)
	keyFile := filepath.Join(t.TempDir(), "new-pub.key")
	// A syntactically valid base64-encoded 32-byte Ed25519 public key.
	pub := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	if err := os.WriteFile(keyFile, []byte(pub), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "auth", "key", "add", "--kid", "dev-new", "--alg", "EdDSA", "--public-key-file", keyFile, "--status", "active")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	authRaw, err := os.ReadFile(filepath.Join(dir, "__auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(authRaw), "dev-new") {
		t.Fatalf("expected new key written, got: %s", authRaw)
	}

	// Retiring dev-primary now should succeed since dev-new is also active.
	res = runCLI(t, dir, nil, "--json", "auth", "key", "retire", "dev-primary")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0 retiring dev-primary, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	// Retiring dev-new now should fail: it is the only active key left.
	res = runCLI(t, dir, nil, "--json", "auth", "key", "retire", "dev-new")
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1 (would leave zero active keys), got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if !envelopeHasErrorCode(t, env, "noActiveSigningKey") {
		t.Fatalf("expected noActiveSigningKey, got %#v", env)
	}
}

func TestCLIAuthKeyAddRejectsPrivateKeyMaterial(t *testing.T) {
	dir := freshConfigDir(t)
	keyFile := filepath.Join(t.TempDir(), "sneaky.pem")
	if err := os.WriteFile(keyFile, []byte("-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "auth", "key", "add", "--kid", "sneaky", "--alg", "EdDSA", "--public-key-file", keyFile)
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if !envelopeHasErrorCode(t, env, "privateKeyRejected") {
		t.Fatalf("expected privateKeyRejected, got %#v", env)
	}
}

func TestCLIAuthTokenIssueClient(t *testing.T) {
	dir := freshConfigDir(t)
	keyFile := filepath.Join(dir, "dev-signing-key.pem")
	if !fileExists(keyFile) {
		t.Skip("dev-signing-key.pem fixture not present in sample-config")
	}
	res := runCLI(t, dir, []string{"DATORIUMDB_SIGNING_KEY_FILE=" + keyFile}, "--json", "auth", "token", "issue", "--kind", "client", "--subject", "test-user")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	env := res.envelope(t)
	token, _ := env["token"].(string)
	if token == "" {
		t.Fatalf("expected non-empty token, got %#v", env)
	}
	if strings.Count(token, ".") != 2 {
		t.Fatalf("expected a JWT with 3 segments, got %q", token)
	}
	// Token issuance must not write config files or bump general.version.
	generalRaw, err := os.ReadFile(filepath.Join(dir, "__general.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generalRaw), `"version": 1`) {
		t.Fatalf("expected general.version to stay at 1, got: %s", generalRaw)
	}
}

func TestCLIAuthTokenIssueMachineRequiresServerName(t *testing.T) {
	dir := freshConfigDir(t)
	keyFile := filepath.Join(dir, "dev-signing-key.pem")
	if !fileExists(keyFile) {
		t.Skip("dev-signing-key.pem fixture not present in sample-config")
	}
	res := runCLI(t, dir, []string{"DATORIUMDB_SIGNING_KEY_FILE=" + keyFile}, "--json", "auth", "token", "issue", "--kind", "machine")
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
}

// --- search ---

func TestCLISearchCreateListDelete(t *testing.T) {
	dir := freshConfigDir(t)
	dataDir := t.TempDir()
	defFile := filepath.Join(t.TempDir(), "search-def.json")
	body := `{
		"$": "SearchDefinition:v1",
		"collection": "Movies",
		"name": "byStatus",
		"version": 1,
		"v1": {
			"clauses": [{"field": "/status", "op": "equals", "value": "active"}],
			"sort": [{"field": "/!", "dir": "asc"}]
		}
	}`
	if err := os.WriteFile(defFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "--data-dir", dataDir, "search", "create", "Movies", "byStatus", defFile)
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	if !fileExists(filepath.Join(dir, "Movies.search.byStatus.json")) {
		t.Fatal("expected Movies.search.byStatus.json to be written")
	}
	if !fileExists(filepath.Join(dir, "Movies.search.byStatus.1.json")) {
		t.Fatal("expected Movies.search.byStatus.1.json to be written")
	}
	if !fileExists(filepath.Join(dataDir, "Movies", ".search", "byStatus")) {
		t.Fatal("expected search result directory to be created")
	}

	res = runCLI(t, dir, nil, "--json", "search", "list")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	searches, ok := env["searches"].([]any)
	if !ok || len(searches) == 0 {
		t.Fatalf("expected non-empty searches list, got %#v", env)
	}

	res = runCLI(t, dir, nil, "--json", "--yes", "search", "delete", "Movies", "byStatus")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	if fileExists(filepath.Join(dir, "Movies.search.byStatus.json")) {
		t.Fatal("expected current search definition file removed")
	}
	if !fileExists(filepath.Join(dir, "Movies.search.byStatus.1.json")) {
		t.Fatal("expected historic search definition version to remain")
	}
}

func TestCLISearchCreateAlreadyExists(t *testing.T) {
	dir := freshConfigDir(t)
	dataDir := t.TempDir()
	defFile := filepath.Join(t.TempDir(), "search-def.json")
	body := `{
		"$": "SearchDefinition:v1",
		"collection": "Movies",
		"name": "dup",
		"version": 1,
		"v1": {"clauses": [{"field": "/status", "op": "equals", "value": "active"}]}
	}`
	if err := os.WriteFile(defFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "--data-dir", dataDir, "search", "create", "Movies", "dup", defFile)
	if res.ExitCode != 0 {
		t.Fatalf("first create should succeed, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	res = runCLI(t, dir, nil, "--json", "--data-dir", dataDir, "search", "create", "Movies", "dup", defFile)
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1 on duplicate create, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if !envelopeHasErrorCode(t, env, "searchAlreadyExists") {
		t.Fatalf("expected searchAlreadyExists, got %#v", env)
	}
}

// --- locking ---

func TestCLILockPreventsConcurrentMutation(t *testing.T) {
	dir := freshConfigDir(t)
	lockPath := filepath.Join(dir, ".datoriumctl.lock")
	if err := os.WriteFile(lockPath, []byte("99999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, dir, nil, "--json", "server", "set", "serverB", "--base-url", "https://serverb.example.com")
	if res.ExitCode != 2 {
		t.Fatalf("expected exit 2 (runtime failure) when lock is held, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	env := res.envelope(t)
	if !envelopeHasErrorCode(t, env, "configLockHeld") {
		t.Fatalf("expected configLockHeld, got %#v", env)
	}
	serversRaw, err := os.ReadFile(filepath.Join(dir, "__servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serversRaw), "serverb.example.com") {
		t.Fatal("expected no mutation while lock is held")
	}
}

func TestCLILockReleasedAfterMutation(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "server", "set", "serverB", "--base-url", "https://serverb.example.com")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
	if fileExists(filepath.Join(dir, ".datoriumctl.lock")) {
		t.Fatal("expected lock file removed after successful mutation")
	}
}

// --- runtime / filesystem errors ---

func TestCLIRuntimeFailureMissingConfigDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	res := runCLI(t, dir, nil, "--json", "config", "validate")
	if res.ExitCode != 2 {
		t.Fatalf("expected exit 2 for missing config dir, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
}

func TestCLIUnknownCommand(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "--json", "frobnicate")
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", res.ExitCode, res.Stdout)
	}
}

// --- human-readable output (non-JSON) ---

func TestCLIHumanOutputConfigValidate(t *testing.T) {
	dir := freshConfigDir(t)
	res := runCLI(t, dir, nil, "config", "validate")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "config is valid") {
		t.Fatalf("expected human-readable success message, got: %s", res.Stdout)
	}
}

func envelopeHasErrorCode(t *testing.T, env map[string]any, code string) bool {
	t.Helper()
	errsRaw, ok := env["errors"].([]any)
	if !ok {
		return false
	}
	for _, e := range errsRaw {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if m["code"] == code {
			return true
		}
	}
	return false
}

var _ io.Writer = (*strings.Builder)(nil)
