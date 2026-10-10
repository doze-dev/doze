package runtime

import (
	"context"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/journal"
	"github.com/doze-dev/doze/internal/runspec"
	"github.com/doze-dev/doze/internal/watch"
)

// A command with `watch` is restarted when a matching file changes, for as long
// as it is awake. The files of the tasks it waits for count too: a restart runs
// those tasks first, and a task re-runs when its inputs changed, so editing a
// migration restarts the app after the migration has run.

// watchSets is what to watch for an instance: its own globs, and the inputs of
// every task it waits for, through its dependencies.
func (r *Runtime) watchSets(decl *config.InstanceDecl, drv engine.Driver) []watch.Set {
	w, ok := drv.(runspec.Watcher)
	if !ok {
		return nil
	}
	own := w.Watches(r.instanceFor(decl, drv))
	if len(own) == 0 {
		return nil // an app that did not ask to be watched is not restarted
	}
	var sets []watch.Set
	for _, s := range own {
		sets = append(sets, watch.Set{Dir: s.Dir, Globs: s.Globs})
	}
	seen := map[string]bool{}
	var walk func(d *config.InstanceDecl)
	walk = func(d *config.InstanceDecl) {
		for _, dep := range d.Deps {
			if seen[dep.Name] {
				continue
			}
			seen[dep.Name] = true
			dd := r.cfg.Lookup(dep.Name)
			if dd == nil {
				continue
			}
			if dd.Kind == config.KindTask {
				if drv, ok := engine.Lookup(dd.Type); ok {
					if ts, ok := r.taskOf(dd, drv); ok && len(ts.Inputs) > 0 {
						sets = append(sets, watch.Set{Dir: ts.Dir, Globs: ts.Inputs})
					}
				}
			}
			walk(dd)
		}
	}
	walk(decl)
	return sets
}

// startWatch begins watching for a running instance and records how to stop.
func (r *Runtime) startWatch(name string, decl *config.InstanceDecl, drv engine.Driver) {
	sets := r.watchSets(decl, drv)
	if len(sets) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	if old := r.watchers[name]; old != nil {
		old()
	}
	r.watchers[name] = cancel
	r.mu.Unlock()
	go watch.Poll(ctx, sets, watch.DefaultInterval, func(path string) {
		r.logf("%q: %s changed, restarting", name, path)
		cctx := journal.WithCause(context.Background(), journal.ByWatch, path)
		if err := r.Stop(cctx, name); err != nil {
			r.logf("restarting %q: %v", name, err)
			return
		}
		bctx, bcancel := context.WithTimeout(cctx, 5*time.Minute)
		defer bcancel()
		if _, err := r.Boot(bctx, name); err != nil {
			r.logf("restarting %q: %v", name, err)
		}
	})
}

func (r *Runtime) stopWatch(name string) {
	r.mu.Lock()
	cancel := r.watchers[name]
	delete(r.watchers, name)
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
