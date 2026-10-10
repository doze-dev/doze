package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/journal"
	"github.com/doze-dev/doze/internal/logstore"
	"github.com/doze-dev/doze/internal/runspec"
)

// The steps around a command's run (lifecycle.md). Each is a journal event
// with how long it took, and its output goes to the instance's log under a
// step tag, so "what did setup print" is a question the logs can answer.

// setupMarker is the file in the data folder that says setup has been done, and
// holds a hash of the commands it was done for. Setup runs when the file is
// missing — a new data folder, or `doze reset` — or when the list changed.
const setupMarker = ".doze-setup"

// stopGrace is how long a command gets to exit after its `stop` steps have
// run, before the signals are sent.
const stopGrace = 10 * time.Second

func stepsFor(drv engine.Driver, inst engine.Instance) (runspec.StepSet, runspec.Stepper, bool) {
	st, ok := drv.(runspec.Stepper)
	if !ok {
		return runspec.StepSet{}, nil, false
	}
	set, ok := st.Steps(inst)
	return set, st, ok
}

func setupHash(lines []string) string {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// setupNeeded reports whether the setup steps have to run for this data folder.
func setupNeeded(dataDir string, lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	have, err := os.ReadFile(filepath.Join(dataDir, setupMarker))
	return err != nil || strings.TrimSpace(string(have)) != setupHash(lines)
}

func markSetupDone(dataDir string, lines []string) error {
	return os.WriteFile(filepath.Join(dataDir, setupMarker), []byte(setupHash(lines)+"\n"), 0o644)
}

// runSteps runs lines one after another, in the set's directory and
// environment, stopping at the first failure. kind is the moment: "setup",
// "on_ready" or "stop".
func (r *Runtime) runSteps(ctx context.Context, name, kind string, lines []string, set runspec.StepSet, run *logstore.Run) error {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", line)
		cmd.Dir, cmd.Env = set.Dir, set.Env
		if run != nil {
			w := run.Writer(logstore.StepPrefix + kind)
			cmd.Stdout, cmd.Stderr = w, w
		}
		started := time.Now()
		err := cmd.Run()
		detail := kind + ": " + line
		if err != nil {
			detail += " — " + err.Error()
		}
		r.journal.Append(journal.Event{Kind: journal.Step, Instance: name, Took: time.Since(started), Detail: detail})
		if err != nil {
			return fmt.Errorf("%s step %q failed: %w", kind, line, err)
		}
	}
	return nil
}

// runStopSteps runs a command's `stop` steps in place of the signals. They are
// expected to make it exit — `redis-cli shutdown`, a drain endpoint — and the
// signals are the fallback when they fail or it does not.
func (r *Runtime) runStopSteps(ctx context.Context, name string, p engine.Process, run *logstore.Run) {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		return
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return
	}
	set, _, ok := stepsFor(drv, r.instanceFor(decl, drv))
	if !ok || len(set.Stop) == 0 {
		return
	}
	if err := r.runSteps(ctx, name, "stop", set.Stop, set, run); err != nil {
		r.logf("stop for %q failed, sending signals: %v", name, err)
		return
	}
	exited := make(chan struct{})
	go func() { _ = p.Wait(); close(exited) }()
	select {
	case <-exited:
	case <-time.After(stopGrace):
		r.logf("%q did not exit after its stop steps, sending signals", name)
	case <-ctx.Done():
	}
}

// rerunOnReady runs the on_ready steps of a command that is running but was
// left not ready by a failed step, and nothing else.
func (r *Runtime) rerunOnReady(ctx context.Context, name string) (engine.Endpoint, error) {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		return engine.Endpoint{}, fmt.Errorf("instance %q is not declared", name)
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return engine.Endpoint{}, fmt.Errorf("no driver for engine %q", decl.Type)
	}
	inst := r.instanceFor(decl, drv)
	set, _, _ := stepsFor(drv, inst)
	r.mu.Lock()
	run := r.runs[name]
	p := r.procs[name]
	r.mu.Unlock()
	started := time.Now()
	r.journal.Append(journal.Event{Kind: journal.Wake, Instance: name, Cause: journal.CauseOf(ctx), Detail: "on_ready again"})
	if err := r.runSteps(ctx, name, "on_ready", set.OnReady, set, run); err != nil {
		r.journal.Append(journal.Event{Kind: journal.Failed, Instance: name, Took: time.Since(started), Detail: err.Error()})
		r.reg.MarkFailed(name, err.Error())
		return engine.Endpoint{}, fmt.Errorf("on_ready for %q: %w", name, err)
	}
	r.mu.Lock()
	delete(r.readyPending, name)
	r.mu.Unlock()
	r.reg.MarkRunning(name, inst.SocketDir, inst.Port, p.PID())
	r.journal.Append(journal.Event{Kind: journal.Ready, Instance: name, Took: time.Since(started)})
	return inst.Endpoint, nil
}

// fetchDownload fetches a service's archive in a child process and returns
// where it was unpacked and its digest. The daemon never makes the request
// itself: an HTTPS client in this process would stay resident for as long as
// the daemon lives (see cmd/doze/fetch.go).
func (r *Runtime) fetchDownload(ctx context.Context, url, expected string) (dir, digest string, err error) {
	self, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	cmd := exec.CommandContext(ctx, self, "__fetch", "--home", r.cfg.Home, "--url", url, "--sha", expected)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	runErr := cmd.Run()
	for _, line := range strings.Split(strings.TrimSpace(errOut.String()), "\n") {
		if strings.HasPrefix(line, "downloading ") || strings.HasPrefix(line, "cached ") {
			r.logf("%s", line)
		}
	}
	if runErr != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return "", "", errors.New(strings.TrimPrefix(msg, "error: "))
	}
	var got struct{ Dir, Digest string }
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		return "", "", fmt.Errorf("fetching %s: unreadable answer %q", url, out.String())
	}
	return got.Dir, got.Digest, nil
}
