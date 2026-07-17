package upgrade

import (
	"context"
	"os"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/idgen"
)

// ConfigSource returns the current establishment config.
type ConfigSource func() *config.Config

type clockULID struct{}

func (clockULID) New() (string, error) { return idgen.New() }

// Excluder prevents the upgrade-agent and change-agent (or two upgrade
// workers) from migrating/patching the same document concurrently.
// LOCAL-ARCHITECTURE.md also requires "the upgrade-agent should run at
// most once per collection at a time"; callers achieve that by giving the
// upgrade-agent a single worker (scheduler.Agent.Workers defaults to 1).
type Excluder interface {
	TryAcquire(key string) bool
	Release(key string)
}

// Agent implements the upgrade-agent's RunOnce unit of work: scan each
// collection for documents whose "$" marker is behind the collection's
// current schema version, and migrate one such document per call.
type Agent struct {
	DataDir   string
	Cfg       ConfigSource
	IDs       IDGenerator
	Exclusion Excluder
	Logf      func(format string, args ...any)
}

func (a *Agent) ids() IDGenerator {
	if a.IDs != nil {
		return a.IDs
	}
	return clockULID{}
}

func (a *Agent) logf(format string, args ...any) {
	if a.Logf != nil {
		a.Logf(format, args...)
	}
}

// RunOnce implements scheduler.Task: migrate at most one stale document
// across every collection, reporting didWork=true if it found one.
func (a *Agent) RunOnce(ctx context.Context) (bool, error) {
	cfg := a.Cfg()
	if cfg == nil {
		return false, nil
	}
	collections, err := fsstore.ListCollections(a.DataDir)
	if err != nil {
		return false, err
	}
	for _, collection := range collections {
		target := cfg.SchemaVersion(collection)
		if target == 0 {
			continue // never upgraded; nothing can be stale
		}
		ids, err := staleDocumentIDs(a.DataDir, collection, cfg)
		if err != nil {
			a.logf("upgrade-agent: scan %s: %v", collection, err)
			continue
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			key := collection + "/" + id
			if a.Exclusion != nil && !a.Exclusion.TryAcquire(key) {
				continue
			}
			changed, err := ApplyToStoredDocument(a.DataDir, cfg, collection, id, a.ids())
			if a.Exclusion != nil {
				a.Exclusion.Release(key)
			}
			if err != nil {
				a.logf("upgrade-agent: migrate %s/%s: %v", collection, id, err)
				return true, err
			}
			if changed {
				return true, nil
			}
			// changed=false with no error means a benign race (document
			// deleted, or already migrated by another path); keep
			// scanning for the next candidate instead of stopping.
		}
	}
	return false, nil
}

// staleDocumentIDs lists every live document ID in collection whose "$"
// marker is behind cfg's current schema version for that collection.
// UPDATE-SCHEMA.md's background migration does not require any particular
// order; this narrow MVP does a full directory scan per call rather than
// keeping a persistent cursor, which is acceptable given collections are
// expected to be small in the MVP's plain-text-json filesystem model.
func staleDocumentIDs(dataDir, collection string, cfg *config.Config) ([]string, error) {
	dir := fsstore.CollectionDir(dataDir, collection)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		doc, err := fsstore.ReadDocumentJSON(fsstore.DocumentPath(dataDir, collection, id))
		if err != nil {
			continue
		}
		if NeedsMigration(cfg, collection, doc) {
			out = append(out, id)
		}
	}
	return out, nil
}
