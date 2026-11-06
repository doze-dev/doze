package view

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// The palette's commands (⌘K, rethought 2026-11-06): what can be done to a
// service, to a stack, and in the app, each a row with its key and its CLI
// form. A stack or a service row can be entered with tab, which narrows
// the palette to that thing's commands; the person's place in the app puts
// its commands first.

// aliases are words people type for a verb doze names differently.
var aliases = map[string]string{
	"start": "wake", "stop": "sleep", "kill": "restart", "reboot": "restart",
	"url": "copy", "browse": "open", "log": "logs", "tail": "logs",
}

// instanceCommands is everything a service can have done to it, the most
// likely first for its state.
func instanceCommands(st hub.StackView, v control.InstanceView, now time.Time) []PaletteRow {
	n, key := v.Name, st.Key
	verb := func(label, glyph, verb, cli, also string, ask bool) PaletteRow {
		r := PaletteRow{Glyph: glyph, Tone: "dim", Label: label, CLI: cli, Also: also, Do: Doing{Kind: "verb", StackKey: key, Verb: verb, Target: n, Ask: ask}}
		if ask {
			r.Key = "⏎ asks first"
		}
		return r
	}
	var out []PaletteRow
	page := PaletteRow{Glyph: "→", Tone: "dim", Label: "Open " + n, Note: "its page", Also: "go show", Do: Doing{Kind: "page", StackKey: key, Page: "instance", Target: n}}
	logs := PaletteRow{Glyph: "≡", Tone: "dim", Label: "Logs of " + n, CLI: "doze logs " + n, Also: "tail log", Do: Doing{Kind: "page", StackKey: key, Page: "logs", Target: n}}
	if v.Kind == "task" {
		out = append(out, verb("Run "+n, "▸", control.VerbTask, "doze task "+n, "start", false), logs, page)
		return out
	}
	awake := display.AwakeNow(v)
	switch {
	case v.Disabled:
		out = append(out, verb("Put "+n+" back in this run", "↑", control.VerbOn, "doze on "+n, "on enable", false))
	case awake:
		out = append(out, verb("Sleep "+n, "↓", control.VerbSleep, "doze sleep "+n, "stop", DialogOf(st, n, control.VerbSleep, now).Needed))
		out = append(out, verb("Restart "+n, "↻", control.VerbRestart, "doze restart "+n, "kill reboot", DialogOf(st, n, control.VerbRestart, now).Needed))
	default:
		out = append(out, verb("Wake "+n, "↑", control.VerbWake, "doze wake "+n, "start", false))
		if display.Troubled(v) {
			out = append(out, verb("Restart "+n, "↻", control.VerbRestart, "doze restart "+n, "kill reboot", false))
		}
	}
	if url := firstOf(v.HTTPS, webURL(v)); url != "" {
		out = append(out, PaletteRow{Glyph: "↗", Tone: "dim", Label: "Open " + n + " in the browser", Note: url, CLI: "open " + url, Also: "browse web", Do: Doing{Kind: "open", StackKey: key, Text: url, Target: n}})
	}
	if c := firstOf(v.Console, v.WebURL); c != "" && v.Kind == "module" {
		out = append(out, PaletteRow{Glyph: "▣", Tone: "dim", Label: "Open " + n + "'s console", Note: c, Also: "admin ui", Do: Doing{Kind: "open", StackKey: key, Text: c, Target: n}})
	}
	out = append(out, logs)
	if line := firstOf(v.HTTPS, v.URL, v.Endpoint); line != "" {
		out = append(out, PaletteRow{Glyph: "⧉", Tone: "dim", Label: "Copy " + n + "'s URL", Note: line, CLI: "doze url " + n, Also: "address", Do: Doing{Kind: "copy", StackKey: key, Text: line, Target: n}})
	}
	if v.URL != "" && v.URL != firstOf(v.HTTPS, v.URL) {
		out = append(out, PaletteRow{Glyph: "⧉", Tone: "dim", Label: "Copy " + n + "'s connection string", Note: v.URL, Also: "dsn url", Do: Doing{Kind: "copy", StackKey: key, Text: v.URL, Target: n}})
	}
	if v.EnvVar != "" && v.URL != "" {
		line := v.EnvVar + "=" + v.URL
		out = append(out, PaletteRow{Glyph: "⧉", Tone: "dim", Label: "Copy as " + v.EnvVar, Note: line, Also: "env .env", Do: Doing{Kind: "copy", StackKey: key, Text: line, Target: n}})
	}
	if cli := clientLine(v.URL); cli != "" {
		out = append(out, PaletteRow{Glyph: "⧉", Tone: "dim", Label: "Copy the " + strings.Fields(cli)[0] + " line", Note: cli, Also: "shell client", Do: Doing{Kind: "copy", StackKey: key, Text: cli, Target: n}})
	}
	if !v.KeepAwake {
		out = append(out, verb("Pin "+n+" awake", "◆", control.VerbPin, "doze pin "+n, "keep", false))
	} else {
		out = append(out, verb("Unpin "+n, "◆", control.VerbPin, "doze pin --off "+n, "", false))
	}
	if !v.Disabled {
		out = append(out, verb("Take "+n+" out of this run", "⊘", control.VerbOff, "doze off "+n, "off disable", display.InUse(v) != ""))
	}
	out = append(out, page)
	if v.Kind == "module" {
		r := verb("Reset "+n+"…", "⚠", control.VerbReset, "doze reset "+n, "wipe", true)
		r.Bad, r.Note = true, "deletes its data; asks first"
		out = append(out, r)
	}
	return out
}

