package change

import (
	"context"
	"fmt"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/search"
)

// SearchRouter delivers a search-result bucket mutation to whichever
// server is the search SOT for that bucket's shard, per SHARDING.md:
// "Search result updates are routed to the SOT-member for the search
// shard."
type SearchRouter interface {
	Upsert(ctx context.Context, collection, searchName string, segments []string, def *search.Definition, key []any, id string, sortVals []search.SortValue) error
	Remove(ctx context.Context, collection, searchName string, segments []string, id string) error
}

// LocalApplier applies a search-result mutation directly to this server's
// local filesystem, using the safe read-modify-write-verify pattern from
// search.ApplyMutation.
type LocalApplier struct {
	DataDir string
	IDs     IDGenerator
}

func (l *LocalApplier) ids() IDGenerator {
	if l.IDs != nil {
		return l.IDs
	}
	return clockULID{}
}

// Upsert implements SearchRouter for the local case.
func (l *LocalApplier) Upsert(_ context.Context, collection, searchName string, segments []string, def *search.Definition, key []any, id string, sortVals []search.SortValue) error {
	path := fsstore.SearchResultPath(l.DataDir, collection, searchName, segments)
	_, _, err := search.ApplyMutation(path, l.ids().New, func(rf *search.ResultFile, existed bool) (bool, error) {
		rf.Search = searchName
		rf.Collection = collection
		if !existed || len(rf.Key) == 0 {
			rf.Key = key
		}
		return rf.Upsert(def, id, sortVals), nil
	})
	return err
}

// Remove implements SearchRouter for the local case.
func (l *LocalApplier) Remove(_ context.Context, collection, searchName string, segments []string, id string) error {
	path := fsstore.SearchResultPath(l.DataDir, collection, searchName, segments)
	_, _, err := search.ApplyMutation(path, l.ids().New, func(rf *search.ResultFile, existed bool) (bool, error) {
		if !existed {
			return false, nil
		}
		return rf.Remove(id), nil
	})
	return err
}

// ShardRouter decides, per bucket, whether to apply a search mutation
// locally (this server is the search shard's SOT) or to hand it off to
// Remote for cross-server delivery.
type ShardRouter struct {
	ServerName string
	Cfg        ConfigSource
	Local      *LocalApplier
	// Remote handles delivery when a different server is the search
	// shard's SOT. It may be nil; see RemoteApplier's doc comment for the
	// current MVP scope of cross-server search delivery.
	Remote *RemoteApplier
}

func (r *ShardRouter) owner(segments []string) (server string, slot byte) {
	slot = search.ShardSlot(segments)
	cfg := r.Cfg()
	return cfg.SOTForSlot(slot), slot
}

// Upsert implements SearchRouter, routing to Local or Remote by shard, and
// (when this server is the search shard's SOT) replicating the same
// mutation out to that shard's read/proxy members, per SHARDING.md/
// SERVER-TO-SERVER-API.md's "Happy-Path Search Result Delivery".
func (r *ShardRouter) Upsert(ctx context.Context, collection, searchName string, segments []string, def *search.Definition, key []any, id string, sortVals []search.SortValue) error {
	owner, slot := r.owner(segments)
	if owner == "" || owner == r.ServerName {
		if err := r.Local.Upsert(ctx, collection, searchName, segments, def, key, id, sortVals); err != nil {
			return err
		}
		return r.replicate(ctx, slot, collection, searchName, segments, "upsert", id, search.SortValuesToJSON(sortVals))
	}
	if r.Remote == nil {
		return fmt.Errorf("search shard %02X for %s.%s is owned by remote server %q; cross-server search delivery is not configured on this agent", slot, collection, searchName, owner)
	}
	return r.Remote.Upsert(ctx, owner, collection, searchName, segments, id, search.SortValuesToJSON(sortVals))
}

// Remove implements SearchRouter, routing to Local or Remote by shard, and
// (when this server is the search shard's SOT) replicating the removal to
// that shard's read/proxy members.
func (r *ShardRouter) Remove(ctx context.Context, collection, searchName string, segments []string, id string) error {
	owner, slot := r.owner(segments)
	if owner == "" || owner == r.ServerName {
		if err := r.Local.Remove(ctx, collection, searchName, segments, id); err != nil {
			return err
		}
		return r.replicate(ctx, slot, collection, searchName, segments, "remove", id, nil)
	}
	if r.Remote == nil {
		return fmt.Errorf("search shard %02X for %s.%s is owned by remote server %q; cross-server search delivery is not configured on this agent", slot, collection, searchName, owner)
	}
	return r.Remote.Remove(ctx, owner, collection, searchName, segments, id)
}

// replicate pushes a search-result mutation this server just applied
// locally (as the search shard's SOT) out to every read/proxy member of
// that same shard slot, per SEARCHING.md's "Search Sharding": read
// members serve search results the same way they serve documents, so
// they need their own local copy of the bucket file. If Remote is not
// configured (e.g. this server cannot authenticate outbound calls),
// replication is a documented no-op gap rather than a hard failure,
// matching this MVP's narrowed scope for search cross-server delivery.
// A failed delivery to a configured target returns an error so the
// change-agent's queue entry is retried on the next scan, per
// RemoteApplier's doc comment ("the SOT may retry push delivery and rely
// on the change-agent's retryable nature").
func (r *ShardRouter) replicate(ctx context.Context, slot byte, collection, searchName string, segments []string, op, id string, sortJSON []any) error {
	if r.Remote == nil {
		return nil
	}
	cfg := r.Cfg()
	assignment, ok := cfg.SlotAssignment(slot)
	if !ok {
		return nil
	}
	targets := dedupExcludingSelf(r.ServerName, assignment.ShardReadMember, assignment.ProxyReadMember)
	for _, target := range targets {
		var err error
		switch op {
		case "upsert":
			err = r.Remote.Upsert(ctx, target, collection, searchName, segments, id, sortJSON)
		case "remove":
			err = r.Remote.Remove(ctx, target, collection, searchName, segments, id)
		}
		if err != nil {
			return fmt.Errorf("replicate search %s to read member %q: %w", op, target, err)
		}
	}
	return nil
}

func dedupExcludingSelf(self string, groups ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, group := range groups {
		for _, name := range group {
			if name == "" || name == self || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}
