// Package cache implements the read-member side of CACHE-UPDATES.md's
// cached-summary distribution: fetching pending cache-update work items
// from SOT-members, applying them to this server's local `.cache` files,
// and building the cached-summary projection used by `read
// {cacheSummaries:true}`.
package cache

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// WorkItem is the JSON shape of one pending cache-update work item, per
// CACHE-UPDATES.md's "Pending Cache Update Work Item" and
// SERVER-TO-SERVER-API.md's "Pending Cache Updates".
type WorkItem struct {
	SourceCollection string         `json:"sourceCollection"`
	SourceDocumentID string         `json:"sourceDocumentId"`
	BeforeVersion    string         `json:"beforeVersion,omitempty"`
	AfterVersion     string         `json:"afterVersion,omitempty"`
	OperationID      string         `json:"operationId"`
	Command          string         `json:"command"`
	Payload          map[string]any `json:"payload"`
}

// WorkItemID builds the opaque "{readServerName}-{sourceDocId}" work item
// identifier used over the wire, mirroring
// replication.WorkItemID/DocIDFromWorkItemID's approach: the authenticated
// caller identity supplies readServerName, so the source collection is
// recovered by scanning (FindPendingCacheUpdate), not encoded in the ID.
func WorkItemID(readServerName, sourceDocumentID string) string {
	return readServerName + "-" + sourceDocumentID
}

// DocIDFromWorkItemID strips the caller's own authenticated readServerName
// prefix from an opaque work item ID and returns the source document ID.
func DocIDFromWorkItemID(itemID, readServerName string) (docID string, ok bool) {
	prefix := readServerName + "-"
	if !strings.HasPrefix(itemID, prefix) {
		return "", false
	}
	return itemID[len(prefix):], true
}

// WriteWorkItem durably records item at readServerName's pending
// cache-update slot for item.SourceCollection/item.SourceDocumentID.
func WriteWorkItem(dataDir, readServerName string, item WorkItem) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := fsstore.PendingCacheUpdatePath(dataDir, item.SourceCollection, readServerName, item.SourceDocumentID)
	return fsstore.WriteFileAtomic(path, data, 0o644)
}

// ReadWorkItem reads one stored pending cache-update work item.
func ReadWorkItem(dataDir, sourceCollection, readServerName, sourceDocID string) (*WorkItem, error) {
	path := fsstore.PendingCacheUpdatePath(dataDir, sourceCollection, readServerName, sourceDocID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var item WorkItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// DeleteWorkItem removes a stored pending cache-update work item. Deleting
// an already-absent item is not an error.
func DeleteWorkItem(dataDir, sourceCollection, readServerName, sourceDocID string) (existed bool, err error) {
	path := fsstore.PendingCacheUpdatePath(dataDir, sourceCollection, readServerName, sourceDocID)
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

// FindWorkItem scans every collection under dataDir for a pending
// cache-update work item targeted at readServerName for sourceDocID.
func FindWorkItem(dataDir, readServerName, sourceDocID string) (sourceCollection string, item *WorkItem, err error) {
	collections, err := fsstore.ListCollections(dataDir)
	if err != nil {
		return "", nil, err
	}
	for _, c := range collections {
		it, err := ReadWorkItem(dataDir, c, readServerName, sourceDocID)
		if err == nil {
			return c, it, nil
		}
		if !os.IsNotExist(err) {
			return "", nil, err
		}
	}
	return "", nil, os.ErrNotExist
}

// ListWorkItemIDs returns up to limit opaque work item IDs targeted at
// readServerName across every collection's .pendingCacheUpdates directory,
// plus the total number found regardless of limit.
func ListWorkItemIDs(dataDir, readServerName string, limit int) (ids []string, total int, err error) {
	collections, err := fsstore.ListCollections(dataDir)
	if err != nil {
		return nil, 0, err
	}
	var all []string
	prefix := readServerName + "."
	for _, c := range collections {
		dir := fsstore.PendingCacheUpdatesDir(dataDir, c)
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
			all = append(all, WorkItemID(readServerName, docID))
		}
	}
	sort.Strings(all)
	total = len(all)
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}
