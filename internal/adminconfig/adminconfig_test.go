package adminconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/envelope"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

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

func hasCode(errs []envelope.Error, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}

const validULID = "01KWHM7R7D3T50G0GH6XN4CRZT"

func TestEnsureCollectionCreate(t *testing.T) {
	configDir := freshConfigDir(t)
	dataDir := t.TempDir()
	store := &Store{ConfigDir: configDir, DataDir: dataDir}

	schema := []byte(`{"kind":"object","children":[{"name":"name","kind":"string"}]}`)
	result, errs := store.EnsureCollection("People", schema, nil)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %#v", errs)
	}
	if !result.Changed || !result.Created || result.SchemaVersion != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.GeneralVersion != 2 {
		t.Fatalf("expected general version 2, got %d", result.GeneralVersion)
	}
	if _, err := os.Stat(filepath.Join(configDir, "People.schema.json")); err != nil {
		t.Fatalf("expected schema file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "People")); err != nil {
		t.Fatalf("expected data dir: %v", err)
	}
}

func TestEnsureCollectionCreateRejectsUpgrade(t *testing.T) {
	configDir := freshConfigDir(t)
	store := &Store{ConfigDir: configDir, DataDir: t.TempDir()}
	schema := []byte(`{"kind":"object","children":[{"name":"name","kind":"string"}]}`)
	upgrade := []byte(`{"from":0,"new_ver_id":"` + validULID + `","updates":[{"op":"remove","path":"/name"}]}`)
	_, errs := store.EnsureCollection("People", schema, upgrade)
	if !hasCode(errs, "invalidArguments") {
		t.Fatalf("expected invalidArguments, got %#v", errs)
	}
}

func TestEnsureCollectionSchemaNoOp(t *testing.T) {
	configDir := freshConfigDir(t)
	store := &Store{ConfigDir: configDir, DataDir: t.TempDir()}

	current, err := os.ReadFile(filepath.Join(configDir, "Movies.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	compact := []byte(strings.ReplaceAll(string(current), "\n", ""))
	result, errs := store.EnsureCollection("Movies", compact, nil)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %#v", errs)
	}
	if result.Changed {
		t.Fatalf("expected no change, got %#v", result)
	}
	if result.SchemaVersion != 0 {
		t.Fatalf("expected schema version 0, got %d", result.SchemaVersion)
	}
}

func TestEnsureCollectionSchemaDrift(t *testing.T) {
	configDir := freshConfigDir(t)
	store := &Store{ConfigDir: configDir, DataDir: t.TempDir()}

	different := []byte(`{"kind":"object","children":[{"name":"other","kind":"string"}]}`)
	_, errs := store.EnsureCollection("Movies", different, nil)
	if !hasCode(errs, "schemaDrift") {
		t.Fatalf("expected schemaDrift, got %#v", errs)
	}
}

func TestEnsureCollectionUpgrade(t *testing.T) {
	configDir := freshConfigDir(t)
	store := &Store{ConfigDir: configDir, DataDir: t.TempDir()}

	upgrade := []byte(`{
		"from": 0,
		"new_ver_id": "` + validULID + `",
		"updates": [
			{"op": "add", "path": "/rating", "value": 0, "schema": {"kind": "number", "default": 0}}
		]
	}`)
	result, errs := store.EnsureCollection("Movies", nil, upgrade)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %#v", errs)
	}
	if !result.Changed || !result.Upgraded || result.SchemaVersion != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.NewVerID != validULID {
		t.Fatalf("expected new_ver_id %s, got %s", validULID, result.NewVerID)
	}
	current, err := os.ReadFile(filepath.Join(configDir, "Movies.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "rating") {
		t.Fatalf("expected upgraded schema to include rating, got: %s", current)
	}
}

func TestEnsureCollectionUpgradeNoOp(t *testing.T) {
	configDir := freshConfigDir(t)
	store := &Store{ConfigDir: configDir, DataDir: t.TempDir()}

	upgrade := []byte(`{
		"from": 0,
		"new_ver_id": "` + validULID + `",
		"updates": [
			{"op": "add", "path": "/rating", "value": 0, "schema": {"kind": "number", "default": 0}}
		]
	}`)
	first, errs := store.EnsureCollection("Movies", nil, upgrade)
	if len(errs) > 0 {
		t.Fatalf("first upgrade failed: %#v", errs)
	}
	if !first.Changed {
		t.Fatal("expected first upgrade to change config")
	}

	second, errs := store.EnsureCollection("Movies", nil, upgrade)
	if len(errs) > 0 {
		t.Fatalf("second ensure failed: %#v", errs)
	}
	if second.Changed {
		t.Fatalf("expected no-op on replay, got %#v", second)
	}
	if second.SchemaVersion != 1 {
		t.Fatalf("expected schema version 1, got %d", second.SchemaVersion)
	}
}

func TestEnsureCollectionUpgradeStaleVersion(t *testing.T) {
	configDir := freshConfigDir(t)
	store := &Store{ConfigDir: configDir, DataDir: t.TempDir()}

	stale := []byte(`{
		"from": 5,
		"new_ver_id": "` + validULID + `",
		"updates": [{"op": "remove", "path": "/status"}]
	}`)
	_, errs := store.EnsureCollection("Movies", nil, stale)
	if !hasCode(errs, "staleSchemaVersion") {
		t.Fatalf("expected staleSchemaVersion, got %#v", errs)
	}
}

func TestEnsureSearchCreate(t *testing.T) {
	configDir := freshConfigDir(t)
	dataDir := t.TempDir()
	store := &Store{ConfigDir: configDir, DataDir: dataDir}

	def := []byte(`{
		"$": "SearchDefinition:v1",
		"collection": "Movies",
		"name": "byStatus",
		"version": 1,
		"v1": {
			"clauses": [{"field": "/status", "op": "equals", "value": "released"}]
		}
	}`)
	result, errs := store.EnsureSearch("Movies", "byStatus", def)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %#v", errs)
	}
	if !result.Changed || result.SearchVersion != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(configDir, "Movies.search.byStatus.json")); err != nil {
		t.Fatalf("expected search file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "Movies", ".search", "byStatus")); err != nil {
		t.Fatalf("expected search data dir: %v", err)
	}
}

