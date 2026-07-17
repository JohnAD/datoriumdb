package config

import (
	"encoding/json"
	"fmt"

	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/ojson"
)

type searchClause struct {
	Field  string `json:"field"`
	Op     string `json:"op"`
	Truth  any    `json:"truth,omitempty"`
	Select any    `json:"select,omitempty"`
}

type searchSort struct {
	Field string `json:"field"`
	Dir   string `json:"dir"`
}

type searchV1Body struct {
	Clauses []searchClause `json:"clauses"`
	Sort    []searchSort   `json:"sort"`
}

type searchDefinitionDoc struct {
	Schema     string       `json:"$"`
	Collection string       `json:"collection"`
	Name       string       `json:"name"`
	Version    int          `json:"version"`
	V1         searchV1Body `json:"v1"`
}

var knownSearchOps = map[string]bool{
	"equals": true, "notEquals": true, "in": true, "notIn": true,
	"greaterThan": true, "greaterThanOrEqual": true, "lessThan": true, "lessThanOrEqual": true,
	"exists": true, "contains": true, "startsWith": true, "endsWith": true,
}

// ValidateSearchDefinition validates a stored search definition document
// per SEARCH-DEFINITION-SCHEMA.md against the known collection schemas.
func ValidateSearchDefinition(raw []byte, collection, name string, schemas map[string]json.RawMessage) []envelope.Error {
	var doc searchDefinitionDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []envelope.Error{{Code: "invalidJSON", Message: err.Error()}}
	}
	var errs []envelope.Error
	if doc.Schema != "SearchDefinition:v1" {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: "/$", Message: "$ must be \"SearchDefinition:v1\"", Actual: doc.Schema})
	}
	if doc.Collection != collection {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: "/collection", Message: "collection must match the CLI argument", Expected: collection, Actual: doc.Collection})
	}
	if doc.Name != name {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: "/name", Message: "name must match the CLI argument", Expected: name, Actual: doc.Name})
	}
	if !ValidSearchName(name) {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: "/name", Message: "search name violates naming conventions", Actual: name})
	}
	if doc.Version <= 0 {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: "/version", Message: "version must be a positive integer", Actual: doc.Version})
	}
	schemaRaw, hasSchema := schemas[collection]
	if !hasSchema {
		errs = append(errs, envelope.Error{Code: "collectionNotFound", Path: "/collection", Message: "collection does not exist", Actual: collection})
	}
	var root ojson.SchemaEntry
	if hasSchema {
		compiled, err := compileDatoriumSchema(schemaRaw)
		if err == nil {
			root = compiled.Root()
		}
	}
	if len(doc.V1.Clauses) == 0 {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: "/v1/clauses", Message: "clauses must be a non-empty array"})
	}
	for i, clause := range doc.V1.Clauses {
		path := fmt.Sprintf("/v1/clauses/%d", i)
		if clause.Field == "" {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/field", Message: "field is required"})
		} else if hasSchema && root.Valid() && !FieldPathResolves(root, clause.Field) {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/field", Message: "field does not resolve to a schema-defined field", Actual: clause.Field})
		}
		if clause.Op == "" {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/op", Message: "op is required"})
		} else if !knownSearchOps[clause.Op] {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/op", Message: "unsupported search operation", Actual: clause.Op})
		}
	}
	for i, s := range doc.V1.Sort {
		path := fmt.Sprintf("/v1/sort/%d", i)
		if s.Field != "/!" && hasSchema && root.Valid() && !FieldPathResolves(root, s.Field) {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/field", Message: "sort field does not resolve to a schema-defined field", Actual: s.Field})
		}
		if s.Dir != "asc" && s.Dir != "desc" {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/dir", Message: "dir must be asc or desc", Actual: s.Dir})
		}
	}
	return errs
}

// FieldPathResolves reports whether a slash-style field path resolves to a
// schema-defined field starting at root.
func FieldPathResolves(root ojson.SchemaEntry, path string) bool {
	return summaryPathResolves(root, path)
}
