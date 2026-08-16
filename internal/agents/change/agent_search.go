package change

import (
	"context"
	"fmt"
	"sync"

	"github.com/JohnAD/datoriumdb/internal/config"
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

// ShardRouter applies a search mutation locally when this server is among
// the assigned targets and pushes the same mutation to every other
// assigned SOT/READ/proxy member of the search shard.
type ShardRouter struct {
	ServerName string
	Cfg        ConfigSource
	Local      *LocalApplier
	// Remote handles delivery to other servers. Nil is only valid for
	// single-node tests where every target is this server.
	Remote *RemoteApplier
}

func (r *ShardRouter) owner(segments []string) (server string, slot byte) {
	slot = search.ShardSlot(segments)
	cfg := r.Cfg()
	return cfg.SOTForSlot(slot), slot
}

// Upsert implements SearchRouter with full-set fan-out.
func (r *ShardRouter) Upsert(ctx context.Context, collection, searchName string, segments []string, def *search.Definition, key []any, id string, sortVals []search.SortValue) error {
	return r.deliver(ctx, collection, searchName, segments, "upsert", id, search.SortValuesToJSON(sortVals), func() error {
		return r.Local.Upsert(ctx, collection, searchName, segments, def, key, id, sortVals)
	})
}

// Remove implements SearchRouter with full-set fan-out.
func (r *ShardRouter) Remove(ctx context.Context, collection, searchName string, segments []string, id string) error {
	return r.deliver(ctx, collection, searchName, segments, "remove", id, nil, func() error {
		return r.Local.Remove(ctx, collection, searchName, segments, id)
	})
}

func (r *ShardRouter) deliver(ctx context.Context, collection, searchName string, segments []string, op, id string, sortJSON []any, localApply func() error) error {
	owner, slot := r.owner(segments)
	cfg := r.Cfg()
	assignment, ok := cfg.SlotAssignment(slot)
	if !ok {
		if owner == "" || owner == r.ServerName {
			return localApply()
		}
		if r.Remote == nil {
			return fmt.Errorf("search shard %02X for %s.%s is owned by remote server %q; cross-server search delivery is not configured on this agent", slot, collection, searchName, owner)
		}
		return r.remoteOne(ctx, owner, collection, searchName, segments, op, id, sortJSON)
	}

	targets := searchShardTargets(owner, assignment)
	selfIsTarget := false
	for _, t := range targets {
		if t == r.ServerName {
			selfIsTarget = true
			break
		}
	}
	if selfIsTarget {
		if err := localApply(); err != nil {
			return err
		}
	}

	var (
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)
	for _, target := range targets {
		if target == r.ServerName {
			continue
		}
		if r.Remote == nil {
			return fmt.Errorf("search shard %02X for %s.%s requires remote delivery to %q; cross-server search delivery is not configured on this agent", slot, collection, searchName, target)
		}
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			if err := r.remoteOne(ctx, target, collection, searchName, segments, op, id, sortJSON); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("replicate search %s to %q: %w", op, target, err)
				}
				mu.Unlock()
			}
		}(target)
	}
	wg.Wait()
	return firstErr
}

func (r *ShardRouter) remoteOne(ctx context.Context, target, collection, searchName string, segments []string, op, id string, sortJSON []any) error {
	switch op {
	case "upsert":
		return r.Remote.Upsert(ctx, target, collection, searchName, segments, id, sortJSON)
	case "remove":
		return r.Remote.Remove(ctx, target, collection, searchName, segments, id)
	default:
		return fmt.Errorf("unknown search operation %q", op)
	}
}

// searchShardTargets returns every server that must hold the search-result
// bucket: the search SOT plus READ/proxy members, deduplicated.
func searchShardTargets(owner string, assignment config.ShardAssignment) []string {
	return dedupOrdered(owner, assignment.ShardReadMember, assignment.ProxyReadMember)
}

func dedupOrdered(first string, groups ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	add(first)
	for _, group := range groups {
		for _, name := range group {
			add(name)
		}
	}
	return out
}
