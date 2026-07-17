package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerRunsPeriodicTicks(t *testing.T) {
	var count atomic.Int32
	s := New(nil)
	s.Register(Agent{
		Name:     "test-agent",
		Interval: 10 * time.Millisecond,
		Task: func(ctx context.Context) (bool, error) {
			count.Add(1)
			return false, nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	time.Sleep(120 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)
	if count.Load() < 3 {
		t.Fatalf("expected at least 3 periodic ticks, got %d", count.Load())
	}
}

func TestSchedulerWakeTriggersImmediateWork(t *testing.T) {
	var count atomic.Int32
	s := New(nil)
	s.Register(Agent{
		Name:     "wake-agent",
		Interval: time.Hour, // effectively never fires on its own
		Task: func(ctx context.Context) (bool, error) {
			count.Add(1)
			return false, nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(10 * time.Millisecond)
	before := count.Load()
	s.Wake("wake-agent")
	time.Sleep(30 * time.Millisecond)
	if count.Load() <= before {
		t.Fatalf("expected Wake to trigger an extra task run: before=%d after=%d", before, count.Load())
	}
}

func TestSchedulerDrainsBacklogWithoutWaitingForTick(t *testing.T) {
	var remaining atomic.Int32
	remaining.Store(5)
	var ran atomic.Int32
	s := New(nil)
	s.Register(Agent{
		Name:     "drain-agent",
		Interval: time.Hour,
		Task: func(ctx context.Context) (bool, error) {
			if remaining.Load() <= 0 {
				return false, nil
			}
			remaining.Add(-1)
			ran.Add(1)
			return true, nil // didWork=true should cause an immediate re-run
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	if ran.Load() != 5 {
		t.Fatalf("expected all 5 backlog items drained without waiting for a tick, got %d", ran.Load())
	}
}

func TestSchedulerStopsOnContextCancel(t *testing.T) {
	var count atomic.Int32
	s := New(nil)
	s.Register(Agent{
		Name:     "cancel-agent",
		Interval: 5 * time.Millisecond,
		Task: func(ctx context.Context) (bool, error) {
			count.Add(1)
			return false, nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	time.Sleep(15 * time.Millisecond)
	cancel()
	time.Sleep(15 * time.Millisecond)
	after := count.Load()
	time.Sleep(30 * time.Millisecond)
	if count.Load() != after {
		t.Fatalf("expected no further task runs after context cancellation: before=%d after=%d", after, count.Load())
	}
}

func TestSchedulerRecoversFromPanickingTask(t *testing.T) {
	var count atomic.Int32
	s := New(nil)
	s.Register(Agent{
		Name:     "panic-agent",
		Interval: 10 * time.Millisecond,
		Task: func(ctx context.Context) (bool, error) {
			count.Add(1)
			panic("boom")
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	if count.Load() < 2 {
		t.Fatalf("expected the scheduler to keep ticking after a panicking task, got %d runs", count.Load())
	}
}
