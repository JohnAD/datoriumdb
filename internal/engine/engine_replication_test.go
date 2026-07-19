package engine

import (
	"testing"

	"github.com/JohnAD/datoriumdb/internal/replication"
)

func TestCreateRetrySameIDReturnsDocumentExists(t *testing.T) {
	eng := testEngine(t)
	id := "01TESTCREATE0000000000001"
	first := eng.Execute(`create Movies ` + id + ` {$: Movies:0, title: "x"}`)
	if first["ok"] != true {
		t.Fatalf("first create failed: %#v", first)
	}
	second := eng.Execute(`create Movies ` + id + ` {$: Movies:0, title: "x"}`)
	if second["ok"] != false {
		t.Fatalf("retried create with the same id must fail: %#v", second)
	}
	if code := firstErrorCode(second); code != "documentExists" {
		t.Fatalf("expected documentExists, got %#v", second)
	}
}

func TestCreateRejectsNullID(t *testing.T) {
	eng := testEngine(t)
	res := eng.Execute(`create Movies null {$: Movies:0, title: "x"}`)
	if res["ok"] != false {
		t.Fatalf("expected failure: %#v", res)
	}
	if code := firstErrorCode(res); code != "documentIdRequired" {
		t.Fatalf("expected documentIdRequired, got %#v", res)
	}
}

func TestPatchRetryAfterSuccessReturnsVersionMismatch(t *testing.T) {
	eng := testEngine(t)
	created := eng.Execute(`create Movies 01TESTPATCH00000000000001 {$: Movies:0, title: "x", status: unreleased}`)
	id, _ := created["id"].(string)
	ver, _ := created["#"].(string)

	patchCmd := `patch Movies ` + id + ` {operationId: 01OPTESTPATCH000000000001, $: Movies:0, #: ` + ver + `, RFC6902: [{op: replace, path: /status, value: released}]}`
	first := eng.Execute(patchCmd)
	if first["ok"] != true {
		t.Fatalf("first patch failed: %#v", first)
	}
	// Patch does not keep durable per-operation replay state. A retry
	// against the pre-patch version is versionMismatch.
	second := eng.Execute(patchCmd)
	if second["ok"] != false {
		t.Fatalf("retried patch must fail once the version has moved: %#v", second)
	}
	if code := firstErrorCode(second); code != "versionMismatch" {
		t.Fatalf("expected versionMismatch, got %#v", second)
	}
}

func TestDeleteRetryAfterGoneReturnsDocumentNotFound(t *testing.T) {
	eng := testEngine(t)
	created := eng.Execute(`create Movies 01TESTDELETE0000000000001 {$: Movies:0, title: "x"}`)
	id, _ := created["id"].(string)
	ver, _ := created["#"].(string)

	deleteCmd := `delete Movies ` + id + ` {operationId: 01OPTESTDELETE00000000001, #: ` + ver + `}`
	first := eng.Execute(deleteCmd)
	if first["ok"] != true {
		t.Fatalf("first delete failed: %#v", first)
	}
	// Delete does not keep durable per-operation replay state. A retry
	// after the document is gone is documentNotFound (READ apply of the
	// staged pending delete remains idempotent on its own).
	second := eng.Execute(deleteCmd)
	if second["ok"] != false {
		t.Fatalf("retried delete must fail once the document is gone: %#v", second)
	}
	if code := firstErrorCode(second); code != "documentNotFound" {
		t.Fatalf("expected documentNotFound, got %#v", second)
	}
}

func TestDistinctCreateIDsAreIndependent(t *testing.T) {
	eng := testEngine(t)
	first := eng.Execute(`create Movies 01TESTFRESH0000000000001A {$: Movies:0, title: "x"}`)
	second := eng.Execute(`create Movies 01TESTFRESH0000000000001B {$: Movies:0, title: "y"}`)
	if first["ok"] != true || second["ok"] != true {
		t.Fatalf("expected both creates to succeed: %#v / %#v", first, second)
	}
	if first["id"] == second["id"] {
		t.Fatalf("expected two distinct documents, got the same id for both")
	}
}

func TestReadRefusedForStaleSOTCheckins(t *testing.T) {
	eng, low, high := multiServerEngine(t, "serverB")
	lowID := idInSlotRange(t, low, high)
	eng.ReadState = &replication.ReadMemberState{StaleThreshold: 2}

	// Before any failed check-ins, routing lets the read through (and it
	// fails with documentNotFound, not staleness).
	res := eng.Execute(`read Movies ` + lowID + ` {}`)
	if code := firstErrorCode(res); code != "documentNotFound" {
		t.Fatalf("expected documentNotFound before staleness, got %#v", res)
	}

	eng.ReadState.RecordCheckinFailure("serverA")
	eng.ReadState.RecordCheckinFailure("serverA")

	res = eng.Execute(`read Movies ` + lowID + ` {}`)
	if res["ok"] != false {
		t.Fatalf("expected reads to be refused once the read-member is stale for its SOT: %#v", res)
	}
	if code := firstErrorCode(res); code != "readMemberStale" {
		t.Fatalf("expected readMemberStale, got %#v", res)
	}
}

func TestReadRefusedForPendingDocument(t *testing.T) {
	eng, low, high := multiServerEngine(t, "serverB")
	lowID := idInSlotRange(t, low, high)
	eng.ReadState = &replication.ReadMemberState{}
	eng.ReadState.MarkPending("Movies", lowID)

	res := eng.Execute(`read Movies ` + lowID + ` {}`)
	if res["ok"] != false {
		t.Fatalf("expected read to be refused for a known-stale document: %#v", res)
	}
	if code := firstErrorCode(res); code != "documentStale" {
		t.Fatalf("expected documentStale, got %#v", res)
	}

	eng.ReadState.ClearPending("Movies", lowID)
	res = eng.Execute(`read Movies ` + lowID + ` {}`)
	if code := firstErrorCode(res); code != "documentNotFound" {
		t.Fatalf("expected documentNotFound once no longer pending, got %#v", res)
	}
}
