package runspec

import "time"

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
	// Err is why the task cannot run at all (an env_file that is not
	// there); the run fails with it before anything starts.
	Err error
}

// Tasker is implemented by an engine that can run an instance as a task.
type Tasker interface {
	// Task returns the task an instance is, and false when it is not one.
	Task(engine.Instance) (TaskSpec, bool)
}

// WatchSet is a directory and the globs in it whose changes restart a command.
type WatchSet struct {
	Dir   string
	Globs []string
}

// Watcher is implemented by an engine whose commands restart on file changes.
type Watcher interface {
	Watches(engine.Instance) []WatchSet
}

// Downloader is implemented by an engine whose commands may be fetched from a
// URL before they first run.
type Downloader interface {
	// Download returns the URL to fetch for this machine, if the instance has
	// one. An error means it declares downloads but none for this platform.
	Download(engine.Instance) (url string, ok bool, err error)

	// SetDownloadRoot says where the fetched archive was unpacked: the
	// directory the command runs in unless its block set `dir`.
	SetDownloadRoot(engine.Instance, string)
}

// Defaults are a stack's defaults for every command it runs: what a block
// takes when it does not say for itself.
type Defaults struct {
	ReadyTimeout      time.Duration
	RestartPolicy     string // "" leaves the engine's own
	RestartBackoff    time.Duration
	RestartMaxRetries int
	Env               map[string]string
}

// Defaulter is implemented by an engine config that takes the stack's
// defaults. set says whether the block wrote an argument itself (a block's
// own word wins; for env, key by key).
type Defaulter interface {
	ApplyDefaults(d Defaults, set func(arg string) bool)
}
