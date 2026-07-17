package ctl

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
)

// confirm prompts the user for a yes/no confirmation before a destructive
// operation, unless --yes was passed. It returns true when the operation
// should proceed.
func confirm(ctx *Context, prompt string) bool {
	if ctx.Yes {
		return true
	}
	if ctx.Stdin == nil {
		return false
	}
	fmt.Fprintf(ctx.Stderr, "%s [y/N]: ", prompt)
	line, _ := bufio.NewReader(ctx.Stdin).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func isMissingFileErr(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// loadReadOnly loads the config directory without full validation, for
// informational commands. It reports filesystem errors as runtime failures
// and malformed JSON as validation failures.
func loadReadOnly(ctx *Context, command string) (*config.Config, Outcome, bool) {
	cfg, err := config.LoadUnvalidated(ctx.ConfigDir)
	if err != nil {
		if isMissingFileErr(err) {
			return nil, RuntimeFail(map[string]any{"command": command}, envelope.Error{Code: "filesystemError", Message: err.Error()}), false
		}
		return nil, ValidationFail(map[string]any{"command": command}, envelope.Error{Code: "invalidJSON", Message: err.Error()}), false
	}
	return cfg, Outcome{}, true
}

// mutateFunc applies an in-memory config change and returns the write plan,
// success fields, and any validation errors found while validating the
// complete candidate config. A non-empty errs means nothing gets written.
type mutateFunc func(cfg *config.Config) (plan *Plan, fields map[string]any, errs []envelope.Error)

// runMutation implements the standard COMMAND-LINE-TOOLS.md mutating
// command lifecycle: acquire the exclusive config lock, load the current
// config, apply the change in memory, validate the candidate config, then
// either report the dry-run plan or commit it atomically.
func runMutation(ctx *Context, command string, mutate mutateFunc) Outcome {
	lock, err := AcquireLock(ctx.ConfigDir)
	if err != nil {
		var heldErr *LockHeldError
		if errors.As(err, &heldErr) {
			return RuntimeFail(map[string]any{"command": command}, envelope.Error{Code: "configLockHeld", Message: err.Error()})
		}
		return RuntimeFail(map[string]any{"command": command}, envelope.Error{Code: "filesystemError", Message: err.Error()})
	}
	defer lock.Release()

	cfg, err := config.LoadUnvalidated(ctx.ConfigDir)
	if err != nil {
		if isMissingFileErr(err) {
			return RuntimeFail(map[string]any{"command": command}, envelope.Error{Code: "filesystemError", Message: err.Error()})
		}
		return ValidationFail(map[string]any{"command": command}, envelope.Error{Code: "invalidJSON", Message: err.Error()})
	}

	plan, fields, errs := mutate(cfg)
	if fields == nil {
		fields = map[string]any{}
	}
	fields["command"] = command
	if len(errs) > 0 {
		return ValidationFail(fields, errs...)
	}
	if plan == nil {
		return OK(fields)
	}
	if ctx.DryRun {
		if names := plan.FilesWritten(); len(names) > 0 {
			fields["filesWritten"] = names
		}
		if names := plan.FilesRemoved(); len(names) > 0 {
			fields["filesRemoved"] = names
		}
		if len(plan.Dirs) > 0 {
			fields["directoriesCreated"] = plan.Dirs
		}
		return DryRunResult(fields)
	}
	if err := plan.Commit(); err != nil {
		return RuntimeFail(fields, envelope.Error{Code: "filesystemError", Message: err.Error()})
	}
	if names := plan.FilesWritten(); len(names) > 0 {
		fields["filesWritten"] = names
	}
	if names := plan.FilesRemoved(); len(names) > 0 {
		fields["filesRemoved"] = names
	}
	if len(plan.Dirs) > 0 {
		fields["directoriesCreated"] = plan.Dirs
	}
	return OK(fields)
}

// stageGeneralBump adds the bumped __general.json write to plan and returns
// the new version number.
func stageGeneralBump(plan *Plan, cfg *config.Config) (int, error) {
	next := cfg.General
	next.General.Version = cfg.General.General.Version + 1
	if err := plan.AddJSONWrite(generalPath(cfg), next); err != nil {
		return 0, err
	}
	return next.General.Version, nil
}

func generalPath(cfg *config.Config) string {
	return filepath.Join(cfg.Dir, "__general.json")
}

func serversPath(cfg *config.Config) string {
	return filepath.Join(cfg.Dir, "__servers.json")
}

func shardMapPath(cfg *config.Config) string {
	return filepath.Join(cfg.Dir, "__shard-map.json")
}

func authPath(cfg *config.Config) string {
	return filepath.Join(cfg.Dir, "__auth.json")
}

func schemaPath(cfg *config.Config, collection string) string {
	return filepath.Join(cfg.Dir, collection+".schema.json")
}

func schemaVersionPath(cfg *config.Config, collection string, ver int) string {
	return filepath.Join(cfg.Dir, collection+".schema."+strconv.Itoa(ver)+".json")
}

// schemaUpdatePath returns the persisted update-list file for the schema
// upgrade that produced version ver, so the change/upgrade agents can
// later replay the exact per-document migration steps
// (UPDATE-SCHEMA.md's add/import/remove/abandon/replace/move/copy/convert
// ops), not just the resulting before/after schema JSON.
func schemaUpdatePath(cfg *config.Config, collection string, ver int) string {
	return filepath.Join(cfg.Dir, collection+".schema."+strconv.Itoa(ver)+".update.json")
}

func searchPath(cfg *config.Config, collection, name string) string {
	return filepath.Join(cfg.Dir, collection+".search."+name+".json")
}

func searchVersionPath(cfg *config.Config, collection, name string, ver int) string {
	return filepath.Join(cfg.Dir, collection+".search."+name+"."+strconv.Itoa(ver)+".json")
}
