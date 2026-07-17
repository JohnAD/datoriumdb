package search

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"time"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// Item is one entry inside a stored matches.json file, per SEARCHING.md.
type Item struct {
	Sort []any  `json:"sort"`
	ID   string `json:"id"`
}

// ResultFile is the JSON shape of a stored search bucket file
// (matches.json), per SEARCHING.md.
type ResultFile struct {
	Version    string `json:"#"`
	Schema     string `json:"$"`
	Search     string `json:"search"`
	Collection string `json:"collection"`
	Key        []any  `json:"key"`
	Items      []Item `json:"items"`
}

// LoadResultFile reads path. If the file does not exist, it returns an
// empty (existed=false) ResultFile rather than an error, since a search
// bucket file is created lazily on first upsert.
func LoadResultFile(path string) (rf *ResultFile, existed bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ResultFile{}, false, nil
		}
		return nil, false, err
	}
	var out ResultFile
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, false, fmt.Errorf("invalid search result JSON %s: %w", path, err)
	}
	return &out, true, nil
}

// WriteResultFile writes rf to path using the same atomic same-directory
// rename as document writes (FILESTYSTEM-STORAGE.md "File Update Safety").
func WriteResultFile(path string, rf *ResultFile) error {
	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fsstore.WriteFileAtomic(path, data, 0o644)
}

// IndexOf returns the index of id inside rf.Items, or -1.
func (rf *ResultFile) IndexOf(id string) int {
	for i, it := range rf.Items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

// Remove deletes id from rf.Items, reporting whether it was present.
func (rf *ResultFile) Remove(id string) bool {
	idx := rf.IndexOf(id)
	if idx < 0 {
		return false
	}
	rf.Items = append(rf.Items[:idx], rf.Items[idx+1:]...)
	return true
}

// Upsert inserts or repositions id at the correct sorted position per
// def.Sort, given freshly computed sort values. It is idempotent: if id is
// already present with the same sort values, it reports changed=false and
// makes no change (AGENT-FOR-CHANGE-DISTRIBUTION.md: "If the document ID
// is already in the correct search file with the correct sort values, no
// change is needed.").
func (rf *ResultFile) Upsert(def *Definition, id string, sortVals []SortValue) (changed bool) {
	dirs := make([]string, len(def.Sort))
	for i, s := range def.Sort {
		dirs[i] = s.Dir
	}
	newSortJSON := SortValuesToJSON(sortVals)
	if idx := rf.IndexOf(id); idx >= 0 {
		if reflect.DeepEqual(rf.Items[idx].Sort, newSortJSON) {
			return false
		}
		rf.Items = append(rf.Items[:idx], rf.Items[idx+1:]...)
	}
	insertAt := len(rf.Items)
	for i, it := range rf.Items {
		if CompareStoredSort(sortVals, it.Sort, dirs) < 0 {
			insertAt = i
			break
		}
	}
	rf.Items = append(rf.Items, Item{})
	copy(rf.Items[insertAt+1:], rf.Items[insertAt:])
	rf.Items[insertAt] = Item{Sort: newSortJSON, ID: id}
	return true
}

// ApplyMutation performs a read-modify-write against path with the same
// "write then re-read and verify #" safety pattern used for documents
// (FILESTYSTEM-STORAGE.md "Version Verification"), retrying mutate against
// fresh state on a lost race. mutate must be deterministic given the
// current file state so a retry safely recomputes the same intended
// change; it returns changed=false to signal "no write needed" (an
// idempotent no-op, e.g. the id is already correctly placed).
func ApplyMutation(path string, newVersionID func() (string, error), mutate func(rf *ResultFile, existed bool) (changed bool, err error)) (applied bool, finalVersion string, err error) {
	const maxAttempts = 10
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		rf, existed, lerr := LoadResultFile(path)
		if lerr != nil {
			return false, "", lerr
		}
		if rf.Schema == "" {
			rf.Schema = "SearchResult:v1"
		}
		changed, merr := mutate(rf, existed)
		if merr != nil {
			return false, "", merr
		}
		if !changed {
			return false, rf.Version, nil
		}
		newVer, verr := newVersionID()
		if verr != nil {
			return false, "", verr
		}
		rf.Version = newVer
		if err := WriteResultFile(path, rf); err != nil {
			return false, "", err
		}
		check, _, cerr := LoadResultFile(path)
		if cerr == nil && check.Version == newVer {
			return true, newVer, nil
		}
		lastErr = cerr
		time.Sleep(time.Duration(10+rand.Intn(40)) * time.Millisecond)
	}
	return false, "", fmt.Errorf("search result apply retries exhausted for %s: %w", path, lastErr)
}
