package search

import (
	"path/filepath"
	"testing"
)

func fakeIDs() func() (string, error) {
	n := 0
	return func() (string, error) {
		n++
		return "VER" + string(rune('0'+n)), nil
	}
}

func TestLoadResultFileMissing(t *testing.T) {
	dir := t.TempDir()
	rf, existed, err := LoadResultFile(filepath.Join(dir, "matches.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if existed {
		t.Fatalf("expected existed=false for a missing file")
	}
	if len(rf.Items) != 0 {
		t.Fatalf("expected empty items")
	}
}

func TestResultFileUpsertOrderingAndIdempotency(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/status","op":"equals","value":"released"}],
		"sort":[{"field":"/title","dir":"asc"}]}}`)

	rf := &ResultFile{}
	sortB := []SortValue{{Present: true, Value: "B"}}
	sortA := []SortValue{{Present: true, Value: "A"}}
	sortC := []SortValue{{Present: true, Value: "C"}}

	if !rf.Upsert(def, "id-b", sortB) {
		t.Fatalf("expected first upsert to report changed")
	}
	if !rf.Upsert(def, "id-a", sortA) {
		t.Fatalf("expected second upsert to report changed")
	}
	if !rf.Upsert(def, "id-c", sortC) {
		t.Fatalf("expected third upsert to report changed")
	}
	if len(rf.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(rf.Items))
	}
	if rf.Items[0].ID != "id-a" || rf.Items[1].ID != "id-b" || rf.Items[2].ID != "id-c" {
		t.Fatalf("expected sorted order a,b,c; got %v %v %v", rf.Items[0].ID, rf.Items[1].ID, rf.Items[2].ID)
	}

	// Idempotent duplicate upsert with the same sort value: no change.
	if rf.Upsert(def, "id-a", sortA) {
		t.Fatalf("expected duplicate upsert with identical sort value to report unchanged")
	}
	if len(rf.Items) != 3 {
		t.Fatalf("expected item count to stay at 3 after a no-op upsert")
	}

	// Repositioning: change id-a's sort value so it moves to the end.
	sortZ := []SortValue{{Present: true, Value: "Z"}}
	if !rf.Upsert(def, "id-a", sortZ) {
		t.Fatalf("expected a repositioning upsert to report changed")
	}
	if rf.Items[len(rf.Items)-1].ID != "id-a" {
		t.Fatalf("expected id-a to move to the end after its sort value changed, got order %v", itemIDs(rf.Items))
	}
}

func itemIDs(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func TestResultFileRemove(t *testing.T) {
	rf := &ResultFile{Items: []Item{{ID: "a"}, {ID: "b"}}}
	if !rf.Remove("a") {
		t.Fatalf("expected remove to report present")
	}
	if len(rf.Items) != 1 || rf.Items[0].ID != "b" {
		t.Fatalf("unexpected items after remove: %v", rf.Items)
	}
	if rf.Remove("does-not-exist") {
		t.Fatalf("expected remove of an absent id to report false")
	}
}

func TestApplyMutationCreatesWritesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matches.json")
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/status","op":"equals","value":"released"}],
		"sort":[{"field":"/title","dir":"asc"}]}}`)
	next := fakeIDs()

	applied, ver1, err := ApplyMutation(path, next, func(rf *ResultFile, existed bool) (bool, error) {
		if existed {
			t.Fatalf("expected file to not exist yet")
		}
		rf.Search = "n"
		rf.Collection = "Movies"
		return rf.Upsert(def, "id1", []SortValue{{Present: true, Value: "A"}}), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied || ver1 == "" {
		t.Fatalf("expected the first mutation to apply and return a version")
	}

	rf, existed, err := LoadResultFile(path)
	if err != nil || !existed {
		t.Fatalf("expected the file to exist after ApplyMutation: err=%v existed=%v", err, existed)
	}
	if len(rf.Items) != 1 || rf.Items[0].ID != "id1" {
		t.Fatalf("unexpected items: %v", rf.Items)
	}
	if rf.Version != ver1 {
		t.Fatalf("expected stored version %q to match returned version %q", rf.Version, ver1)
	}

	// Idempotent no-op: upserting the exact same id/sort again should not
	// bump the version or change anything.
	applied2, ver2, err := ApplyMutation(path, next, func(rf *ResultFile, existed bool) (bool, error) {
		return rf.Upsert(def, "id1", []SortValue{{Present: true, Value: "A"}}), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied2 {
		t.Fatalf("expected the second identical mutation to be a no-op")
	}
	if ver2 != ver1 {
		t.Fatalf("expected version to stay at %q after a no-op, got %q", ver1, ver2)
	}
}

func TestApplyMutationRemoveOnMissingFileIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matches.json")
	next := fakeIDs()
	applied, _, err := ApplyMutation(path, next, func(rf *ResultFile, existed bool) (bool, error) {
		if existed {
			t.Fatalf("expected no existing file")
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied {
		t.Fatalf("expected remove-on-missing-file to be a no-op")
	}
}
