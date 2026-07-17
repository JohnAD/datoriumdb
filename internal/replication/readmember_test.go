package replication

import "testing"

func TestReadMemberStateStaleThreshold(t *testing.T) {
	state := &ReadMemberState{StaleThreshold: 3}
	if state.IsStaleForSOT("serverA") {
		t.Fatalf("expected fresh state to not be stale")
	}
	if got := state.RecordCheckinFailure("serverA"); got != 1 {
		t.Fatalf("expected failure count 1, got %d", got)
	}
	if state.IsStaleForSOT("serverA") {
		t.Fatalf("expected not stale after 1/3 failures")
	}
	state.RecordCheckinFailure("serverA")
	if state.IsStaleForSOT("serverA") {
		t.Fatalf("expected not stale after 2/3 failures")
	}
	if got := state.RecordCheckinFailure("serverA"); got != 3 {
		t.Fatalf("expected failure count 3, got %d", got)
	}
	if !state.IsStaleForSOT("serverA") {
		t.Fatalf("expected stale after reaching the configured threshold (3 failed check-ins ~= readMemberCheckinSeconds*3 per REPLICATION-FAILURE-HANDLING.md)")
	}

	// A different SOT-member's shard slots are unaffected.
	if state.IsStaleForSOT("serverC") {
		t.Fatalf("expected staleness to be tracked independently per SOT-member")
	}

	state.RecordCheckinSuccess("serverA")
	if state.IsStaleForSOT("serverA") {
		t.Fatalf("expected a successful check-in to clear staleness")
	}
	if got := state.FailedCheckins("serverA"); got != 0 {
		t.Fatalf("expected failure counter reset to 0, got %d", got)
	}
}

func TestReadMemberStateDefaultThreshold(t *testing.T) {
	state := &ReadMemberState{} // StaleThreshold unset
	for i := 0; i < 2; i++ {
		state.RecordCheckinFailure("serverA")
	}
	if state.IsStaleForSOT("serverA") {
		t.Fatalf("expected not stale below the default threshold of 3")
	}
	state.RecordCheckinFailure("serverA")
	if !state.IsStaleForSOT("serverA") {
		t.Fatalf("expected stale at the default threshold of 3")
	}
}

func TestReadMemberStatePerDocumentPending(t *testing.T) {
	state := &ReadMemberState{}
	if state.IsPending("Movies", "doc1") {
		t.Fatalf("expected fresh state to have no pending documents")
	}
	state.MarkPending("Movies", "doc1")
	if !state.IsPending("Movies", "doc1") {
		t.Fatalf("expected doc1 to be marked pending")
	}
	if state.IsPending("Movies", "doc2") {
		t.Fatalf("expected doc2 to be unaffected")
	}
	state.ClearPending("Movies", "doc1")
	if state.IsPending("Movies", "doc1") {
		t.Fatalf("expected doc1 to no longer be pending after clear")
	}
}
