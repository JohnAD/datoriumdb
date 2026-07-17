package idgen

import (
	"crypto/rand"
	"io"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Clock returns the current time used for ULID generation.
type Clock func() time.Time

var (
	mu    sync.Mutex
	clock Clock = func() time.Time { return time.Now().UTC() }
	ent   io.Reader
)

func init() {
	ent = ulid.Monotonic(rand.Reader, 0)
}

// SetClock replaces the clock used for ULID timestamps. Intended for tests.
func SetClock(c Clock) (restore func()) {
	mu.Lock()
	prev := clock
	clock = c
	mu.Unlock()
	return func() {
		mu.Lock()
		clock = prev
		mu.Unlock()
	}
}

// New returns a new ULID string.
func New() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	id, err := ulid.New(ulid.Timestamp(clock()), ent)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// MustNew returns a new ULID or panics.
func MustNew() string {
	id, err := New()
	if err != nil {
		panic(err)
	}
	return id
}

// ValidDocumentID reports whether id is safe for filesystem document names.
// IDs must be non-empty, not "." / "..", and contain only [A-Za-z0-9_-].
func ValidDocumentID(id string) bool {
	if id == "" || id == "." || id == ".." || id == "null" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}
