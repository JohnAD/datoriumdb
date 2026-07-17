package engine

import (
	"encoding/json"

	"github.com/JohnAD/datoriumdb/internal/agents/cache"
)

// buildCacheSummaries implements ACCESS-LANGUAGE.md's `read
// {cacheSummaries: true}`: for every DatoriumCachedRef field on doc,
// resolve the local cached snapshot (or a lost-reference stub) and project
// the field's declared summary paths, grouping by target collection and
// document ID and merging fields when multiple reference fields point at
// the same document (ACCESS-LANGUAGE.md: "If multiple cached references
// target the same document, their requested fields are combined into one
// returned object.").
func (e *Engine) buildCacheSummaries(doc map[string]any, schemaRaw json.RawMessage, haveSchema bool) map[string]any {
	out := map[string]any{}
	if !haveSchema || doc == nil {
		return out
	}
	refs, err := cache.FindRefFields(schemaRaw, doc)
	if err != nil {
		return out
	}
	for _, ref := range refs {
		cached, ok, lerr := cache.LoadCacheFile(e.DataDir, ref.TargetCollection, ref.TargetID)
		if lerr != nil {
			continue
		}
		if !ok {
			// AGENT-FOR-CHANGE-DISTRIBUTION.md "Lost References": a
			// referenced document with no local cache file yet is
			// reported the same as a resolvable-but-deleted one (a full
			// record with a null revision). Creating the stub here also
			// gives a future pending cache-update work item something to
			// update in place (CACHE-UPDATES.md's "No Sweeping Reads").
			stub, serr := cache.EnsureStub(e.DataDir, ref.TargetCollection, ref.TargetID)
			if serr != nil {
				continue
			}
			cached = stub
		}
		summary := cache.BuildSummary(cached, ref.Summary)
		byCollection, _ := out[ref.TargetCollection].(map[string]any)
		if byCollection == nil {
			byCollection = map[string]any{}
			out[ref.TargetCollection] = byCollection
		}
		if existing, ok := byCollection[ref.TargetID].(map[string]any); ok {
			for k, v := range summary {
				existing[k] = v
			}
		} else {
			byCollection[ref.TargetID] = summary
		}
	}
	return out
}
