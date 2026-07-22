package replication

import (
	"testing"

	"github.com/JohnAD/datoriumdb/internal/envelope"
)

func TestOperationStateTransitions(t *testing.T) {
	dir := t.TempDir()
	item := DocumentWorkItem{
		Collection:   "Movies",
		ID:           "01DOC0000000000000000000A",
		AfterVersion: "01VER0000000000000000000A",
		OperationID:  "01OP00000000000000000000A",
		Command:      "create",
		Payload:      mustPayload(map[string]any{"!": "01DOC0000000000000000000A"}),
	}
	op, err := Begin(dir, item)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if op.State != StateReceived {
		t.Fatalf("expected initial state received, got %s", op.State)
	}

	reloaded, err := Load(dir, item.OperationID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.State != StateReceived || reloaded.Item.Collection != "Movies" {
		t.Fatalf("unexpected reloaded operation: %#v", reloaded)
	}

	transitions := []State{StateValidated, StateCommittedLocal, StateReplicating, StateReplicated}
	for _, want := range transitions {
		if err := op.SetState(dir, want); err != nil {
			t.Fatalf("SetState(%s): %v", want, err)
		}
		reloaded, err := Load(dir, item.OperationID)
		if err != nil {
			t.Fatalf("Load after SetState(%s): %v", want, err)
		}
		if reloaded.State != want {
			t.Fatalf("expected persisted state %s, got %s", want, reloaded.State)
		}
	}
	if !op.State.Terminal() {
		t.Fatalf("expected replicated to be terminal")
	}
}

func TestOperationFailedStateIsTerminal(t *testing.T) {
	dir := t.TempDir()
	op, err := Begin(dir, DocumentWorkItem{Collection: "Movies", ID: "d1", OperationID: "op1", Command: "create"})
	if err != nil {
		t.Fatal(err)
	}
	if err := op.SetState(dir, StateFailed); err != nil {
		t.Fatal(err)
	}
	if !op.State.Terminal() {
		t.Fatalf("expected failed to be terminal")
	}
	for _, s := range []State{StateReceived, StateValidated, StateCommittedLocal, StateReplicating} {
		if s.Terminal() {
			t.Fatalf("expected %s to be non-terminal", s)
		}
	}
}

func TestBeginRequiresOperationID(t *testing.T) {
	dir := t.TempDir()
	if _, err := Begin(dir, DocumentWorkItem{Collection: "Movies", ID: "d1", Command: "create"}); err == nil {
		t.Fatalf("expected error for missing operationId")
	}
}

func TestOperationPersistsResponseForIdempotentRetry(t *testing.T) {
	dir := t.TempDir()
	op, err := Begin(dir, DocumentWorkItem{Collection: "Movies", ID: "d1", OperationID: "op1", Command: "create"})
	if err != nil {
		t.Fatal(err)
	}
	op.Response = envelope.OK(map[string]any{"command": "create", "id": "d1"})
	if err := op.Save(dir); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(dir, "op1")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Response["id"] != "d1" {
		t.Fatalf("expected persisted response to round-trip, got %#v", reloaded.Response)
	}
}

func TestListIncompleteFiltersTerminalOperations(t *testing.T) {
	dir := t.TempDir()
	mustBegin := func(id string) *Operation {
		op, err := Begin(dir, DocumentWorkItem{Collection: "Movies", ID: id, OperationID: id, Command: "create"})
		if err != nil {
			t.Fatal(err)
		}
		return op
	}

	replicated := mustBegin("op-replicated")
	if err := replicated.SetState(dir, StateReplicated); err != nil {
		t.Fatal(err)
	}
	failed := mustBegin("op-failed")
	if err := failed.SetState(dir, StateFailed); err != nil {
		t.Fatal(err)
	}
	committed := mustBegin("op-committed-local")
	if err := committed.SetState(dir, StateCommittedLocal); err != nil {
		t.Fatal(err)
	}
	replicating := mustBegin("op-replicating")
	if err := replicating.SetState(dir, StateReplicating); err != nil {
		t.Fatal(err)
	}

	incomplete, err := ListIncomplete(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(incomplete) != 2 {
		t.Fatalf("expected 2 incomplete operations, got %d: %#v", len(incomplete), incomplete)
	}
	ids := map[string]bool{}
	for _, op := range incomplete {
		ids[op.OperationID] = true
	}
	if !ids["op-committed-local"] || !ids["op-replicating"] {
		t.Fatalf("expected committedLocal and replicating operations, got %#v", ids)
	}
}

func TestListIncompleteOnMissingDir(t *testing.T) {
	dir := t.TempDir()
	incomplete, err := ListIncomplete(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(incomplete) != 0 {
		t.Fatalf("expected no incomplete operations, got %#v", incomplete)
	}
}
