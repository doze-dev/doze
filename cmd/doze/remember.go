package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/projects"
	"github.com/doze-dev/doze/internal/ui"
)

// A stack remembers what was started, so a bare `doze up` can offer the same
// set again (internal/projects). up records what it was asked for; down
// records the roots of what was awake, which also counts services woken since.

// enabledNames is every enabled instance, in declaration order.
func enabledNames(cfg *config.Config) []string {
	var all []string
	for _, d := range cfg.Instances {
		if d.Enabled {
			all = append(all, d.Name)
		}
	}
	return all
}

// lastSet is the set recorded last time, keeping only names still declared
// and enabled. It is nil when nothing was recorded, everything was started,
// or what it starts is everything anyway.
func lastSet(cfg *config.Config) []string {
	p, ok, err := projects.Load(cfg.ProjectDir())
	if err != nil || !ok {
		return nil
	}
	var keep []string
	for _, n := range p.LastSet {
		if d := cfg.Lookup(n); d != nil && d.Enabled {
			keep = append(keep, n)
		}
	}
	if len(keep) == 0 || len(bootClosure(cfg, keep)) == len(bootClosure(cfg, enabledNames(cfg))) {
		return nil
	}
	return keep
}

// chooseUpTargets decides what a bare `doze up` starts: everything, or last
// time's set when it was smaller. --last and --all decide without asking; on
// a terminal it asks, with "as last time" the default; anywhere else it
// starts everything and says how to get last time's set.
func chooseUpTargets(cfg *config.Config, last, all bool) ([]string, error) {
	everything := enabledNames(cfg)
	prev := lastSet(cfg)
	switch {
	case all:
		return everything, nil
	case last:
		if prev == nil {
			fmt.Fprintln(os.Stderr, ui.ErrMuted("nothing smaller was started last time: starting everything"))
			return everything, nil
		}
		return prev, nil
	case prev == nil:
		return everything, nil
	case assumeYes:
		return prev, nil
	case canPrompt():
		fmt.Fprintf(os.Stderr, "Last time: %s. Start the same? %s ", strings.Join(prev, ", "), ui.ErrMuted("[Y/n, a = everything]"))
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "y", "yes":
			return prev, nil
		}
		return everything, nil
	}
	fmt.Fprintln(os.Stderr, ui.ErrMuted("last time only "+strings.Join(prev, ", ")+" started; doze up --last starts just those"))
	return everything, nil
}

// rememberUp records what up was asked to start: names, or everything.
func rememberUp(cfg *config.Config, names []string) {
	if len(names) == len(enabledNames(cfg)) {
		names = nil
	}
	remember(cfg, func(p *projects.Project) { p.LastUp, p.LastSet = time.Now(), names })
}

// rememberDown records the roots of what was awake when down ran: the awake
// instances no other awake instance needs. A stack that wasn't running keeps
// what it had.
func rememberDown(cfg *config.Config, snap *control.Snapshot) {
	remember(cfg, func(p *projects.Project) {
		p.LastDown = time.Now()
		if snap == nil {
			return
		}
		awake := map[string]bool{}
		for _, v := range snap.Instances {
			if v.State == "active" || v.State == "idle" || v.State == "booting" {
				awake[v.Name] = true
			}
		}
		if len(awake) == 0 {
			return
		}
		needed := map[string]bool{}
		for n := range awake {
			if d := cfg.Lookup(n); d != nil {
				for _, dep := range d.Deps {
					if awake[dep.Name] {
						needed[dep.Name] = true
					}
				}
			}
		}
		var roots []string
		for _, n := range enabledNames(cfg) {
			if awake[n] && !needed[n] {
				roots = append(roots, n)
			}
		}
		if len(bootClosure(cfg, roots)) == len(bootClosure(cfg, enabledNames(cfg))) {
			roots = nil
		}
		p.LastSet = roots
	})
}

// remember updates the project's record. It is a convenience, so a failure
// is a warning, never the command's failure.
func remember(cfg *config.Config, fn func(*projects.Project)) {
	abs, _ := filepath.Abs(cfg.Path())
	err := projects.Update(cfg.ProjectDir(), func(p *projects.Project) {
		p.Config, p.Name = abs, cfg.Stack()
		fn(p)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.ErrMuted("doze: could not remember this run: "+err.Error()))
	}
}
