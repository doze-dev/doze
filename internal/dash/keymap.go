package dash

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
)

// The keys of each page, said once. The bar along the bottom and the help
// overlay (?) are both drawn from the same list, so they cannot disagree about
// what a key does, and a key is never in one and not the other.

// hint is one key: how to show it, what it does in a word or two, what it does
// in a sentence (for the overlay), and how long it survives a narrow window:
// the higher the rank the sooner it goes. Rank 0 never goes.
type hint struct {
	key, label, help string
	rank             int
}

// bar draws hints as the key line, dropping the highest-ranked (and, among
// equals, the last) until it fits beside extra.
func (m *Model) bar(hs []hint, w int, extra string) string {
	s := m.st
	hs = append([]hint(nil), hs...)
	build := func() string {
		parts := make([]string, len(hs))
		for i, h := range hs {
			parts[i] = s.cap(h.key, h.label)
		}
		return strings.Join(parts, "  ")
	}
	line := build()
	for len(hs) > 1 && width(line)+width(extra) > w {
		worst := -1
		for i, h := range hs {
			if h.rank > 0 && (worst < 0 || h.rank >= hs[worst].rank) {
				worst = i
			}
		}
		if worst < 0 {
			break
		}
		hs = append(hs[:worst], hs[worst+1:]...)
		line = build()
	}
	if room := w - width(line); extra != "" && width(extra) > room {
		extra = ansi.Truncate(extra, max(room, 0), "…")
	}
	return line + extra
}

// homeHints are the keys of Home.
func (m *Model) homeHints() []hint {
	restart, restartHelp := "restart", "stop and start it; asks first when something is using it"
	if v, ok := m.selected(); ok && v.Kind == "task" {
		restart, restartHelp = "rerun", "run the task again, whatever its state"
	}
	return []hint{
		{"↑↓", "move", "move the selection; the wheel does too; click selects, a second click opens", 2},
		{"⏎", "focus", "open the selected instance in full", 2},
		{"w", "wake", "start it now, and what it needs", 2},
		{"s", "sleep", "stop it, keeping its data; asks first when something is using it", 2},
		{"r", restart, restartHelp, 2},
		{"l", "logs", "this instance's logs", 3},
		{"L", "all logs", "every instance's logs, merged in time order", 5},
		{"y", "copy", "send the address to the clipboard (Y writes a file instead)", 4},
		{"o", "sort " + sortTitles[m.sortBy], "sort within each group: name, memory, state", 4},
		{"t", "tree", "show what needs what, as a tree", 4},
		{"/", "filter", "narrow the table to rows matching every word typed", 3},
		{"v", "select", "select lines of the logs beside or under the list, to copy them; or drag with the mouse", 4},
		{":", "command", "the palette: every action, with its command-line form", 1},
		{"?", "help", "this list", 0},
		{"q", "quit", "leave; the stack keeps running", 1},
	}
}

// focusHints are the keys of an instance's page, for its current tab.
func (m *Model) focusHints(v control.InstanceView, ts []tab, cur tab) []hint {
	var hs []hint
	if len(ts) > 1 {
		hs = append(hs, hint{"tab", "next tab", "the next tab of this instance (1-5 go straight to one)", 2})
	}
	logs := cur.id == tabLogs
	if strings.HasPrefix(cur.id, "console:") && m.con != nil {
		hs = append(hs, hint{"↑↓", "select", "move through the rows of the focused table", 2})
		for _, a := range m.con.actions {
			hs = append(hs, hint{actionKey(a), strings.ToLower(a.Label), "the module's action: " + a.Label, 3})
		}
	}
	if cur.id == tabWeb {
		hs = append(hs, hint{"⏎", "open in browser", "open its own console in the browser", 2})
	} else if hasWeb(v) {
		hs = append(hs, hint{"o", "open console", "open its own console in the browser", 4})
	}
	if !logs && v.Kind != "task" {
		hs = append(hs, hint{"l", "logs", "go to its logs", 3})
	}
	if logs {
		hs = append(hs,
			hint{"↑↓", "scroll", "back through the lines kept; PgUp PgDn a page, g the oldest, G follows again; the wheel too", 2},
			hint{"f", "level", "cycle the lowest level shown", 4},
			hint{"/", "search", "only the lines holding what is typed", 3},
			hint{"t", "time", "log times as the clock or as how long ago", 4},
			hint{"v", "select", "select lines to copy or read whole; or drag with the mouse", 4})
		if len(m.runIDs()) > 1 {
			hs = append(hs, hint{"[ ]", "earlier runs", "move between this instance's runs", 4})
		}
		hs = append(hs, hint{"y", "copy lines", "send the lines shown to the clipboard (Y writes a file)", 4})
	} else if m.url(v) != "" {
		hs = append(hs, hint{"y", "copy address", "send the address to the clipboard (Y writes a file)", 4})
	}
	if v.Kind == "task" {
		hs = append(hs, hint{"r", "run now", "run the task again", 2})
	} else {
		hs = append(hs,
			hint{"w", "wake", "start it now", 2},
			hint{"s", "sleep", "stop it, keeping its data; asks first when something is using it", 2},
			hint{"r", "restart", "stop and start it; asks first when something is using it", 2})
		if v.Kind == "module" || v.Kind == "" {
			hs = append(hs, hint{"p", "pin awake", "keep it awake past the idle time", 4})
		}
	}
	return append(hs,
		hint{":", "command", "the palette: every action, with its command-line form", 1},
		hint{"?", "help", "this list", 0},
		hint{"esc", "back", "back to the stack", 0})
}

