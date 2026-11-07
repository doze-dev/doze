//go:build purego

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/doze-dev/doze/internal/desk/serve"
	"github.com/doze-dev/doze/internal/desk/view"
)

// API is the service the page calls, bound to Wails. Its TypeScript side is
// generated into frontend/src/bindings (make bindings); a test keeps the two
// in step. Every method is a thin call into serve.Service, which is also
// what the browser reaches over HTTP, so the page behaves the same in both.
type API struct {
	s       *serve.Service
	painted func(ms int)
}

// Stacks is every stack, for the tab strip.
func (a *API) Stacks() []serve.StackSummary { return a.s.Stacks() }

// Pending is where the page should start: the stack doze open asked for.
func (a *API) Pending() serve.Nav { return a.s.Pending() }

// Version is the doze this app is, for the sidebar's foot.
func (a *API) Version() string { return a.s.Version() }

// Home is a stack's home page.
func (a *API) Home(key string) view.Home { return a.s.Home(key) }

// Instance is an instance's page.
func (a *API) Instance(key, name string) view.Instance { return a.s.Instance(key, name) }

// Logs is a window of an instance's lines, or of every instance's.
func (a *API) Logs(key string, q view.LogsQuery) view.Logs { return a.s.Logs(key, q) }

// Focus says what the page shows; the hub and the pushes follow it.
func (a *API) Focus(f serve.Focus) { a.s.SetFocus(f) }

// Blur says the page is hidden.
func (a *API) Blur() { a.s.Blur() }

// Do runs a verb on a stack.
func (a *API) Do(key, verb, target string) error { return a.s.Do(key, verb, target) }

// DoAll runs a verb on several targets in order.
func (a *API) DoAll(key, verb string, targets []string) error { return a.s.DoAll(key, verb, targets) }

// Dialog is the confirmation for a verb on an instance.
func (a *API) Dialog(key, name, verb string) view.Dialog { return a.s.Dialog(key, name, verb) }

// Palette is the ⌘K list for a query.
func (a *API) Palette(key, selected, scope, query string) view.Palette {
	return a.s.Palette(key, selected, scope, query)
}

// RememberCommand keeps a command done from ⌘K for its Recent section.
func (a *API) RememberCommand(c view.RecentCommand) { a.s.RememberCommand(c) }

// OpenTerminal opens a terminal in a stack's folder.
func (a *API) OpenTerminal(dir string) error {
	return a.s.OpenTerminal(dir)
}

// Recent is the last twenty things said, newest first.
func (a *API) Recent() []serve.Notice { return a.s.Recent() }

// Say adds something the page did to the notices.
func (a *API) Say(text string, bad bool) { a.s.Say(text, bad) }

// Theme tells Go which theme the page uses.
func (a *API) Theme(theme string) { a.s.SetTheme(theme) }

// Mode tells Go the mode the page uses: dark, light or system.
func (a *API) Mode(mode string) { a.s.SetMode(mode) }

// Dismiss puts away a stack action's failure.
func (a *API) Dismiss(key string) { a.s.Dismiss(key) }

// Painted reports the page's first paint, for the desk bench.
func (a *API) Painted(ms int) {
	if a.painted != nil {
		a.painted(ms)
	}
}

// Log is the page's line to the app's stderr: what it chose, what failed.
// The web view's own console is not visible from outside the window.
func (a *API) Log(line string) {
	fmt.Fprintln(os.Stderr, "page: "+line)
}

func newService(s *serve.Service, painted func(int)) application.Service {
	return application.NewService(&API{s: s, painted: painted})
}

// Console is an instance's console tab, resolved for the selection.
func (a *API) Console(key, name, tab string, sel map[string]int) view.Console {
	return a.s.Console(key, name, tab, sel)
}

// Action is the confirmation for a module's action.
func (a *API) Action(key, name string, act view.Action) view.Dialog {
	return a.s.Action(key, name, act)
}

// Admin runs a module's action on an instance.
func (a *API) Admin(key, name, action, resource, input string) error {
	return a.s.Admin(key, name, action, resource, input)
}

// Empty is the page for a folder with no doze.hcl yet.
func (a *API) Empty(dir string) view.Empty { return a.s.Empty(dir) }

// Setup sets a folder up: init, add <arg>, or a starter file.
func (a *API) Setup(dir, action, arg string) error { return a.s.Setup(dir, action, arg) }

