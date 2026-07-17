package fsstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CollectionDir returns {dataDir}/{collection}.
func CollectionDir(dataDir, collection string) string {
	return filepath.Join(dataDir, collection)
}

// DocumentPath returns the live document path.
func DocumentPath(dataDir, collection, id string) string {
	return filepath.Join(CollectionDir(dataDir, collection), id+".json")
}

// PreviousDocumentPath returns the oldest-undistributed previous document path.
func PreviousDocumentPath(dataDir, collection, id string) string {
	return filepath.Join(CollectionDir(dataDir, collection), "."+id+".json")
}

// DeletedDocumentPath returns the soft-deleted document path (same as previous).
func DeletedDocumentPath(dataDir, collection, id string) string {
	return PreviousDocumentPath(dataDir, collection, id)
}

// EnsureCollectionDir creates the collection directory if needed.
func EnsureCollectionDir(dataDir, collection string) error {
	return os.MkdirAll(CollectionDir(dataDir, collection), 0o755)
}

// SafeID reports whether id is usable as a document filename component.
func SafeID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, string(os.PathSeparator)) {
		return false
	}
	cleaned := filepath.Clean(id)
	if cleaned != id || strings.HasPrefix(cleaned, "..") {
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

// WriteDocumentJSON writes a document object as pretty JSON.
func WriteDocumentJSON(path string, doc map[string]any) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteFileAtomic(path, data, 0o644)
}

// ReadDocumentJSON reads a document object from path.
func ReadDocumentJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid document JSON %s: %w", path, err)
	}
	return doc, nil
}

// PreservePreviousIfAbsent renames live document to .{id}.json only when that
// previous file does not already exist (oldest undistributed previous wins).
func PreservePreviousIfAbsent(dataDir, collection, id string) error {
	live := DocumentPath(dataDir, collection, id)
	prev := PreviousDocumentPath(dataDir, collection, id)
	if _, err := os.Stat(prev); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(live); err != nil {
		return err
	}
	return os.Rename(live, prev)
}

// SoftDeleteDocument moves the live document to the previous/deleted path,
// preserving an existing previous file by removing the live document instead.
func SoftDeleteDocument(dataDir, collection, id string) error {
	live := DocumentPath(dataDir, collection, id)
	prev := PreviousDocumentPath(dataDir, collection, id)
	if _, err := os.Stat(prev); err == nil {
		return os.Remove(live)
	}
	return os.Rename(live, prev)
}

// ChangeQueueDir returns the per-collection change queue directory.
func ChangeQueueDir(dataDir, collection string) string {
	return filepath.Join(CollectionDir(dataDir, collection), ".changeQueue")
}

// EnqueueChange writes an empty .queue marker for the change agent.
func EnqueueChange(dataDir, collection, id, change string) error {
	dir := ChangeQueueDir(dataDir, collection)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s__%s__%s.queue", change, collection, id)
	return WriteFileAtomic(filepath.Join(dir, name), []byte(""), 0o644)
}

// OperationsDir returns durable per-operation state directory.
func OperationsDir(dataDir string) string {
	return filepath.Join(dataDir, ".operations")
}

// OperationPath returns the durable operation record path.
func OperationPath(dataDir, operationID string) string {
	return filepath.Join(OperationsDir(dataDir), operationID+".json")
}

// SearchDir returns the collection's precompiled-search result tree root,
// e.g. {dataDir}/{collection}/.search.
func SearchDir(dataDir, collection string) string {
	return filepath.Join(CollectionDir(dataDir, collection), ".search")
}

// SearchResultPath returns the matches.json path for one encoded search
// bucket, per SEARCHING.md's "Search File Paths".
func SearchResultPath(dataDir, collection, searchName string, encodedSegments []string) string {
	parts := append([]string{SearchDir(dataDir, collection), searchName}, encodedSegments...)
	parts = append(parts, "matches.json")
	return filepath.Join(parts...)
}

// PendingCacheUpdatesDir returns the source collection's pending
// cache-update work item directory, per CACHE-UPDATES.md:
// /db/{CollectionName}/.pendingCacheUpdates.
func PendingCacheUpdatesDir(dataDir, sourceCollection string) string {
	return filepath.Join(CollectionDir(dataDir, sourceCollection), ".pendingCacheUpdates")
}

// PendingCacheUpdatePath returns one pending cache-update work item file
// path: {sourceCollection}/.pendingCacheUpdates/{readServerName}.{docId}.json.
func PendingCacheUpdatePath(dataDir, sourceCollection, readServerName, docID string) string {
	return filepath.Join(PendingCacheUpdatesDir(dataDir, sourceCollection), readServerName+"."+docID+".json")
}

// CacheDir returns this server's cached-summary root for one source
// collection: {dataDir}/.cache/{SourceCollection}. Per CACHE-UPDATES.md,
// "Each read server stores at most one cached summary file for a given
// source collection and source document ID" shared across every local
// collection/field that references it, so cache storage is rooted at the
// data directory rather than under any one declaring collection.
func CacheDir(dataDir, sourceCollection string) string {
	return filepath.Join(dataDir, ".cache", sourceCollection)
}

// CachePath returns the local cached-summary file path for one source
// document.
func CachePath(dataDir, sourceCollection, sourceDocID string) string {
	return filepath.Join(CacheDir(dataDir, sourceCollection), sourceDocID+".json")
}

// ListCollections returns the collection directory names directly under
// dataDir (any entry that is a directory and does not start with a dot).
func ListCollections(dataDir string) ([]string, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// ListQueueEntries returns the .queue and .taken filenames currently in a
// collection's .changeQueue directory (not full paths).
func ListQueueEntries(dataDir, collection string) ([]string, error) {
	dir := ChangeQueueDir(dataDir, collection)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	return out, nil
}

// ParseQueueFilename parses "{change}__{collection}__{id}.queue" or
// "...taken", per AGENT-FOR-CHANGE-DISTRIBUTION.md.
func ParseQueueFilename(name string) (change, collection, id, ext string, ok bool) {
	var base string
	switch {
	case strings.HasSuffix(name, ".queue"):
		ext = "queue"
		base = strings.TrimSuffix(name, ".queue")
	case strings.HasSuffix(name, ".taken"):
		ext = "taken"
		base = strings.TrimSuffix(name, ".taken")
	default:
		return "", "", "", "", false
	}
	parts := strings.SplitN(base, "__", 3)
	if len(parts) != 3 {
		return "", "", "", "", false
	}
	return parts[0], parts[1], parts[2], ext, true
}

// QueueEntryPath returns the full path to a change-queue entry filename
// (as returned by ListQueueEntries) inside collection's .changeQueue dir.
func QueueEntryPath(dataDir, collection, filename string) string {
	return filepath.Join(ChangeQueueDir(dataDir, collection), filename)
}

// TakenQueuePath returns the .taken path for a claimed queue entry.
func TakenQueuePath(dataDir, collection, change, id string) string {
	name := fmt.Sprintf("%s__%s__%s.taken", change, collection, id)
	return filepath.Join(ChangeQueueDir(dataDir, collection), name)
}
