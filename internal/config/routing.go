package config

import (
	"sort"

	"github.com/JohnAD/datoriumdb/internal/shard"
)

// SlotAssignment returns the shard map assignment covering slot, if any.
func (c *Config) SlotAssignment(slot byte) (ShardAssignment, bool) {
	if c == nil {
		return ShardAssignment{}, false
	}
	for raw, assignment := range c.ShardMap.ShardMap.Default {
		r, err := shard.ParseRange(raw)
		if err != nil {
			continue
		}
		if r.Contains(slot) {
			return assignment, true
		}
	}
	return ShardAssignment{}, false
}

// SOTForSlot returns the SHARD_SOT_MEMBER server name assigned to slot, or
// "" if the shard map does not cover that slot.
func (c *Config) SOTForSlot(slot byte) string {
	a, ok := c.SlotAssignment(slot)
	if !ok {
		return ""
	}
	return a.ShardSOTMember
}

// ServesSlot reports whether serverName is the SOT, a shard read member, or
// a proxy read member for slot.
func (c *Config) ServesSlot(serverName string, slot byte) bool {
	a, ok := c.SlotAssignment(slot)
	if !ok {
		return false
	}
	if a.ShardSOTMember == serverName {
		return true
	}
	for _, n := range a.ShardReadMember {
		if n == serverName {
			return true
		}
	}
	for _, n := range a.ProxyReadMember {
		if n == serverName {
			return true
		}
	}
	return false
}

// AllReadMembers returns the deduplicated, sorted union of every
// SHARD_READ_MEMBER and PROXY_READ_MEMBER server name across the whole
// shard map. Because the MVP only has a single global shardMap.default
// (ESTABLISHMENT-CONFIG.md: "Collection-specific shard maps may be added
// later"), this is the candidate set of read servers that could hold a
// cached summary for any document in any collection.
func (c *Config) AllReadMembers() []string {
	if c == nil {
		return nil
	}
	set := map[string]bool{}
	for _, assignment := range c.ShardMap.ShardMap.Default {
		for _, n := range assignment.ShardReadMember {
			set[n] = true
		}
		for _, n := range assignment.ProxyReadMember {
			set[n] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// AllSOTMembers returns the deduplicated, sorted union of every
// SHARD_SOT_MEMBER server name across the whole shard map. A read member
// checking in for pending cache-update work must contact every such
// server (except itself), because a cached reference can point at any
// collection and this MVP has a single global shard map
// (CACHE-UPDATES.md: "each read member contacts the relevant SOT-members
// every general.cacheUpdateCheckinSeconds seconds").
func (c *Config) AllSOTMembers() []string {
	if c == nil {
		return nil
	}
	set := map[string]bool{}
	for _, assignment := range c.ShardMap.ShardMap.Default {
		if assignment.ShardSOTMember != "" {
			set[assignment.ShardSOTMember] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ServerBaseURL returns the baseURL for a known server name, or "".
func (c *Config) ServerBaseURL(name string) string {
	if c == nil {
		return ""
	}
	if s, ok := c.Servers.Servers[name]; ok {
		return s.BaseURL
	}
	return ""
}
