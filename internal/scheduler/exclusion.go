package scheduler

import "sync"

// ExclusionSet tracks a set of keys currently "in progress", so a caller
// can prevent unsafe overlap between concurrent workers.
// LOCAL-ARCHITECTURE.md requires this for two cases: "The same document
// should not be processed by two workers at the same time" (keyed by
// collection+id) and "The upgrade-agent should run at most once per
// collection at a time" (keyed by collection name).
type ExclusionSet struct {
	mu     sync.Mutex
	active map[string]bool
}

// NewExclusionSet returns an empty ExclusionSet.
func NewExclusionSet() *ExclusionSet {
	return &ExclusionSet{active: map[string]bool{}}
}

// TryAcquire reports whether key was free and, if so, marks it active and
// returns true. The caller must call Release(key) when done.
func (e *ExclusionSet) TryAcquire(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active[key] {
		return false
	}
	e.active[key] = true
	return true
}

// Release frees key so a future TryAcquire can succeed.
func (e *ExclusionSet) Release(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.active, key)
}

// Active reports whether key is currently held.
func (e *ExclusionSet) Active(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.active[key]
}
