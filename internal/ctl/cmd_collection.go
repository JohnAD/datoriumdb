package ctl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	mutate := func(cfg *config.Config) (*Plan, map[string]any, []envelope.Error) {
		fields := map[string]any{"collection": name}
		if !config.ValidCollectionName(name) {
			return nil, fields, []envelope.Error{{Code: "invalidCollectionName", Message: "collection name violates naming conventions", Actual: name}}
		}
		if _, exists := cfg.Schemas[name]; exists {
			return nil, fields, []envelope.Error{{Code: "collectionAlreadyExists", Message: "collection already exists", Actual: name}}
		}
		if err := config.ValidateOJSONSchemaBytes(rawSchema); err != nil {
			return nil, fields, []envelope.Error{{Code: "invalidSchema", Message: err.Error()}}
		}
		if errs := config.ValidateCollectionSchemaRules(rawSchema, cfg.Schemas); len(errs) > 0 {
			return nil, fields, errs
		}
		pretty, err := ReindentJSON(rawSchema)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "invalidJSON", Message: err.Error()}}
		}

		cfg.Schemas[name] = json.RawMessage(pretty)
		if cfg.SchemaHistory[name] == nil {
			cfg.SchemaHistory[name] = map[int]json.RawMessage{}
		}
		cfg.SchemaHistory[name][0] = json.RawMessage(pretty)
		cfg.SchemaVersions[name] = 0
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}

		plan := &Plan{}
		plan.AddWrite(schemaPath(cfg, name), pretty)
		plan.AddWrite(schemaVersionPath(cfg, name, 0), pretty)
		plan.AddDir(filepath.Join(ctx.DataDir, name))
		nextVersion, err := stageGeneralBump(plan, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields["schemaVersion"] = 0
		fields["generalVersion"] = nextVersion
		return plan, fields, nil
	}
	return runMutation(ctx, "collection.create", mutate)
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

	mutate := func(cfg *config.Config) (*Plan, map[string]any, []envelope.Error) {
		fields := map[string]any{"collection": name}
		currentSchema, ok := cfg.Schemas[name]
		if !ok {
			return nil, fields, []envelope.Error{{Code: "collectionNotFound", Message: "collection does not exist", Actual: name}}
		}
		currentVersion := cfg.SchemaVersion(name)
		if errs := spec.Validate(currentVersion); len(errs) > 0 {
			return nil, fields, errs
		}
		newSchemaBytes, err := schemapatch.Apply(currentSchema, spec)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "invalidSchemaUpgrade", Message: err.Error()}}
		}
		if err := config.ValidateOJSONSchemaBytes(newSchemaBytes); err != nil {
			return nil, fields, []envelope.Error{{Code: "invalidSchemaUpgrade", Message: err.Error()}}
		}
		tempSchemas := make(map[string]json.RawMessage, len(cfg.Schemas))
		for k, v := range cfg.Schemas {
			tempSchemas[k] = v
		}
		tempSchemas[name] = newSchemaBytes
		if errs := config.ValidateCollectionSchemaRules(newSchemaBytes, tempSchemas); len(errs) > 0 {
			return nil, fields, errs
		}

		newVer := currentVersion + 1
		newSchemaBytes = ensureTrailingNewline(newSchemaBytes)
		cfg.Schemas[name] = json.RawMessage(newSchemaBytes)
		if cfg.SchemaHistory[name] == nil {
			cfg.SchemaHistory[name] = map[int]json.RawMessage{}
		}
		cfg.SchemaHistory[name][newVer] = json.RawMessage(newSchemaBytes)
		cfg.SchemaVersions[name] = newVer
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}

		plan := &Plan{}
		plan.AddWrite(schemaPath(cfg, name), newSchemaBytes)
		plan.AddWrite(schemaVersionPath(cfg, name, newVer), newSchemaBytes)
		plan.AddWrite(schemaUpdatePath(cfg, name, newVer), ensureTrailingNewline(rawUpgrade))
		nextGeneralVersion, err := stageGeneralBump(plan, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields["fromVersion"] = currentVersion
		fields["toVersion"] = newVer
		fields["generalVersion"] = nextGeneralVersion
		return plan, fields, nil
	}
	return runMutation(ctx, "collection.upgrade", mutate)
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
	var schema any
	_ = json.Unmarshal(raw, &schema)
	fields := map[string]any{
		"command":    "collection.show",
		"collection": name,
		"version":    version,
		"schema":     schema,
	}
	human := fmt.Sprintf("collection: %s\nversion: %d\nschema:\n%s\n", name, version, string(raw))
	return OKHuman(fields, human)
}

func ensureTrailingNewline(b []byte) []byte {
	if len(b) == 0 || b[len(b)-1] != '\n' {
		return append(b, '\n')
	}
	return b
}
