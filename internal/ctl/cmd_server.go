package ctl

import (
	"fmt"
	"sort"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
)

func cmdServerList(ctx *Context, _ []string) Outcome {
	cfg, outcome, ok := loadReadOnly(ctx, "server.list")
	if !ok {
		return outcome
	}
	names := make([]string, 0, len(cfg.Servers.Servers))
	for name := range cfg.Servers.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	type entry struct {
		Name    string `json:"name"`
		BaseURL string `json:"baseURL"`
	}
	entries := make([]entry, 0, len(names))
	human := ""
	for _, name := range names {
		url := cfg.Servers.Servers[name].BaseURL
		entries = append(entries, entry{Name: name, BaseURL: url})
		human += fmt.Sprintf("%s\t%s\n", name, url)
	}
	return OKHuman(map[string]any{"command": "server.list", "servers": entries}, human)
}

func cmdServerSet(ctx *Context, args []string) Outcome {
	baseURL, args, hasURL := extractFlag(args, "--base-url")
	if len(args) < 1 {
		return SimpleValidationError("server.set", "invalidArguments", "usage: server set <serverName> --base-url <url>")
	}
	name := args[0]
	if !hasURL || baseURL == "" {
		return ValidationFail(map[string]any{"command": "server.set", "server": name}, envelope.Error{Code: "invalidArguments", Message: "--base-url is required"})
	}

	mutate := func(cfg *config.Config) (*Plan, map[string]any, []envelope.Error) {
		fields := map[string]any{"server": name}
		if !config.ValidServerName(name) {
			return nil, fields, []envelope.Error{{Code: "invalidArguments", Message: "server name must be a non-empty identifier without whitespace", Actual: name}}
		}
		if !config.ValidBaseURL(baseURL) {
			return nil, fields, []envelope.Error{{Code: "invalidBaseURL", Message: "baseURL must be an absolute URL with scheme and host", Actual: baseURL}}
		}
		if cfg.Servers.Servers == nil {
			cfg.Servers.Servers = map[string]config.ServerEntry{}
		}
		cfg.Servers.Servers[name] = config.ServerEntry{BaseURL: baseURL}
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}

		plan := &Plan{}
		if err := plan.AddJSONWrite(serversPath(cfg), cfg.Servers); err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		nextVersion, err := stageGeneralBump(plan, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields["baseURL"] = baseURL
		fields["generalVersion"] = nextVersion
		return plan, fields, nil
	}
	return runMutation(ctx, "server.set", mutate)
}

func cmdServerRemove(ctx *Context, args []string) Outcome {
	if len(args) < 1 {
		return SimpleValidationError("server.remove", "invalidArguments", "usage: server remove <serverName>")
	}
	name := args[0]

	mutate := func(cfg *config.Config) (*Plan, map[string]any, []envelope.Error) {
		fields := map[string]any{"server": name}
		if _, ok := cfg.Servers.Servers[name]; !ok {
			return nil, fields, []envelope.Error{{Code: "serverNotFound", Message: "server does not exist", Actual: name}}
		}
		if cfg.General.General.EstablishmentServer == name {
			return nil, fields, []envelope.Error{{Code: "serverStillReferenced", Message: "server is referenced by general.establishmentServer", Actual: name}}
		}
		if referencedByShardMap(cfg, name) {
			return nil, fields, []envelope.Error{{Code: "serverStillReferenced", Message: "server is referenced by shardMap.default", Actual: name}}
		}
		if !confirm(ctx, fmt.Sprintf("Remove server %q?", name)) {
			return nil, fields, []envelope.Error{{Code: "cancelled", Message: "operation cancelled; pass --yes to skip confirmation"}}
		}
		delete(cfg.Servers.Servers, name)
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}
		plan := &Plan{}
		if err := plan.AddJSONWrite(serversPath(cfg), cfg.Servers); err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		nextVersion, err := stageGeneralBump(plan, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields["generalVersion"] = nextVersion
		return plan, fields, nil
	}
	return runMutation(ctx, "server.remove", mutate)
}

func referencedByShardMap(cfg *config.Config, name string) bool {
	for _, assignment := range cfg.ShardMap.ShardMap.Default {
		if assignment.ShardSOTMember == name {
			return true
		}
		for _, m := range assignment.ShardReadMember {
			if m == name {
				return true
			}
		}
		for _, m := range assignment.ProxyReadMember {
			if m == name {
				return true
			}
		}
	}
	return false
}