// clientLine is the shell client a connection string opens, when doze
// knows one.
func clientLine(url string) string {
	switch {
	case strings.HasPrefix(url, "postgres://"), strings.HasPrefix(url, "postgresql://"):
		return fmt.Sprintf("psql %q", url)
	case strings.HasPrefix(url, "redis://"), strings.HasPrefix(url, "rediss://"):
		return "redis-cli -u " + url
	case strings.HasPrefix(url, "mongodb://"):
		return fmt.Sprintf("mongosh %q", url)
	case strings.HasPrefix(url, "mysql://"):
		return fmt.Sprintf("mysql --uri %q", url)
	}
	return ""
}

// stackCommands is everything a stack can have done to it, for its state.
func stackCommands(st hub.StackView) []PaletteRow {
	key, name := st.Key, st.Name
	stack := func(label, glyph, verb, cli, also string) PaletteRow {
		return PaletteRow{Glyph: glyph, Tone: "dim", Label: label, CLI: cli, Also: also, Do: Doing{Kind: "stack", StackKey: key, Verb: verb}}
	}
	var out []PaletteRow
	if st.Running {
		out = append(out,
			PaletteRow{Glyph: "↓", Tone: "dim", Label: "Sleep everything in " + name, Note: "frees memory; the next connection wakes each", CLI: "doze sleep", Also: "stop all", Do: Doing{Kind: "verb", StackKey: key, Verb: control.VerbSleep}},
			PaletteRow{Glyph: "↑", Tone: "dim", Label: "Wake everything in " + name, CLI: "doze wake", Also: "start all", Do: Doing{Kind: "verb", StackKey: key, Verb: control.VerbWake}},
			stack("Restart "+name, "↻", "restart", "doze down && doze up --last", "kill reboot"),
			stack("Down "+name, "■", "down", "doze down", "stop off"),
			stack("Start everything else in "+name, "↑", "up-all", "doze up --all", "up all"),
		)
	} else {
		out = append(out,
			stack("Up "+name+" as last time", "↑", "up-last", "doze up --last", "start on"),
			stack("Up everything in "+name, "↑", "up-all", "doze up --all", "start all"),
			PaletteRow{Glyph: "☐", Tone: "dim", Label: "Choose what starts in " + name + "…", Also: "pick select", Do: Doing{Kind: "choose", StackKey: key}},
		)
	}
	out = append(out,
		PaletteRow{Glyph: "→", Tone: "dim", Label: "Open " + name, Note: "its overview", Also: "go show", Do: Doing{Kind: "page", StackKey: key, Page: "home"}},
		PaletteRow{Glyph: "≡", Tone: "dim", Label: "All logs of " + name, CLI: "doze logs", Also: "tail", Do: Doing{Kind: "page", StackKey: key, Page: "logs"}},
		PaletteRow{Glyph: "◷", Tone: "dim", Label: "Timeline of " + name, Also: "history events", Do: Doing{Kind: "page", StackKey: key, Page: "timeline"}},
	)
	if st.Dir != "" {
		out = append(out,
			PaletteRow{Glyph: "✎", Tone: "dim", Label: "Edit " + name + "'s doze.hcl", Also: "config hcl", Do: Doing{Kind: "open", StackKey: key, Text: "file://" + st.Dir + "/doze.hcl"}},
			PaletteRow{Glyph: "▤", Tone: "dim", Label: "Show " + name + " in Finder", Note: display.Tilde(st.Dir), Also: "folder reveal", Do: Doing{Kind: "open", StackKey: key, Text: "file://" + st.Dir}},
			PaletteRow{Glyph: "›_", Tone: "dim", Label: "Open a terminal in " + name, Note: display.Tilde(st.Dir), Also: "shell cd", Do: Doing{Kind: "terminal", StackKey: key, Text: st.Dir}},
			PaletteRow{Glyph: "⧉", Tone: "dim", Label: "Copy the command to bring " + name + " up", Note: "cd " + display.Tilde(st.Dir) + " && doze up", Also: "cli", Do: Doing{Kind: "copy", StackKey: key, Text: "cd " + shellDir(st.Dir) + " && doze up"}},
		)
	}
	r := PaletteRow{Glyph: "⌫", Tone: "dim", Label: "Remove " + name + " from doze…", Note: "stops it and stops listing it; every file stays", Also: "forget delete", Do: Doing{Kind: "stack", StackKey: key, Verb: "remove"}}
	out = append(out, r)
	return out
}

