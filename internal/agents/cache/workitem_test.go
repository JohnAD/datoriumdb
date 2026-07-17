package cache

import (
	"os"
	"testing"
)

func TestWorkItemIDRoundTrip(t *testing.T) {
	id := WorkItemID("serverB", "doc1")
	docID, ok := DocIDFromWorkItemID(id, "serverB")
	if !ok || docID != "doc1" {
		t.Fatalf("expected round trip to recover doc1, got %q ok=%v", docID, ok)
	}
	if _, ok := DocIDFromWorkItemID(id, "serverC"); ok {
		t.Fatalf("expected a mismatched read server prefix to fail")
	}
}

func TestWriteReadDeleteWorkItem(t *testing.T) {
	dir := t.TempDir()
	item := WorkItem{
		SourceCollection: "Movies",
		SourceDocumentID: "id1",
		Command:          "create",
		OperationID:      "op1",
		Payload:          map[string]any{"title": "T"},
	}
	if err := WriteWorkItem(dir, "serverB", item); err != nil {
		t.Fatalf("WriteWorkItem: %v", err)
	}
	got, err := ReadWorkItem(dir, "Movies", "serverB", "id1")
	if err != nil {
		t.Fatalf("ReadWorkItem: %v", err)
	}
	if got.SourceDocumentID != "id1" || got.Command != "create" || got.Payload["title"] != "T" {
		t.Fatalf("unexpected work item: %+v", got)
	}

	existed, err := DeleteWorkItem(dir, "Movies", "serverB", "id1")
	if err != nil || !existed {
		t.Fatalf("expected DeleteWorkItem to report existed=true, err=%v", err)
	}
	if _, err := ReadWorkItem(dir, "Movies", "serverB", "id1"); !os.IsNotExist(err) {
		t.Fatalf("expected work item to be gone, err=%v", err)
	}
	existed2, err := DeleteWorkItem(dir, "Movies", "serverB", "id1")
	if err != nil || existed2 {
		t.Fatalf("expected deleting an absent item to be a no-op, existed=%v err=%v", existed2, err)
	}
}

func TestFindWorkItemScansCollections(t *testing.T) {
	dir := t.TempDir()
	item := WorkItem{SourceCollection: "Reviews", SourceDocumentID: "r1", Command: "patch", Payload: map[string]any{}}
	if err := WriteWorkItem(dir, "serverB", item); err != nil {
		t.Fatalf("WriteWorkItem: %v", err)
	}
	col, found, err := FindWorkItem(dir, "serverB", "r1")
	if err != nil {
		t.Fatalf("FindWorkItem: %v", err)
	}
	if col != "Reviews" || found.SourceDocumentID != "r1" {
		t.Fatalf("unexpected find result: col=%q item=%+v", col, found)
	}
	if _, _, err := FindWorkItem(dir, "serverB", "does-not-exist"); err == nil {
		t.Fatalf("expected an error for an absent work item")
	}
}

func TestListWorkItemIDsAcrossCollectionsWithLimit(t *testing.T) {
	dir := t.TempDir()
	for i, col := range []string{"Movies", "Movies", "Reviews"} {
		item := WorkItem{SourceCollection: col, SourceDocumentID: "d" + string(rune('0'+i)), Command: "create", Payload: map[string]any{}}
		if err := WriteWorkItem(dir, "serverB", item); err != nil {
			t.Fatalf("WriteWorkItem: %v", err)
		}
	}
	ids, total, err := ListWorkItemIDs(dir, "serverB", 0)
	if err != nil {
		t.Fatalf("ListWorkItemIDs: %v", err)
	}
	if total != 3 || len(ids) != 3 {
		t.Fatalf("expected 3 items total, got total=%d ids=%v", total, ids)
	}
	limited, totalLimited, err := ListWorkItemIDs(dir, "serverB", 2)
	if err != nil {
		t.Fatalf("ListWorkItemIDs limited: %v", err)
	}
	if totalLimited != 3 || len(limited) != 2 {
		t.Fatalf("expected total=3 but a 2-item page, got total=%d ids=%v", totalLimited, limited)
	}

	// A different read server should see none of serverB's items.
	otherIDs, otherTotal, err := ListWorkItemIDs(dir, "serverC", 0)
	if err != nil {
		t.Fatalf("ListWorkItemIDs other: %v", err)
	}
	if otherTotal != 0 || len(otherIDs) != 0 {
		t.Fatalf("expected no items for an unrelated read server, got %v", otherIDs)
	}
}
