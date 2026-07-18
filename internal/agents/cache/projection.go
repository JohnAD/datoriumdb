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

// RefField is one resolved DatoriumCachedRef value found while walking a
// document against its schema. FieldName is a slash-separated path relative
// to the document root (for example "movieRef", "todoLists", or
// "profile/lists").
type RefField struct {
	FieldName        string
	TargetCollection string
	TargetID         string
	Summary          []string
}

// FindRefFields walks doc against schemaRaw and returns every resolvable
// DatoriumCachedRef string value. Per SCHEMAS.md's "Cached Document Summary
// References", the stored value itself carries {collection}/{id}, so no
// disambiguation against custom.collections is needed here (that list is
// only a write-time validation constraint, per COMMAND-LINE-TOOLS.md).
//
// The walk is recursive: nested objects, arrays of cached-ref strings, and
// arrays of objects (and deeper nesting) are all searched.
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
	collectRefFields(root, doc, "", &out)
	return out, nil
}

func collectRefFields(entry ojson.SchemaEntry, value any, path string, out *[]RefField) {
	if !entry.Valid() || value == nil {
		return
	}

	if entry.Format() == "DatoriumCachedRef" {
		s, ok := value.(string)
		if !ok {
			return
		}
		name := path
		if name == "" {
			name = entry.Name()
		}
		if rf, ok := refFieldFrom(name, s, entry); ok {
			*out = append(*out, rf)
		}
		return
	}

	switch entry.Kind() {
	case ojson.KindObject:
		m, ok := value.(map[string]any)
		if !ok {
			return
		}
		for _, child := range entry.Children() {
			raw, ok := m[child.Name()]
			if !ok {
				continue
			}
			collectRefFields(child, raw, joinFieldPath(path, child.Name()), out)
		}
	case ojson.KindArray:
		items := entry.Items()
		if !items.Valid() {
			return
		}
		arr, ok := value.([]any)
		if !ok {
			return
		}
		arrayPath := path
		if arrayPath == "" {
			arrayPath = entry.Name()
		}
		for _, elem := range arr {
			if items.Format() == "DatoriumCachedRef" {
				// Array of ref strings: attribute each hit to the array field path.
				collectRefFields(items, elem, arrayPath, out)
				continue
			}
			// Array of objects (or deeper): keep the array path as the
			// prefix and let object children append their names.
			collectRefFields(items, elem, arrayPath, out)
		}
	}
}

func joinFieldPath(base, name string) string {
	switch {
	case base == "":
		return name
	case name == "":
		return base
	default:
		return base + "/" + name
	}
}

func refFieldFrom(fieldName, stored string, schemaEntry ojson.SchemaEntry) (RefField, bool) {
	collection, id, ok := ParseRef(stored)
	if !ok {
		return RefField{}, false
	}
	return RefField{
		FieldName:        fieldName,
		TargetCollection: collection,
		TargetID:         id,
		Summary:          stringItems(schemaEntry.Custom().Get("summary")),
	}, true
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
