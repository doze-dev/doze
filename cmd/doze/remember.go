package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/fault"
	"github.com/doze-dev/doze/internal/projects"
	"github.com/doze-dev/doze/internal/ui"
)

// A stack remembers what was started, so a bare `doze up` can offer the same
// set again (internal/projects). up records what it was asked for, plus what
// was already running; down adds what was awake. Neither shrinks the set: a
// service that slept while idle was still part of the stack.

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

// isEverything reports whether starting names starts every enabled instance.
func isEverything(cfg *config.Config, names []string) bool {
	return len(bootClosure(cfg, names)) == len(bootClosure(cfg, enabledNames(cfg)))
}

// declared keeps the names still declared and enabled, in their order.
func declared(cfg *config.Config, names []string) []string {
	var keep []string
	for _, n := range names {
		if d := cfg.Lookup(n); d != nil && d.Enabled {
			keep = append(keep, n)
		}
	}
	return keep
}

// lastSet is the set recorded last time, keeping only names still declared
// and enabled. It is nil when nothing was recorded, everything was started,
// or what it starts is everything anyway. A record that can't be read is an
// error, so --last can say so rather than claim nothing was started.
func lastSet(cfg *config.Config) ([]string, error) {
	p, ok, err := projects.Load(cfg.ProjectDir())
	if err != nil || !ok {
		return nil, err
	}
	keep := declared(cfg, p.LastSet)
	if len(keep) == 0 || isEverything(cfg, keep) {
		return nil, nil
	}
	return keep, nil
}

// chooseUpTargets decides what a bare `doze up` starts: everything, or last
// time's set when it was smaller. --last and --all decide without asking. On
// a terminal it asks, "as last time" the default, and -y answers that. With
// no terminal it starts everything and says how to get last time's set, -y or
// not: a script must get the same stack whatever someone ran before.
func chooseUpTargets(cfg *config.Config, last, all bool) ([]string, error) {
	everything := enabledNames(cfg)
	prev, err := lastSet(cfg)
	if err != nil {
		warn("could not read last time's set (" + err.Error() + "): starting everything")
		return everything, nil
	}
	switch {
	case all:
		return everything, nil
	case last:
		if prev == nil {
			warn("nothing smaller was started last time: starting everything")
			return everything, nil
		}
		return prev, nil
	case prev == nil:
		return everything, nil
	case canPrompt() && assumeYes:
		warn("starting last time's set: " + strings.Join(prev, ", "))
		return prev, nil
	case canPrompt():
		fmt.Fprintf(os.Stderr, "Last time: %s. Start the same? %s ", strings.Join(prev, ", "), ui.ErrMuted("[Y/n, a = everything]"))
		yes, err := askYes(os.Stdin)
		if err != nil {
			return nil, err
		}
		if yes {
			return prev, nil
		}
		return everything, nil
	}
	warn("last time only " + strings.Join(prev, ", ") + " started; doze up --last starts just those")
	return everything, nil
}

// askYes reads one answer, yes unless it says otherwise. End of input is no,
// and Ctrl-C stops the command instead of counting as an answer.
func askYes(in io.Reader) (bool, error) {
	type answer struct {
		line string
		err  error
	}
	got := make(chan answer, 1)
	go func() {
		line, err := bufio.NewReader(in).ReadString('\n')
		got <- answer{line, err}
	}()
	select {
	case <-commandContext().Done():
		fmt.Fprintln(os.Stderr)
		return false, fault.With(fault.Interrupted, errors.New("interrupted"))
	case a := <-got:
		if a.err != nil && strings.TrimSpace(a.line) == "" {
			return false, nil
		}
		switch strings.ToLower(strings.TrimSpace(a.line)) {
		case "", "y", "yes":
			return true, nil
		}
		return false, nil
	}
}

// awakeNames is what a snapshot shows running, in any state but asleep.
func awakeNames(snap *control.Snapshot) []string {
	if snap == nil {
		return nil
	}
	var out []string
	for _, v := range snap.Instances {
		switch v.State {
		case "", "reaped", "asleep", "disabled", "done":
		default:
			out = append(out, v.Name)
		}
	}
	return out
}

// rootsOf reduces names to the ones no other of them needs, in declaration
// order: what to ask for to start them all again.
func rootsOf(cfg *config.Config, names []string) []string {
	in := map[string]bool{}
	for _, n := range names {
		in[n] = true
	}
	needed := map[string]bool{}
	for n := range in {
		if d := cfg.Lookup(n); d != nil {
			for _, dep := range d.Deps {
				if in[dep.Name] {
					needed[dep.Name] = true
				}
			}
		}
	}
	var roots []string
	for _, n := range enabledNames(cfg) {
		if in[n] && !needed[n] {
			roots = append(roots, n)
		}
	}
	return roots
}

// rememberUp records what up was asked to start, joined with what was
// already running (snap), so adding a service to a running stack keeps the
// rest.
func rememberUp(cfg *config.Config, names []string, snap *control.Snapshot) {
	set := rootsOf(cfg, append(append([]string{}, names...), awakeNames(snap)...))
	if isEverything(cfg, set) {
		set = nil
	}
	remember(cfg, func(p *projects.Project) { p.LastUp, p.LastSet = time.Now(), set })
}

// rememberDown adds what was awake when down ran to what up recorded: a
// service woken since counts, and one that slept while idle is not dropped.
func rememberDown(cfg *config.Config, snap *control.Snapshot) {
	remember(cfg, func(p *projects.Project) {
		p.LastDown = time.Now()
		awake := awakeNames(snap)
		if len(awake) == 0 || len(p.LastSet) == 0 {
			// Nothing to add, or everything was started: keep what up recorded.
			return
		}
		set := rootsOf(cfg, append(declared(cfg, p.LastSet), awake...))
		if isEverything(cfg, set) {
			set = nil
		}
		p.LastSet = set
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
		warn("could not remember this run: " + err.Error())
	}
}
