package replication

import (
	"bytes"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

func TestFileApplierIdempotentCreateAndDelete(t *testing.T) {
	dir := t.TempDir()
	_ = fsstore.EnsureCollectionDir(dir, "Movies")
	applier := &FileApplier{DataDir: dir}
	item := FileWorkItem{
		Collection: "Movies", ID: "doc1", Filename: "a.bin",
		ContentType: "application/octet-stream", ByteSize: 4,
		SHA256: "", Version: "v1", OperationID: "op1", Command: "fileCreate",
	}
	payload := []byte("abcd")
	applied, err := applier.ApplyStream(item, bytes.NewReader(payload), 0)
	if err != nil || !applied {
		t.Fatalf("apply: %v applied=%v", err, applied)
	}
	// Capture sha from manifest
	entries, err := fsstore.ReadFilesManifest(dir, "Movies", "doc1")
	if err != nil || len(entries) != 1 {
		t.Fatalf("manifest %#v err=%v", entries, err)
	}
	item.SHA256 = entries[0].SHA256
	item.ByteSize = entries[0].ByteSize
	applied, err = applier.ApplyStream(item, bytes.NewReader(payload), 0)
	if err != nil || !applied {
		t.Fatalf("re-apply: %v", err)
	}
	del := item
	del.Command = "fileDelete"
	applied, err = applier.ApplyMetadataOnly(del)
	if err != nil || !applied {
		t.Fatalf("delete: %v", err)
	}
	applied, err = applier.ApplyMetadataOnly(del)
	if err != nil || !applied {
		t.Fatalf("re-delete: %v", err)
	}
}

func TestParseFileWorkItemID(t *testing.T) {
	id, name, ok := ParseFileWorkItemID("serverB-doc1-photo.png", "serverB")
	if !ok || id != "doc1" || name != "photo.png" {
		t.Fatalf("got %q %q %v", id, name, ok)
	}
}

func TestPendingFileWriteCoalesce(t *testing.T) {
	dir := t.TempDir()
	_ = fsstore.EnsureCollectionDir(dir, "Movies")
	a := FileWorkItem{Collection: "Movies", ID: "doc1", Filename: "f.bin", Version: "v1", Command: "fileCreate", OperationID: "op1"}
	b := FileWorkItem{Collection: "Movies", ID: "doc1", Filename: "f.bin", Version: "v2", Command: "fileUpdate", OperationID: "op2"}
	if err := WritePendingFileWrite(dir, "Movies", "serverB", a); err != nil {
		t.Fatal(err)
	}
	if err := WritePendingFileWrite(dir, "Movies", "serverB", b); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPendingFileWrite(dir, "Movies", "serverB", "doc1", "f.bin")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "v2" {
		t.Fatalf("expected coalesced v2, got %#v", got)
	}
}
