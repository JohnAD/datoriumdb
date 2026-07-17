package engine

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

func TestCreateReadPatchDelete(t *testing.T) {
	eng := testEngine(t)
	created := eng.Execute(`create Movies null {$: Movies:0, title: "The Matrix", releaseYear: 1999, status: released}`)
	if created["ok"] != true {
		t.Fatalf("create failed: %#v", created)
	}
	id, _ := created["id"].(string)
	path := fsstore.DocumentPath(eng.DataDir, "Movies", id)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "operationId") {
		t.Fatalf("operationId must not be persisted: %s", raw)
	}
	read := eng.Execute(`read Movies ` + id + ` {extraFields: true}`)
	if read["ok"] != true {
		t.Fatalf("read failed: %#v", read)
	}
	ver, _ := created["#"].(string)
	patched := eng.Execute(`patch Movies ` + id + ` {$: Movies:0, #: ` + ver + `, RFC6902: [{op: replace, path: /status, value: archived}]}`)
	if patched["ok"] != true {
		t.Fatalf("patch failed: %#v", patched)
	}
	prev := fsstore.PreviousDocumentPath(eng.DataDir, "Movies", id)
	if _, err := os.Stat(prev); err != nil {
		t.Fatalf("expected previous document: %v", err)
	}
	versions, _ := patched["versions"].(map[string]any)
	after, _ := versions["after"].(string)
	deleted := eng.Execute(`delete Movies ` + id + ` {#: ` + after + `}`)
	if deleted["ok"] != true {
		t.Fatalf("delete failed: %#v", deleted)
	}
}

func TestInvalidDocumentID(t *testing.T) {
	eng := testEngine(t)
	res := eng.Execute(`create Movies ../evil {$: Movies:0, title: "x"}`)
	if res["ok"] != false {
		t.Fatalf("expected failure: %#v", res)
	}
	if code := firstErrorCode(res); code != "invalidDocumentId" {
		t.Fatalf("unexpected error: %#v", res)
	}
}

func TestCreateBangMismatch(t *testing.T) {
	eng := testEngine(t)
	res := eng.Execute(`create Movies ABC123 {$: Movies:0, !: OTHER, title: "x"}`)
	if res["ok"] != false {
		t.Fatalf("expected failure: %#v", res)
	}
}

func TestPatchSchemaValidation(t *testing.T) {
	eng := testEngine(t)
	created := eng.Execute(`create Movies null {$: Movies:0, title: "x", status: released}`)
	id, _ := created["id"].(string)
	ver, _ := created["#"].(string)
	res := eng.Execute(`patch Movies ` + id + ` {$: Movies:0, #: ` + ver + `, RFC6902: [{op: remove, path: /title}]}`)
	if res["ok"] != false {
		t.Fatalf("expected schema failure: %#v", res)
	}
}

func TestConcurrentSameVersionPatch(t *testing.T) {
	eng := testEngine(t)
	created := eng.Execute(`create Movies null {$: Movies:0, title: "x", status: released}`)
	id, _ := created["id"].(string)
	ver, _ := created["#"].(string)
	var success atomic.Int32
	var mismatch atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := eng.Execute(`patch Movies ` + id + ` {$: Movies:0, #: ` + ver + `, RFC6902: [{op: replace, path: /status, value: archived}]}`)
			if res["ok"] == true {
				success.Add(1)
				return
			}
			if firstErrorCode(res) == "versionMismatch" {
				mismatch.Add(1)
				return
			}
			t.Errorf("unexpected failure: %#v", res)
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("expected exactly one success, got %d", success.Load())
	}
	if mismatch.Load() != 7 {
		t.Fatalf("expected 7 versionMismatch, got %d", mismatch.Load())
	}
}

func TestQueueWriteSurfaced(t *testing.T) {
	eng := testEngine(t)
	created := eng.Execute(`create Movies null {$: Movies:0, title: "x"}`)
	if created["ok"] != true {
		t.Fatalf("%#v", created)
	}
	id, _ := created["id"].(string)
	queue := filepath.Join(fsstore.ChangeQueueDir(eng.DataDir, "Movies"), "create__Movies__"+id+".queue")
	if _, err := os.Stat(queue); err != nil {
		t.Fatal(err)
	}
}

func testEngine(t *testing.T) *Engine {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, ".config")
	if err := copyDir("../../testdata/sample-config", configDir); err != nil {
		t.Fatal(err)
	}
	eng := &Engine{ConfigDir: configDir, DataDir: root, ServerName: "serverA"}
	if err := eng.Reload(); err != nil {
		t.Fatal(err)
	}
	return eng
}

func firstErrorCode(res envelope.Result) string {
	errs, ok := res["errors"].([]envelope.Error)
	if !ok || len(errs) == 0 {
		return ""
	}
	return errs[0].Code
}

func copyDir(src, dst string) error {
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
