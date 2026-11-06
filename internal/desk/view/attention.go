package view

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Attention is what needs the person now, across every stack and the
// machine (D19): red items are broken (something asked for is not
// happening), yellow ones are worth doing. Every surface (the bell, the menu
// bar, ⌘K, notifications) reads this one list. An item leaves when its
// cause is gone; "Not now" hides one until what it says changes.
type Attention struct {
	Red    int    `json:"red"`
	Yellow int    `json:"yellow"`
	Items  []Item `json:"items"`
	// Hidden counts items put aside with Not now.
	Hidden int `json:"hidden,omitempty"`
}

// Item is one thing that needs the person.
type Item struct {
	// ID names the item across pushes: its kind, stack and instance.
	ID string `json:"id"`
	// Print is what it says, for Not now: a new print shows it again.
	Print string `json:"print"`
	Red   bool   `json:"red"`
	Kind  string `json:"kind"`
	// Key is the stack's key, Stack its name ("" for the machine).
	Key      string `json:"key,omitempty"`
	Stack    string `json:"stack,omitempty"`
	Instance string `json:"instance,omitempty"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	// Mono says Detail is a program's own words, set in the code face.
	Mono  bool  `json:"mono,omitempty"`
	Fixes []Fix `json:"fixes,omitempty"`
	// Show is the page that explains it.
	Show *Place `json:"show,omitempty"`
	// Since is when it began, as far as doze knows; zero when unknown.
	Since time.Time `json:"since,omitzero"`
	// Fixed says the cause went away a moment ago: shown grey, then gone.
	Fixed bool `json:"fixed,omitempty"`
}

// Fix is one way out, as a button. Kind says what the page calls:
//
//	do       a verb on the instance (restart, task), through its dialog
//	stack    a stack action (up-last, restart, remove), through its dialog
//	holder   stop the process holding a port (PID), then start the instance
//	repair   the machine's setup behind the administrator prompt
//	clean    clear what was left behind, all but data
//	dismiss  put a failed start away
//	edit     open doze.hcl (Path) in the editor
//	copy     copy Text
type Fix struct {
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Verb    string `json:"verb,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Path    string `json:"path,omitempty"`
	Text    string `json:"text,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// Place is a page to go to: a stack's home, an instance, Settings.
type Place struct {
	Key      string `json:"key"`
	Page     string `json:"page"`
	Instance string `json:"instance,omitempty"`
	Section  string `json:"section,omitempty"`
}

// StackFacts is what the service knows about a stack beyond the hub: its
// failed action, its doze.hcl's state, a port a crash says was taken.
type StackFacts struct {
	Failed  *StackFailure
	Config  *ConfigError
	Changed *ConfigChanged
	// ChangedAt is the saved file's time: what "changed" says for Not now,
	// where the words ("4m ago") move on their own.
	ChangedAt time.Time
	Missing   string
	// Held finds the process on a port a crash named; nil when unknown.
	Held func(port int) *PortHolder
	// Port is the port a crash's words say was taken, or 0.
	Port func(v control.InstanceView) int
}

// StackItems are a stack's items: its instances in trouble, then the stack
// itself.
func StackItems(st hub.StackView, f StackFacts, now time.Time) []Item {
	name := st.Name
	if name == "" && st.Dir != "" {
		name = baseName(st.Dir)
	}
	home := &Place{Key: st.Key, Page: "home"}
	var out []Item
	if f.Missing != "" {
		// Nothing else about a stack whose files are gone is worth saying.
		what := "folder is gone"
		if f.Missing == "doze.hcl is gone" {
			what = "doze.hcl is gone"
		}
		return []Item{{
			ID: "missing:" + st.Key, Print: f.Missing, Kind: "missing", Key: st.Key, Stack: name,
			Title: name + "'s " + what, Detail: display.Tilde(st.Dir) + " was moved or deleted",
			Fixes: []Fix{{Label: "Remove…", Kind: "stack", Verb: "remove", Primary: true}},
		}}
	}
	if f.Failed != nil {
		it := Item{ID: "failed:" + st.Key, Red: true, Kind: "failed", Key: st.Key, Stack: name, Title: f.Failed.What, Show: home, Mono: true}
		if len(f.Failed.Lines) > 0 {
			it.Detail = f.Failed.Lines[len(f.Failed.Lines)-1]
		}
		it.Print = it.Title + "|" + it.Detail
		if !st.Running {
			it.Fixes = append(it.Fixes, Fix{Label: "Try again", Kind: "stack", Verb: "up-last", Primary: true})
		}
		it.Fixes = append(it.Fixes, Fix{Label: "Dismiss", Kind: "dismiss"})
		out = append(out, it)
	}
	if st.Running && st.Conn == hub.ConnDown {
		out = append(out, Item{
			ID: "daemon:" + st.Key, Print: "down", Red: true, Kind: "daemon", Key: st.Key, Stack: name,
			Title: name + "'s daemon is not answering", Detail: "what it shows is from " + display.Ago(now.Sub(st.LastFrame)) + " ago; doze keeps trying",
			Fixes: []Fix{{Label: "Restart " + name, Kind: "stack", Verb: "restart", Primary: true}}, Show: home,
		})
	}
	if st.Running && st.Conn == hub.ConnLive {
		for _, v := range st.Instances {
			if it, ok := instanceItem(st, name, v, f, now); ok {
				out = append(out, it)
			}
		}
	}
	if f.Config != nil {
		it := Item{
			ID: "config:" + st.Key, Kind: "config", Key: st.Key, Stack: name,
			Title: name + "'s doze.hcl does not load", Detail: f.Config.Summary, Show: home,
			Fixes: []Fix{{Label: "Edit doze.hcl", Kind: "edit", Path: f.Config.File, Primary: true}},
		}
		if f.Config.Line > 0 {
			it.Detail = fmt.Sprintf("line %d · %s", f.Config.Line, f.Config.Summary)
		}
		it.Print = it.Detail
		if st.Running {
			it.Detail += " · the file it started with runs on"
		}
		out = append(out, it)
	} else if f.Changed != nil && st.Running {
		out = append(out, Item{
			ID: "changed:" + st.Key, Print: f.ChangedAt.String(), Kind: "changed", Key: st.Key, Stack: name,
			Title: "doze.hcl changed", Detail: f.Changed.Text + "; restart " + name + " to run it",
			Fixes: []Fix{{Label: "Restart " + name, Kind: "stack", Verb: "restart", Primary: true}}, Show: home,
		})
	}
	return out
}

// instanceItem is an instance in trouble: crashing, failed, not ready in
// time, or a task whose last run failed.
func instanceItem(st hub.StackView, stack string, v control.InstanceView, f StackFacts, now time.Time) (Item, bool) {
	if it, ok := stuckWaking(st, stack, v, now); ok {
		return it, true
	}
	if !display.Troubled(v) {
		return Item{}, false
	}
	it := Item{
		ID: "instance:" + st.Key + "/" + v.Name, Red: true, Kind: "instance", Key: st.Key, Stack: stack, Instance: v.Name,
		Show: &Place{Key: st.Key, Page: "instance", Instance: v.Name},
	}
	if v.Task != nil && v.Task.Result == "failed" {
		it.Kind = "task"
		it.Title = v.Name + " failed"
		it.Detail = fmt.Sprintf("exit %d", v.Task.ExitCode)
		if e := display.FirstLine(v.LastError); e != "" {
			it.Detail += " · " + e
		}
		it.Mono = true
		it.Since = v.Task.RanAt
		it.Print = v.Task.RanAt.String()
		it.Fixes = []Fix{{Label: "Run again", Kind: "do", Verb: "task", Primary: true}, {Label: "Output", Kind: "show"}}
		return it, true
	}
	errLine := display.FirstLine(v.LastError)
	// Since is the last time the journal saw it crash or fail.
	for i := len(st.Events) - 1; i >= 0; i-- {
		if e := st.Events[i]; e.Instance == v.Name && (e.Kind == "crash" || e.Kind == "failed" || e.Kind == "give_up") {
			it.Since = e.Time
			break
		}
	}
	switch {
	case strings.Contains(v.LastError, "not become ready"):
		it.Title = v.Name + " did not become ready"
		it.Detail = "it started but never answered; its logs usually say why"
	case v.State == "crashing":
		it.Title = v.Name + " keeps crashing"
		it.Detail = errLine
		if v.RestartMax > 0 {
			try := fmt.Sprintf("try %d of %d", v.RestartCount, v.RestartMax)
			if !v.NextRestart.IsZero() && v.NextRestart.After(now) {
				try += fmt.Sprintf(", next in %ds", int(v.NextRestart.Sub(now).Seconds()+0.5))
			}
			it.Detail = joinDot(it.Detail, try)
		}
	default:
		it.Title = v.Name + " stopped and waits for you"
		it.Detail = errLine
	}
	it.Mono = errLine != "" && it.Detail == errLine
	it.Print = v.State + "|" + errLine
	it.Fixes = []Fix{{Label: "Restart", Kind: "do", Verb: "restart", Primary: true}, {Label: "Logs", Kind: "show"}}
	if f.Port != nil && f.Held != nil {
		if port := f.Port(v); port > 0 {
			if h := f.Held(port); h != nil && h.PID > 0 && !h.Doze {
				it.Detail = fmt.Sprintf("port %d is held by %s (pid %d)", port, h.Command, h.PID)
				it.Mono = false
				if v.State == "crashing" && v.RestartMax > 0 {
					it.Detail += fmt.Sprintf(" · try %d of %d", v.RestartCount, v.RestartMax)
				}
				it.Fixes = []Fix{{Label: fmt.Sprintf("Stop pid %d and start %s", h.PID, v.Name), Kind: "holder", PID: h.PID, Primary: true}, {Label: "Show", Kind: "show"}}
			}
		}
	}
	return it, true
}

// MachineItems are the machine's parts not in place, as one item per
// section, and what stacks left behind.
func MachineItems(checks []Check, repairable bool, leftovers, data int) []Item {
	var out []Item
	bySection := map[string][]Check{}
	var order []string
	for _, c := range checks {
		if c.OK {
			continue
		}
		if _, ok := bySection[c.Section]; !ok {
			order = append(order, c.Section)
		}
		bySection[c.Section] = append(bySection[c.Section], c)
	}
	for _, sec := range order {
		cs := bySection[sec]
		var parts []string
		for _, c := range cs {
			parts = append(parts, c.Name)
		}
		it := Item{ID: "machine:" + sec, Print: strings.Join(parts, ","), Kind: "machine", Detail: "missing: " + strings.Join(parts, ", "), Show: &Place{Page: "settings", Section: "machine"}}
		switch sec {
		case "domains":
			it.Title = "Your domains are not set up on this Mac"
			it.Detail = "one admin prompt: a resolver route and the doze certificate authority"
		case "names":
			it.Title = ".doze names are not set up on this Mac"
		case "app":
			it.Title = cs[0].Text
			if it.Title == "" {
				it.Title = cs[0].Name + " is not in place"
			}
			it.Detail = ""
		default:
			it.Title = sec + " is not in place"
		}
		if repairable {
			it.Fixes = append(it.Fixes, Fix{Label: "Set up (one password)", Kind: "repair", Primary: true})
		} else if cs[0].Fix != "" {
			it.Fixes = append(it.Fixes, Fix{Label: "Copy the command", Kind: "copy", Text: cs[0].Fix, Primary: true})
		}
		out = append(out, it)
	}
	if leftovers > 0 {
		out = append(out, Item{
			ID: "leftovers", Print: fmt.Sprint(leftovers), Kind: "leftovers",
			Title:  fmt.Sprintf("%d thing%s left behind", leftovers, plural(leftovers)),
			Detail: "processes and routes from stacks that went out of step with doze",
			Fixes:  []Fix{{Label: "Clean up", Kind: "clean", Primary: true}},
			Show:   &Place{Page: "settings", Section: "machine"},
		})
	}
	if data > 0 {
		// Data is deleted one folder at a time, after asking: in Settings.
		out = append(out, Item{
			ID: "leftdata", Print: fmt.Sprint(data), Kind: "leftdata",
			Title:  fmt.Sprintf("%d data folder%s no stack uses", data, plural(data)),
			Detail: "kept until you look at each one in Settings › Machine",
			Show:   &Place{Page: "settings", Section: "machine"},
		})
	}
	return out
}

// AttentionOf sorts the items, red first, newest first within each, drops
// those put aside (notNow: ID → the print it had), and counts.
func AttentionOf(items []Item, notNow map[string]string) Attention {
	a := Attention{Items: []Item{}}
	for _, it := range items {
		if notNow[it.ID] != "" && notNow[it.ID] == it.Print {
			a.Hidden++
			continue
		}
		a.Items = append(a.Items, it)
		switch {
		case it.Fixed:
		case it.Red:
			a.Red++
		default:
			a.Yellow++
		}
	}
	sort.SliceStable(a.Items, func(i, j int) bool {
		x, y := a.Items[i], a.Items[j]
		if x.Fixed != y.Fixed {
			return !x.Fixed
		}
		if x.Red != y.Red {
			return x.Red
		}
		return x.Since.After(y.Since)
	})
	return a
}

func joinDot(a, b string) string {
	if a == "" {
		return b
	}
	return a + " · " + b
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func baseName(dir string) string {
	dir = strings.TrimRight(dir, "/")
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		return dir[i+1:]
	}
	return dir
}

// Preflight is what Up found before starting a stopped stack (the
// PreFlight board). Blocked says something red is in the way, and the
// page asks; otherwise Up just goes.
type Preflight struct {
	Stack   string     `json:"stack"`
	Blocked bool       `json:"blocked"`
	Checks  []PreCheck `json:"checks"`
}

// PreCheck is one line: ok, warn (it starts anyway) or bad (in the way).
type PreCheck struct {
	Tone   string `json:"tone"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Mono   bool   `json:"mono,omitempty"`
	// Note is a second line under the detail, in plain words.
	Note  string `json:"note,omitempty"`
	Fixes []Fix  `json:"fixes,omitempty"`
}

