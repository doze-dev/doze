// Package fault gives an error a kind that survives wrapping and the control
// protocol, so the CLI can turn it into an exit code and a JSON error code
// without matching message text.
package fault

import (
	"context"
	"errors"
)

// Kind is what went wrong, as a stable word a script can match. The CLI maps
// each to an exit code (cmd/doze/exitcode.go).
type Kind string

const (
	Failed      Kind = "failed"    // anything without a better kind
	Usage       Kind = "usage"     // bad arguments or flags
	NotFound    Kind = "not_found" // a name the stack doesn't declare
	Invalid     Kind = "config_invalid"
	NoConfig    Kind = "no_config"   // no doze.hcl here or above
	DaemonDown  Kind = "daemon_down" // the command needs a running stack
	Timeout     Kind = "timeout"     // a deadline passed first
	NeedConfirm Kind = "needs_confirmation"
	Interrupted Kind = "interrupted" // Ctrl-C or SIGTERM
)

type kinded struct {
	kind Kind
	err  error
}

func (e *kinded) Error() string { return e.err.Error() }
func (e *kinded) Unwrap() error { return e.err }

// With gives err a kind; a nil err stays nil.
func With(kind Kind, err error) error {
	if err == nil {
		return nil
	}
	return &kinded{kind: kind, err: err}
}

// Of is err's kind: the outermost one given with With, else Timeout or
// Interrupted for an expired or cancelled context, else Failed. A nil err has
// no kind ("").
func Of(err error) Kind {
	if err == nil {
		return ""
	}
	var k *kinded
	if errors.As(err, &k) {
		return k.kind
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return Timeout
	case errors.Is(err, context.Canceled):
		return Interrupted
	}
	return Failed
}
