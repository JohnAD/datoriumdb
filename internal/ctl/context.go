package ctl

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/JohnAD/datoriumdb/internal/envelope"
)

// Exit codes documented in COMMAND-LINE-TOOLS.md.
const (
	ExitOK             = 0
	ExitValidation     = 1
	ExitRuntime        = 2
	ExitDryRunComplete = 3
)

// Context carries global CLI options and I/O for one command invocation.
type Context struct {
	ConfigDir string
	DataDir   string
	DryRun    bool
	JSON      bool
	Quiet     bool
	Yes       bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Outcome is the result of running one command: the application envelope,
// the process exit code, and an optional pre-rendered human-readable body.
// When Human is empty, Emit falls back to a generic renderer.
type Outcome struct {
	Result envelope.Result
	Code   int
	Human  string
}

// OK builds a success outcome with exit code 0.
func OK(fields map[string]any) Outcome {
	return Outcome{Result: envelope.OK(fields), Code: ExitOK}
}

// OKHuman builds a success outcome with exit code 0 and custom human text.
func OKHuman(fields map[string]any, human string) Outcome {
	return Outcome{Result: envelope.OK(fields), Code: ExitOK, Human: human}
}

// DryRunResult builds a dry-run success outcome with exit code 3.
func DryRunResult(fields map[string]any) Outcome {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["dryRun"] = true
	return Outcome{Result: envelope.OK(fields), Code: ExitDryRunComplete}
}

// ValidationFail builds a validation/user-input failure outcome (exit 1).
func ValidationFail(fields map[string]any, errs ...envelope.Error) Outcome {
	return Outcome{Result: envelope.Fail(fields, errs...), Code: ExitValidation}
}

// RuntimeFail builds a filesystem/lock/unexpected failure outcome (exit 2).
func RuntimeFail(fields map[string]any, errs ...envelope.Error) Outcome {
	return Outcome{Result: envelope.Fail(fields, errs...), Code: ExitRuntime}
}

// SimpleValidationError is a convenience for a single ad-hoc error.
func SimpleValidationError(command, code, message string) Outcome {
	return ValidationFail(map[string]any{"command": command}, envelope.Error{Code: code, Message: message})
}

// ValidationFailSimple is an alias for SimpleValidationError.
func ValidationFailSimple(command, code, message string) Outcome {
	return SimpleValidationError(command, code, message)
}

// RuntimeFailSimple builds a single-error runtime failure outcome (exit 2).
func RuntimeFailSimple(command, code, message string) Outcome {
	return RuntimeFail(map[string]any{"command": command}, envelope.Error{Code: code, Message: message})
}

// Emit writes the outcome to Stdout/Stderr according to Context.JSON and
// Context.Quiet, and returns the process exit code.
func (c *Context) Emit(o Outcome) int {
	if c.JSON {
		data, err := json.MarshalIndent(o.Result, "", "  ")
		if err != nil {
			fmt.Fprintln(c.Stderr, err.Error())
			return ExitRuntime
		}
		fmt.Fprintln(c.Stdout, string(data))
		return o.Code
	}

	ok, _ := o.Result["ok"].(bool)
	if !ok {
		if errsList, ok := o.Result["errors"].([]envelope.Error); ok {
			for _, e := range errsList {
				if e.Path != "" {
					fmt.Fprintf(c.Stderr, "%s: %s (%s)\n", e.Code, e.Message, e.Path)
				} else {
					fmt.Fprintf(c.Stderr, "%s: %s\n", e.Code, e.Message)
				}
			}
		} else {
			fmt.Fprintln(c.Stderr, "command failed")
		}
		return o.Code
	}

	if c.Quiet {
		return o.Code
	}
	if o.Human != "" {
		fmt.Fprint(c.Stdout, o.Human)
		if len(o.Human) == 0 || o.Human[len(o.Human)-1] != '\n' {
			fmt.Fprintln(c.Stdout)
		}
		return o.Code
	}
	fmt.Fprintln(c.Stdout, "ok")
	for k, v := range o.Result {
		if k == "ok" {
			continue
		}
		fmt.Fprintf(c.Stdout, "%s: %v\n", k, v)
	}
	return o.Code
}
