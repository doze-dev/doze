package runtime

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/inputs"
	"github.com/doze-dev/doze/internal/journal"
	"github.com/doze-dev/doze/internal/logstore"
	"github.com/doze-dev/doze/internal/runspec"
	"github.com/doze-dev/doze/internal/state"
	"github.com/doze-dev/doze/internal/supervisor"
)

// A task runs to completion and records how it went (lifecycle.md). It is an
// instance of the command engine like an app, so what it references is booted
// and held while it runs, its output is a log run, and its start and end are
// journal events; what differs is that its start is decided here.

func (r *Runtime) taskStatePath() string { return state.Path(r.cfg.Path()) }

// TaskRecord returns how a task last ran.
func (r *Runtime) TaskRecord(name string) (state.TaskRecord, bool) {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	st, err := state.Load(r.taskStatePath())
	if err != nil {
		return state.TaskRecord{}, false
	}
	rec, ok := st.Tasks[name]
	return rec, ok
}

func (r *Runtime) setTaskRecord(name string, rec *state.TaskRecord) {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	st, err := state.Load(r.taskStatePath())
	if err != nil {
		r.logf("cannot record task %q: %v", name, err)
		return
	}
	if rec == nil {
		delete(st.Tasks, name)
	} else {
		if st.Tasks == nil {
			st.Tasks = map[string]state.TaskRecord{}
		}
		st.Tasks[name] = *rec
	}
	if err := st.Save(r.taskStatePath()); err != nil {
		r.logf("cannot record task %q: %v", name, err)
	}
}

// taskOf returns the task an instance is.
func (r *Runtime) taskOf(decl *config.InstanceDecl, drv engine.Driver) (runspec.TaskSpec, bool) {
	t, ok := drv.(runspec.Tasker)
	if !ok {
		return runspec.TaskSpec{}, false
	}
	return t.Task(r.instanceFor(decl, drv))
}

// RunTask runs a task now, whatever its state: `doze task <name>`.
func (r *Runtime) RunTask(ctx context.Context, name string) error {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		return fmt.Errorf("task %q is not declared in %s", name, configName(r.cfg))
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return fmt.Errorf("no driver for engine %q", decl.Type)
	}
	ts, ok := r.taskOf(decl, drv)
	if !ok {
		return fmt.Errorf("%s is not a task", decl.Title())
	}
	_, err, _ := r.group.Do(name, func() (any, error) {
		return engine.Endpoint{}, r.runTask(ctx, name, decl, ts, true)
	})
	return err
}

// whyRun decides whether a task has to run, and says why.
func (r *Runtime) whyRun(ctx context.Context, name string, ts runspec.TaskSpec, hash string, force bool) (string, bool) {
	by := ""
	if c := journal.CauseOf(ctx); c.Kind == journal.ByDependency && c.By != "" {
		by = " (needed by " + c.By + ")"
	}
	if force {
		return "asked for", true
	}
	if ts.Manual {
		return "", false
	}
	rec, ok := r.TaskRecord(name)
	switch {
	case !ok:
		return "never ran" + by, true
	case rec.Result != "done":
		return "failed last time" + by, true
	case len(ts.Inputs) > 0 && rec.Inputs != hash:
		return "inputs changed" + by, true
	}
	return "", false
}

func (r *Runtime) runTask(ctx context.Context, name string, decl *config.InstanceDecl, ts runspec.TaskSpec, force bool) error {
	hash := ""
	if len(ts.Inputs) > 0 {
		h, _, err := inputs.Hash(ts.Dir, ts.Inputs)
		if err != nil {
			return fmt.Errorf("task %q: reading its inputs: %w", name, err)
		}
		hash = h
	}
	why, run := r.whyRun(ctx, name, ts, hash, force)
	if !run {
		return nil // up to date, or manual and not asked for
	}

	started := time.Now()
	r.reg.MarkBooting(name)
	r.journal.Append(journal.Event{Kind: journal.Wake, Instance: name, Cause: journal.CauseOf(ctx), Detail: why})
	r.logf("running task %q (%s)…", name, why)

	var held []string
	release := func() {
		for _, dn := range held {
			r.Release(dn)
		}
	}
	fail := func(err error, exit int) error {
		release()
		rec := state.TaskRecord{Result: "failed", ExitCode: exit, TookMS: time.Since(started).Milliseconds(), RanAt: started, Why: why, Inputs: hash}
		r.setTaskRecord(name, &rec)
		r.journal.Append(journal.Event{Kind: journal.Failed, Instance: name, Took: time.Since(started), Detail: err.Error()})
		r.reg.MarkFailed(name, err.Error())
		return err
	}

	if len(decl.Deps) > 0 {
		_, h, err := r.bootDeps(ctx, name, decl.Deps)
		if err != nil {
			return fail(err, 0)
		}
		held = h
	}

	lr, err := r.logs.Begin(name)
	if err != nil {
		return fail(fmt.Errorf("opening the log for %q: %w", name, err), 0)
	}
	defer lr.Close()
	cmd := exec.Command("sh", "-c", ts.Run)
	cmd.Dir, cmd.Env = ts.Dir, ts.Env
	p, err := supervisor.StartTree(cmd, supervisor.Sink{
		Stdout: lr.Writer(logstore.Stdout),
		Stderr: lr.Writer(logstore.Stderr),
		Tail:   func() []string { return r.recentLogs(name) },
		Term:   true,
	})
	if err != nil {
		return fail(fmt.Errorf("task %q: %w", name, err), 0)
	}
	waited := make(chan error, 1)
	go func() { waited <- p.Wait() }()
	var werr error
	select {
	case werr = <-waited:
	case <-ctx.Done():
		_ = p.Stop(context.Background())
		<-waited
		return fail(fmt.Errorf("task %q was stopped: %w", name, ctx.Err()), 0)
	}
	if werr != nil {
		code := -1
		var ee *exec.ExitError
		if errors.As(werr, &ee) {
			code = ee.ExitCode()
		}
		tail := r.recentLogs(name)
		if len(tail) > 10 {
			tail = tail[len(tail)-10:]
		}
		msg := fmt.Sprintf("task %q failed: exit %d", name, code)
		if len(tail) > 0 {
			msg += "\n" + strings.Join(tail, "\n")
		}
		return fail(errors.New(msg), code)
	}

	release()
	took := time.Since(started)
	rec := state.TaskRecord{Result: "done", TookMS: took.Milliseconds(), RanAt: started, Why: why, Inputs: hash}
	r.setTaskRecord(name, &rec)
	r.journal.Append(journal.Event{Kind: journal.Ready, Instance: name, Took: took, Detail: "exit 0"})
	r.reg.MarkDone(name)
	r.logf("task %q done in %s", name, took.Round(time.Millisecond))
	return nil
}

// forgetTask drops a task's record, so it runs again at the next trigger.
func (r *Runtime) forgetTask(name string) { r.setTaskRecord(name, nil) }
