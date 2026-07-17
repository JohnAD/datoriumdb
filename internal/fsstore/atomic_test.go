package fsstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicVisible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.json")
	if err := WriteFileAtomic(path, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("unexpected content %q", data)
	}
}

func TestWriteDocumentJSONVerified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	doc := map[string]any{"!": "x", "#": "ver1", "title": "t"}
	if err := WriteDocumentJSONVerified(path, doc); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDocumentJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["#"] != "ver1" {
		t.Fatalf("got %#v", got)
	}
}

func TestPreservePreviousIfAbsent(t *testing.T) {
	root := t.TempDir()
	if err := EnsureCollectionDir(root, "Movies"); err != nil {
		t.Fatal(err)
	}
	live := DocumentPath(root, "Movies", "abc")
	if err := WriteDocumentJSON(live, map[string]any{"!": "abc", "#": "1"}); err != nil {
		t.Fatal(err)
	}
	if err := PreservePreviousIfAbsent(root, "Movies", "abc"); err != nil {
		t.Fatal(err)
	}
	prev := PreviousDocumentPath(root, "Movies", "abc")
	if _, err := os.Stat(prev); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Fatalf("live should be gone after preserve rename")
	}
	// Recreate live and ensure older previous is kept.
	if err := WriteDocumentJSON(live, map[string]any{"!": "abc", "#": "2"}); err != nil {
		t.Fatal(err)
	}
	if err := PreservePreviousIfAbsent(root, "Movies", "abc"); err != nil {
		t.Fatal(err)
	}
	doc, err := ReadDocumentJSON(prev)
	if err != nil {
		t.Fatal(err)
	}
	if doc["#"] != "1" {
		t.Fatalf("expected oldest previous, got %#v", doc)
	}
}

func TestSafeID(t *testing.T) {
	if !SafeID("01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Fatal("expected valid")
	}
	if SafeID("../x") || SafeID("a/b") || SafeID("") {
		t.Fatal("expected invalid")
	}
}
