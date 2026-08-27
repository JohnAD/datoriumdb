package ctl

import (
	"fmt"
	"os"
	"sort"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/search"
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

	fields := map[string]any{"collection": collection, "search": name}
	cfg, outcome, ok := loadReadOnly(ctx, "search.create")
	if !ok {
		return outcome
	}
	if _, exists := cfg.Searches[collection][name]; exists {
		return ValidationFail(fields, envelope.Error{Code: "searchAlreadyExists", Message: "search definition already exists", Actual: name})
	}
	if errs := config.ValidateSearchDefinition(raw, collection, name, cfg.Schemas); len(errs) > 0 {
		return ValidationFail(fields, errs...)
	}
	if def, err := search.ParseDefinition(raw); err == nil {
		if schemaRaw, ok := cfg.Schemas[collection]; ok {
			if compiled, cerr := config.CompileSchemaBytes(schemaRaw); cerr == nil {
				if verrs := def.Validate(compiled.Root(), cfg.Schemas); len(verrs) > 0 {
					return ValidationFail(fields, verrs...)
				}
			}
		}
	}
	defObj, outcome, ok := parseJSONObject(raw)
	if !ok {
		outcome.Result["command"] = "search.create"
		return outcome
	}

	out := postAdminCommand(ctx, "searchEnsure", collection, name, defObj)
	if out.Result != nil {
		out.Result["command"] = "search.create"
	}
	return out
}

func cmdSearchDelete(ctx *Context, args []string) Outcome {
	if len(args) < 2 {
		return SimpleValidationError("search.delete", "invalidArguments", "usage: search delete <CollectionName> <SearchName>")
	}
	collection, name := args[0], args[1]

	out := postAdminCommand(ctx, "searchDelete", collection, name, map[string]any{})
	if out.Result != nil {
		out.Result["command"] = "search.delete"
	}
	return out
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
