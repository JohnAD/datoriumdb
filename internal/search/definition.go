// Package search implements the narrow MVP precompiled-search model
// described in tech-docs/SEARCHING.md, tech-docs/SEARCH-DEFINITION-SCHEMA.md,
// and tech-docs/SEARCHING-V1.md: definition validation, live-variable
// binding, selected constant `in` buckets, canonical path encoding, shard
// calculation, clause evaluation against a document, and null/missing-aware
// deterministic sorting.
//
// Per the locked MVP scope, only a narrow operation subset is implemented:
// `equals` (string/boolean/null-comparison), `in` (string, with a mandatory
// live `select` variable — the server never unions buckets), and `exists`
// (any kind, with a truth variable and optional `hideNulls`). Every other
// V1 operation documented in SEARCHING-V1*.md is recognized by name but
// rejected as not-yet-implemented, per SEARCHING-V1.md's "MVP
// Implementation Scope".
package search

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/ojson"
)

// Op is a v1 search clause operation name.
type Op string

const (
	OpEquals Op = "equals"
	OpIn     Op = "in"
	OpExists Op = "exists"
)

// mvpImplementedOps is the set of ops this implementation evaluates. All
// other documented V1 ops parse but are rejected by Validate.
var mvpImplementedOps = map[Op]bool{
	OpEquals: true,
	OpIn:     true,
	OpExists: true,
}

// knownV1Ops mirrors the full op vocabulary from SEARCHING-V1.md so
// Validate can distinguish "unsupported in MVP" from "not a real op".
var knownV1Ops = map[Op]bool{
	OpEquals: true, "scalarEquals": true, "preciselyEquals": true, "hashEquals": true,
	OpIn: true, "scalarIn": true, "preciselyIn": true, OpExists: true, "missing": true,
	"contains": true, "endsWith": true, "greaterThan": true, "lessThan": true,
	"greaterThanOrEqual": true, "lessThanOrEqual": true, "between": true,
	"startsWith": true, "containsText": true,
}

// Clause is one parsed v1 search clause. Value may hold a JSON scalar
// constant, an array of constants (for `in`), a variable reference string
// (leading "$"), or nil (structural clauses such as `exists`, where the
// truth variable is carried in Value itself per SEARCH-DEFINITION-SCHEMA.md).
type Clause struct {
	Field     string
	Op        Op
	Value     any
	Truth     string // variable name without validation of leading $, "" if constant/absent
	Select    string // variable name, "" if absent
	HideNulls bool
}

// SortSpec is one parsed sort entry.
type SortSpec struct {
	Field string
	Dir   string // "asc" or "desc"
}

// Definition is a parsed SearchDefinition:v1 document.
type Definition struct {
	Schema     string
	Collection string
	Name       string
	Version    int
	Clauses    []Clause
	Sort       []SortSpec
}

type rawClause struct {
	Field     string `json:"field"`
	Op        string `json:"op"`
	Value     any    `json:"value,omitempty"`
	Truth     any    `json:"truth,omitempty"`
	Select    any    `json:"select,omitempty"`
	HideNulls bool   `json:"hideNulls,omitempty"`
}

type rawSort struct {
	Field string `json:"field"`
	Dir   string `json:"dir"`
}

type rawV1Body struct {
	Clauses []rawClause `json:"clauses"`
	Sort    []rawSort   `json:"sort"`
}

type rawDefinition struct {
	Schema     string    `json:"$"`
	Collection string    `json:"collection"`
	Name       string    `json:"name"`
	Version    int       `json:"version"`
	V1         rawV1Body `json:"v1"`
}

// ParseDefinition decodes a stored SearchDefinition:v1 document.
func ParseDefinition(raw []byte) (*Definition, error) {
	var doc rawDefinition
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("invalid search definition JSON: %w", err)
	}
	def := &Definition{
		Schema:     doc.Schema,
		Collection: doc.Collection,
		Name:       doc.Name,
		Version:    doc.Version,
	}
	for _, c := range doc.V1.Clauses {
		truth, _ := c.Truth.(string)
		sel, _ := c.Select.(string)
		def.Clauses = append(def.Clauses, Clause{
			Field:     c.Field,
			Op:        Op(c.Op),
			Value:     c.Value,
			Truth:     truth,
			Select:    sel,
			HideNulls: c.HideNulls,
		})
	}
	for _, s := range doc.V1.Sort {
		def.Sort = append(def.Sort, SortSpec{Field: s.Field, Dir: s.Dir})
	}
	return def, nil
}

// IsVariable reports whether v is a live-query variable reference such as
// "$status".
func IsVariable(v any) (name string, ok bool) {
	s, isStr := v.(string)
	if !isStr || !strings.HasPrefix(s, "$") || len(s) < 2 {
		return "", false
	}
	return s, true
}