// stuckFor is how long a wake may take before doze says so; quietFor is
// how long it must have said nothing. The daemon's own ready check (60 s
// unless doze.hcl allows more) fails a start that never answers; this is
// for one that hangs without failing: still waking, and silent.
const (
	stuckFor = 5 * time.Minute
	quietFor = 2 * time.Minute
)

// StuckWaking is stuckWaking, for notifications (D8's third rule).
func StuckWaking(st hub.StackView, v control.InstanceView, now time.Time) (Item, bool) {
	return stuckWaking(st, st.Name, v, now)
}

// stuckWaking is an instance waking for stuckFor with no journal word
// from it for quietFor.
func stuckWaking(st hub.StackView, stack string, v control.InstanceView, now time.Time) (Item, bool) {
	if v.State != "booting" {
		return Item{}, false
	}
	var woke, heard time.Time
	for i := len(st.Events) - 1; i >= 0; i-- {
		e := st.Events[i]
		if e.Instance != v.Name {
			continue
		}
		if heard.IsZero() {
			heard = e.Time
		}
		if e.Kind == "wake" {
			woke = e.Time
			break
		}
	}
	if woke.IsZero() || now.Sub(woke) < stuckFor || now.Sub(heard) < quietFor {
		return Item{}, false
	}
	return Item{
		ID: "instance:" + st.Key + "/" + v.Name, Print: "stuck|" + woke.String(), Red: true, Kind: "instance", Key: st.Key, Stack: stack, Instance: v.Name,
		Title:  v.Name + " is still waking",
		Detail: fmt.Sprintf("for %s, with no word from it for %s; its logs usually say why", display.Ago(now.Sub(woke)), display.Ago(now.Sub(heard))),
		Since:  woke,
		Fixes:  []Fix{{Label: "Restart", Kind: "do", Verb: "restart", Primary: true}, {Label: "Logs", Kind: "show"}},
		Show:   &Place{Key: st.Key, Page: "instance", Instance: v.Name},
	}, true
}
