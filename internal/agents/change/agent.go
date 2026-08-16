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
	// CachePush delivers pending cache-update work items to remote read
	// members in one shot. Nil means only durable pending files are
	// written for remote targets (pull agent catch-up).
	CachePush CachePusher
	Exclusion Excluder
	Logf      func(format string, args ...any)
}

// CachePusher pushes one cache-update work item to a remote read/proxy
// member. Local self-targets are applied by the change-agent directly.
type CachePusher interface {
	Push(ctx context.Context, targetServer string, item cache.WorkItem) error
}

// ProcessResult reports whether synchronous search/cache distribution
// finished for one change-queue entry. Complete=false is informational;
// Err is set only when processing failed hard enough that the queue entry
// must remain for background retry.
type ProcessResult struct {
	Complete bool
	Err      error
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

// ProcessNow claims and processes the specific change-queue entry for
// (change, collection, id). It is safe to call from the write path while
// the background RunOnce loop is also running: the exclusion set and
// .queue→.taken rename serialize ownership. Complete is true only when
// search mutations and cache applies reached every required target.
func (a *Agent) ProcessNow(ctx context.Context, change, collection, id string) ProcessResult {
	cfg := a.Cfg()
	if cfg == nil {
		return ProcessResult{Complete: false}
	}
	key := collection + "/" + id
	if a.Exclusion != nil && !a.Exclusion.TryAcquire(key) {
		return ProcessResult{Complete: false}
	}
	if a.Exclusion != nil {
		defer a.Exclusion.Release(key)
	}
	filename := change + "__" + collection + "__" + id + ".queue"
	takenPath := fsstore.TakenQueuePath(a.DataDir, collection, change, id)
	queuePath := fsstore.QueueEntryPath(a.DataDir, collection, filename)
	ext := "queue"
	if _, err := os.Stat(takenPath); err == nil {
		ext = "taken"
	} else if _, err := os.Stat(queuePath); err != nil {
		if os.IsNotExist(err) {
			// Already drained by a concurrent worker — treat as incomplete
			// for this response because we cannot prove derived data landed.
			return ProcessResult{Complete: false}
		}
		return ProcessResult{Complete: false, Err: err}
	}
	complete, _, err := a.runEntryResult(ctx, cfg, change, collection, id, filename, ext)
	return ProcessResult{Complete: complete, Err: err}
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
// distribution work is durably handed off, per AGENT-FOR-CHANGE-DISTRIBUTION.md.
func (a *Agent) runEntry(ctx context.Context, cfg *config.Config, change, collection, id, filename, ext string) (bool, error) {
	_, claimed, err := a.runEntryResult(ctx, cfg, change, collection, id, filename, ext)
	return claimed, err
}

// runEntryResult is the shared claim/process/cleanup path used by RunOnce
// and ProcessNow. complete is true only when every search/cache target
// applied; a durable cache pending fallback still allows cleanup and
// returns complete=false with err=nil. claimed is false when another
// worker already took the queue entry.
func (a *Agent) runEntryResult(ctx context.Context, cfg *config.Config, change, collection, id, filename, ext string) (complete, claimed bool, err error) {
	takenPath := fsstore.TakenQueuePath(a.DataDir, collection, change, id)
	if ext == "queue" {
		queuePath := fsstore.QueueEntryPath(a.DataDir, collection, filename)
		if err := os.Rename(queuePath, takenPath); err != nil {
			if os.IsNotExist(err) {
				return false, false, nil
			}
			return false, false, fmt.Errorf("claim %s: %w", filename, err)
		}
	}
	complete, err = a.process(ctx, cfg, change, collection, id)
	if err != nil {
		return false, true, err
	}
	prevPath := fsstore.PreviousDocumentPath(a.DataDir, collection, id)
	if remErr := os.Remove(prevPath); remErr != nil && !os.IsNotExist(remErr) {
		return false, true, fmt.Errorf("remove previous dotfile: %w", remErr)
	}
	if remErr := os.Remove(takenPath); remErr != nil && !os.IsNotExist(remErr) {
		return false, true, fmt.Errorf("remove taken marker: %w", remErr)
	}
	return complete, true, nil
}

func (a *Agent) process(ctx context.Context, cfg *config.Config, change, collection, id string) (complete bool, err error) {
	currentPath := fsstore.DocumentPath(a.DataDir, collection, id)
	prevPath := fsstore.PreviousDocumentPath(a.DataDir, collection, id)
	currentDoc, err := readOptionalDoc(currentPath)
	if err != nil {
		return false, fmt.Errorf("read current document: %w", err)
	}
	prevDoc, err := readOptionalDoc(prevPath)
	if err != nil {
		return false, fmt.Errorf("read previous document: %w", err)
	}
	cacheComplete, err := a.distributeCache(ctx, cfg, collection, id, change, currentDoc, prevDoc)
	if err != nil {
		return false, fmt.Errorf("cache distribution: %w", err)
	}
	if err := a.distributeSearch(ctx, cfg, collection, id, prevDoc, currentDoc); err != nil {
		return false, fmt.Errorf("search distribution: %w", err)
	}
	return cacheComplete, nil
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
		oldBuckets, err := search.EvaluateDocument(def, prevDoc)
		if err != nil {
			return fmt.Errorf("evaluate previous document against %s: %w", name, err)
		}
		newBuckets, err := search.EvaluateDocument(def, currentDoc)
		if err != nil {
			return fmt.Errorf("evaluate current document against %s: %w", name, err)
		}
		oldByKey := bucketIndex(oldBuckets)
		newByKey := bucketIndex(newBuckets)
		for key, oldRes := range oldByKey {
			if _, keep := newByKey[key]; keep {
				continue
			}
			if err := a.Router.Remove(ctx, collection, name, oldRes.Segments, id); err != nil {
				return fmt.Errorf("remove from old %s bucket: %w", name, err)
			}
		}
		if len(newBuckets) > 0 {
			sortVals := search.ComputeSortValues(def, currentDoc)
			for key, newRes := range newByKey {
				_ = key
				if err := a.Router.Upsert(ctx, collection, name, newRes.Segments, def, newRes.Key, id, sortVals); err != nil {
					return fmt.Errorf("upsert into %s bucket: %w", name, err)
				}
			}
		}
	}
	return nil
}

func bucketIndex(buckets []search.EvalResult) map[string]search.EvalResult {
	out := make(map[string]search.EvalResult, len(buckets))
	for _, b := range buckets {
		if !b.Matched {
			continue
		}
		out[search.ShardInput(b.Segments)] = b
	}
	return out
}

// distributeCache implements AGENT-FOR-CHANGE-DISTRIBUTION.md's "Cache
// Distribution": if any collection schema declares a DatoriumCachedRef
// field that may point at collection, durably stage a pending cache-update
// work item for every candidate read server, then attempt one-shot apply
// (local for self, push for remotes). Targets that do not acknowledge keep
// their pending file for pull-agent catch-up; that is not a hard error.
func (a *Agent) distributeCache(ctx context.Context, cfg *config.Config, collection, id, change string, currentDoc, prevDoc map[string]any) (complete bool, err error) {
	if !anySchemaReferencesCollection(cfg, collection) {
		return true, nil
	}
	targets := cfg.AllReadMembers()
	if len(targets) == 0 {
		return true, nil
	}
	item := cache.WorkItem{
		SourceCollection: collection,
		SourceDocumentID: id,
		Command:          change,
	}
	opID, err := a.ids().New()
	if err != nil {
		return false, err
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
			return true, nil
		}
		item.AfterVersion, _ = currentDoc["#"].(string)
		if prevDoc != nil {
			item.BeforeVersion, _ = prevDoc["#"].(string)
		}
		item.Payload = currentDoc
	}
	complete = true
	for _, server := range targets {
		if err := cache.WriteWorkItem(a.DataDir, server, item); err != nil {
			return false, fmt.Errorf("write pending cache update for %s: %w", server, err)
		}
		if server == a.ServerName {
			if _, err := cache.Apply(a.DataDir, item); err != nil {
				complete = false
				a.logf("change-agent: local cache apply for %s/%s: %v", collection, id, err)
				continue
			}
			if _, err := cache.DeleteWorkItem(a.DataDir, collection, server, id); err != nil {
				complete = false
				a.logf("change-agent: delete local pending cache update for %s/%s: %v", collection, id, err)
			}
			continue
		}
		if a.CachePush == nil {
			complete = false
			continue
		}
		if err := a.CachePush.Push(ctx, server, item); err != nil {
			complete = false
			a.logf("change-agent: cache push to %s for %s/%s: %v", server, collection, id, err)
			continue
		}
		if _, err := cache.DeleteWorkItem(a.DataDir, collection, server, id); err != nil {
			complete = false
			a.logf("change-agent: delete pending cache update after push to %s: %v", server, err)
		}
	}
	return complete, nil
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
