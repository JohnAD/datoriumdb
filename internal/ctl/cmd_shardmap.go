package ctl

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
)

func cmdShardMapSet(ctx *Context, args []string) Outcome {
	if len(args) < 1 {
		return SimpleValidationError("shard-map.set", "invalidArguments", "usage: shard-map set <shard-map-file.json>")
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return ValidationFailSimple("shard-map.set", "invalidJSON", fmt.Sprintf("cannot read shard map file: %v", err))
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return ValidationFailSimple("shard-map.set", "invalidJSON", err.Error())
	}
	var shardMapGeneric map[string]json.RawMessage
	if smRaw, ok := generic["shardMap"]; ok {
		if err := json.Unmarshal(smRaw, &shardMapGeneric); err != nil {
			return ValidationFailSimple("shard-map.set", "invalidJSON", err.Error())
		}
	}
	if _, hasCollections := shardMapGeneric["collections"]; hasCollections {
		return ValidationFailSimple("shard-map.set", "unsupportedShardMapFeature", "collection-specific shard map overrides are not part of the MVP")
	}
	var parsed config.ShardMapFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ValidationFailSimple("shard-map.set", "invalidJSON", err.Error())
	}

	mutate := func(cfg *config.Config) (*Plan, map[string]any, []envelope.Error) {
		fields := map[string]any{}
		if errs := config.ValidateShardMapBody(parsed.ShardMap, cfg.Servers.Servers); len(errs) > 0 {
			return nil, fields, errs
		}
		cfg.ShardMap = parsed
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}
		plan := &Plan{}
		if err := plan.AddJSONWrite(shardMapPath(cfg), cfg.ShardMap); err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		nextVersion, err := stageGeneralBump(plan, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields["generalVersion"] = nextVersion
		fields["ranges"] = len(cfg.ShardMap.ShardMap.Default)
		return plan, fields, nil
	}
	return runMutation(ctx, "shard-map.set", mutate)
}

func cmdShardMapShow(ctx *Context, _ []string) Outcome {
	cfg, outcome, ok := loadReadOnly(ctx, "shard-map.show")
	if !ok {
		return outcome
	}
	ranges := make([]string, 0, len(cfg.ShardMap.ShardMap.Default))
	for r := range cfg.ShardMap.ShardMap.Default {
		ranges = append(ranges, r)
	}
	sort.Strings(ranges)
	human := ""
	for _, r := range ranges {
		a := cfg.ShardMap.ShardMap.Default[r]
		human += fmt.Sprintf("%s\tSOT=%s\tREAD=%v\tPROXY=%v\n", r, a.ShardSOTMember, a.ShardReadMember, a.ProxyReadMember)
	}
	return OKHuman(map[string]any{"command": "shard-map.show", "default": cfg.ShardMap.ShardMap.Default}, human)
}
