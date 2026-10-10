package runspec

import (
	"time"

	"github.com/doze-dev/doze-sdk/engine"
)

// Command is what a `service` block means to the engine that runs it: the
// attributes after config has decoded and checked them. The `app` block reaches
// the same engine through its own decoder; a service reaches it through here,
// so the two nouns are one engine with different presets (lifecycle.md).
type Command struct {
	Name    string
	Run     string
	Dir     string // absolute
	Port    int    // 0: no port
	Env     map[string]string
	EnvFile string // absolute, "" if none

	Setup, OnReady, Stop []string
	Ready                *Probe
	ReadyTimeout         time.Duration

	// Restart fields are as written; zero values take the engine's defaults.
	RestartPolicy     string
	RestartBackoff    time.Duration
	RestartMaxRetries int

	Sleep     Sleep
	Front     string
	Console   string
	QuietLogs bool

	// Service selects the service preset: woken by a connection or a
	// dependant, and put to sleep when idle.
	Service bool
}

// CommandBuilder is implemented by the engine behind `app` and `service`. The
// config package finds it by type assertion on the registered driver, so it
// does not import the engine.
type CommandBuilder interface {
	CommandConfig(Command) (engine.EngineConfig, error)
}
