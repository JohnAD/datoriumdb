package replication

import (
	"testing"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

func TestApplierCreateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	applier := &Applier{DataDir: dir}
	item := DocumentWorkItem{
		Collection:   "Movies",
		ID:           "doc1",
		AfterVersion: "v1",
		OperationID:  "op1",
		Command:      "create",
		Payload:      mustPayload(map[string]any{"!": "doc1", "$": "Movies:0", "#": "v1", "title": "The Matrix"}),
	}
	applied, err := applier.Apply(item)
	if err != nil || !applied {
		t.Fatalf("first apply: applied=%v err=%v", applied, err)
	}
	doc, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(dir, "Movies", "doc1"))
	if err != nil {
		t.Fatal(err)
	}
	if doc["title"] != "The Matrix" {
		t.Fatalf("unexpected document: %#v", doc)
	}

	// Duplicate delivery (same operation, same resulting version) must not
	// error and must not require the payload to change.
	applied, err = applier.Apply(item)
	if err != nil || !applied {
		t.Fatalf("duplicate apply: applied=%v err=%v", applied, err)
	}
}

func TestApplierCreateConflictOnDifferentVersion(t *testing.T) {
	dir := t.TempDir()
	applier := &Applier{DataDir: dir}
	first := DocumentWorkItem{
		Collection: "Movies", ID: "doc1", AfterVersion: "v1", OperationID: "op1", Command: "create",
		Payload: mustPayload(map[string]any{"!": "doc1", "#": "v1"}),
	}
	if _, err := applier.Apply(first); err != nil {
		t.Fatal(err)
	}
	second := DocumentWorkItem{
		Collection: "Movies", ID: "doc1", AfterVersion: "v2", OperationID: "op2", Command: "create",
		Payload: mustPayload(map[string]any{"!": "doc1", "#": "v2"}),
	}
	if _, err := applier.Apply(second); err == nil {
		t.Fatalf("expected a conflict error for a differently-versioned create of the same document")
	}
}

func TestApplierPatchAppliesOpsAndBumpsVersion(t *testing.T) {
	dir := t.TempDir()
	applier := &Applier{DataDir: dir}
	create := DocumentWorkItem{
		Collection: "Movies", ID: "doc1", AfterVersion: "v1", OperationID: "op1", Command: "create",
		Payload: mustPayload(map[string]any{"!": "doc1", "$": "Movies:0", "#": "v1", "status": "unreleased"}),
	}
	if _, err := applier.Apply(create); err != nil {
		t.Fatal(err)
	}
	patch := DocumentWorkItem{
		Collection: "Movies", ID: "doc1", BeforeVersion: "v1", AfterVersion: "v2", OperationID: "op2", Command: "patch",
		Patch: []map[string]any{
			{"op": "replace", "path": "/status", "value": "released"},
			{"op": "replace", "path": "/#", "value": "v2"},
		},
	}
	applied, err := applier.Apply(patch)
	if err != nil || !applied {
		t.Fatalf("patch apply: applied=%v err=%v", applied, err)
	}
	doc, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(dir, "Movies", "doc1"))
	if err != nil {
		t.Fatal(err)
	}
	if doc["status"] != "released" || doc["#"] != "v2" {
		t.Fatalf("unexpected document after patch: %#v", doc)
	}
	prev := fsstore.PreviousDocumentPath(dir, "Movies", "doc1")
	if _, err := fsstore.ReadDocumentJSON(prev); err != nil {
		t.Fatalf("expected previous-document dotfile to be preserved: %v", err)
	}

	// Duplicate delivery of the same patch must be a no-op, not a second
	// application of the RFC 6902 ops.
	applied, err = applier.Apply(patch)
	if err != nil || !applied {
		t.Fatalf("duplicate patch apply: applied=%v err=%v", applied, err)
	}
	doc, err = fsstore.ReadDocumentJSON(fsstore.DocumentPath(dir, "Movies", "doc1"))
	if err != nil {
		t.Fatal(err)
	}
	if doc["status"] != "released" || doc["#"] != "v2" {
		t.Fatalf("expected duplicate apply to be a no-op, got %#v", doc)
	}
}

func TestApplierPatchMissingLocalDocumentErrors(t *testing.T) {
	dir := t.TempDir()
	applier := &Applier{DataDir: dir}
	patch := DocumentWorkItem{
		Collection: "Movies", ID: "missing", BeforeVersion: "v1", AfterVersion: "v2", OperationID: "op1", Command: "patch",
		Patch: []map[string]any{{"op": "replace", "path": "/#", "value": "v2"}},
	}
	if _, err := applier.Apply(patch); err == nil {
		t.Fatalf("expected an error when patching a document this replica never received")
	}
}

func TestApplierDeleteIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	applier := &Applier{DataDir: dir}
	create := DocumentWorkItem{
		Collection: "Movies", ID: "doc1", AfterVersion: "v1", OperationID: "op1", Command: "create",
		Payload: mustPayload(map[string]any{"!": "doc1", "#": "v1"}),
	}
	if _, err := applier.Apply(create); err != nil {
		t.Fatal(err)
	}
	del := DocumentWorkItem{Collection: "Movies", ID: "doc1", BeforeVersion: "v1", OperationID: "op2", Command: "delete"}
	applied, err := applier.Apply(del)
	if err != nil || !applied {
		t.Fatalf("delete apply: applied=%v err=%v", applied, err)
	}
	if _, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(dir, "Movies", "doc1")); err == nil {
		t.Fatalf("expected live document to be gone after delete")
	}

	// Duplicate delivery of the same delete is a no-op success.
	applied, err = applier.Apply(del)
	if err != nil || !applied {
		t.Fatalf("duplicate delete apply: applied=%v err=%v", applied, err)
	}
}

func TestApplierUnknownCommand(t *testing.T) {
	dir := t.TempDir()
	applier := &Applier{DataDir: dir}
	if _, err := applier.Apply(DocumentWorkItem{Collection: "Movies", ID: "doc1", Command: "bogus"}); err == nil {
		t.Fatalf("expected an error for an unknown replication command")
	}
}
