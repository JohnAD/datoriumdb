package ctl

import (
	"fmt"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/shard"
)

func cmdConfigValidate(ctx *Context, _ []string) Outcome {
	cfg, err := config.LoadUnvalidated(ctx.ConfigDir)
	if err != nil {
		if isMissingFileErr(err) {
			return RuntimeFailSimple("config.validate", "filesystemError", err.Error())
		}
		return ValidationFailSimple("config.validate", "invalidJSON", err.Error())
	}
	errs := cfg.ValidateDetailed()
	fields := map[string]any{
		"command":        "config.validate",
		"generalVersion": cfg.General.General.Version,
	}
	if len(errs) > 0 {
		return ValidationFail(fields, errs...)
	}
	human := fmt.Sprintf("config is valid (general.version=%d)\n", cfg.General.General.Version)
	return OKHuman(fields, human)
}

func cmdConfigShow(ctx *Context, _ []string) Outcome {
	cfg, outcome, ok := loadReadOnly(ctx, "config.show")
	if !ok {
		return outcome
	}
	shardComplete := shardMapComplete(cfg)
	fields := map[string]any{
		"command":          "config.show",
		"name":             cfg.General.General.Name,
		"establishment":    cfg.General.General.EstablishmentServer,
		"generalVersion":   cfg.General.General.Version,
		"servers":          len(cfg.Servers.Servers),
		"collections":      len(cfg.Schemas),
		"shardMapComplete": shardComplete,
	}
	human := fmt.Sprintf(
		"database: %s\nestablishment server: %s\ngeneral.version: %d\nservers: %d\ncollections: %d\nshardMap.default complete: %v\n",
		cfg.General.General.Name, cfg.General.General.EstablishmentServer, cfg.General.General.Version,
		len(cfg.Servers.Servers), len(cfg.Schemas), shardComplete,
	)
	return OKHuman(fields, human)
}

func shardMapComplete(cfg *config.Config) bool {
	if cfg.ShardMap.ShardMap.Default == nil {
		return false
	}
	var ranges []shard.Range
	for raw := range cfg.ShardMap.ShardMap.Default {
		r, err := shard.ParseRange(raw)
		if err != nil {
			return false
		}
		ranges = append(ranges, r)
	}
	return shard.ValidateFullCoverage(ranges) == nil
}
