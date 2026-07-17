// Package upgrade implements AGENT-FOR-COLLECTION-UPGRADE.md /
// UPDATE-SCHEMA.md's document migration: replaying a collection's
// persisted per-version update lists (config.SchemaUpdateHistory) against
// one document's content to bring it forward to the collection's current
// schema version, either from the background upgrade-agent or on access
// (engine read/patch).
package upgrade

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/schemapatch"
)

// DocVersion parses the numeric version suffix from a document's "$"
// marker ("{collection}:{ver}"), per CONVENTIONS.md.
func DocVersion(doc map[string]any) (int, bool) {
	marker, _ := doc["$"].(string)
	idx := strings.LastIndex(marker, ":")
	if idx < 0 || idx == len(marker)-1 {
		return 0, false
	}
	v, err := strconv.Atoi(marker[idx+1:])
	if err != nil {
		return 0, false
	}
	return v, true
}

// NeedsMigration reports whether doc's schema marker is behind
// collection's current schema version in cfg.
func NeedsMigration(cfg *config.Config, collection string, doc map[string]any) bool {
	if cfg == nil || doc == nil {
		return false
	}
	current, ok := DocVersion(doc)
	if !ok {
		return false
	}
	return current < cfg.SchemaVersion(collection)
}

// MigrateDocument advances doc's content (leaving "!" and "#" untouched)
// from its current schema version to targetVersion by replaying each
// intervening version's persisted update list in order, per
// UPDATE-SCHEMA.md: "After the schema is advanced, document conversion
// should not fail." A missing persisted update list for any required step
// is treated as a hard migration error rather than silently skipped, since
// this implementation cannot safely guess the intended per-document
// transform.
func MigrateDocument(cfg *config.Config, collection string, doc map[string]any, targetVersion int) (changed bool, err error) {
	current, ok := DocVersion(doc)
	if !ok {
		return false, fmt.Errorf("document has no valid $ schema marker to migrate from")
	}
	if current >= targetVersion {
		return false, nil
	}
	history := cfg.SchemaUpdateHistory[collection]
	for v := current + 1; v <= targetVersion; v++ {
		rawSpec, ok := history[v]
		if !ok {
			return false, fmt.Errorf("missing persisted update list for %s schema version %d; cannot safely migrate this document", collection, v)
		}
		spec, err := schemapatch.ParseUpdateSpec(rawSpec)
		if err != nil {
			return false, fmt.Errorf("parse persisted update list for %s version %d: %w", collection, v, err)
		}
		if _, err := schemapatch.ApplyToDocument(doc, spec); err != nil {
			return false, fmt.Errorf("apply update list for %s version %d: %w", collection, v, err)
		}
	}
	doc["$"] = fmt.Sprintf("%s:%d", collection, targetVersion)
	return true, nil
}
