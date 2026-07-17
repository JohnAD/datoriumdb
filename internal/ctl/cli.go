// Package ctl implements the datoriumctl administrative command-line tool
// behavior described in tech-docs/COMMAND-LINE-TOOLS.md. cmd/datoriumctl is
// a thin wrapper around Run so the CLI logic is unit-testable.
package ctl

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const defaultConfigDir = "/db/.config"

// Run parses args (excluding the program name), executes the requested
// command, writes output to stdout/stderr, and returns the process exit
// code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	configDir, args, _ := extractFlag(args, "--config-dir")
	if configDir == "" {
		configDir = defaultConfigDir
	}
	dataDir, args, hasDataDir := extractFlag(args, "--data-dir")
	if !hasDataDir || dataDir == "" {
		if filepath.Base(configDir) == ".config" {
			dataDir = filepath.Dir(configDir)
		} else {
			dataDir = "/db"
		}
	}
	dryRun, args := extractBoolFlag(args, "--dry-run")
	asJSON, args := extractBoolFlag(args, "--json")
	quiet, args := extractBoolFlag(args, "--quiet")
	yes, args := extractBoolFlag(args, "--yes")

	ctx := &Context{
		ConfigDir: configDir,
		DataDir:   dataDir,
		DryRun:    dryRun,
		JSON:      asJSON,
		Quiet:     quiet,
		Yes:       yes,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
	}

	if len(args) == 0 {
		fmt.Fprint(stderr, usage())
		return ExitValidation
	}

	outcome := dispatch(ctx, args)
	return ctx.Emit(outcome)
}

func dispatch(ctx *Context, args []string) Outcome {
	command := args[0]
	rest := args[1:]
	switch command {
	case "config":
		return dispatchSub(ctx, rest, "config", map[string]func(*Context, []string) Outcome{
			"validate": cmdConfigValidate,
			"show":     cmdConfigShow,
		})
	case "collection":
		return dispatchSub(ctx, rest, "collection", map[string]func(*Context, []string) Outcome{
			"create":  cmdCollectionCreate,
			"upgrade": cmdCollectionUpgrade,
			"list":    cmdCollectionList,
			"show":    cmdCollectionShow,
		})
	case "server":
		return dispatchSub(ctx, rest, "server", map[string]func(*Context, []string) Outcome{
			"list":   cmdServerList,
			"set":    cmdServerSet,
			"remove": cmdServerRemove,
		})
	case "shard-map":
		return dispatchSub(ctx, rest, "shard-map", map[string]func(*Context, []string) Outcome{
			"set":  cmdShardMapSet,
			"show": cmdShardMapShow,
		})
	case "general":
		return dispatchSub(ctx, rest, "general", map[string]func(*Context, []string) Outcome{
			"set": cmdGeneralSet,
		})
	case "auth":
		return dispatchSub(ctx, rest, "auth", map[string]func(*Context, []string) Outcome{
			"show":  cmdAuthShow,
			"set":   cmdAuthSet,
			"key":   nil, // handled specially below for its own subcommands
			"token": nil,
		})
	case "search":
		return dispatchSub(ctx, rest, "search", map[string]func(*Context, []string) Outcome{
			"create": cmdSearchCreate,
			"delete": cmdSearchDelete,
			"list":   cmdSearchList,
		})
	case "help", "--help", "-h":
		fmt.Fprint(ctx.Stdout, usage())
		return Outcome{Result: map[string]any{"ok": true}, Code: ExitOK, Human: ""}
	default:
		return SimpleValidationError("unknown", "unknownCommand", fmt.Sprintf("unknown command %q", command))
	}
}

func dispatchSub(ctx *Context, args []string, group string, handlers map[string]func(*Context, []string) Outcome) Outcome {
	if group == "auth" {
		return dispatchAuth(ctx, args)
	}
	if len(args) == 0 {
		return SimpleValidationError(group, "missingSubcommand", fmt.Sprintf("%s requires a subcommand", group))
	}
	sub := args[0]
	handler, ok := handlers[sub]
	if !ok || handler == nil {
		return SimpleValidationError(group+"."+sub, "unknownCommand", fmt.Sprintf("unknown %s subcommand %q", group, sub))
	}
	return handler(ctx, args[1:])
}

func dispatchAuth(ctx *Context, args []string) Outcome {
	if len(args) == 0 {
		return SimpleValidationError("auth", "missingSubcommand", "auth requires a subcommand")
	}
	switch args[0] {
	case "show":
		return cmdAuthShow(ctx, args[1:])
	case "set":
		return cmdAuthSet(ctx, args[1:])
	case "key":
		if len(args) < 2 {
			return SimpleValidationError("auth.key", "missingSubcommand", "auth key requires a subcommand (add|retire)")
		}
		switch args[1] {
		case "add":
			return cmdAuthKeyAdd(ctx, args[2:])
		case "retire":
			return cmdAuthKeyRetire(ctx, args[2:])
		default:
			return SimpleValidationError("auth.key", "unknownCommand", fmt.Sprintf("unknown auth key subcommand %q", args[1]))
		}
	case "token":
		if len(args) < 2 || args[1] != "issue" {
			return SimpleValidationError("auth.token", "unknownCommand", "unknown auth token subcommand")
		}
		return cmdAuthTokenIssue(ctx, args[2:])
	default:
		return SimpleValidationError("auth", "unknownCommand", fmt.Sprintf("unknown auth subcommand %q", args[0]))
	}
}

func usage() string {
	lines := []string{
		"usage: datoriumctl <command> [subcommand] [args...] [options...]",
		"",
		"commands:",
		"  config validate|show",
		"  collection create|upgrade|list|show",
		"  server list|set|remove",
		"  shard-map set|show",
		"  general set",
		"  auth show|set|key add|key retire|token issue",
		"  search create|delete|list",
		"",
		"global options: --config-dir <path> --data-dir <path> --dry-run --json --quiet --yes",
	}
	return strings.Join(lines, "\n") + "\n"
}
