package idgen

import (
	"crypto/rand"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

// Clock returns the current time used for ULID generation.
type Clock func() time.Time

// MaxDocumentIDRunes is the CONVENTIONS.md limit on document ID length.
const MaxDocumentIDRunes = 255

// MaxDocumentIDBytes is the UTF-8 byte limit that keeps generated filenames
// under common NAME_MAX (255) budgets. PreviousDocumentPath uses
// ".{id}.json" (6 extra bytes), so the ID itself may be at most 249 bytes.
const MaxDocumentIDBytes = 249

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

// ValidDocumentID reports whether id matches CONVENTIONS.md document-ID
// rules: non-empty; not ".", "..", or "null"; no leading '.'; at most
// MaxDocumentIDRunes runes and MaxDocumentIDBytes UTF-8 bytes; charset
// [A-Za-z0-9_.-].
func ValidDocumentID(id string) bool {
	if id == "" || id == "." || id == ".." || id == "null" {
		return false
	}
	if strings.HasPrefix(id, ".") {
		return false
	}
	if utf8.RuneCountInString(id) > MaxDocumentIDRunes {
		return false
	}
	if len(id) > MaxDocumentIDBytes {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}
