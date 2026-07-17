// Package change implements the change-agent described in
// tech-docs/AGENT-FOR-CHANGE-DISTRIBUTION.md: it drains
// .changeQueue/*.queue entries (claiming each as .taken), computes the net
// difference between the oldest undistributed previous-document dotfile
// and the current document, distributes that change to precompiled
// searches and pending cache-update work, and only then cleans up the
// previous-document dotfile and the .taken marker.
package change

import (
	"context"
	"fmt"
	"os"

	"github.com/JohnAD/datoriumdb/internal/agents/cache"
	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/idgen"
	"github.com/JohnAD/datoriumdb/internal/search"
)

// ConfigSource returns the current establishment config. It is a function
// (rather than a stored pointer) because the engine can reload config
// concurrently with agent runs (LOCAL-ARCHITECTURE.md: "concurrent agents
// may be changing routing/replication/auth").
type ConfigSource func() *config.Config

// IDGenerator creates version/operation IDs. Matches engine.IDGenerator so
// callers can share one implementation (or a deterministic test double).
type IDGenerator interface {
	New() (string, error)
}

// clockULID is the default ULID-backed generator.
type clockULID struct{}

func (clockULID) New() (string, error) { return idgen.New() }

// Excluder prevents two workers from processing the same (collection, id)
// concurrently. scheduler.ExclusionSet satisfies this; nil disables the
// check (safe for the MVP default of one change-agent worker).
type Excluder interface {
	TryAcquire(key string) bool
	Release(key string)
}

// Agent implements one change-agent worker's RunOnce unit of work, for use
// as a scheduler.Task.
type Agent struct {
	DataDir    string
	ServerName string
	Cfg        ConfigSource
	IDs        IDGenerator
	Router     SearchRouter
	Exclusion  Excluder
	Logf       func(format string, args ...any)
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

// RunOnce claims and fully processes at most one change-queue entry across
// every collection, reporting didWork=true if it found one (regardless of
// whether processing succeeded, so the scheduler's error log fires and the
// caller can decide whether to loop again immediately).
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
		entries, err := fsstore.ListQueueEntries(a.DataDir, collection)
		if err != nil {
			a.logf("change-agent: list %s queue: %v", collection, err)
			continue
		}
		for _, name := range entries {
			change, col, id, ext, ok := fsstore.ParseQueueFilename(name)
			if !ok {
				continue
			}
			key := col + "/" + id
			if a.Exclusion != nil && !a.Exclusion.TryAcquire(key) {
				continue
			}
			did, perr := a.runEntry(ctx, cfg, change, col, id, name, ext)
			if a.Exclusion != nil {
				a.Exclusion.Release(key)
			}
			if did {
				if perr != nil {
					a.logf("change-agent: %s %s/%s: %v", change, col, id, perr)
				}
				return true, perr
			}
		}
	}
	return false, nil
}

// runEntry claims (if needed) and processes one queue entry, then cleans
// up the previous-document dotfile and the .taken marker only after
// distribution succeeds, per AGENT-FOR-CHANGE-DISTRIBUTION.md.
func (a *Agent) runEntry(ctx context.Context, cfg *config.Config, change, collection, id, filename, ext string) (bool, error) {
	takenPath := fsstore.TakenQueuePath(a.DataDir, collection, change, id)
	if ext == "queue" {
		queuePath := fsstore.QueueEntryPath(a.DataDir, collection, filename)
		if err := os.Rename(queuePath, takenPath); err != nil {
			if os.IsNotExist(err) {
				// Another worker/process claimed it first.
				return false, nil
			}
			return false, fmt.Errorf("claim %s: %w", filename, err)
		}
	}
	// ext == "taken" means this entry was already claimed by a previous
	// run (possibly interrupted before cleanup); resume it. Processing is
	// idempotent, so re-running it is safe.
	if err := a.process(ctx, cfg, change, collection, id); err != nil {
		return true, err
	}
	prevPath := fsstore.PreviousDocumentPath(a.DataDir, collection, id)
	if err := os.Remove(prevPath); err != nil && !os.IsNotExist(err) {
		return true, fmt.Errorf("remove previous dotfile: %w", err)
	}
	if err := os.Remove(takenPath); err != nil && !os.IsNotExist(err) {
		return true, fmt.Errorf("remove taken marker: %w", err)
	}
	return true, nil
}

