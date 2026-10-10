package process

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

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
		Download: c.Download, dirSet: c.DirSet,
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
		Dir: cfg.workDir(), Env: cfg.envFor(inst),
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
	return runspec.TaskSpec{Run: cfg.Run, Dir: cfg.workDir(), Env: cfg.envFor(inst), Inputs: cfg.Inputs, Manual: cfg.Manual}, true
}

var _ runspec.Watcher = Driver{}

// Watches implements runspec.Watcher: the globs of `watch`, in the app's dir.
func (Driver) Watches(inst engine.Instance) []runspec.WatchSet {
	cfg, ok := inst.Spec.(*Config)
	if !ok || len(cfg.Watch) == 0 {
		return nil
	}
	return []runspec.WatchSet{{Dir: cfg.Dir, Globs: cfg.Watch}}
}

// workDir is where the command runs: the unpacked download, unless the block
// named a directory.
func (c *Config) workDir() string {
	if c.root != "" && !c.dirSet {
		return c.root
	}
	return c.Dir
}

var _ runspec.Downloader = Driver{}

// platformKey is the key of the map form of `download` for this machine.
func platformKey() string {
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return "mac"
	case runtime.GOOS == "darwin":
		return "mac_intel"
	case runtime.GOARCH == "arm64":
		return "linux_arm"
	}
	return "linux"
}

// Download implements runspec.Downloader.
func (Driver) Download(inst engine.Instance) (string, bool, error) {
	cfg, ok := inst.Spec.(*Config)
	if !ok || len(cfg.Download) == 0 {
		return "", false, nil
	}
	if u, ok := cfg.Download[""]; ok {
		return u, true, nil
	}
	if u, ok := cfg.Download[platformKey()]; ok {
		return u, true, nil
	}
	var have []string
	for k := range cfg.Download {
		have = append(have, k)
	}
	sort.Strings(have)
	return "", false, fmt.Errorf("download has no URL for this machine (%s); it has %s", platformKey(), strings.Join(have, ", "))
}

// SetDownloadRoot implements runspec.Downloader.
func (Driver) SetDownloadRoot(inst engine.Instance, dir string) {
	if cfg, ok := inst.Spec.(*Config); ok {
		cfg.root = dir
	}
}