// shellDir is a folder as a shell takes it.
func shellDir(dir string) string {
	if strings.ContainsAny(dir, " '\"$`\\") {
		return "'" + strings.ReplaceAll(dir, "'", `'\''`) + "'"
	}
	return dir
}

// appCommands is what the app itself can do, wherever you are.
func appCommands() []PaletteRow {
	app := func(label, glyph, id, key, also string) PaletteRow {
		return PaletteRow{Glyph: glyph, Tone: "dim", Label: label, Key: key, Also: also, Do: Doing{Kind: "app", Text: id}}
	}
	out := []PaletteRow{
		app("Home", "⌂", "home", "⌘0", "library stacks"),
		app("Needs you", "◉", "bell", "⌘⇧N", "problems alerts"),
		app("Settings", "⚙", "settings", "⌘,", "preferences"),
		app("Machine", "⚙", "settings:machine", "", "doctor setup"),
		{Glyph: "⌘O", Tone: "dim", Label: "Open folder…", Note: "add a project to Home", Key: "⌘O", Also: "add project", Do: Doing{Kind: "folder"}},
		app("New stack…", "+", "new", "", "create init"),
		app("Set up this Mac", "⚙", "repair", "", "repair doctor fix"),
		app("Clean up what was left behind", "✧", "cleanup", "", "zombies leftovers"),
		app("Keyboard shortcuts", "?", "help", "?", "keys help"),
		app("Copy doze's version", "⧉", "version", "", "about"),
	}
	for _, m := range []string{"system", "light", "dark"} {
		out = append(out, app("Mode: "+m, "◐", "mode:"+m, "", "appearance theme"))
	}
	for _, t := range []string{"violet", "teal", "burgundy", "amber", "slate"} {
		out = append(out, app("Theme: "+t, "●", "theme:"+t, "", "colour color"))
	}
	for _, d := range []string{"compact", "cosy", "roomy"} {
		out = append(out, app("Rows: "+d, "≡", "density:"+d, "", "density spacing"))
	}
	out = append(out, app("Open at login: on", "⏻", "login:on", "", "startup"), app("Open at login: off", "⏻", "login:off", "", "startup"))
	return out
}

