package replication

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// DocumentWorkItem is the pending/replicated document write shape from
// REPLICATION-FAILURE-HANDLING.md ("Pending Writes Layout") and
// SERVER-TO-SERVER-API.md ("Happy-Path Document Write Delivery"). For
// create, Payload carries the full document. For patch, Patch carries an
// RFC 6902 operation list that includes the database-owned "/#" replace so
// every read member converges on the same version. For delete, neither
// Payload nor Patch is required; BeforeVersion identifies the version being
// removed.
type DocumentWorkItem struct {
	Collection    string           `json:"collection"`
	ID            string           `json:"id"`
	BeforeVersion string           `json:"beforeVersion,omitempty"`
	AfterVersion  string           `json:"afterVersion,omitempty"`
	OperationID   string           `json:"operationId"`
	Command       string           `json:"command"`
	Patch         []map[string]any `json:"patch,omitempty"`
	Payload       map[string]any   `json:"payload,omitempty"`
}

// PendingWritesDir returns {dataDir}/{collection}/.pendingWrites.
func PendingWritesDir(dataDir, collection string) string {
	return filepath.Join(fsstore.CollectionDir(dataDir, collection), ".pendingWrites")
}

// PendingWritePath returns the pending write file path for one target
// server and one document, per REPLICATION-FAILURE-HANDLING.md's
// "{readServerName}.{docId}.json" naming.
func PendingWritePath(dataDir, collection, targetServer, docID string) string {
	return filepath.Join(PendingWritesDir(dataDir, collection), targetServer+"."+docID+".json")
}

// WritePendingWrite durably records item as pending delivery to targetServer.
func WritePendingWrite(dataDir, collection, targetServer string, item DocumentWorkItem) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fsstore.WriteFileAtomic(PendingWritePath(dataDir, collection, targetServer, item.ID), data, 0o644)
}

// ReadPendingWrite reads a stored pending write item.
func ReadPendingWrite(dataDir, collection, targetServer, docID string) (*DocumentWorkItem, error) {
	data, err := os.ReadFile(PendingWritePath(dataDir, collection, targetServer, docID))
	if err != nil {
		return nil, err
	}
	var item DocumentWorkItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// DeletePendingWrite removes a stored pending write item. Removing an
// already-absent item is not an error, matching SERVER-TO-SERVER-API.md's
// "Complete Pending Document Write Work" existing:false semantics.
func DeletePendingWrite(dataDir, collection, targetServer, docID string) (existed bool, err error) {
	path := PendingWritePath(dataDir, collection, targetServer, docID)
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

// WorkItemID builds the opaque "{serverName}-{docId}" work item identifier
// documented in SERVER-TO-SERVER-API.md. Callers should treat the result as
// opaque; DocIDFromWorkItemID is the only supported way to recover the
// document ID, and only for the same serverName that produced the ID.
func WorkItemID(serverName, docID string) string {
	return serverName + "-" + docID
}

// DocIDFromWorkItemID strips the caller's own authenticated serverName
// prefix from an opaque work item ID and returns the document ID. Because
// server-to-server endpoints already require the authenticated machine
// identity to equal serverName (AUTHENTICATION.md / SERVER-TO-SERVER-API.md),
// this avoids ambiguity from hyphens that may appear in server or document
// names: the caller can only ever ask about its own work items.
func DocIDFromWorkItemID(itemID, serverName string) (docID string, ok bool) {
	prefix := serverName + "-"
	if !strings.HasPrefix(itemID, prefix) {
		return "", false
	}
	return itemID[len(prefix):], true
}

// collectionDirs lists the non-dot subdirectories of dataDir, which are the
// database's collection directories (fsstore.CollectionDir), skipping
// database-owned dot-directories such as .operations.
func collectionDirs(dataDir string) ([]string, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, e.Name())
	}
	return out, nil
}

// FindPendingWrite scans every collection directory under dataDir for a
// pending write targeted at targetServer for docID. Work item IDs encode
// only the target server and document ID (matching the documented
// "{serverName}-{docId}" shape), not the collection, so locating the
// concrete file requires this scan. Document IDs are ULIDs and effectively
// unique across collections in practice.
func FindPendingWrite(dataDir, targetServer, docID string) (collection string, item *DocumentWorkItem, err error) {
	dirs, err := collectionDirs(dataDir)
	if err != nil {
		return "", nil, err
	}
	for _, c := range dirs {
		it, err := ReadPendingWrite(dataDir, c, targetServer, docID)
		if err == nil {
			return c, it, nil
		}
		if !os.IsNotExist(err) {
			return "", nil, err
		}
	}
	return "", nil, os.ErrNotExist
}

// ListPendingWorkItemIDs returns up to limit opaque work item IDs targeted
// at targetServer across every collection, plus the total number found
// regardless of limit (SERVER-TO-SERVER-API.md's totalItems semantics).
// Results are sorted for deterministic pagination-free listing.
func ListPendingWorkItemIDs(dataDir, targetServer string, limit int) (ids []string, total int, err error) {
	dirs, err := collectionDirs(dataDir)
	if err != nil {
		return nil, 0, err
	}
	var all []string
	prefix := targetServer + "."
	for _, c := range dirs {
		dir := PendingWritesDir(dataDir, c)
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
			docID := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json")
			all = append(all, WorkItemID(targetServer, docID))
		}
	}
	sort.Strings(all)
	total = len(all)
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}
