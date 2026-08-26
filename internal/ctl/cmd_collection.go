package ctl

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/schemapatch"
)

func cmdCollectionCreate(ctx *Context, args []string) Outcome {
	if len(args) < 2 {
		return SimpleValidationError("collection.create", "invalidArguments", "usage: collection create <CollectionName> <schema-file.json>")
	}
	name := args[0]
	schemaFile := args[1]

	rawSchema, err := os.ReadFile(schemaFile)
	if err != nil {
		return ValidationFailSimple("collection.create", "invalidSchema", fmt.Sprintf("cannot read schema file: %v", err))
	}

	fields := map[string]any{"collection": name}
	if !config.ValidCollectionName(name) {
		return ValidationFail(fields, envelope.Error{Code: "invalidCollectionName", Message: "collection name violates naming conventions", Actual: name})
	}
	if err := config.ValidateOJSONSchemaBytes(rawSchema); err != nil {
		return ValidationFail(fields, envelope.Error{Code: "invalidSchema", Message: err.Error()})
	}
	cfg, outcome, ok := loadReadOnly(ctx, "collection.create")
	if !ok {
		return outcome
	}
	if _, exists := cfg.Schemas[name]; exists {
		return ValidationFail(fields, envelope.Error{Code: "collectionAlreadyExists", Message: "collection already exists", Actual: name})
	}
	if errs := config.ValidateCollectionSchemaRules(rawSchema, cfg.Schemas); len(errs) > 0 {
		return ValidationFail(fields, errs...)
	}
	schemaObj, outcome, ok := parseJSONObject(rawSchema)
	if !ok {
		outcome.Result["command"] = "collection.create"
		return outcome
	}

	detail := map[string]any{"schema": schemaObj}
	out := postAdminCommand(ctx, "collectionEnsure", name, "", detail)
	if out.Result != nil {
		out.Result["command"] = "collection.create"
	}
	return out
}

func cmdCollectionUpgrade(ctx *Context, args []string) Outcome {
	if len(args) < 2 {
		return SimpleValidationError("collection.upgrade", "invalidArguments", "usage: collection upgrade <CollectionName> <upgrade-file.json>")
	}
	name := args[0]
	upgradeFile := args[1]

	rawUpgrade, err := os.ReadFile(upgradeFile)
	if err != nil {
		return ValidationFailSimple("collection.upgrade", "invalidSchemaUpgrade", fmt.Sprintf("cannot read upgrade file: %v", err))
	}
	spec, err := schemapatch.ParseUpdateSpec(rawUpgrade)
	if err != nil {
		return ValidationFailSimple("collection.upgrade", "invalidSchemaUpgrade", err.Error())
	}

	fields := map[string]any{"collection": name}
	cfg, outcome, ok := loadReadOnly(ctx, "collection.upgrade")
	if !ok {
		return outcome
	}
	currentSchema, exists := cfg.Schemas[name]
	if !exists {
		return ValidationFail(fields, envelope.Error{Code: "collectionNotFound", Message: "collection does not exist", Actual: name})
	}
	currentVersion := cfg.SchemaVersion(name)
	if errs := spec.Validate(currentVersion); len(errs) > 0 {
		return ValidationFail(fields, errs...)
	}
	newSchemaBytes, err := schemapatch.Apply(currentSchema, spec)
	if err != nil {
		return ValidationFail(fields, envelope.Error{Code: "invalidSchemaUpgrade", Message: err.Error()})
	}
	if err := config.ValidateOJSONSchemaBytes(newSchemaBytes); err != nil {
		return ValidationFail(fields, envelope.Error{Code: "invalidSchemaUpgrade", Message: err.Error()})
	}
	tempSchemas := make(map[string]json.RawMessage, len(cfg.Schemas))
	for k, v := range cfg.Schemas {
		tempSchemas[k] = v
	}
	tempSchemas[name] = newSchemaBytes
	if errs := config.ValidateCollectionSchemaRules(newSchemaBytes, tempSchemas); len(errs) > 0 {
		return ValidationFail(fields, errs...)
	}

	upgradeObj, outcome, ok := parseJSONObject(rawUpgrade)
	if !ok {
		outcome.Result["command"] = "collection.upgrade"
		return outcome
	}
	detail := map[string]any{"upgrade": upgradeObj}
	out := postAdminCommand(ctx, "collectionEnsure", name, "", detail)
	if out.Result != nil {
		out.Result["command"] = "collection.upgrade"
	}
	return out
}

func cmdCollectionList(ctx *Context, _ []string) Outcome {
	cfg, outcome, ok := loadReadOnly(ctx, "collection.list")
	if !ok {
		return outcome
	}
	names := make([]string, 0, len(cfg.Schemas))
	for name := range cfg.Schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	type entry struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
	}
	entries := make([]entry, 0, len(names))
	human := ""
	for _, name := range names {
		ver := cfg.SchemaVersion(name)
		entries = append(entries, entry{Name: name, Version: ver})
		human += fmt.Sprintf("%s\t%d\n", name, ver)
	}
	fields := map[string]any{"command": "collection.list", "collections": entries}
	return OKHuman(fields, human)
}

func cmdCollectionShow(ctx *Context, args []string) Outcome {
	if len(args) < 1 {
		return SimpleValidationError("collection.show", "invalidArguments", "usage: collection show <CollectionName> [--version <ver>]")
	}
	name := args[0]
	ver, args, err := extractIntFlag(args[1:], "--version")
	_ = args
	if err != nil {
		return ValidationFailSimple("collection.show", "invalidArguments", err.Error())
	}
	cfg, outcome, ok := loadReadOnly(ctx, "collection.show")
	if !ok {
		return outcome
	}
	if _, exists := cfg.Schemas[name]; !exists {
		return ValidationFail(map[string]any{"command": "collection.show", "collection": name}, envelope.Error{Code: "collectionNotFound", Message: "collection does not exist", Actual: name})
	}
	var raw json.RawMessage
	var version int
	if ver != nil {
		raw, ok = cfg.SchemaHistory[name][*ver]
		if !ok {
			return ValidationFail(map[string]any{"command": "collection.show", "collection": name}, envelope.Error{Code: "invalidArguments", Message: "schema version not found", Path: "/version", Actual: *ver})
		}
		version = *ver
	} else {
		raw = cfg.Schemas[name]
		version = cfg.SchemaVersion(name)
	}
	fields := map[string]any{
		"command":    "collection.show",
		"collection": name,
		"version":    version,
		"schema":     json.RawMessage(raw),
	}
	human := fmt.Sprintf("collection: %s\nversion: %d\nschema:\n%s\n", name, version, string(raw))
	return OKHuman(fields, human)
}
