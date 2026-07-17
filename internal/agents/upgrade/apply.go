package upgrade

import (
	"os"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// IDGenerator creates version IDs for a migrated document.
type IDGenerator interface {
	New() (string, error)
}

// ApplyToStoredDocument loads collection/id, migrates it if its schema
// marker is behind the collection's current version, durably writes the
// migrated content back with a fresh version (UPDATE-SCHEMA.md's
// "Background Migration": "Each document change made by the background
// agent is a normal patch"), and enqueues change-agent work so derived
// data (search, cache) picks up the new content. It reports changed=false
// with no error when the document was already current or has since been
// deleted (a benign race with a concurrent delete).
func ApplyToStoredDocument(dataDir string, cfg *config.Config, collection, id string, ids IDGenerator) (changed bool, err error) {
	path := fsstore.DocumentPath(dataDir, collection, id)
	doc, err := fsstore.ReadDocumentJSON(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !NeedsMigration(cfg, collection, doc) {
		return false, nil
	}
	if _, err := MigrateDocument(cfg, collection, doc, cfg.SchemaVersion(collection)); err != nil {
		return false, err
	}
	version, err := ids.New()
	if err != nil {
		return false, err
	}
	doc["#"] = version
	if err := fsstore.PreservePreviousIfAbsent(dataDir, collection, id); err != nil {
		return false, err
	}
	if err := fsstore.WriteDocumentJSONVerified(path, doc); err != nil {
		return false, err
	}
	if err := fsstore.EnqueueChange(dataDir, collection, id, "patch"); err != nil {
		return false, err
	}
	return true, nil
}
