package fsstore

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamAndCommitBinary(t *testing.T) {
	dir := t.TempDir()
	collection, docID, name := "Movies", "01TESTDOC000000000000001", "photo.png"
	if err := EnsureCollectionDir(dir, collection); err != nil {
		t.Fatal(err)
	}
	// Parent document required by higher layers; storage commit does not check.
	if err := WriteFileAtomic(DocumentPath(dir, collection, docID), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := []byte("hello-binary")
	staged, err := StageWriteBinary(dir, collection, docID, name, bytes.NewReader(payload), DefaultMaxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	entry := FileEntry{
		Name: name, ContentType: "image/png", ByteSize: staged.ByteSize,
		SHA256: staged.SHA256, Version: "v1", OperationID: "op1",
	}
	if err := CommitStagedBinary(dir, collection, docID, entry, staged); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(BinaryPath(dir, collection, docID, name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("bytes = %q", got)
	}
	entries, err := ReadFilesManifest(dir, collection, docID)
	if err != nil || len(entries) != 1 || entries[0].Name != name {
		t.Fatalf("manifest %#v err=%v", entries, err)
	}
}

func TestFileTooLarge(t *testing.T) {
	dir := t.TempDir()
	_, err := StageWriteBinary(dir, "Movies", "doc1", "big.bin", bytes.NewReader(make([]byte, 100)), 50)
	if !IsFileTooLarge(err) {
		t.Fatalf("expected fileTooLarge, got %v", err)
	}
}

func TestRecoverFileOpsBlobCommitted(t *testing.T) {
	dir := t.TempDir()
	collection, docID, name := "Movies", "doc1", "a.txt"
	if err := EnsureCollectionDir(dir, collection); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(LFSDir(dir, collection, docID), 0o755); err != nil {
		t.Fatal(err)
	}
	final := BinaryPath(dir, collection, docID, name)
	if err := os.WriteFile(final, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	j := FileOpJournal{
		Command: "create", Filename: name, ContentType: "text/plain",
		ByteSize: 1, SHA256: "aa", Version: "v1", OperationID: "op9", Phase: "blobCommitted",
	}
	if err := WriteFileOpJournal(dir, collection, docID, j); err != nil {
		t.Fatal(err)
	}
	if err := RecoverFileOps(dir, collection, docID); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadFilesManifest(dir, collection, docID)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected recovered manifest, got %#v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(FileOpsDir(dir, collection, docID), "op9.json")); !os.IsNotExist(err) {
		t.Fatalf("journal should be cleared, err=%v", err)
	}
}

func TestCascadeDeleteDocumentFiles(t *testing.T) {
	dir := t.TempDir()
	collection, docID := "Movies", "doc1"
	_ = EnsureCollectionDir(dir, collection)
	_ = os.MkdirAll(LFSDir(dir, collection, docID), 0o755)
	_ = os.WriteFile(BinaryPath(dir, collection, docID, "a.txt"), []byte("x"), 0o644)
	_ = WriteFilesManifest(dir, collection, docID, []FileEntry{{Name: "a.txt", Version: "v1"}})
	if err := CascadeDeleteDocumentFiles(dir, collection, docID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(LFSDir(dir, collection, docID)); !os.IsNotExist(err) {
		t.Fatal("lfs dir should be gone")
	}
	if _, err := os.Stat(FilesManifestPath(dir, collection, docID)); !os.IsNotExist(err) {
		t.Fatal("manifest should be gone")
	}
}

func TestSoftDeleteCascadesFiles(t *testing.T) {
	dir := t.TempDir()
	collection, docID := "Movies", "doc1"
	_ = EnsureCollectionDir(dir, collection)
	_ = WriteFileAtomic(DocumentPath(dir, collection, docID), []byte("{}\n"), 0o644)
	_ = os.MkdirAll(LFSDir(dir, collection, docID), 0o755)
	_ = os.WriteFile(BinaryPath(dir, collection, docID, "a.txt"), []byte("x"), 0o644)
	_ = WriteFilesManifest(dir, collection, docID, []FileEntry{{Name: "a.txt", Version: "v1"}})
	if err := SoftDeleteDocument(dir, collection, docID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(BinaryPath(dir, collection, docID, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("attachment should be cascaded")
	}
}

func TestManifestFieldOrder(t *testing.T) {
	dir := t.TempDir()
	_ = WriteFilesManifest(dir, "Movies", "doc1", []FileEntry{
		{Name: "b.txt", ContentType: "t", ByteSize: 1, SHA256: "s", Version: "v", OperationID: "o"},
	})
	raw, err := os.ReadFile(FilesManifestPath(dir, "Movies", "doc1"))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(raw))
	want := `{"name":"b.txt","contentType":"t","byteSize":1,"sha256":"s","version":"v","operationId":"o"}`
	if line != want {
		t.Fatalf("got %s", line)
	}
}

func TestStreamWriteBinary(t *testing.T) {
	dir := t.TempDir()
	collection, docID, name := "Movies", "docStream", "pic.bin"
	if err := EnsureCollectionDir(dir, collection); err != nil {
		t.Fatal(err)
	}
	payload := []byte("streamed-bytes")
	res, err := StreamWriteBinary(dir, collection, docID, name, bytes.NewReader(payload), 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.ByteSize != int64(len(payload)) || res.Path == "" || res.SHA256 == "" {
		t.Fatalf("unexpected result %#v", res)
	}
	got, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q", got)
	}

	_, err = StreamWriteBinary(dir, collection, docID, "../evil", bytes.NewReader(payload), 100)
	if err == nil {
		t.Fatal("expected invalid file name")
	}
	_, err = StreamWriteBinary(dir, collection, docID, "big.bin", bytes.NewReader(make([]byte, 20)), 5)
	if !IsFileTooLarge(err) {
		t.Fatalf("expected fileTooLarge, got %v", err)
	}
}

func TestRecoverAllFileOps(t *testing.T) {
	dir := t.TempDir()
	if err := RecoverAllFileOps(filepath.Join(dir, "missing")); err != nil {
		t.Fatalf("missing data dir should succeed: %v", err)
	}
	collection, docID, name := "Movies", "doc1", "a.txt"
	if err := EnsureCollectionDir(dir, collection); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(LFSDir(dir, collection, docID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BinaryPath(dir, collection, docID, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	j := FileOpJournal{
		Command: "create", Filename: name, ContentType: "text/plain",
		ByteSize: 1, SHA256: "aa", Version: "v1", OperationID: "opAll", Phase: "blobCommitted",
	}
	if err := WriteFileOpJournal(dir, collection, docID, j); err != nil {
		t.Fatal(err)
	}
	if err := RecoverAllFileOps(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadFilesManifest(dir, collection, docID)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected recovered manifest %#v err=%v", entries, err)
	}
}

func TestDeletedDocumentPathAndReadDocumentBytes(t *testing.T) {
	dir := t.TempDir()
	collection, id := "Movies", "doc1"
	if err := EnsureCollectionDir(dir, collection); err != nil {
		t.Fatal(err)
	}
	path := DocumentPath(dir, collection, id)
	if err := WriteFileAtomic(path, []byte(`{"ok":true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := ReadDocumentBytes(path)
	if err != nil || !bytes.Equal(raw, []byte(`{"ok":true}`+"\n")) {
		t.Fatalf("ReadDocumentBytes got %q err=%v", raw, err)
	}
	if DeletedDocumentPath(dir, collection, id) != PreviousDocumentPath(dir, collection, id) {
		t.Fatal("DeletedDocumentPath must match PreviousDocumentPath")
	}
}
