package scheduler

import "testing"

func TestExclusionSetTryAcquireRelease(t *testing.T) {
	e := NewExclusionSet()
	if !e.TryAcquire("a") {
		t.Fatalf("expected first acquire of a free key to succeed")
	}
	if e.TryAcquire("a") {
		t.Fatalf("expected a second acquire of a held key to fail")
	}
	if !e.Active("a") {
		t.Fatalf("expected key to be reported active")
	}
	e.Release("a")
	if e.Active("a") {
		t.Fatalf("expected key to be inactive after release")
	}
	if !e.TryAcquire("a") {
		t.Fatalf("expected acquire to succeed again after release")
	}
}

func TestExclusionSetIndependentKeys(t *testing.T) {
	e := NewExclusionSet()
	if !e.TryAcquire("a") || !e.TryAcquire("b") {
		t.Fatalf("expected independent keys to be acquirable concurrently")
	}
}
