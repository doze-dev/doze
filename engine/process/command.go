package process

import (
	"fmt"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/runspec"
)

var _ runspec.CommandBuilder = Driver{}

// CommandConfig implements runspec.CommandBuilder: a `service` block becomes
// the same Config an `app` block decodes to, with the service preset.
func (Driver) CommandConfig(c runspec.Command) (engine.EngineConfig, error) {
	cfg := &Config{
		Dir: c.Dir, Run: c.Run, Env: c.Env, EnvFile: c.EnvFile,
		Setup: c.Setup, OnReady: c.OnReady, Stop: c.Stop,
		Ready: c.Ready, ReadyTimeout: c.ReadyTimeout,
		Sleep: c.Sleep, Front: c.Front, Console: c.Console, QuietLogs: c.QuietLogs,
		Service: c.Service, Task: c.Task, Inputs: c.Inputs, Manual: c.Manual,
		Restart: Restart{Policy: engine.RestartOnFailure, Backoff: defaultBackoff, MaxRetries: defaultMaxRetries},
	}
	if c.RestartPolicy != "" {
		cfg.Restart.Policy = engine.RestartPolicy(c.RestartPolicy)
	}
	if c.RestartBackoff > 0 {
		cfg.Restart.Backoff = c.RestartBackoff
	}
	if c.RestartMaxRetries > 0 {
		cfg.Restart.MaxRetries = c.RestartMaxRetries
	}
	if cfg.Restart.Policy == engine.RestartNo || cfg.Task {
		cfg.Restart.MaxRetries = 0
	}
	if err := cfg.BindPort(c.Port); err != nil {
		return nil, fmt.Errorf("%s: %w", c.Name, err)
	}
	return cfg, nil
}

var _ runspec.Stepper = Driver{}

// Steps implements runspec.Stepper.
func (Driver) Steps(inst engine.Instance) (runspec.StepSet, bool) {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg.Task {
		return runspec.StepSet{}, false
	}
	return runspec.StepSet{
		Setup: cfg.Setup, OnReady: cfg.OnReady, Stop: cfg.Stop,
		Dir: cfg.Dir, Env: cfg.envFor(inst),
	}, true
}

// TermFirst implements runspec.Stepper: a command is asked to stop with
// SIGTERM, not the SIGINT a database engine is written against.
func (Driver) TermFirst() bool { return true }

var _ runspec.Tasker = Driver{}

// Task implements runspec.Tasker.
func (Driver) Task(inst engine.Instance) (runspec.TaskSpec, bool) {
	cfg, ok := inst.Spec.(*Config)
	if !ok || !cfg.Task {
		return runspec.TaskSpec{}, false
	}
	return runspec.TaskSpec{Run: cfg.Run, Dir: cfg.Dir, Env: cfg.envFor(inst), Inputs: cfg.Inputs, Manual: cfg.Manual}, true
}
