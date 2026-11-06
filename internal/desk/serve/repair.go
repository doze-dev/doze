package serve

// Repair is the Doctor table's one button: what each failing part's Fix
// does in a terminal, done from the app. The names and the domains are the
// CLI's own commands (doze dns-setup, doze trust in each stack that
// declares domains), run as the person, so their logic stays in one place;
// their privileged step asks with the system's administrator prompt
// instead of sudo (adminprompt.Env). Each part that needs root asks once.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze/internal/adminprompt"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/view"
	"github.com/doze-dev/doze/internal/display"
)

// pathLink is where Repair puts doze so a terminal finds it.
const pathLink = "/usr/local/bin/doze"

// SetRepair says how this face asks for an administrator's password, as
// adminprompt.Run does. Nil (the default, and a browser's) says it cannot:
// the Doctor table keeps its Copy fix buttons only.
func (s *Service) SetRepair(admin func(what, script string) error) {
	s.mu.Lock()
	s.admin = admin
	s.mu.Unlock()
	s.changed()
}

// repairStep is one part of a repair: a doze command run in a folder, or a
// script run as root.
type repairStep struct {
	what   string // for what is said: "the .doze names"
	dir    string
	args   []string
	script string
	// prune removes the stale routes in-process (clearRoutes).
	prune bool
}

// fixable says some part is missing and has a fix.
func fixable(checks []view.Check) bool {
	for _, c := range checks {
		if !c.OK && c.Fix != "" {
			return true
		}
	}
	return false
}

// repairSteps are the steps that put the failing parts of checks in place.
func repairSteps(stacks []hub.StackView, checks []view.Check, self string) []repairStep {
	need := map[string]bool{}
	for _, c := range checks {
		if !c.OK && c.Fix != "" {
			need[c.Section] = true
		}
	}
	var steps []repairStep
	if need["names"] {
		steps = append(steps, repairStep{what: "the .doze names", args: []string{"dns-setup"}})
	}
	if need["domains"] {
		any := false
		for _, st := range stacks {
			if len(view.Domains(st)) > 0 {
				any = true
				steps = append(steps, repairStep{what: st.Name + "'s domains", dir: st.Dir, args: []string{"trust"}})
			}
		}
		// doze trust prunes stale routes in the same prompt; with no stack
		// to run it in, the app prunes them itself.
		if !any {
			for _, c := range checks {
				if !c.OK && strings.HasPrefix(c.Name, "stale route ") {
					steps = append(steps, repairStep{what: "remove stale routes", prune: true})
					break
				}
			}
		}
	}
	if need["app"] && self != "" {
		steps = append(steps, repairStep{
			what:   "put doze on your PATH",
			script: "mkdir -p /usr/local/bin\nln -sf " + adminprompt.Quote(self) + " " + pathLink,
		})
	}
	return steps
}

// Repair puts the machine's missing parts in place, in the background; the
// Library is pushed with Repairing set, and again with the fresh report
// when it is done. Asked twice, the second is a no-op.
func (s *Service) Repair() error {
	s.mu.Lock()
	admin := s.admin
	if admin == nil {
		s.mu.Unlock()
		return errors.New("this doze cannot ask for an administrator's password here: copy the fix into a terminal")
	}
	if s.repairing {
		s.mu.Unlock()
		return nil
	}
	s.repairing = true
	s.mu.Unlock()
	s.changed()
	go s.repair(admin, s.hub.Stacks())
	return nil
}

func (s *Service) repair(admin func(what, script string) error, stacks []hub.StackView) {
	s.forgetMachine()
	steps := repairSteps(stacks, s.doctor(stacks), selfPath())
	done := false
	defer func() {
		s.forgetMachine()
		fresh := s.doctor(stacks)
		s.mu.Lock()
		s.doctorChecks, s.doctorAt, s.repairing = fresh, s.now(), false
		s.mu.Unlock()
		s.changed()
		if !done {
			return
		}
		var still []string
		for _, c := range fresh {
			if !c.OK && c.Fix != "" {
				still = append(still, c.Name)
			}
		}
		if len(still) == 0 {
			s.Say("this machine is set up", false)
		} else {
			s.Say("repaired what could be; still missing: "+strings.Join(still, ", "), true)
		}
	}()
	if len(steps) == 0 {
		s.Say("nothing to repair: this machine is set up", false)
		return
	}
	for _, st := range steps {
		err := s.runStep(admin, st)
		if errors.Is(err, adminprompt.ErrCancelled) {
			s.Say("repair cancelled; nothing more was changed", false)
			return
		}
		if err != nil {
			s.Say("setting up "+st.what+" failed: "+display.Humanise(err), true)
			return
		}
	}
	done = true
}

func (s *Service) runStep(admin func(what, script string) error, st repairStep) error {
	if st.prune {
		return s.clearRoutes(names.StaleRoutes(s.hub.Home()))
	}
	if st.script != "" {
		return admin(st.what, st.script)
	}
	out, err := s.run(st.dir, st.args)
	if err == nil {
		return nil
	}
	if strings.Contains(out, adminprompt.CancelledText) {
		return adminprompt.ErrCancelled
	}
	if last := lastLine(out); last != "" {
		return errors.New(last)
	}
	return err
}

// RepairPlan is what Repair would run, for the page to show before anyone
// types a password: each doze command's script (its --print), and the
// scripts Repair runs itself. Empty when nothing is missing. Note that
// `doze trust --print` keeps the stack's domains on record and makes the
// certificate authority when it is missing, as it does in a terminal.
func (s *Service) RepairPlan() string {
	stacks := s.hub.Stacks()
	s.forgetMachine()
	var b strings.Builder
	for _, st := range repairSteps(stacks, s.doctor(stacks), selfPath()) {
		if st.prune {
			fmt.Fprintf(&b, "# %s\n", st.what)
			_ = names.Trust(names.Options{Print: true, Out: &b}, names.TrustRequest{Home: s.hub.Home(), Prune: names.StaleRoutes(s.hub.Home())})
			b.WriteString("\n")
			continue
		}
		if st.script != "" {
			fmt.Fprintf(&b, "# %s\nsudo sh <<'DOZE_SETUP'\n%s\nDOZE_SETUP\n\n", st.what, st.script)
			continue
		}
		where := ""
		if st.dir != "" {
			where = " (in " + display.Tilde(st.dir) + ")"
		}
		fmt.Fprintf(&b, "# doze %s%s\n", strings.Join(st.args, " "), where)
		out, err := s.run(st.dir, append(append([]string{}, st.args...), "--print"))
		if err != nil && strings.TrimSpace(out) == "" {
			out = err.Error()
		}
		b.WriteString(strings.TrimRight(out, "\n") + "\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// forgetMachine drops the cached machine checks, so the next look is fresh.
func (s *Service) forgetMachine() {
	s.mu.Lock()
	s.trustAt, s.doctorAt = time.Time{}, time.Time{}
	s.mu.Unlock()
}

// runSelf runs this doze with args in dir, as the person, its privileged
// step behind the system's prompt.
func runSelf(dir string, args []string) (string, error) {
	self := selfPath()
	if self == "" {
		return "", errors.New("cannot find this program to run it")
	}
	cmd := exec.Command(self, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "NO_COLOR=1", adminprompt.Env+"=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return p
}

// lastLine is the last non-empty line of out: where a command says why.
func lastLine(out string) string {
	out = strings.TrimSpace(out)
	if i := strings.LastIndex(out, "\n"); i >= 0 {
		out = strings.TrimSpace(out[i+1:])
	}
	return out
}
