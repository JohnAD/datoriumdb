package ctl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanOrderedWritesPutsGeneralLast(t *testing.T) {
	p := &Plan{}
	p.AddWrite("/db/.config/__general.json", []byte("{}"))
	p.AddWrite("/db/.config/__servers.json", []byte("{}"))
	p.AddWrite("/db/.config/Movies.schema.json", []byte("{}"))

	ordered := p.orderedWrites()
	if len(ordered) != 3 {
		t.Fatalf("expected 3 writes, got %d", len(ordered))
	}
	last := ordered[len(ordered)-1]
	if filepath.Base(last.Path) != "__general.json" {
		t.Fatalf("expected __general.json last, got %s", last.Path)
	}
}

func TestPlanFilesWrittenOrder(t *testing.T) {
	p := &Plan{}
	p.AddWrite("/db/.config/__general.json", []byte("{}"))
	p.AddWrite("/db/.config/Movies.schema.json", []byte("{}"))
	names := p.FilesWritten()
	if len(names) != 2 || names[len(names)-1] != "__general.json" {
		t.Fatalf("expected __general.json last in FilesWritten, got %v", names)
	}
}

func TestPlanCommitWritesFilesAndCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	p := &Plan{}
	p.AddWrite(filepath.Join(dir, "Movies.schema.json"), []byte(`{"kind":"object"}`))
	p.AddWrite(filepath.Join(dir, "__general.json"), []byte(`{"general":{}}`))
	p.AddDir(filepath.Join(dir, "Movies"))

	if err := p.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Movies.schema.json")); err != nil {
		t.Fatalf("expected schema file written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "__general.json")); err != nil {
		t.Fatalf("expected general file written: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "Movies"))
	if err != nil || !info.IsDir() {
		t.Fatalf("expected Movies directory created: %v", err)
	}
}

func TestPlanCommitRemovesFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Movies.search.byStatus.json")
	if err := os.WriteFile(target, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Plan{}
	p.AddRemove(target)
	if err := p.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err: %v", err)
	}
}

func TestReindentJSONPreservesFieldOrder(t *testing.T) {
	raw := []byte(`{"b":1,"a":2}`)
	out, err := ReindentJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if bIdx, aIdx := indexOf(s, `"b"`), indexOf(s, `"a"`); bIdx == -1 || aIdx == -1 || bIdx > aIdx {
		t.Fatalf("expected field order preserved (b before a), got: %s", s)
	}
	if s[len(s)-1] != '\n' {
		t.Fatalf("expected trailing newline, got: %q", s)
	}
}

func TestReindentJSONPrettyPrintsCompactSchema(t *testing.T) {
	raw := []byte(`{"kind":"object","children":[{"name":"title","kind":"string","required":true},{"name":"owner","kind":"string","format":"DatoriumDirectRef","required":true}]}`)
	out, err := ReindentJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if indexOf(s, "{\n  \"kind\": \"object\"") < 0 {
		t.Fatalf("expected pretty-printed kind-first schema, got:\n%s", s)
	}
	if indexOf(s, `"kind"`) > indexOf(s, `"children"`) {
		t.Fatalf("expected kind before children:\n%s", s)
	}
}

func TestReindentJSONRejectsInvalidJSON(t *testing.T) {
	if _, err := ReindentJSON([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