func TestEnsureSearchNoOp(t *testing.T) {
	configDir := freshConfigDir(t)
	store := &Store{ConfigDir: configDir, DataDir: t.TempDir()}

	def := []byte(`{
		"$": "SearchDefinition:v1",
		"collection": "Movies",
		"name": "byStatus",
		"version": 1,
		"v1": {
			"clauses": [{"field": "/status", "op": "equals", "value": "released"}]
		}
	}`)
	if _, errs := store.EnsureSearch("Movies", "byStatus", def); len(errs) > 0 {
		t.Fatalf("create failed: %#v", errs)
	}

	compact := []byte(strings.ReplaceAll(string(def), "\n", ""))
	result, errs := store.EnsureSearch("Movies", "byStatus", compact)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %#v", errs)
	}
	if result.Changed {
		t.Fatalf("expected no change, got %#v", result)
	}
}

func TestEnsureSearchConflict(t *testing.T) {
	configDir := freshConfigDir(t)
	store := &Store{ConfigDir: configDir, DataDir: t.TempDir()}

	def := []byte(`{
		"$": "SearchDefinition:v1",
		"collection": "Movies",
		"name": "byStatus",
		"version": 1,
		"v1": {
			"clauses": [{"field": "/status", "op": "equals", "value": "released"}]
		}
	}`)
	if _, errs := store.EnsureSearch("Movies", "byStatus", def); len(errs) > 0 {
		t.Fatalf("create failed: %#v", errs)
	}

	different := []byte(`{
		"$": "SearchDefinition:v1",
		"collection": "Movies",
		"name": "byStatus",
		"version": 1,
		"v1": {
			"clauses": [{"field": "/status", "op": "equals", "value": "draft"}]
		}
	}`)
	_, errs := store.EnsureSearch("Movies", "byStatus", different)
	if !hasCode(errs, "searchDefinitionConflict") {
		t.Fatalf("expected searchDefinitionConflict, got %#v", errs)
	}
}

func TestDeleteSearchMissing(t *testing.T) {
	configDir := freshConfigDir(t)
	store := &Store{ConfigDir: configDir, DataDir: t.TempDir()}

	result, errs := store.DeleteSearch("Movies", "missing")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %#v", errs)
	}
	if result.Changed {
		t.Fatalf("expected no change, got %#v", result)
	}
}

func TestDeleteSearchPresent(t *testing.T) {
	configDir := freshConfigDir(t)
	store := &Store{ConfigDir: configDir, DataDir: t.TempDir()}

	def := []byte(`{
		"$": "SearchDefinition:v1",
		"collection": "Movies",
		"name": "byStatus",
		"version": 1,
		"v1": {
			"clauses": [{"field": "/status", "op": "equals", "value": "released"}]
		}
	}`)
	if _, errs := store.EnsureSearch("Movies", "byStatus", def); len(errs) > 0 {
		t.Fatalf("create failed: %#v", errs)
	}

	result, errs := store.DeleteSearch("Movies", "byStatus")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %#v", errs)
	}
	if !result.Changed {
		t.Fatalf("expected change, got %#v", result)
	}
	if _, err := os.Stat(filepath.Join(configDir, "Movies.search.byStatus.json")); !os.IsNotExist(err) {
		t.Fatalf("expected search file removed, err: %v", err)
	}
}
