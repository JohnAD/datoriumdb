// Package scheduler implements the in-process agent scheduler described in
// tech-docs/LOCAL-ARCHITECTURE.md: one worker per agent type by default,
// context cancellation for shutdown, wake channels so writes can trigger
// immediate work, and periodic scans as a safety net. It also provides a
// keyed exclusion set so a caller can prevent unsafe overlap ("The same
// document should not be processed by two workers at the same time", "The
// upgrade-agent should run at most once per collection at a time").
package scheduler

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

// Task is one unit of agent work. RunOnce should perform at most one small
// piece of work (for example, claim and process a single change-queue
// entry) and report whether it did anything. When it returns didWork=true,
// the scheduler calls it again immediately (without waiting for the next
// tick or wake), so a worker drains a backlog quickly. When it returns
// didWork=false, the worker goes back to waiting for a tick or a wake.
type Task func(ctx context.Context) (didWork bool, err error)

// Agent describes one registered background agent.
type Agent struct {
	// Name identifies the agent for logging and Wake/registration lookup.
	Name string
	// Interval is the periodic safety-scan period. Required.
	Interval time.Duration
	// Task is the unit of work the agent performs.
	Task Task
	// Workers is the worker-goroutine count for this agent. Defaults to 1
	// per LOCAL-ARCHITECTURE.md's "default MVP scheduler can be
	// conservative, with one worker per agent type".
	Workers int
	// Logger overrides the scheduler's default logger for this agent.
	Logger *log.Logger
}

// Scheduler runs a set of registered Agents, each with its own wake
// channel, periodic ticker, and worker goroutines, until Stop is called or
// the context passed to Start is cancelled.
type Scheduler struct {
	mu     sync.Mutex
	agents map[string]*runningAgent
	logger *log.Logger
}

type runningAgent struct {
	agent Agent
	wake  chan struct{}
}

// New creates an empty Scheduler. logger may be nil to use the standard
// package-level logger.
func New(logger *log.Logger) *Scheduler {
	return &Scheduler{agents: map[string]*runningAgent{}, logger: logger}
}

// Register adds an agent definition. It must be called before Start.
// Registering two agents with the same Name replaces the earlier one.
func (s *Scheduler) Register(a Agent) {
	if a.Workers <= 0 {
		a.Workers = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[a.Name] = &runningAgent{agent: a, wake: make(chan struct{}, 1)}
}

// Wake schedules an immediate extra work attempt for the named agent's
// workers, in addition to its periodic scan. It never blocks and is a
// no-op for an unregistered or not-yet-started agent name.
func (s *Scheduler) Wake(name string) {
	s.mu.Lock()
	ra, ok := s.agents[name]
	s.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ra.wake <- struct{}{}:
	default:
	}
}

// Start launches every registered agent's worker goroutines and returns
// immediately. Each worker runs until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	agents := make([]*runningAgent, 0, len(s.agents))
	for _, ra := range s.agents {
		agents = append(agents, ra)
	}
	s.mu.Unlock()
	for _, ra := range agents {
		for i := 0; i < ra.agent.Workers; i++ {
			go s.runWorker(ctx, ra)
		}
	}
}

func (s *Scheduler) runWorker(ctx context.Context, ra *runningAgent) {
	logger := ra.agent.Logger
	if logger == nil {
		logger = s.logger
	}
	interval := ra.agent.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(jitterInterval(interval))
	defer ticker.Stop()
	for {
		// Drain any immediately available work before waiting again, so
		// a backlog is processed promptly instead of one item per tick.
		for {
			did, err := s.safeRun(ctx, ra.agent)
			if err != nil {
				logf(logger, "agent %s: %v", ra.agent.Name, err)
			}
			if ctx.Err() != nil {
				return
			}
			if !did {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-ra.wake:
		}
	}
}

// safeRun recovers from a panicking Task so one bad agent tick cannot take
// down the whole scheduler.
func (s *Scheduler) safeRun(ctx context.Context, a Agent) (did bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &panicError{agent: a.Name, value: r}
		}
	}()
	if a.Task == nil {
		return false, nil
	}
	return a.Task(ctx)
}

type panicError struct {
	agent string
	value any
}

func (p *panicError) Error() string {
	return "agent " + p.agent + " panicked: " + toString(p.value)
}

func toString(v any) string {
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return fmt.Sprint(v)
}

func logf(logger *log.Logger, format string, args ...any) {
	if logger != nil {
		logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// jitterInterval adds up to 10% random jitter so multiple agents' periodic
// scans do not all fire in lockstep.
func jitterInterval(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	jitter := time.Duration(rand.Int63n(int64(base) / 10))
	return base + jitter
}