// Validate checks def against SEARCH-DEFINITION-SCHEMA.md structural rules
// plus the MVP op whitelist, given the compiled schema for def.Collection.
// existingCollections is used only to confirm def.Collection exists.
func (def *Definition) Validate(root ojson.SchemaEntry, existingCollections map[string]json.RawMessage) []envelope.Error {
	var errs []envelope.Error
	if def.Schema != "SearchDefinition:v1" {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: "/$", Message: `$ must be "SearchDefinition:v1"`, Actual: def.Schema})
	}
	if _, ok := existingCollections[def.Collection]; !ok {
		errs = append(errs, envelope.Error{Code: "collectionNotFound", Path: "/collection", Message: "search collection does not exist", Actual: def.Collection})
	}
	if def.Version <= 0 {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: "/version", Message: "version must be a positive integer", Actual: def.Version})
	}
	if len(def.Clauses) == 0 {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: "/v1/clauses", Message: "clauses must be a non-empty array"})
	}
	for i, c := range def.Clauses {
		errs = append(errs, def.validateClause(i, c, root)...)
	}
	for i, s := range def.Sort {
		path := fmt.Sprintf("/v1/sort/%d", i)
		if s.Field != "/!" {
			if !root.Valid() {
				errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/field", Message: "sort field does not resolve to a schema-defined field", Actual: s.Field})
			} else if _, ok := ResolveFieldSchema(root, s.Field); !ok {
				errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/field", Message: "sort field does not resolve to a schema-defined field", Actual: s.Field})
			}
		}
		if s.Dir != "asc" && s.Dir != "desc" {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/dir", Message: "dir must be asc or desc", Actual: s.Dir})
		}
	}
	return errs
}

func (def *Definition) validateClause(i int, c Clause, root ojson.SchemaEntry) []envelope.Error {
	var errs []envelope.Error
	path := fmt.Sprintf("/v1/clauses/%d", i)
	if c.Field == "" {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/field", Message: "field is required"})
	}
	var fieldSchema ojson.SchemaEntry
	var fieldOK bool
	if c.Field != "" && root.Valid() {
		fieldSchema, fieldOK = ResolveFieldSchema(root, c.Field)
		if !fieldOK {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/field", Message: "field does not resolve to a schema-defined field", Actual: c.Field})
		}
	}
	if c.Op == "" {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/op", Message: "op is required"})
		return errs
	}
	if !knownV1Ops[c.Op] {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/op", Message: "unsupported search operation", Actual: string(c.Op)})
		return errs
	}
	if !mvpImplementedOps[c.Op] {
		errs = append(errs, envelope.Error{Code: "unsupportedInMVP", Path: path + "/op", Message: "this V1 operation is specified but not implemented by the narrow MVP search evaluator; only equals, in (with select), and exists are supported", Actual: string(c.Op)})
		return errs
	}
	if c.HideNulls && c.Op != OpExists {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/hideNulls", Message: "hideNulls is only supported on exists clauses"})
	}
	switch c.Op {
	case OpEquals:
		errs = append(errs, def.validateEquals(path, c, fieldSchema, fieldOK)...)
	case OpIn:
		errs = append(errs, def.validateIn(path, c, fieldSchema, fieldOK)...)
	case OpExists:
		errs = append(errs, def.validateExists(path, c)...)
	}
	return errs
}

func (def *Definition) validateEquals(path string, c Clause, fieldSchema ojson.SchemaEntry, fieldOK bool) []envelope.Error {
	var errs []envelope.Error
	if varName, isVar := IsVariable(c.Value); isVar {
		if !strings.HasPrefix(varName, "$") {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/value", Message: "variable must start with $"})
		}
		if c.Truth != "" {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/truth", Message: "equals with a variable value must not also declare truth"})
		}
		return errs
	}
	if c.Value == nil {
		if c.Truth == "" {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/truth", Message: "equals value:null requires a truth variable"})
		}
		return errs
	}
	if fieldOK {
		switch c.Value.(type) {
		case string:
			if fieldSchema.Kind() != ojson.KindString {
				errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/value", Message: "string equals value requires a string field"})
			}
		case bool:
			if fieldSchema.Kind() != ojson.KindBoolean {
				errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/value", Message: "boolean equals value requires a boolean field"})
			}
		default:
			errs = append(errs, envelope.Error{Code: "unsupportedInMVP", Path: path + "/value", Message: "the narrow MVP evaluator only supports string/boolean/null equals constants"})
		}
	}
	return errs
}

func (def *Definition) validateIn(path string, c Clause, fieldSchema ojson.SchemaEntry, fieldOK bool) []envelope.Error {
	var errs []envelope.Error
	if c.Select == "" {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/select", Message: "constant multi-value in requires a live select variable (locked MVP decision); the server never unions buckets"})
	} else if !strings.HasPrefix(c.Select, "$") {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/select", Message: "select must be a variable starting with $"})
	}
	if c.Truth != "" {
		errs = append(errs, envelope.Error{Code: "unsupportedInMVP", Path: path + "/truth", Message: "the narrow MVP in evaluator only supports the select form, not truth-partitioned in"})
	}
	values, ok := c.Value.([]any)
	if !ok || len(values) == 0 {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/value", Message: "in requires a non-empty array of constant values"})
		return errs
	}
	for _, v := range values {
		s, isStr := v.(string)
		if !isStr {
			errs = append(errs, envelope.Error{Code: "unsupportedInMVP", Path: path + "/value", Message: "the narrow MVP in evaluator only supports string constants"})
			continue
		}
		if fieldOK && fieldSchema.Kind() != ojson.KindString {
			errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/value", Message: "string in value requires a string field"})
		}
		_ = s
	}
	return errs
}

func (def *Definition) validateExists(path string, c Clause) []envelope.Error {
	var errs []envelope.Error
	varName, isVar := IsVariable(c.Value)
	if !isVar {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/value", Message: "exists requires value to be a truth variable"})
		return errs
	}
	if !strings.HasPrefix(varName, "$") {
		errs = append(errs, envelope.Error{Code: "invalidSearchDefinition", Path: path + "/value", Message: "truth variable must start with $"})
	}
	return errs
}