// logsPageHints are the keys of the stack's logs.
func (m *Model) logsPageHints() []hint {
	return []hint{
		{"↑↓", "scroll", "back through the lines kept; PgUp PgDn a page, g the oldest, G follows again; the wheel too", 2},
		{"/", "search", "only the lines holding what is typed", 3},
		{"f", "level", "cycle the lowest level shown", 4},
		{"t", "time", "log times as the clock or as how long ago", 4},
		{"v", "select", "select lines to copy or read whole; or drag with the mouse", 4},
		{"space", "pause", "hold still while reading; space again follows", 4},
		{"y", "copy", "send the lines shown to the clipboard (Y writes a file)", 4},
		{":", "command", "the palette", 1},
		{"?", "help", "this list", 0},
		{"esc", "back", "back to the stack", 0},
	}
}

// pageHints are the keys of whatever page is showing.
func (m *Model) pageHints() []hint {
	switch m.page {
	case pageLogs:
		return m.logsPageHints()
	case pageFocus:
		if v, ok := m.selected(); ok {
			ts := m.tabs(v)
			if m.tab >= len(ts) {
				return m.homeHints()
			}
			return m.focusHints(v, ts, ts[m.tab])
		}
	}
	return m.homeHints()
}

// helpLines is the help overlay: this page's keys, the keys everywhere, the
// mouse, and what the meters are.
func (m *Model) helpLines(int) []string {
	s := m.st
	pad := func(k string) string { return s.key.Render(k) + strings.Repeat(" ", max(10-width(k)-2, 1)) }
	var out []string
	title := map[int]string{pageHome: "Home", pageFocus: "This page", pageLogs: "All logs"}[m.page]
	out = append(out, s.a.Bold(true).Render(title))
	for _, h := range m.pageHints() {
		out = append(out, pad(h.key)+s.w.Render(fit(h.label, 14))+" "+s.d.Render(h.help))
	}
	out = append(out, "", s.a.Bold(true).Render("Everywhere"),
		pad("esc")+s.w.Render(fit("back", 14))+" "+s.d.Render("leave a page, clear a filter or a search, close a box"),
		pad("ctrl+c")+s.w.Render(fit("quit", 14))+" "+s.d.Render("leave the dash"),
		"", s.a.Bold(true).Render("Mouse"),
		pad("click")+s.w.Render(fit("select", 14))+" "+s.d.Render("a row; a second click opens it"),
		pad("wheel")+s.w.Render(fit("scroll", 14))+" "+s.d.Render("the logs, or the selection on Home"),
		pad("drag")+s.w.Render(fit("select lines", 14))+" "+s.d.Render("over logs, to copy them"),
		pad("shift")+s.w.Render(fit("native select", 14))+" "+s.d.Render("hold it to drag-select any text (the dash holds the mouse)"),
		"", s.a.Bold(true).Render("What the meters are"),
		s.d.Render("  mem      what the awake instances hold now (resident size from ps, summed; shared pages are counted in each)"),
		s.d.Render("  cpu      cores in use: 1 core is one core fully busy, so 2.5 cores is two and a half busy; from ps, a recent average"),
		s.d.Render("  bars     the whole bar is what is consumed, cut by instance, biggest first; the line under names each colour"),
		s.d.Render("  charts   a reading a second; \"last 4m\" is how much the daemon has kept"),
	)
	return out
}
