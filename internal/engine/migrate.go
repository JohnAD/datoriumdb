package engine

import (
	"github.com/JohnAD/datoriumdb/internal/agents/upgrade"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// migrateOnAccess implements UPDATE-SCHEMA.md's on-the-fly migration path
// for `read`: "individual documents are updated on-the-fly when the
// system responds to access requests for those documents." If doc is
// already current, it is returned unchanged. Otherwise it is migrated,
// durably persisted with a fresh version, and re-read so the caller always
// sees the exact content that was written.
//
// This narrow MVP only wires on-access migration into read, not patch: a
// patch's client-supplied expected version is checked against the
// document as it existed before the patch's own edits, and migrating the
// document first (which bumps "#") would need extra care to keep that
// optimistic-concurrency check meaningful. Patch-triggered migration is
// left to the background upgrade-agent, which will pick the document up
// on its own scan.
func (e *Engine) migrateOnAccess(collection, id string, doc map[string]any) (map[string]any, error) {
	if !upgrade.NeedsMigration(e.Cfg, collection, doc) {
		return doc, nil
	}
	if _, err := upgrade.ApplyToStoredDocument(e.DataDir, e.Cfg, collection, id, e.ids()); err != nil {
		return doc, err
	}
	fresh, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(e.DataDir, collection, id))
	if err != nil {
		return doc, err
	}
	return fresh, nil
}
