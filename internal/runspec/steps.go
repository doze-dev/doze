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

// TaskSpec is what the core needs to run a command to completion.
type TaskSpec struct {
	Run    string
	Dir    string
	Env    []string
	Inputs []string
	Manual bool
}

// Tasker is implemented by an engine that can run an instance as a task.
type Tasker interface {
	// Task returns the task an instance is, and false when it is not one.
	Task(engine.Instance) (TaskSpec, bool)
}
