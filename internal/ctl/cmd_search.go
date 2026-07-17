package ctl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
)

func cmdSearchCreate(ctx *Context, args []string) Outcome {
	if len(args) < 3 {
		return SimpleValidationError("search.create", "invalidArguments", "usage: search create <CollectionName> <SearchName> <search-definition-file.json>")
	}
	collection, name, defFile := args[0], args[1], args[2]
	raw, err := os.ReadFile(defFile)
	if err != nil {
		return ValidationFailSimple("search.create", "invalidJSON", fmt.Sprintf("cannot read search definition file: %v", err))
	}

	mutate := func(cfg *config.Config) (*Plan, map[string]any, []envelope.Error) {
		fields := map[string]any{"collection": collection, "search": name}
		if _, exists := cfg.Searches[collection][name]; exists {
			return nil, fields, []envelope.Error{{Code: "searchAlreadyExists", Message: "search definition already exists", Actual: name}}
		}
		if errs := config.ValidateSearchDefinition(raw, collection, name, cfg.Schemas); len(errs) > 0 {
			return nil, fields, errs
		}
		pretty, err := ReindentJSON(raw)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "invalidJSON", Message: err.Error()}}
		}
		if cfg.Searches[collection] == nil {
			cfg.Searches[collection] = map[string]json.RawMessage{}
		}
		cfg.Searches[collection][name] = json.RawMessage(pretty)
		if cfg.SearchHistory[collection] == nil {
			cfg.SearchHistory[collection] = map[string]map[int]json.RawMessage{}
		}
		if cfg.SearchHistory[collection][name] == nil {
			cfg.SearchHistory[collection][name] = map[int]json.RawMessage{}
		}
		cfg.SearchHistory[collection][name][1] = json.RawMessage(pretty)
		if cfg.SearchVersions[collection] == nil {
			cfg.SearchVersions[collection] = map[string]int{}
		}
		cfg.SearchVersions[collection][name] = 1
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}

		plan := &Plan{}
		plan.AddWrite(searchPath(cfg, collection, name), pretty)
		plan.AddWrite(searchVersionPath(cfg, collection, name, 1), pretty)
		plan.AddDir(filepath.Join(ctx.DataDir, collection, ".search", name))
		nextVersion, err := stageGeneralBump(plan, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields["searchVersion"] = 1
		fields["generalVersion"] = nextVersion
		return plan, fields, nil
	}
	return runMutation(ctx, "search.create", mutate)
}

func cmdSearchDelete(ctx *Context, args []string) Outcome {
	if len(args) < 2 {
		return SimpleValidationError("search.delete", "invalidArguments", "usage: search delete <CollectionName> <SearchName>")
	}
	collection, name := args[0], args[1]

	mutate := func(cfg *config.Config) (*Plan, map[string]any, []envelope.Error) {
		fields := map[string]any{"collection": collection, "search": name}
		if _, exists := cfg.Searches[collection][name]; !exists {
			return nil, fields, []envelope.Error{{Code: "searchNotFound", Message: "search definition does not exist", Actual: name}}
		}
		if !confirm(ctx, fmt.Sprintf("Delete search %q on collection %q?", name, collection)) {
			return nil, fields, []envelope.Error{{Code: "cancelled", Message: "operation cancelled; pass --yes to skip confirmation"}}
		}
		delete(cfg.Searches[collection], name)
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}
		plan := &Plan{}
		plan.AddRemove(searchPath(cfg, collection, name))
		nextVersion, err := stageGeneralBump(plan, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields["generalVersion"] = nextVersion
		return plan, fields, nil
	}
	return runMutation(ctx, "search.delete", mutate)
}

func cmdSearchList(ctx *Context, _ []string) Outcome {
	cfg, outcome, ok := loadReadOnly(ctx, "search.list")
	if !ok {
		return outcome
	}
	type entry struct {
		Collection string `json:"collection"`
		Name       string `json:"name"`
		Version    int    `json:"version"`
	}
	var entries []entry
	collections := make([]string, 0, len(cfg.Searches))
	for c := range cfg.Searches {
		collections = append(collections, c)
	}
	sort.Strings(collections)
	human := ""
	for _, c := range collections {
		names := make([]string, 0, len(cfg.Searches[c]))
		for n := range cfg.Searches[c] {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			ver := cfg.SearchVersions[c][n]
			entries = append(entries, entry{Collection: c, Name: n, Version: ver})
			human += fmt.Sprintf("%s\t%s\t%d\n", c, n, ver)
		}
	}
	return OKHuman(map[string]any{"command": "search.list", "searches": entries}, human)
}
