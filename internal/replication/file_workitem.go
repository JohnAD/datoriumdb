package replication

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// FileWorkItem is one attachment create/update/delete for binary replication.
// Create/update carry content metadata; the bytes travel as a raw HTTP body on
// happy-path push. Delete is metadata-only.
type FileWorkItem struct {
	Collection  string `json:"collection"`
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType,omitempty"`
	ByteSize    int64  `json:"byteSize,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Version     string `json:"version"`
	OperationID string `json:"operationId"`
	Command     string `json:"command"` // fileCreate, fileUpdate, fileDelete
}

// PendingFileWritesDir returns {dataDir}/{collection}/.pendingFileWrites.
func PendingFileWritesDir(dataDir, collection string) string {
	return fsstore.PendingFileWritesDir(dataDir, collection)
}

// PendingFileWritePath returns {target}.{docId}.{filename}.json under pendingFileWrites.
// Filename is the attachment basename (already SafeFileName-validated); dots in
// the basename are preserved. Parsing recovers docID by requiring a known
// target prefix and treating the final ".json" suffix as terminator, with
// docID taken as the ULID-safe segment after target and before the filename
// remainder — see ParsePendingFileWriteName.
func PendingFileWritePath(dataDir, collection, targetServer, docID, filename string) string {
	return filepath.Join(PendingFileWritesDir(dataDir, collection), targetServer+"."+docID+"."+filename+".json")
}

// WritePendingFileWrite records the latest desired file state for targetServer.
// A later write replaces any older pending state for the same key; delete
// supersedes pending content writes.
func WritePendingFileWrite(dataDir, collection, targetServer string, item FileWorkItem) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(PendingFileWritesDir(dataDir, collection), 0o755); err != nil {
		return err
	}
	return fsstore.WriteFileAtomic(PendingFileWritePath(dataDir, collection, targetServer, item.ID, item.Filename), data, 0o644)
}

// ReadPendingFileWrite reads one pending file write.
func ReadPendingFileWrite(dataDir, collection, targetServer, docID, filename string) (*FileWorkItem, error) {
	data, err := os.ReadFile(PendingFileWritePath(dataDir, collection, targetServer, docID, filename))
	if err != nil {
		return nil, err
	}
	var item FileWorkItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// DeletePendingFileWrite removes a pending file write. Absent is success.
func DeletePendingFileWrite(dataDir, collection, targetServer, docID, filename string) (existed bool, err error) {
	path := PendingFileWritePath(dataDir, collection, targetServer, docID, filename)
	if _, statErr := os.Stat(path); statErr != nil {
		if os.IsNotExist(statErr) {
			return false, nil
		}
		return false, statErr
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

// FileWorkItemID builds "{serverName}-{docId}-{filename}" opaque ids.
func FileWorkItemID(serverName, docID, filename string) string {
	return serverName + "-" + docID + "-" + filename
}

// ParseFileWorkItemID strips serverName prefix and recovers docID/filename.
// When multiple '-' splits are valid SafeID/SafeFileName pairs, the longest
// document ID wins (period and hyphenated IDs are supported).
func ParseFileWorkItemID(itemID, serverName string) (docID, filename string, ok bool) {
	prefix := serverName + "-"
	if !strings.HasPrefix(itemID, prefix) {
		return "", "", false
	}
	rest := itemID[len(prefix):]
	for i := 0; i < len(rest); i++ {
		if rest[i] != '-' {
			continue
		}
		candID := rest[:i]
		candFile := rest[i+1:]
		if fsstore.SafeID(candID) && fsstore.SafeFileName(candFile) {
			if !ok || len(candID) > len(docID) {
				docID, filename, ok = candID, candFile, true
			}
		}
	}
	return docID, filename, ok
}

// FindPendingFileWrite scans collections for one pending file write.
func FindPendingFileWrite(dataDir, targetServer, docID, filename string) (collection string, item *FileWorkItem, err error) {
	dirs, err := collectionDirs(dataDir)
	if err != nil {
		return "", nil, err
	}
	for _, c := range dirs {
		it, err := ReadPendingFileWrite(dataDir, c, targetServer, docID, filename)
		if err == nil {
			return c, it, nil
		}
		if !os.IsNotExist(err) {
			return "", nil, err
		}
	}
	return "", nil, os.ErrNotExist
}

// ListPendingFileWorkItemIDs lists opaque pending file work item IDs for targetServer.
func ListPendingFileWorkItemIDs(dataDir, targetServer string, limit int) (ids []string, total int, err error) {
	dirs, err := collectionDirs(dataDir)
	if err != nil {
		return nil, 0, err
	}
	var all []string
	prefix := targetServer + "."
	for _, c := range dirs {
		dir := PendingFileWritesDir(dataDir, c)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, 0, err
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".json") || !strings.HasPrefix(name, prefix) {
				continue
			}
			docID, filename, ok := parsePendingFileWriteName(name, targetServer)
			if !ok {
				continue
			}
			all = append(all, FileWorkItemID(targetServer, docID, filename))
		}
	}
	sort.Strings(all)
	total = len(all)
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}

// parsePendingFileWriteName parses "{target}.{docId}.{filename}.json".
func parsePendingFileWriteName(name, targetServer string) (docID, filename string, ok bool) {
	prefix := targetServer + "."
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
		return "", "", false
	}
	rest := strings.TrimSuffix(name[len(prefix):], ".json")
	// docID is SafeID (no dots in practice for ULIDs, but period document IDs
	// are allowed). Period IDs are constrained; they still cannot contain the
	// character sequence used as separator ambiguity easily. We take the
	// first segment matching SafeID length rules by splitting on the first
	// '.' only when the left side is a valid SafeID — but period IDs contain
	// dots. Use fsstore.SafeID on progressive prefixes ending at dots.
	// Fallback: read the JSON for authoritative ids when listing already
	// opened the file. For listing we only need the opaque work item id, so
	// recover by reading the file when ambiguous.
	dot := strings.IndexByte(rest, '.')
	if dot <= 0 {
		return "", "", false
	}
	// Prefer longest prefix that is a SafeID and leaves a SafeFileName remainder.
	for i := len(rest) - 1; i > 0; i-- {
		if rest[i] != '.' {
			continue
		}
		candID := rest[:i]
		candFile := rest[i+1:]
		if fsstore.SafeID(candID) && fsstore.SafeFileName(candFile) {
			return candID, candFile, true
		}
	}
	_ = dot
	return "", "", false
}
