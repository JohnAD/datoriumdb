package replication

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPendingWritePathMatchesDocumentedNaming(t *testing.T) {
	dir := t.TempDir()
	got := PendingWritePath(dir, "Movies", "serverB", "01KWDRHGK2GXE2B0VZ85GT546T")
	want := filepath.Join(dir, "Movies", ".pendingWrites", "serverB.01KWDRHGK2GXE2B0VZ85GT546T.json")
	if got != want {
		t.Fatalf("unexpected pending write path\n got: %s\nwant: %s", got, want)
	}
}

func TestWriteReadDeletePendingWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	item := DocumentWorkItem{
		Collection:    "Movies",
		ID:            "doc1",
		BeforeVersion: "before1",
		AfterVersion:  "after1",
		OperationID:   "op1",
		Command:       "patch",
		Patch:         []map[string]any{{"op": "replace", "path": "/status", "value": "released"}},
	}
	if err := WritePendingWrite(dir, "Movies", "serverB", item); err != nil {
		t.Fatal(err)
	}
	path := PendingWritePath(dir, "Movies", "serverB", "doc1")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected pending write file to exist: %v", err)
	}

	got, err := ReadPendingWrite(dir, "Movies", "serverB", "doc1")
	if err != nil {
		t.Fatal(err)
	}
	if got.OperationID != "op1" || got.Command != "patch" || len(got.Patch) != 1 {
		t.Fatalf("unexpected pending write content: %#v", got)
	}

	existed, err := DeletePendingWrite(dir, "Movies", "serverB", "doc1")
	if err != nil {
		t.Fatal(err)
	}
	if !existed {
		t.Fatalf("expected existed=true on first delete")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected pending write file to be removed")
	}

	// A second delete of an already-cleared item is not an error and
	// reports existed=false, matching SERVER-TO-SERVER-API.md's
	// existing:false semantics.
	existed, err = DeletePendingWrite(dir, "Movies", "serverB", "doc1")
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Fatalf("expected existed=false for an already-deleted item")
	}
}

func TestWorkItemIDRoundTrip(t *testing.T) {
	id := WorkItemID("serverB", "01KWDRHGK2GXE2B0VZ85GT546T")
	if id != "serverB-01KWDRHGK2GXE2B0VZ85GT546T" {
		t.Fatalf("unexpected work item id: %s", id)
	}
	docID, ok := DocIDFromWorkItemID(id, "serverB")
	if !ok || docID != "01KWDRHGK2GXE2B0VZ85GT546T" {
		t.Fatalf("expected round-trip decode, got docID=%q ok=%v", docID, ok)
	}
	if _, ok := DocIDFromWorkItemID(id, "serverWrong"); ok {
		t.Fatalf("expected decode to fail for a non-owning server name")
	}
}

func TestWorkItemIDHandlesHyphenatedServerNames(t *testing.T) {
	// Because decoding strips exactly the caller's own authenticated
	// server name as a prefix, hyphens inside server or document names do
	// not create ambiguity.
	id := WorkItemID("server-b-east", "doc-with-hyphens-123")
	docID, ok := DocIDFromWorkItemID(id, "server-b-east")
	if !ok || docID != "doc-with-hyphens-123" {
		t.Fatalf("expected hyphen-tolerant round-trip, got docID=%q ok=%v", docID, ok)
	}
}

func TestFindPendingWriteScansAllCollections(t *testing.T) {
	dir := t.TempDir()
	if err := WritePendingWrite(dir, "People", "serverB", DocumentWorkItem{Collection: "People", ID: "docA", Command: "create", OperationID: "op1"}); err != nil {
		t.Fatal(err)
	}
	if err := WritePendingWrite(dir, "Movies", "serverB", DocumentWorkItem{Collection: "Movies", ID: "docB", Command: "create", OperationID: "op2"}); err != nil {
		t.Fatal(err)
	}
	collection, item, err := FindPendingWrite(dir, "serverB", "docB")
	if err != nil {
		t.Fatal(err)
	}
	if collection != "Movies" || item.ID != "docB" {
		t.Fatalf("unexpected find result: collection=%s item=%#v", collection, item)
	}
	if _, _, err := FindPendingWrite(dir, "serverB", "missing"); !os.IsNotExist(err) {
		t.Fatalf("expected os.ErrNotExist for a missing item, got %v", err)
	}
}

func TestListPendingWorkItemIDsAcrossCollectionsWithLimitAndTotal(t *testing.T) {
	dir := t.TempDir()
	for i, collection := range []string{"Movies", "Movies", "People"} {
		docID := "doc" + string(rune('A'+i))
		if err := WritePendingWrite(dir, collection, "serverB", DocumentWorkItem{Collection: collection, ID: docID, Command: "create", OperationID: "op" + string(rune('A'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	// A pending write for a different target server must not be listed.
	if err := WritePendingWrite(dir, "Movies", "serverC", DocumentWorkItem{Collection: "Movies", ID: "docZ", Command: "create", OperationID: "opZ"}); err != nil {
		t.Fatal(err)
	}

	ids, total, err := ListPendingWorkItemIDs(dir, "serverB", 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(ids) != 3 {
		t.Fatalf("expected 3 total items with no limit, got total=%d ids=%#v", total, ids)
	}
	for _, id := range ids {
		if _, ok := DocIDFromWorkItemID(id, "serverB"); !ok {
			t.Fatalf("expected every listed item to decode against serverB: %s", id)
		}
	}

	limited, total, err := ListPendingWorkItemIDs(dir, "serverB", 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("expected totalItems to reflect the full count regardless of limit, got %d", total)
	}
	if len(limited) != 2 {
		t.Fatalf("expected limit to cap returned items to 2, got %d", len(limited))
	}
}

func TestListPendingWorkItemIDsWithNoPendingWritesDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Movies"), 0o755); err != nil {
		t.Fatal(err)
	}
	ids, total, err := ListPendingWorkItemIDs(dir, "serverB", 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(ids) != 0 {
		t.Fatalf("expected no items, got total=%d ids=%#v", total, ids)
	}
}
