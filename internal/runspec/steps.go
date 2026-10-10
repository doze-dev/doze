package runspec

import "github.com/doze-dev/doze-sdk/engine"

// StepSet is the commands that surround a command's run, with where and how
// to run them. The core runs these itself, so that each one is a journal
// event and its output lands in the instance's log under a step tag.
type StepSet struct {
	Setup   []string // once per data folder
	OnReady []string // every wake, after ready
	Stop    []string // every sleep, instead of the default signals
	Dir     string
	Env     []string
}

// Stepper is implemented by an engine whose lifecycle steps the core runs.
type Stepper interface {
	// Steps returns the steps of an instance, and false when it has none to
	// run in this way (a database module).
	Steps(engine.Instance) (StepSet, bool)

	// TermFirst reports that the engine's commands are stopped with SIGTERM.
	TermFirst() bool
}
