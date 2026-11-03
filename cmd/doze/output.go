package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/fault"
	"github.com/doze-dev/doze/internal/ui"
)

// The output contract, for scripts and agents:
//
//   - stdout carries what a command produced; stderr carries progress,
//     warnings and errors.
//   - --json (or DOZE_JSON=1) makes stdout one envelope (below), on success
//     and on failure alike, for every command that has a result to give.
//     A command without one refuses --json rather than printing text.
//   - -q prints nothing on stdout: the exit code is the answer.
//   - -y (or DOZE_YES=1) answers yes to every question; with no terminal and
//     no -y, a command that must ask exits 9 instead.

// Global flags, set by the root command.
var (
	jsonMode  bool
	quiet     bool
	noColor   bool
	assumeYes bool
	// bootBudget caps how long one instance may take to come up (--timeout).
	// A process that never answers its readiness probe fails within this
	// instead of hanging the command.
	bootBudget = 5 * time.Minute
)

// jsonKey marks a command that has a JSON result (see jsonCapable).
const jsonKey = "doze/json"

// jsonCapable marks cmd as having a result for --json.
func jsonCapable(cmd *cobra.Command) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[jsonKey] = "true"
	return cmd
}

// The command's result, kept until main writes the envelope.
var (
	// stdout is the real standard output. In JSON and quiet modes os.Stdout is
	// swapped for /dev/null, so a command's text lines can't reach a program
	// reading the envelope; the envelope itself goes here.
	stdout     io.Writer = os.Stdout
	result     any
	warnings   []string
	ranCommand string // the command path, e.g. "doze status"
)

// setResult records what the command produced. In text mode it is unused.
func setResult(v any) { result = v }

// streamed is set by a command that wrote an NDJSON stream instead of a
// result (logs -f): its lines, ending with an "end" line, are the output, and
// main writes no envelope after them.
var streamed bool

// writeStream writes one NDJSON line to the real stdout: {"type": typ, …the
// fields of v}. The last line of a stream has type "end".
func writeStream(typ string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	// The type goes first, then the value's own fields.
	line := append([]byte(`{"type":`+fmt.Sprintf("%q", typ)), ',')
	if string(b) == "{}" || string(b) == "null" {
		line = append(line[:len(line)-1], '}')
	} else {
		line = append(line, b[1:]...)
	}
	_, err = stdout.Write(append(line, '\n'))
	return err
}

// endStream ends an NDJSON stream with how it ended.
func endStream(err error) {
	streamed = true
	end := struct {
		OK    bool       `json:"ok"`
		Error *errorBody `json:"error,omitempty"`
	}{OK: err == nil}
	if err != nil {
		end.Error = &errorBody{Code: fault.Of(err), Message: err.Error(), Exit: codeOf(err)}
	}
	_ = writeStream("end", end)
}

// envelope is --json's one shape for every command.
type envelope struct {
	Schema   string     `json:"schema"` // "doze/v1"
	Command  string     `json:"command"`
	OK       bool       `json:"ok"`
	Data     any        `json:"data,omitempty"`
	Warnings []string   `json:"warnings,omitempty"`
	Error    *errorBody `json:"error,omitempty"`
}

type errorBody struct {
	Code    fault.Kind `json:"code"`
	Message string     `json:"message"`
	Exit    int        `json:"exit"`
}

// writeEnvelope prints the command's result, or its error, as the envelope.
func writeEnvelope(w io.Writer, err error) {
	env := envelope{Schema: "doze/v1", Command: ranCommand, OK: err == nil, Data: result, Warnings: warnings}
	if err != nil {
		env.Error = &errorBody{Code: fault.Of(err), Message: err.Error(), Exit: codeOf(err)}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(env)
}

// addOutputFlags registers the global output flags on root.
func addOutputFlags(root *cobra.Command) {
	f := root.PersistentFlags()
	f.BoolVar(&jsonMode, "json", false, "print the result as JSON (one envelope; DOZE_JSON=1)")
	f.BoolVarP(&quiet, "quiet", "q", false, "print nothing on stdout; the exit code is the answer")
	f.BoolVar(&noColor, "no-color", false, "no colour (NO_COLOR=1)")
	f.BoolVarP(&assumeYes, "yes", "y", false, "answer yes to every question (DOZE_YES=1)")
	f.DurationVar(&bootBudget, "timeout", bootBudget, "how long one service may take to become ready")
}

// applyOutputFlags runs before every command: environment equivalents, the
// combinations that make no sense, and the side effects of each flag.
func applyOutputFlags(cmd *cobra.Command) error {
	ranCommand = cmd.CommandPath()
	// DOZE_JSON=1 asks for JSON wherever there is a result to give; it never
	// breaks a command that has none (run, logs, the dash). Only an explicit
	// --json is refused there.
	if !cmd.Flags().Changed("json") && os.Getenv("DOZE_JSON") == "1" && cmd.Annotations[jsonKey] == "true" {
		jsonMode = true
	}
	if !cmd.Flags().Changed("yes") && os.Getenv("DOZE_YES") == "1" {
		assumeYes = true
	}
	if noColor {
		ui.DisableColor()
	}
	if jsonMode && cmd.Annotations[jsonKey] != "true" {
		return usageError(fmt.Errorf("%s has no JSON result; drop --json", ranCommand))
	}
	if jsonMode && quiet {
		return usageError(fmt.Errorf("--json and -q ask for opposite things; pick one"))
	}
	if bootBudget <= 0 {
		return usageError(fmt.Errorf("--timeout must be positive"))
	}
	if quiet || jsonMode {
		devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		os.Stdout = devnull
	}
	return nil
}

// canPrompt reports whether a command may ask a question: only on a terminal,
// and never when its output is JSON for a program.
func canPrompt() bool { return !jsonMode && isInteractive() }
