package cache

import (
	"fmt"

	"github.com/JohnAD/datoriumdb/internal/docjson"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// Apply applies a pending cache-update work item to this server's local
// cache file, per CACHE-UPDATES.md's "No Sweeping Reads": "If a cached
// summary file exists for that source ID, the read member updates it in
// place. If no cached summary file exists, there is nothing to update for
// that location." In the second case, applied is still true (the work
// item's intent — this location's cache being current — is already
// satisfied), so the caller can complete/delete the work item either way.
func Apply(dataDir string, item WorkItem) (applied bool, err error) {
	existing, ok, err := LoadCacheFile(dataDir, item.SourceCollection, item.SourceDocumentID)
	if err != nil {
		return false, err
	}
	if !ok {
		return true, nil
	}
	if ver, _ := existing["#"].(string); item.AfterVersion != "" && ver == item.AfterVersion {
		return true, nil // already applied
	}
	path := fsstore.CachePath(dataDir, item.SourceCollection, item.SourceDocumentID)
	switch item.Command {
	case "delete":
		// CACHE-UPDATES.md: "the cache file is not deleted... It remains
		// a full cached summary record... with its deleted or missing
		// state represented inside the summary... requested SOT summary
		// fields are omitted."
		doc := map[string]any{"!": item.SourceDocumentID, "#": nil}
		raw, err := docjson.EncodeMap(doc)
		if err != nil {
			return false, err
		}
		if err := fsstore.WriteDocumentJSON(path, raw); err != nil {
			return false, err
		}
		return true, nil
	case "create", "patch":
		if item.Payload == nil {
			return false, fmt.Errorf("cache-agent: %s work item for %s/%s is missing payload", item.Command, item.SourceCollection, item.SourceDocumentID)
		}
		raw, err := docjson.EncodeMap(item.Payload)
		if err != nil {
			return false, err
		}
		if err := fsstore.WriteDocumentJSON(path, raw); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, fmt.Errorf("cache-agent: unknown command %q", item.Command)
	}
}
