// Package actions is doze's single action registry: the one table of verbs a
// user can run against a stack. The CLI's cobra commands and the dashboard's
// keybindings and `:` command palette are projections of this table — an
// action declared here shows up in both surfaces (or deliberately, visibly, in
// one), so the two ways of driving doze cannot drift apart silently. A parity
// test in cmd/doze walks this registry and fails the build when a CLI-scoped
// action has no matching command.
//
// The registry holds facts (verb, arguments, destructiveness, the control-
// socket op it maps to); each surface keeps its own thin execution adapter —
// cobra RunE on the CLI, tea.Cmd plumbing in the dash.
package actions

import "strings"

// ArgKind says what a verb takes after its name.
type ArgKind int

const (
	// ArgNone — the verb takes no argument.
	ArgNone ArgKind = iota
	// ArgInstanceOptional — an optional instance name; empty means "all"/aggregate.
	ArgInstanceOptional
	// ArgInstanceRequired — exactly one instance name.
	ArgInstanceRequired
)

// Kind says how a verb executes.
type Kind int

const (
	// KindOp — one control-socket request: Request{Op: action.Op, DB: arg}.
	// (An empty arg on ArgInstanceOptional follows the op's own "all" meaning.)
	KindOp Kind = iota
	// KindLocal — surface-specific plumbing (exec a client shell, print env,
	// scaffold a config); the registry documents it, the surface implements it.
	KindLocal
)

// Action is one verb in the registry.
type Action struct {
	Name    string
	Aliases []string
	Summary string // one line; palette rows and help text render this
	Arg     ArgKind
	Confirm bool // destructive: interactive surfaces (the dash) confirm before
	// running; the CLI's equivalent is an explicit prompt or -y flag on its own
	// command (e.g. reset's), never a silent default.
	Kind Kind
	Op   string // control-socket op for KindOp ("" otherwise)
	// OpAcceptsAll — for KindOp + ArgInstanceOptional: the daemon op accepts an
	// empty DB as "all" (up/down/apply do). When false (boot), a surface
	// implements "all" by iterating the enabled instances client-side.
	OpAcceptsAll bool
	CLI          bool   // projected as a headless cobra command
	Dash         bool   // offered in the dash palette
	Key          string // dash keybinding when one exists ("" = palette-only)
}

// registry is ordered: palette suggestions and docs render in this order, most
// used first.
var registry = []Action{
	// Human lifecycle verbs live in the dash only — the CLI keeps just the
	// automation core (up/down/sync/status/env/run) and the before-the-dash
	// tools (init/lint/doctor).
	{Name: "wake", Aliases: []string{"boot"}, Summary: "wake a service now (with its dependencies); no arg wakes all",
		Arg: ArgInstanceOptional, Kind: KindOp, Op: "boot", CLI: false, Dash: true, Key: "w"},
	{Name: "sleep", Aliases: []string{"reap"}, Summary: "put a service to sleep (dependents first); data is kept; no arg sleeps all awake",
		Arg: ArgInstanceOptional, Kind: KindOp, Op: "down", OpAcceptsAll: true, Confirm: true, CLI: false, Dash: true, Key: "s"},
	{Name: "restart", Summary: "restart a service's backend",
		Arg: ArgInstanceRequired, Kind: KindOp, Op: "restart", Confirm: true, CLI: false, Dash: true, Key: "R"},
	// reset maps to the daemon's "reset" op (stop + wipe data dirs; the next
	// wake re-provisions and re-converges) — NOT "destroy", which is the sync
	// lifecycle's drop-the-declared-objects and leaves the data dir (and a
	// stale converged marker) behind.
	{Name: "reset", Summary: "wipe a service's data and re-provision fresh",
		Arg: ArgInstanceRequired, Kind: KindOp, Op: "reset", Confirm: true, CLI: false, Dash: true},
	{Name: "up", Summary: "converge structure and wake every enabled service",
		Arg: ArgInstanceOptional, Kind: KindOp, Op: "up", OpAcceptsAll: true, CLI: true, Dash: true},
	{Name: "sync", Aliases: []string{"apply"}, Summary: "reconcile a service with the config (create new, update changed, prune removed)",
		Arg: ArgInstanceOptional, Kind: KindOp, Op: "apply", OpAcceptsAll: true, CLI: true, Dash: true},
	{Name: "pin", Aliases: []string{"keepawake"}, Summary: "keep a service awake past its idle timeout",
		Arg: ArgInstanceRequired, Kind: KindOp, Op: "keepawake", CLI: false, Dash: true, Key: "p"},
	{Name: "logs", Summary: "show a service's logs; no arg aggregates all",
		Arg: ArgInstanceOptional, Kind: KindLocal, CLI: false, Dash: true},
	{Name: "env", Summary: "show the connection env vars (DATABASE_URL, AWS_ENDPOINT_URL_*, …)",
		Arg: ArgInstanceOptional, Kind: KindLocal, CLI: true, Dash: false},
	{Name: "url", Aliases: []string{"copy"}, Summary: "copy a service's connect line",
		Arg: ArgInstanceRequired, Kind: KindLocal, CLI: false, Dash: true, Key: "y"},

	// Headless-only verbs: the dash either IS the thing (status) or the verb
	// makes no sense inside a live view (down kills the daemon under it; init
	// and lint are editor-loop tools). Declared here so the registry reads as
	// the complete verb table and docs generate from one place.
	{Name: "status", Aliases: []string{"tree", "ls", "ps"}, Summary: "show every service: state, endpoint, resource use",
		Arg: ArgNone, Kind: KindLocal, CLI: true},
	{Name: "down", Summary: "sleep every service and stop the daemon",
		Arg: ArgNone, Kind: KindLocal, Confirm: false, CLI: true},
	{Name: "run", Summary: "ensure the daemon is up, then run a command",
		Arg: ArgNone, Kind: KindLocal, CLI: true},
	{Name: "lint", Summary: "validate the config without touching anything",
		Arg: ArgNone, Kind: KindLocal, CLI: true},
	{Name: "init", Summary: "scaffold a doze.hcl",
		Arg: ArgNone, Kind: KindLocal, CLI: true},
	{Name: "doctor", Summary: "diagnose the doze environment and configuration",
		Arg: ArgNone, Kind: KindLocal, CLI: true},
}

// All returns the registry in display order.
func All() []Action { return registry }

// Dash returns the palette-visible actions in display order.
func Dash() []Action {
	var out []Action
	for _, a := range registry {
		if a.Dash {
			out = append(out, a)
		}
	}
	return out
}

// Lookup resolves a verb or alias (case-insensitive) to its action.
func Lookup(verb string) (Action, bool) {
	v := strings.ToLower(verb)
	for _, a := range registry {
		if a.Name == v {
			return a, true
		}
		for _, al := range a.Aliases {
			if al == v {
				return a, true
			}
		}
	}
	return Action{}, false
}

// Match returns the dash actions whose name or aliases start with prefix —
// the palette's suggestion source. An empty prefix returns everything.
func Match(prefix string) []Action {
	p := strings.ToLower(prefix)
	var out []Action
	for _, a := range Dash() {
		if strings.HasPrefix(a.Name, p) {
			out = append(out, a)
			continue
		}
		for _, al := range a.Aliases {
			if strings.HasPrefix(al, p) {
				out = append(out, a)
				break
			}
		}
	}
	return out
}
