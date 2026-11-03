package main

import (
	"errors"
	"fmt"

	"github.com/doze-dev/doze/internal/fault"
)

// exitCodes is what a script can branch on: every code doze exits with, by the
// kind of error behind it. `doze run` exits with its command's own code
// instead (128+signal when a signal killed it). Codes are added, never
// renumbered.
var exitCodes = []struct {
	code    int
	kind    fault.Kind
	meaning string
}{
	{0, "", "it worked"},
	{1, fault.Failed, "it failed, for a reason with no code of its own"},
	{2, fault.Usage, "the command line is wrong: an unknown command or flag, a missing argument"},
	{4, fault.NotFound, "the stack declares no instance or task by that name"},
	{5, fault.Invalid, "the config doesn't load: an error in it, or a module it names won't fetch or start"},
	{6, fault.NoConfig, "there is no doze.hcl here or in any parent directory"},
	{7, fault.DaemonDown, "the command needs a running stack and doze isn't running"},
	{8, fault.Timeout, "it took longer than allowed"},
	{9, fault.NeedConfirm, "it needs a yes and there is no terminal to ask: pass the flag the message names"},
	{130, fault.Interrupted, "it was interrupted (Ctrl-C or SIGTERM)"},
}

// codeOf is the exit code for err.
func codeOf(err error) int {
	if err == nil {
		return 0
	}
	var child exitCodeError
	if errors.As(err, &child) {
		return int(child)
	}
	kind := fault.Of(err)
	for _, c := range exitCodes {
		if c.kind == kind {
			return c.code
		}
	}
	return 1
}

// exitCodeError carries a child process's exit code up to main without
// bypassing deferred cleanup the way a raw os.Exit would.
type exitCodeError int

func (e exitCodeError) Error() string { return fmt.Sprintf("exit code %d", int(e)) }

// shownError is an error the command has already shown in its own words:
// main exits with its code and prints nothing more.
type shownError struct{ err error }

func (e shownError) Error() string { return e.err.Error() }
func (e shownError) Unwrap() error { return e.err }

// shown marks err as already reported to the user.
func shown(err error) error {
	if err == nil {
		return nil
	}
	return shownError{err}
}

// usageError marks a command-line mistake (exit 2).
func usageError(err error) error { return fault.With(fault.Usage, err) }
