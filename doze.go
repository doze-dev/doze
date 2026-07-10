package doze

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/doze-dev/doze-sdk/binaries"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/daemonctl"
	"github.com/doze-dev/doze/internal/hostboot"
)

// Options configures a Session.
type Options struct {
	// ConfigPath is the doze config file. Empty searches upward for doze.hcl from
	// the working directory (the CLI's default).
	ConfigPath string
	// Vars are --var overrides applied to the config.
	Vars map[string]string
	// Home is the shared cache root (binaries + fetched modules). Empty uses
	// DOZE_HOME or ~/.doze.
	Home string
	// Logf receives engine/daemon progress. nil discards it.
	Logf func(string, ...any)
	// DozeBinary is the executable Attach spawns for the background daemon. Empty
	// uses this process's own executable (the normal case for the CLI; embedders
	// that aren't the doze binary must set it to a doze binary).
	DozeBinary string
	// NoSpawn makes Attach fail rather than start a daemon that isn't running.
	NoSpawn bool
}

// Session is a connection to a doze daemon for one config. Safe for concurrent
// use. Close it when done.
type Session struct {
	cfg    *config.Config
	client *control.Client
	host   *hostboot.Host
	logf   func(string, ...any)

	// served is set in Serve mode: the in-process daemon and its lifecycle.
	served      *daemon.Daemon
	serveCancel context.CancelFunc
	serveDone   chan error
}

// Attach connects to the background daemon for the config (spawning it unless
// NoSpawn), returning a Session whose lifecycle commands steer it. The stack
// keeps running after Close; use Shutdown to stop it.
func Attach(ctx context.Context, opts Options) (*Session, error) {
	s, err := newSession(opts)
	if err != nil {
		return nil, err
	}
	if !daemonctl.Running(s.cfg) {
		if opts.NoSpawn {
			s.host.Close()
			return nil, fmt.Errorf("doze daemon is not running for %s (NoSpawn set)", s.cfg.Path())
		}
		bin := opts.DozeBinary
		if bin == "" {
			if bin, err = os.Executable(); err != nil {
				s.host.Close()
				return nil, err
			}
		}
		abs, err := filepath.Abs(s.cfg.Path())
		if err != nil {
			s.host.Close()
			return nil, err
		}
		if err := daemonctl.Start(s.cfg, bin, abs); err != nil {
			s.host.Close()
			return nil, err
		}
	}
	return s, nil
}

// Serve runs the daemon in this process and returns a Session bound to it. It
// errors if a daemon is already running for the config. Close or Shutdown stops
// it; the stack does not outlive the process.
func Serve(ctx context.Context, opts Options) (*Session, error) {
	s, err := newSession(opts)
	if err != nil {
		return nil, err
	}
	if daemonctl.Running(s.cfg) {
		s.host.Close()
		return nil, fmt.Errorf("a doze daemon is already running for %s — use Attach", s.cfg.Path())
	}
	d, err := daemon.New(s.cfg, s.logf)
	if err != nil {
		s.host.Close()
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(runCtx) }()
	s.served, s.serveCancel, s.serveDone = d, cancel, done

	// Wait for the control socket to accept before returning, so the first
	// lifecycle call doesn't race the listener. Bail out promptly if the daemon
	// exits during startup (a bind failure) rather than spinning forever.
	readyCtx, readyCancel := context.WithTimeout(ctx, 15*time.Second)
	defer readyCancel()
	for {
		if s.client.Available() {
			return s, nil
		}
		select {
		case runErr := <-done:
			s.host.Close()
			if runErr != nil {
				return nil, fmt.Errorf("daemon exited during startup: %w", runErr)
			}
			return nil, fmt.Errorf("daemon exited during startup")
		case <-readyCtx.Done():
			cancel()
			<-done
			s.host.Close()
			return nil, fmt.Errorf("daemon did not come up: %w", readyCtx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// newSession initializes the engine host and loads the config, shared by both
// entry points.
func newSession(opts Options) (*Session, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	home := opts.Home
	if home == "" {
		home = defaultHome()
	}
	configPath := opts.ConfigPath

	host, err := hostboot.Init(hostboot.Options{
		Home: home,
		Logf: logf,
		LockPath: func() string {
			return filepath.Join(configDir(configPath), binaries.LockFileName)
		},
		// An embedder is not a mutating CLI command; don't rewrite doze.lock pins
		// out from under it.
		PersistLock: func() bool { return false },
	})
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadWithVars(configPath, opts.Vars)
	if err != nil {
		host.Close()
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no doze config found (looked for %q) — set Options.ConfigPath", firstNonEmpty(configPath, "doze.hcl"))
		}
		return nil, err
	}
	return &Session{
		cfg:    cfg,
		client: control.NewClient(daemon.ControlSocketPath(cfg)),
		host:   host,
		logf:   logf,
	}, nil
}

func defaultHome() string {
	if h := os.Getenv("DOZE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".doze")
}

func configDir(path string) string {
	if path == "" {
		return "."
	}
	return filepath.Dir(path)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
