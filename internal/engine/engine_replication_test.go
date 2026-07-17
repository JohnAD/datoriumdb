package engine

import (
	"reflect"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/replication"
)

func TestIdempotentRetryCreateReturnsSameResponse(t *testing.T) {
	eng := testEngine(t)
	first := eng.Execute(`create Movies null {operationId: 01OPTESTCREATE00000000001, $: Movies:0, title: "x"}`)
	if first["ok"] != true {
		t.Fatalf("first create failed: %#v", first)
	}
	second := eng.Execute(`create Movies null {operationId: 01OPTESTCREATE00000000001, $: Movies:0, title: "x"}`)
	if second["ok"] != true {
		t.Fatalf("retried create must still report success: %#v", second)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected identical replayed response.\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestIdempotentRetryPatchDoesNotReapply(t *testing.T) {
	eng := testEngine(t)
	created := eng.Execute(`create Movies null {$: Movies:0, title: "x", status: unreleased}`)
	id, _ := created["id"].(string)
	ver, _ := created["#"].(string)

	patchCmd := `patch Movies ` + id + ` {operationId: 01OPTESTPATCH000000000001, $: Movies:0, #: ` + ver + `, RFC6902: [{op: replace, path: /status, value: released}]}`
	first := eng.Execute(patchCmd)
	if first["ok"] != true {
		t.Fatalf("first patch failed: %#v", first)
	}
	second := eng.Execute(patchCmd)
	if second["ok"] != true {
		t.Fatalf("retried patch must still report success: %#v", second)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected identical replayed response.\nfirst:  %#v\nsecond: %#v", first, second)
	}

	// A third, truly new patch attempt against the now-stale "before"
	// version must fail normally (this proves the retry above did not
	// silently re-apply and bump the version a second time).
	third := eng.Execute(patchCmd)
	if !reflect.DeepEqual(first, third) {
		t.Fatalf("expected a third replay to still return the cached response: %#v", third)
	}
}

func TestIdempotentRetryDeleteAfterDocumentGone(t *testing.T) {
	eng := testEngine(t)
	created := eng.Execute(`create Movies null {$: Movies:0, title: "x"}`)
	id, _ := created["id"].(string)
	ver, _ := created["#"].(string)

	deleteCmd := `delete Movies ` + id + ` {operationId: 01OPTESTDELETE00000000001, #: ` + ver + `}`
	first := eng.Execute(deleteCmd)
	if first["ok"] != true {
		t.Fatalf("first delete failed: %#v", first)
	}
	// Without idempotent replay, retrying against an already-deleted
	// document would normally return documentNotFound.
	second := eng.Execute(deleteCmd)
	if second["ok"] != true {
		t.Fatalf("retried delete must still report success even though the document is now gone: %#v", second)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected identical replayed response.\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestFreshOperationIDsAreNotTreatedAsRetries(t *testing.T) {
	eng := testEngine(t)
	first := eng.Execute(`create Movies null {operationId: 01OPTESTFRESH0000000001A, $: Movies:0, title: "x"}`)
	second := eng.Execute(`create Movies null {operationId: 01OPTESTFRESH0000000001B, $: Movies:0, title: "y"}`)
	if first["ok"] != true || second["ok"] != true {
		t.Fatalf("expected both distinct-operationId creates to succeed: %#v / %#v", first, second)
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
