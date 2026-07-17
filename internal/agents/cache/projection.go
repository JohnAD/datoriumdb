package cache

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/ojson"
)

// RefPrefix is SCHEMAS.md's "Cached Document Summary References" storage
// convention: "@@__{collection}__{id}".
const RefPrefix = "@@__"

// ParseRef parses a DatoriumCachedRef field's stored string value into its
// target collection and document ID.
func ParseRef(s string) (collection, id string, ok bool) {
	if !strings.HasPrefix(s, RefPrefix) {
		return "", "", false
	}
	rest := s[len(RefPrefix):]
	parts := strings.SplitN(rest, "__", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// EncodeRef builds the "@@__{collection}__{id}" stored string form.
func EncodeRef(collection, id string) string {
	return RefPrefix + collection + "__" + id
}

// RefField is one resolved DatoriumCachedRef field found directly on a
// document (top-level fields only; see FindRefFields).
type RefField struct {
	FieldName        string
	TargetCollection string
	TargetID         string
	Summary          []string
}

// FindRefFields scans doc's top-level schema fields for DatoriumCachedRef
// fields with a resolvable stored value. Per SCHEMAS.md's "Cached Document
// Summary References", the value itself carries {collection}/{id}, so no
// disambiguation against custom.collections is needed here (that list is
// only a write-time validation constraint, per COMMAND-LINE-TOOLS.md).
//
// This narrow MVP only looks at top-level object fields, not fields nested
// inside child objects or array items; nested cached-reference fields are
// a documented gap (see AGENT-FOR-COLLECTION-UPGRADE / CACHE-UPDATES open
// details).
func FindRefFields(schemaRaw json.RawMessage, doc map[string]any) ([]RefField, error) {
	schema, err := config.CompileSchemaBytes(schemaRaw)
	if err != nil {
		return nil, err
	}
	root := schema.Root()
	if !root.Valid() {
		return nil, nil
	}
	var out []RefField
	for _, child := range root.Children() {
		if child.Format() != "DatoriumCachedRef" {
			continue
		}
		raw, ok := doc[child.Name()]
		if !ok {
			continue
		}
		s, isStr := raw.(string)
		if !isStr {
			continue
		}
		collection, id, ok := ParseRef(s)
		if !ok {
			continue
		}
		out = append(out, RefField{
			FieldName:        child.Name(),
			TargetCollection: collection,
			TargetID:         id,
			Summary:          stringItems(child.Custom().Get("summary")),
		})
	}
	return out, nil
}

func stringItems(v ojson.JSONValue) []string {
	if !v.IsArray() {
		return nil
	}
	var out []string
	for _, item := range v.Items() {
		if item.IsString() {
			out = append(out, item.String())
		}
	}
	return out
}

// BuildSummary projects summaryPaths out of a cached raw source-document
// snapshot (as stored in a local .cache file), per SCHEMAS.md's "Cached
// Document Summary References" projection rules: paths that don't resolve
// in the snapshot are skipped, and "!"/"$"/"#" always ride along so the
// client can interpret the record (matching ACCESS-LANGUAGE.md's
// cacheSummaries example, which includes "!" and "$" alongside summary
// fields).
func BuildSummary(cached map[string]any, summaryPaths []string) map[string]any {
	out := map[string]any{}
	if bang, ok := cached["!"]; ok {
		out["!"] = bang
	}
	if marker, ok := cached["$"]; ok {
		out["$"] = marker
	}
	out["#"] = cached["#"]
	for _, p := range summaryPaths {
		v, ok := lookupPath(cached, p)
		if !ok {
			continue
		}
		out[p] = v
	}
	return out
}

func lookupPath(doc map[string]any, path string) (any, bool) {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var cur any = doc
	for _, seg := range segments {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[seg]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// LoadCacheFile reads this server's local cached snapshot for
// (sourceCollection, sourceDocID), if any.
func LoadCacheFile(dataDir, sourceCollection, sourceDocID string) (map[string]any, bool, error) {
	path := fsstore.CachePath(dataDir, sourceCollection, sourceDocID)
	doc, err := fsstore.ReadDocumentJSON(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return doc, true, nil
}

// EnsureStub creates a lost-reference stub cache file ({"!":id,"#":null})
// if one does not already exist yet, so a future pending cache-update work
// item (which, per CACHE-UPDATES.md's "No Sweeping Reads", only updates an
// *existing* cache file) has something to update. Read-triggered stub
// creation is this implementation's documented resolution of
// CACHE-UPDATES.md's open detail on how a cache file is first created for
// a reference to a not-yet-cached document; see cache/doc.go.
func EnsureStub(dataDir, sourceCollection, sourceDocID string) (map[string]any, error) {
	existing, ok, err := LoadCacheFile(dataDir, sourceCollection, sourceDocID)
	if err != nil {
		return nil, err
	}
	if ok {
		return existing, nil
	}
	stub := map[string]any{"!": sourceDocID, "#": nil}
	if err := os.MkdirAll(fsstore.CacheDir(dataDir, sourceCollection), 0o755); err != nil {
		return nil, err
	}
	path := fsstore.CachePath(dataDir, sourceCollection, sourceDocID)
	if err := fsstore.WriteDocumentJSON(path, stub); err != nil {
		return nil, err
	}
	return stub, nil
}
