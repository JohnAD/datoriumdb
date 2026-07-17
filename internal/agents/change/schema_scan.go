package change

import "encoding/json"

// schemaNode is a minimal decode of an OJSON collection schema document,
// used only to find DatoriumCachedRef fields for cache-update fan-out.
type schemaNode struct {
	Kind     string          `json:"kind"`
	Format   string          `json:"format"`
	Children []schemaNode    `json:"children"`
	Items    *schemaNode     `json:"items"`
	Custom   json.RawMessage `json:"custom"`
}

type customCollections struct {
	Collections []string `json:"collections"`
}

// schemaReferencesCollection reports whether raw declares any
// DatoriumCachedRef field whose custom.collections includes target.
func schemaReferencesCollection(raw json.RawMessage, target string) bool {
	var root schemaNode
	if err := json.Unmarshal(raw, &root); err != nil {
		return false
	}
	return nodeReferencesCollection(root, target)
}

func nodeReferencesCollection(n schemaNode, target string) bool {
	if n.Format == "DatoriumCachedRef" && len(n.Custom) > 0 {
		var custom customCollections
		if err := json.Unmarshal(n.Custom, &custom); err == nil {
			for _, c := range custom.Collections {
				if c == target {
					return true
				}
			}
		}
	}
	for _, child := range n.Children {
		if nodeReferencesCollection(child, target) {
			return true
		}
	}
	if n.Items != nil && nodeReferencesCollection(*n.Items, target) {
		return true
	}
	return false
}