// filterRows keeps the rows a query matches, best first, the label's
// matched runes marked. An empty query keeps them all, in order.
func filterRows(rows []PaletteRow, query string) []PaletteRow {
	query = strings.TrimSpace(query)
	if query == "" {
		return rows
	}
	type hit struct {
		row   PaletteRow
		score int
		order int
	}
	var hits []hit
	for i, r := range rows {
		if at, ok := display.FuzzyMatch(query, r.Label); ok {
			r.At = at
			hits = append(hits, hit{r, display.FuzzyScore(at), i})
			continue
		}
		if r.Also != "" && wordsMatch(query, r.Also) {
			hits = append(hits, hit{r, 1 << 20, i})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score < hits[j].score
		}
		return hits[i].order < hits[j].order
	})
	out := make([]PaletteRow, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.row)
	}
	return out
}

// wordsMatch says every word of the query starts a word of also.
func wordsMatch(query, also string) bool {
	words := strings.Fields(strings.ToLower(also))
	for _, q := range strings.Fields(strings.ToLower(query)) {
		found := false
		for _, w := range words {
			if strings.HasPrefix(w, q) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// scoped is the palette inside a stack (inst empty) or a service: its
// commands, and a stack's services to enter.
func scoped(st hub.StackView, inst, query string, now time.Time) ([]PaletteSection, []string) {
	crumbs := []string{st.Name}
	if inst != "" {
		snap := snapshot(st)
		v, ok := display.Find(snap, inst)
		if !ok {
			return nil, crumbs
		}
		crumbs = append(crumbs, inst)
		rows := filterRows(instanceCommands(st, v, now), query)
		if len(rows) == 0 {
			return nil, crumbs
		}
		return []PaletteSection{{Title: inst, Rows: rows}}, crumbs
	}
	var out []PaletteSection
	if rows := filterRows(stackCommands(st), query); len(rows) > 0 {
		out = append(out, PaletteSection{Title: st.Name, Rows: rows})
	}
	if st.Running {
		snap := snapshot(st)
		var svc []PaletteRow
		for _, r := range display.Rows(snap) {
			row := instanceRow(st, snap, r.V, now, "tab ›")
			row.Enter = st.Key + "#" + r.V.Name
			svc = append(svc, row)
		}
		if rows := filterRows(svc, query); len(rows) > 0 {
			out = append(out, PaletteSection{Title: "Services", Rows: rows})
		}
	}
	return out, crumbs
}

// suggested is two or three commands for where the person is: the service
// on screen, else the stack on screen, else what Home offers.
func suggested(stacks []hub.StackView, current, inst string, now time.Time) []PaletteRow {
	for _, st := range stacks {
		if st.Key != current {
			continue
		}
		if inst != "" {
			if v, ok := display.Find(snapshot(st), inst); ok {
				return headOf(instanceCommands(st, v, now), 4)
			}
		}
		// A stack's three: up, start a few, or edit when it is stopped;
		// sleep, read the logs, or restart when it runs.
		want := []string{"Sleep everything", "All logs", "Restart"}
		if !st.Running {
			want = []string{"Up ", "Choose what starts", "Edit "}
		}
		var out []PaletteRow
		for _, w := range want {
			for _, r := range stackCommands(st) {
				if strings.HasPrefix(r.Label, w) {
					out = append(out, r)
					break
				}
			}
		}
		return out
	}
	return nil
}

func headOf(rows []PaletteRow, n int) []PaletteRow {
	if len(rows) > n {
		return rows[:n]
	}
	return rows
}

// RecentCommand is a command done from the palette, kept in prefs so
// Recent is what you did, not where you went.
type RecentCommand struct {
	Glyph string `json:"glyph"`
	Label string `json:"label"`
	Note  string `json:"note,omitempty"`
	Do    Doing  `json:"do"`
}

// recentCommands are the kept commands still meaningful: their stack is
// still known.
func recentCommands(stacks []hub.StackView, kept []json.RawMessage) []PaletteRow {
	known := map[string]bool{"": true}
	for _, st := range stacks {
		known[st.Key] = true
	}
	var out []PaletteRow
	for _, raw := range kept {
		var c RecentCommand
		if json.Unmarshal(raw, &c) != nil || !known[c.Do.StackKey] {
			continue
		}
		out = append(out, PaletteRow{Glyph: c.Glyph, Tone: "dim", Label: c.Label, Note: c.Note, Do: c.Do})
		if len(out) == 4 {
			break
		}
	}
	return out
}