// Open opens a URL in the person's browser: an app's own domain.
// Open opens a URL in the browser, or a file in its app; a path written
// with ~ is the person's home.
func (a *API) Open(url string) {
	if strings.HasPrefix(url, "file://~/") {
		if home, err := os.UserHomeDir(); err == nil {
			url = "file://" + filepath.Join(home, strings.TrimPrefix(url, "file://~/"))
		}
	}
	_ = application.Get().Browser.OpenURL(url)
}

// Copy puts text on the clipboard. The page cannot: the browser's clipboard
// is refused to a wails:// origin.
func (a *API) Copy(text string) error {
	if !application.Get().Clipboard.SetText(text) {
		return errors.New("the clipboard refused the text")
	}
	return nil
}

// Library is the first tab: every stack, the help strip, the machine line.
func (a *API) Library() view.Library { return a.s.Library() }

// Leftovers are what stacks left behind, out of step with doze: stray
// engines and daemons, state with no stack, stale routes.
func (a *API) Leftovers() []view.Leftover { return a.s.Leftovers() }

// Attention is Needs you: what is wrong now, everywhere, each with its fix.
func (a *API) Attention() view.Attention { return a.s.Attention() }

// Preflight looks before a stopped stack starts: its files, its ports,
// its domains. Blocked says the page should ask first.
func (a *API) Preflight(key, action string, names []string) view.Preflight {
	return a.s.Preflight(key, action, names)
}

// PreflightFix is a way out from the checks: stop, without, or anyway.
func (a *API) PreflightFix(key, action string, names []string, kind string, pid int, without string) error {
	return a.s.PreflightFix(key, action, names, kind, pid, without)
}

// PreflightInstance looks before one service of a running stack starts.
func (a *API) PreflightInstance(key, name string) view.Preflight {
	return a.s.PreflightInstance(key, name)
}

// PreflightInstanceFix is a way out for one service: stop, or anyway.
func (a *API) PreflightInstanceFix(key, name, verb, kind string, pid int) error {
	return a.s.PreflightInstanceFix(key, name, verb, kind, pid)
}

// Away is what happened while the window was closed, once; nil for nothing.
func (a *API) Away() *view.Away { return a.s.Away() }

// NotNow puts a Needs you item aside until what it says (print) changes;
// an empty print brings it back.
func (a *API) NotNow(id, print string) { a.s.NotNow(id, print) }

// ClearLeftover clears one leftover by its ID, or every one that deletes no
// data with "all".
func (a *API) ClearLeftover(id string) error { return a.s.ClearLeftover(id) }

// Stack acts on a whole stack: up-last, up-all, up (with names), down, remove.
func (a *API) Stack(key, action string, names []string) error { return a.s.Stack(key, action, names) }

// Timeline is a stack's last half hour: lanes of awake spans and crashes.
func (a *API) Activity(key, span, inst string) view.Activity { return a.s.Activity(key, span, inst) }

// Update says whether a newer doze is out; force asks again now.
func (a *API) Update(force bool) view.Update { return a.s.Update(force) }

// StopHolder stops the process holding a crashed service's port, then restarts it.
func (a *API) StopHolder(key, name string, pid int) error { return a.s.StopHolder(key, name, pid) }

// SetOpenAtLogin starts the app at login, in the menu bar only, or stops.
func (a *API) SetOpenAtLogin(on bool) error { return a.s.SetOpenAtLogin(on) }

// DismissHints hides the Library's help strip for good.
func (a *API) DismissHints() { a.s.DismissHints() }

// Repair puts the machine's missing setup in place, behind the system's
// administrator prompt; the Library is pushed while it runs and after.
func (a *API) Repair() error { return a.s.Repair() }

// RepairPlan is what Repair would run, to show before the password.
func (a *API) RepairPlan() string { return a.s.RepairPlan() }

// SetView remembers a stack's overview view: tiles, list, map or grid.
func (a *API) SetView(key, name string) { a.s.SetView(key, name) }

// ChooseFolder shows the folder picker and opens what was chosen, which
// lands on the stack's tab, or on the page for a folder with no doze.hcl.
func (a *API) ChooseFolder() string {
	dir, err := application.Get().Dialog.OpenFile().CanChooseDirectories(true).CanChooseFiles(false).SetTitle("Open a project folder").PromptForSingleSelection()
	if err != nil || dir == "" {
		return ""
	}
	a.s.Open(dir)
	return dir
}
