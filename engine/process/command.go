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
		Service: c.Service,
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
	if cfg.Restart.Policy == engine.RestartNo {
		cfg.Restart.MaxRetries = 0
	}
	if err := cfg.BindPort(c.Port); err != nil {
		return nil, fmt.Errorf("%s: %w", c.Name, err)
	}
	return cfg, nil
}