func (a *Agent) process(ctx context.Context, cfg *config.Config, change, collection, id string) error {
	currentPath := fsstore.DocumentPath(a.DataDir, collection, id)
	prevPath := fsstore.PreviousDocumentPath(a.DataDir, collection, id)
	currentDoc, err := readOptionalDoc(currentPath)
	if err != nil {
		return fmt.Errorf("read current document: %w", err)
	}
	prevDoc, err := readOptionalDoc(prevPath)
	if err != nil {
		return fmt.Errorf("read previous document: %w", err)
	}
	if err := a.distributeCache(cfg, collection, id, change, currentDoc, prevDoc); err != nil {
		return fmt.Errorf("cache distribution: %w", err)
	}
	if err := a.distributeSearch(ctx, cfg, collection, id, prevDoc, currentDoc); err != nil {
		return fmt.Errorf("search distribution: %w", err)
	}
	return nil
}

func readOptionalDoc(path string) (map[string]any, error) {
	doc, err := fsstore.ReadDocumentJSON(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return doc, nil
}

// distributeSearch implements AGENT-FOR-CHANGE-DISTRIBUTION.md's "Search
// Distribution": evaluate every search definition owned by collection
// against both the previous and current document states, and route any
// bucket change to the search shard's SOT.
func (a *Agent) distributeSearch(ctx context.Context, cfg *config.Config, collection, id string, prevDoc, currentDoc map[string]any) error {
	defs := cfg.Searches[collection]
	for name, raw := range defs {
		def, err := search.ParseDefinition(raw)
		if err != nil {
			return fmt.Errorf("parse search definition %s: %w", name, err)
		}
		oldRes, err := search.EvaluateDocument(def, prevDoc)
		if err != nil {
			return fmt.Errorf("evaluate previous document against %s: %w", name, err)
		}
		newRes, err := search.EvaluateDocument(def, currentDoc)
		if err != nil {
			return fmt.Errorf("evaluate current document against %s: %w", name, err)
		}
		sameBucket := oldRes.Matched && newRes.Matched && segmentsEqual(oldRes.Segments, newRes.Segments)
		if oldRes.Matched && !sameBucket {
			if err := a.Router.Remove(ctx, collection, name, oldRes.Segments, id); err != nil {
				return fmt.Errorf("remove from old %s bucket: %w", name, err)
			}
		}
		if newRes.Matched {
			sortVals := search.ComputeSortValues(def, currentDoc)
			if err := a.Router.Upsert(ctx, collection, name, newRes.Segments, def, newRes.Key, id, sortVals); err != nil {
				return fmt.Errorf("upsert into %s bucket: %w", name, err)
			}
		}
	}
	return nil
}

func segmentsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// distributeCache implements AGENT-FOR-CHANGE-DISTRIBUTION.md's "Cache
// Distribution": if any collection schema declares a DatoriumCachedRef
// field that may point at collection, queue a pending cache-update work
// item for every candidate read server.
func (a *Agent) distributeCache(cfg *config.Config, collection, id, change string, currentDoc, prevDoc map[string]any) error {
	if !anySchemaReferencesCollection(cfg, collection) {
		return nil
	}
	targets := cfg.AllReadMembers()
	if len(targets) == 0 {
		return nil
	}
	item := cache.WorkItem{
		SourceCollection: collection,
		SourceDocumentID: id,
		Command:          change,
	}
	opID, err := a.ids().New()
	if err != nil {
		return err
	}
	item.OperationID = opID
	switch change {
	case "delete":
		if prevDoc != nil {
			item.BeforeVersion, _ = prevDoc["#"].(string)
		}
		// CACHE-UPDATES.md: "the work item should instead contain enough
		// delete metadata for the read member to write a full cached
		// summary record for the deleted reference state."
		item.Payload = map[string]any{"!": id, "#": nil}
	default: // create, patch
		if currentDoc == nil {
			// Nothing to distribute; the document may have been deleted
			// again before this queue entry was reached.
			return nil
		}
		item.AfterVersion, _ = currentDoc["#"].(string)
		if prevDoc != nil {
			item.BeforeVersion, _ = prevDoc["#"].(string)
		}
		item.Payload = currentDoc
	}
	for _, server := range targets {
		if err := cache.WriteWorkItem(a.DataDir, server, item); err != nil {
			return fmt.Errorf("write pending cache update for %s: %w", server, err)
		}
	}
	return nil
}

// anySchemaReferencesCollection reports whether any collection schema has a
// DatoriumCachedRef field whose custom.collections includes collection.
func anySchemaReferencesCollection(cfg *config.Config, collection string) bool {
	for _, raw := range cfg.Schemas {
		if schemaReferencesCollection(raw, collection) {
			return true
		}
	}
	return false
}
