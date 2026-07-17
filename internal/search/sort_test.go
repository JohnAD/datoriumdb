package search

import "testing"

func TestComputeSortValuesMissingNullKnown(t *testing.T) {
	def := defFrom(t, `{"$":"SearchDefinition:v1","collection":"Movies","name":"n","version":1,
		"v1":{"clauses":[{"field":"/status","op":"equals","value":"released"}],
		"sort":[{"field":"/title","dir":"asc"},{"field":"/retiredAt","dir":"asc"},{"field":"/!","dir":"asc"}]}}`)

	doc := map[string]any{"!": "id1", "title": "The Matrix", "retiredAt": nil}
	vals := ComputeSortValues(def, doc)
	if len(vals) != 3 {
		t.Fatalf("expected 3 sort values, got %d", len(vals))
	}
	if !vals[0].Present || vals[0].Null || vals[0].Value != "The Matrix" {
		t.Fatalf("unexpected title sort value: %+v", vals[0])
	}
	if !vals[1].Present || !vals[1].Null {
		t.Fatalf("expected retiredAt to be present-and-null: %+v", vals[1])
	}
	if !vals[2].Present || vals[2].Value != "id1" {
		t.Fatalf("expected id sort value from /!: %+v", vals[2])
	}

	docNoTitle := map[string]any{"!": "id2"}
	vals2 := ComputeSortValues(def, docNoTitle)
	if vals2[0].Present {
		t.Fatalf("expected missing title to be Present=false: %+v", vals2[0])
	}
}

func TestSortValuesToAndFromJSON(t *testing.T) {
	vals := []SortValue{
		{Present: true, Value: "abc"},
		{Present: true, Null: true},
		{Present: false},
	}
	j := SortValuesToJSON(vals)
	if j[0] != "abc" || j[1] != nil || j[2] != nil {
		t.Fatalf("unexpected JSON form: %v", j)
	}
	back := SortValuesFromJSON(j)
	if !back[0].Present || back[0].Null || back[0].Value != "abc" {
		t.Fatalf("unexpected round-trip value 0: %+v", back[0])
	}
	// Both explicit-null and missing serialize as JSON null, so decoding
	// always yields Null (documented simplification).
	if !back[1].Present || !back[1].Null {
		t.Fatalf("unexpected round-trip value 1: %+v", back[1])
	}
	if !back[2].Present || !back[2].Null {
		t.Fatalf("unexpected round-trip value 2 (missing collapses to null): %+v", back[2])
	}
}

func TestCompareStoredSortOrdering(t *testing.T) {
	dirs := []string{"asc"}
	known := []SortValue{{Present: true, Value: "b"}}
	if CompareStoredSort(known, []any{"a"}, dirs) <= 0 {
		t.Fatalf("expected b > a ascending")
	}
	if CompareStoredSort(known, []any{"c"}, dirs) >= 0 {
		t.Fatalf("expected b < c ascending")
	}

	// null sorts after known, both asc and desc.
	nullVal := []SortValue{{Present: true, Null: true}}
	if CompareStoredSort(nullVal, []any{"a"}, []string{"asc"}) <= 0 {
		t.Fatalf("expected null to sort after known value (asc)")
	}
	if CompareStoredSort(nullVal, []any{"a"}, []string{"desc"}) <= 0 {
		t.Fatalf("expected null to sort after known value (desc)")
	}

	// missing sorts after null.
	missingVal := []SortValue{{Present: false}}
	if CompareStoredSort(missingVal, []any{nil}, []string{"asc"}) <= 0 {
		t.Fatalf("expected missing to sort after null")
	}

	// desc flips known-value comparisons but not the missing/null ranking.
	if CompareStoredSort(known, []any{"a"}, []string{"desc"}) >= 0 {
		t.Fatalf("expected descending direction to flip known-value comparison")
	}
}

func TestCompareStoredSortNumbersAndBooleans(t *testing.T) {
	nums := []SortValue{{Present: true, Value: 5.0}}
	if CompareStoredSort(nums, []any{3.0}, []string{"asc"}) <= 0 {
		t.Fatalf("expected 5 > 3")
	}
	bools := []SortValue{{Present: true, Value: false}}
	if CompareStoredSort(bools, []any{true}, []string{"asc"}) >= 0 {
		t.Fatalf("expected false to sort before true")
	}
}
