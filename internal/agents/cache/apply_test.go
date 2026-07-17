package cache

import "testing"

func TestApplyNoOpWithoutExistingCacheFile(t *testing.T) {
	dir := t.TempDir()
	item := WorkItem{SourceCollection: "Movies", SourceDocumentID: "id1", Command: "create", Payload: map[string]any{"title": "T"}}
	applied, err := Apply(dir, item)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !applied {
		t.Fatalf("expected applied=true (intent satisfied) even with no existing cache file")
	}
	if _, ok, _ := LoadCacheFile(dir, "Movies", "id1"); ok {
		t.Fatalf("expected no cache file to be created when none already existed (no sweeping reads)")
	}
}

func TestApplyCreateAndPatchUpdatesExistingCache(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureStub(dir, "Movies", "id1"); err != nil {
		t.Fatalf("EnsureStub: %v", err)
	}
	create := WorkItem{
		SourceCollection: "Movies", SourceDocumentID: "id1", Command: "create",
		AfterVersion: "v1", Payload: map[string]any{"!": "id1", "#": "v1", "title": "T1"},
	}
	if applied, err := Apply(dir, create); err != nil || !applied {
		t.Fatalf("Apply create: applied=%v err=%v", applied, err)
	}
	loaded, _, _ := LoadCacheFile(dir, "Movies", "id1")
	if loaded["title"] != "T1" || loaded["#"] != "v1" {
		t.Fatalf("unexpected cache after create: %+v", loaded)
	}

	patch := WorkItem{
		SourceCollection: "Movies", SourceDocumentID: "id1", Command: "patch",
		BeforeVersion: "v1", AfterVersion: "v2", Payload: map[string]any{"!": "id1", "#": "v2", "title": "T2"},
	}
	if applied, err := Apply(dir, patch); err != nil || !applied {
		t.Fatalf("Apply patch: applied=%v err=%v", applied, err)
	}
	loaded2, _, _ := LoadCacheFile(dir, "Movies", "id1")
	if loaded2["title"] != "T2" || loaded2["#"] != "v2" {
		t.Fatalf("unexpected cache after patch: %+v", loaded2)
	}
}

func TestApplyIsIdempotentOnAlreadyAppliedVersion(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureStub(dir, "Movies", "id1"); err != nil {
		t.Fatalf("EnsureStub: %v", err)
	}
	item := WorkItem{
		SourceCollection: "Movies", SourceDocumentID: "id1", Command: "create",
		AfterVersion: "v1", Payload: map[string]any{"!": "id1", "#": "v1", "title": "T1"},
	}
	if applied, err := Apply(dir, item); err != nil || !applied {
		t.Fatalf("first Apply: applied=%v err=%v", applied, err)
	}
	// Re-apply the exact same item (e.g. a retried request after a crash
	// right after writing but before the SOT recorded completion): the
	// version guard must make this a safe no-op, not a re-write.
	if applied, err := Apply(dir, item); err != nil || !applied {
		t.Fatalf("second Apply: applied=%v err=%v", applied, err)
	}
	loaded, _, _ := LoadCacheFile(dir, "Movies", "id1")
	if loaded["title"] != "T1" {
		t.Fatalf("unexpected cache after idempotent re-apply: %+v", loaded)
	}
}

func TestApplyDeleteKeepsCacheFileWithNilVersion(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureStub(dir, "Movies", "id1"); err != nil {
		t.Fatalf("EnsureStub: %v", err)
	}
	create := WorkItem{SourceCollection: "Movies", SourceDocumentID: "id1", Command: "create", AfterVersion: "v1", Payload: map[string]any{"!": "id1", "#": "v1", "title": "T1"}}
	if _, err := Apply(dir, create); err != nil {
		t.Fatalf("Apply create: %v", err)
	}
	del := WorkItem{SourceCollection: "Movies", SourceDocumentID: "id1", Command: "delete", BeforeVersion: "v1"}
	applied, err := Apply(dir, del)
	if err != nil || !applied {
		t.Fatalf("Apply delete: applied=%v err=%v", applied, err)
	}
	loaded, ok, _ := LoadCacheFile(dir, "Movies", "id1")
	if !ok {
		t.Fatalf("expected the cache file to still exist after delete")
	}
	if loaded["#"] != nil {
		t.Fatalf("expected version to become nil after delete, got %v", loaded["#"])
	}
	if _, hasTitle := loaded["title"]; hasTitle {
		t.Fatalf("expected the summary fields to be gone after delete: %+v", loaded)
	}
}

func TestApplyUnknownCommandErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureStub(dir, "Movies", "id1"); err != nil {
		t.Fatalf("EnsureStub: %v", err)
	}
	item := WorkItem{SourceCollection: "Movies", SourceDocumentID: "id1", Command: "bogus"}
	if _, err := Apply(dir, item); err == nil {
		t.Fatalf("expected an error for an unknown command")
	}
}

func TestApplyMissingPayloadErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureStub(dir, "Movies", "id1"); err != nil {
		t.Fatalf("EnsureStub: %v", err)
	}
	item := WorkItem{SourceCollection: "Movies", SourceDocumentID: "id1", Command: "create"}
	if _, err := Apply(dir, item); err == nil {
		t.Fatalf("expected an error for a create/patch work item with no payload")
	}
}
