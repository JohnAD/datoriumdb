package replication

import (
	"sync"
)

// ReadMemberState tracks catch-up staleness for a SHARD_READ_MEMBER or
// PROXY_READ_MEMBER, per REPLICATION-FAILURE-HANDLING.md's "Read-Member
// Catch-Up": per-document staleness while a pending write is being fetched
// and applied, plus a per-SOT-server "too old to read from" gate once
// check-ins with that SOT-member have failed too many times in a row.
type ReadMemberState struct {
	mu sync.Mutex

	// StaleThreshold is general.readMemberFailedCheckinsBeforeStale: the
	// number of consecutive failed check-ins with a given SOT-member
	// before this read-member refuses all reads for that SOT-member's
	// shard slots.
	StaleThreshold int

	failedCheckins map[string]int
	staleSOT       map[string]bool
	pendingDocs    map[string]bool // key: collection + "/" + documentID
}

func docKey(collection, documentID string) string {
	return collection + "/" + documentID
}

func (s *ReadMemberState) init() {
	if s.failedCheckins == nil {
		s.failedCheckins = map[string]int{}
	}
	if s.staleSOT == nil {
		s.staleSOT = map[string]bool{}
	}
	if s.pendingDocs == nil {
		s.pendingDocs = map[string]bool{}
	}
}

// RecordCheckinSuccess resets the failure counter for sotServer and clears
// its stale-for-reads flag.
func (s *ReadMemberState) RecordCheckinSuccess(sotServer string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	delete(s.failedCheckins, sotServer)
	delete(s.staleSOT, sotServer)
}

// RecordCheckinFailure increments the consecutive-failure counter for
// sotServer, marking it stale once StaleThreshold is reached. Returns the
// new consecutive-failure count.
func (s *ReadMemberState) RecordCheckinFailure(sotServer string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	s.failedCheckins[sotServer]++
	count := s.failedCheckins[sotServer]
	threshold := s.StaleThreshold
	if threshold <= 0 {
		threshold = 3
	}
	if count >= threshold {
		s.staleSOT[sotServer] = true
	}
	return count
}

// IsStaleForSOT reports whether this read-member has declared itself too
// old to read from for sotServer's shard slots.
func (s *ReadMemberState) IsStaleForSOT(sotServer string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	return s.staleSOT[sotServer]
}

// FailedCheckins returns the current consecutive-failure count for
// sotServer (0 if none recorded or the last check-in succeeded).
func (s *ReadMemberState) FailedCheckins(sotServer string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	return s.failedCheckins[sotServer]
}

// MarkPending flags one document as known out of date: "If a read-member
// knows a specific document is out of date, it should refuse reads for
// that document until it catches up."
func (s *ReadMemberState) MarkPending(collection, documentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	s.pendingDocs[docKey(collection, documentID)] = true
}

// ClearPending un-flags a document once catch-up durably applies it.
func (s *ReadMemberState) ClearPending(collection, documentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	delete(s.pendingDocs, docKey(collection, documentID))
}

// IsPending reports whether a specific document is currently known stale.
func (s *ReadMemberState) IsPending(collection, documentID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	return s.pendingDocs[docKey(collection, documentID)]
}
