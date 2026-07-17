package config

import (
	"encoding/json"
	"fmt"

	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/ojson"
)

// datoriumStringFormats registers DatoriumDB's custom OJSON string formats
// so the compiler accepts them. Values are validated as opaque strings here;
// the document-content reference convention (SCHEMAS.md) is enforced at
// write time by the engine, not at schema-compile time.
var datoriumStringFormats = buildDatoriumStringFormats()

func buildDatoriumStringFormats() *ojson.StringFormatRegistry {
	registry := ojson.NewStringFormatRegistry()
	noop := ojson.StringFormatFunc(func(string) error { return nil })
	_ = registry.Register("DatoriumDirectRef", noop, nil)
	_ = registry.Register("DatoriumCachedRef", noop, nil)
	return registry
}

// compileDatoriumSchema compiles raw as an OJSON schema, recognizing the
// DatoriumDirectRef and DatoriumCachedRef string formats.
func compileDatoriumSchema(raw []byte) (ojson.JSONSchema, error) {
	return ojson.CompileSchemaBytes(raw, ojson.WithStringFormats(datoriumStringFormats))
}

// CompileSchemaBytes compiles a collection schema with DatoriumDB formats registered.
func CompileSchemaBytes(raw []byte) (ojson.JSONSchema, error) {
	return compileDatoriumSchema(raw)
}

// ValidateOJSONSchemaBytes compiles raw as a strict OJSON schema and
// requires the root kind to be "object".
func ValidateOJSONSchemaBytes(raw []byte) error {
	schema, err := compileDatoriumSchema(raw)
	if err != nil {
		return err
	}
	if schema.Kind() != ojson.KindObject {
		return fmt.Errorf("schema root must be kind object")
	}
	return nil
}

// ValidateCollectionSchemaRules enforces DatoriumDB-specific schema rules on
// top of plain OJSON compilation: DatoriumDirectRef/DatoriumCachedRef
// formats are only valid on kind string, and DatoriumCachedRef fields must
// declare custom.collections and custom.summary correctly. existingSchemas
// is used to check that cached-reference summary paths resolve against
// target collections that already exist; targets that don't exist yet are
// skipped, matching COMMAND-LINE-TOOLS.md.
func ValidateCollectionSchemaRules(raw []byte, existingSchemas map[string]json.RawMessage) []envelope.Error {
	schema, err := compileDatoriumSchema(raw)
	if err != nil {
		return []envelope.Error{{Code: "invalidSchema", Message: err.Error()}}
	}
	if schema.Kind() != ojson.KindObject {
		return []envelope.Error{{Code: "invalidSchema", Message: "schema root must be kind object"}}
	}
	var errs []envelope.Error
	walkSchemaEntry(schema.Root(), "", existingSchemas, &errs)
	return errs
}

func walkSchemaEntry(entry ojson.SchemaEntry, path string, existingSchemas map[string]json.RawMessage, errs *[]envelope.Error) {
	if !entry.Valid() {
		return
	}
	format := entry.Format()
	if format == "DatoriumDirectRef" || format == "DatoriumCachedRef" {
		if entry.Kind() != ojson.KindString {
			*errs = append(*errs, envelope.Error{
				Code:    "invalidSchema",
				Path:    path,
				Message: fmt.Sprintf("%s format is only valid on kind string", format),
			})
		}
		if format == "DatoriumCachedRef" {
			validateCachedRef(entry, path, existingSchemas, errs)
		}
	}
	for _, child := range entry.Children() {
		walkSchemaEntry(child, path+"/"+child.Name(), existingSchemas, errs)
	}
	if items := entry.Items(); items.Valid() {
		walkSchemaEntry(items, path+"/items", existingSchemas, errs)
	}
}

func validateCachedRef(entry ojson.SchemaEntry, path string, existingSchemas map[string]json.RawMessage, errs *[]envelope.Error) {
	custom := entry.Custom()
	collections := custom.Get("collections")
	if !collections.IsArray() || collections.Len() == 0 {
		*errs = append(*errs, envelope.Error{
			Code:    "invalidSchema",
			Path:    path + "/custom/collections",
			Message: "DatoriumCachedRef fields must include custom.collections as a non-empty array",
		})
		collections = ojson.NewVoid()
	}
	summary := custom.Get("summary")
	if !summary.IsArray() {
		*errs = append(*errs, envelope.Error{
			Code:    "invalidSchema",
			Path:    path + "/custom/summary",
			Message: "DatoriumCachedRef fields must include custom.summary as an array of strings",
		})
		return
	}
	var summaryPaths []string
	for _, item := range summary.Items() {
		if !item.IsString() {
			*errs = append(*errs, envelope.Error{
				Code:    "invalidSchema",
				Path:    path + "/custom/summary",
				Message: "custom.summary items must be strings",
			})
			continue
		}
		summaryPaths = append(summaryPaths, item.String())
	}
	if !collections.IsArray() {
		return
	}
	var targetCollections []string
	for _, item := range collections.Items() {
		if item.IsString() {
			targetCollections = append(targetCollections, item.String())
		}
	}
	for _, target := range targetCollections {
		targetRaw, ok := existingSchemas[target]
		if !ok {
			// Target collection does not exist yet; nothing to check.
			continue
		}
		targetSchema, err := compileDatoriumSchema(targetRaw)
		if err != nil {
			continue
		}
		resolvedInAny := false
		for _, sp := range summaryPaths {
			if summaryPathResolves(targetSchema.Root(), sp) {
				resolvedInAny = true
				break
			}
		}
		if len(summaryPaths) > 0 && !resolvedInAny {
			*errs = append(*errs, envelope.Error{
				Code:    "invalidSchema",
				Path:    path + "/custom/summary",
				Message: fmt.Sprintf("no summary path resolves against target collection %q", target),
			})
		}
	}
}

func summaryPathResolves(root ojson.SchemaEntry, path string) bool {
	segments := splitFieldPath(path)
	node := root
	for _, seg := range segments {
		if !node.Valid() {
			return false
		}
		child := node.Child(seg)
		if !child.Valid() {
			return false
		}
		node = child
	}
	return node.Valid()
}

func splitFieldPath(path string) []string {
	trimmed := path
	for len(trimmed) > 0 && trimmed[0] == '/' {
		trimmed = trimmed[1:]
	}
	if trimmed == "" {
		return nil
	}
	var segments []string
	start := 0
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == '/' {
			segments = append(segments, trimmed[start:i])
			start = i + 1
		}
	}
	segments = append(segments, trimmed[start:])
	return segments
}
