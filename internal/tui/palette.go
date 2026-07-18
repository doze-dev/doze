package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/doze-dev/doze/internal/actions"
)

// palMaxRows caps the suggestion list overlaid above the prompt.
const palMaxRows = 8

// paletteLocals are the dash-only view verbs merged into the `:` suggestions.
// The palette is for fleet-level and view-level moves — wake/sleep everything,
// restart, reset, sync, theme, filter — plus `open`, the door to a service's
// web console (managing contents happens there, not in the dash).
var paletteLocals = []struct {
	name, summary string
	aliases       []string
	arg           bool // takes an (optional) argument → completing appends a space
}{
	{"open", "open the service's web console / URL in a browser", []string{"console"}, true},
	{"theme", "cycle the color theme, or switch to one by name", nil, true},
	{"filter", "filter the instance list; bare `filter` clears it", nil, true},
	{"apps", "the APPS view — htop for the process fleet", nil, false},
	{"help", "show the key & legend overlay", nil, false},
	{"quit", "leave the dash", nil, false},
}

// fuzzyScore ranks how well query q matches candidate s: -1 for no match, else
// higher is better. Match classes beat position beats brevity: exact (4) >
// prefix (3) > word-start after -_. or space (2) > substring (1) > subsequence
// (0); within a class, earlier first-hit and shorter candidates win.
func fuzzyScore(q, s string) int {
	q, s = strings.ToLower(q), strings.ToLower(s)
	if q == "" {
		// Everything matches an empty query with a FLAT score: the stable sort
		// then preserves curated order (locals as declared, registry order).
		return 4 * 1000
	}
	rank, first := -1, -1
	switch idx := strings.Index(s, q); {
	case s == q:
		rank, first = 4, 0
	case idx == 0:
		rank, first = 3, 0
	case idx > 0 && isWordStart(s, idx):
		rank, first = 2, idx
	case idx > 0:
		rank, first = 1, idx
	default: // subsequence: every query rune in order
		at := 0
		for _, r := range q {
			i := strings.IndexRune(s[at:], r)
			if i < 0 {
				return -1
			}
			if first < 0 {
				first = at + i
			}
			at += i + 1
		}
		rank = 0
	}
	// (rank+1)*1000 keeps every real match positive (-1 stays the only "no
	// match"); each penalty is clamped under 500 so classes never bleed.
	return (rank+1)*1000 - min(first, 400) - min(max(0, len(s)-len(q)), 400)
}

// isWordStart reports whether s[idx] begins a word (after - _ . or a space).
func isWordStart(s string, idx int) bool {
	switch s[idx-1] {
	case '-', '_', '.', ' ':
		return true
	}
	return false
}

// palSuggestion is one completable row of the palette's suggestion list.
type palSuggestion struct {
	insert  string // what Tab completes into the input
	label   string // primary column (verb or instance name)
	summary string // dim column (action summary or engine type)
	space   bool   // completing appends a trailing space (verb taking an arg)
}

// paletteSuggestions computes the rows for the current input, fuzzy-ranked:
// local verbs (console, send, theme, …) lead, registry actions follow, then
// instance-name completions once a verb and a space are in place.
func (m model) paletteSuggestions() []palSuggestion {
	verb, argPrefix, hasArg := strings.Cut(m.palInput, " ")
	if !hasArg { // verb position
		type scoredRow struct {
			s     palSuggestion
			score int
		}
		var locals, regs []scoredRow
		seen := map[string]bool{}
		// View verbs lead the list so the console family is always visible when
		// the palette opens — headline commands, not registry rows buried past
		// the row cap.
		for _, lv := range paletteLocals {
			sc := fuzzyScore(verb, lv.name)
			for _, al := range lv.aliases {
				sc = max(sc, fuzzyScore(verb, al))
			}
			if sc < 0 {
				continue
			}
			seen[lv.name] = true
			locals = append(locals, scoredRow{palSuggestion{insert: lv.name, label: lv.name, summary: lv.summary, space: lv.arg}, sc})
		}
		for _, a := range actions.Dash() {
			if seen[a.Name] {
				continue
			}
			sc := fuzzyScore(verb, a.Name)
			for _, al := range a.Aliases {
				sc = max(sc, fuzzyScore(verb, al))
			}
			if sc < 0 {
				continue
			}
			regs = append(regs, scoredRow{palSuggestion{
				insert: a.Name, label: a.Name, summary: a.Summary,
				space: a.Arg != actions.ArgNone,
			}, sc})
		}
		out := make([]palSuggestion, 0, len(locals)+len(regs))
		for _, grp := range [][]scoredRow{locals, regs} {
			sort.SliceStable(grp, func(a, b int) bool { return grp[a].score > grp[b].score })
			for _, r := range grp {
				out = append(out, r.s)
			}
		}
		return out
	}

	// Argument position.
	var engineOK func(string) bool // narrow instance rows to engines the verb works on
	switch lv := strings.ToLower(verb); lv {
	case "theme":
		var out []palSuggestion
		for _, t := range themes {
			if fuzzyScore(argPrefix, t.name) >= 0 {
				out = append(out, palSuggestion{insert: t.name, label: t.name, summary: "theme"})
			}
		}
		return out
	case "filter", "help", "quit", "q":
		return nil // free text / no argument
	default:
		if _, isLocal := findLocal(lv); !isLocal {
			act, ok := actions.Lookup(verb)
			if !ok || act.Arg == actions.ArgNone {
				return nil
			}
		}
	}
	type scoredRow struct {
		s     palSuggestion
		score int
	}
	var rows []scoredRow
	for _, in := range m.resp.Instances {
		if engineOK != nil && !engineOK(in.Engine) {
			continue
		}
		sc := fuzzyScore(argPrefix, in.Name)
		if sc < 0 {
			continue
		}
		rows = append(rows, scoredRow{palSuggestion{insert: in.Name, label: in.Name, summary: in.Engine}, sc})
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].score != rows[b].score {
			return rows[a].score > rows[b].score
		}
		return rows[a].s.label < rows[b].s.label
	})
	out := make([]palSuggestion, len(rows))
	for i, r := range rows {
		out[i] = r.s
	}
	return out
}

// findLocal resolves a palette local verb by name or alias.
func findLocal(v string) (int, bool) {
	for i, lv := range paletteLocals {
		if lv.name == v {
			return i, true
		}
		for _, al := range lv.aliases {
			if al == v {
				return i, true
			}
		}
	}
	return -1, false
}

// cappedSuggestions is paletteSuggestions cut to the rendered window.
func (m model) cappedSuggestions() []palSuggestion {
	s := m.paletteSuggestions()
	if len(s) > palMaxRows {
		s = s[:palMaxRows]
	}
	return s
}

// handlePaletteKey drives the prompt: printable characters edit, Tab/→ completes
// the highlighted suggestion, ↑↓ move it, Enter executes, Esc closes.
func (m model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sugs := m.cappedSuggestions()
	switch msg.String() {
	case "esc", "ctrl+c":
		m.paletteMode, m.palInput, m.palSel = false, "", 0
		return m, nil
	case "enter":
		// Enter runs the HIGHLIGHTED row whenever the typed text doesn't already
		// resolve on its own — arrowing to a suggestion and pressing Enter must
		// run that suggestion (requiring Tab first is not discoverable).
		if len(sugs) > 0 {
			s := sugs[clampi(m.palSel, 0, len(sugs)-1)]
			verb, arg, hasArg := strings.Cut(m.palInput, " ")
			if !hasArg {
				if !paletteVerbResolves(strings.TrimSpace(verb)) {
					m.palInput = s.insert
				}
			} else if a := strings.TrimSpace(arg); a != "" && fuzzyScore(a, s.insert) >= 0 {
				m.palInput = verb + " " + s.insert
			}
		}
		return m.paletteExec()
	case "up", "ctrl+p":
		if m.palSel > 0 {
			m.palSel--
		}
		return m, nil
	case "down", "ctrl+n":
		if m.palSel < len(sugs)-1 {
			m.palSel++
		}
		return m, nil
	case "tab": // complete the highlighted suggestion (not →: an arrow that
		// rewrites your input reads as a bug in an append-only field)
		if len(sugs) > 0 {
			s := sugs[clampi(m.palSel, 0, len(sugs)-1)]
			if verb, _, hasArg := strings.Cut(m.palInput, " "); hasArg {
				m.palInput = verb + " " + s.insert
			} else {
				m.palInput = s.insert
				if s.space {
					m.palInput += " "
				}
			}
			m.palSel = 0
		}
		return m, nil
	case "backspace":
		if r := []rune(m.palInput); len(r) > 0 {
			m.palInput = string(r[:len(r)-1])
		}
		m.palSel = 0
		return m, nil
	}
	switch {
	case msg.Type == tea.KeyRunes:
		m.palInput += string(msg.Runes)
		m.palSel = 0
	case msg.String() == " ": // the space key arrives as its own key type
		m.palInput += " "
		m.palSel = 0
	}
	return m, nil
}

// paletteVerbResolves reports whether typed text already names a runnable verb
// on its own — a local view verb or a registry action/alias. When it doesn't,
// Enter adopts the highlighted suggestion instead of failing on a prefix.
func paletteVerbResolves(v string) bool {
	if v == "" {
		return false
	}
	lv := strings.ToLower(v)
	if lv == "q" {
		return true
	}
	for _, l := range paletteLocals {
		if l.name == lv {
			return true
		}
		for _, al := range l.aliases {
			if al == lv {
				return true
			}
		}
	}
	_, ok := actions.Lookup(lv)
	return ok
}

// paletteExec parses and runs the typed command: local view verbs first, then
// the action registry (aliases like :boot / :reap / :destroy resolve there).
func (m model) paletteExec() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.palInput)
	m.paletteMode, m.palInput, m.palSel = false, "", 0
	if input == "" {
		return m, nil
	}
	verb, arg, _ := strings.Cut(input, " ")
	arg = strings.TrimSpace(arg)

	switch strings.ToLower(verb) {
	case "apps":
		m.appsMode, m.appsSel, m.appsPending = true, 0, ""
		m.setFlash(stDim, "APPS · esc returns to the fleet")
		return m, nil
	case "open", "console":
		v, ok := m.selected()
		if arg != "" {
			if v, ok = m.instanceByName(arg); !ok {
				m.setFlashErr("✗ unknown service " + arg)
				return m, nil
			}
		}
		if !ok {
			m.setFlashErr("✗ :open needs a service name")
			return m, nil
		}
		return m.openWeb(v)
	case "theme":
		if arg == "" {
			applyTheme(activeTheme + 1)
		} else {
			found := -1
			for i, t := range themes {
				if t.name == strings.ToLower(arg) {
					found = i
				}
			}
			if found < 0 {
				m.setFlashErr("✗ no theme " + arg + " — " + themeNames())
				return m, nil
			}
			applyTheme(found)
		}
		saveTheme()
		m.setFlash(stAccent, "theme · "+themes[activeTheme].name)
		return m, nil
	case "filter":
		m.filter.SetValue(arg)
		m.cursor = 0
		if arg == "" {
			m.setFlash(stDim, "filter cleared")
		} else {
			m.setFlash(stDim, "filter · "+arg)
		}
		return m, nil
	case "help":
		m.showHelp = true
		return m, nil
	case "quit", "q":
		return m, tea.Quit
	}

	act, ok := actions.Lookup(verb)
	if !ok {
		m.setFlashErr("✗ unknown command :" + verb + " — try :help")
		return m, nil
	}
	name := arg
	switch {
	case name != "" && act.Arg != actions.ArgNone:
		if _, ok := m.instanceByName(name); !ok {
			m.setFlashErr("✗ unknown service " + name)
			return m, nil
		}
	case name == "" && act.Arg == actions.ArgInstanceRequired:
		v, ok := m.selected() // default to the selection, like the keybindings
		if !ok {
			m.setFlashErr("✗ :" + act.Name + " needs a service name")
			return m, nil
		}
		name = v.Name
	}
	if act.Confirm { // destructive → the same y/n footer confirm as d / R
		m.dashPending = act.Op + ":" + name
		return m, nil
	}
	return m.runAction(act, name)
}

// runAction executes a resolved registry action (after any confirm).
func (m model) runAction(act actions.Action, name string) (tea.Model, tea.Cmd) {
	if act.Kind == actions.KindOp {
		if name == "" && !act.OpAcceptsAll {
			// The registry says this op's handler has no empty-means-all (boot,
			// today) — fan out over the fleet client-side like the CLI does.
			var cmds []tea.Cmd
			for _, in := range m.resp.Instances {
				if in.Disabled {
					continue
				}
				cmds = append(cmds, do(m.client, act.Op, in.Name))
			}
			if len(cmds) == 0 {
				m.setFlash(stDim, "nothing to "+act.Name)
				return m, nil
			}
			m.setFlash(stDim, fmt.Sprintf("%s: %d service(s)…", act.Name, len(cmds)))
			return m, tea.Batch(cmds...)
		}
		m.setFlash(stDim, act.Name+" "+orAllServices(name)+"…")
		return m, do(m.client, act.Op, name)
	}
	// KindLocal — the dash's own plumbing.
	switch act.Name {
	case "url":
		v, _ := m.instanceByName(name)
		url := connectLine(v)
		if url == "" {
			m.setFlash(stDim, "nothing to connect to on "+name)
			return m, nil
		}
		if err := clipboard.WriteAll(url); err != nil {
			m.setFlashErr("✗ copy failed: " + err.Error())
		} else {
			m.setFlash(stGreen, "✓ copied "+name+" connect")
		}
		return m, nil
	case "logs":
		if name != "" {
			if !m.selectInstance(name) {
				m.setFlashErr("✗ unknown service " + name)
				return m, nil
			}
			return m, m.onSelect()
		}
		m.logVP.GotoBottom()
		return m, nil
	}
	m.setFlashErr("✗ :" + act.Name + " isn't available in the dash")
	return m, nil
}

// themeNames lists the switchable themes for the :theme error hint.
func themeNames() string {
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.name
	}
	return strings.Join(names, "/")
}

// paletteView is the Spotlight panel: a bordered box, ~60% of the window (min
// 46 cols), input on top, the suggestion rows beneath.
func (m model) paletteView() string {
	w := clampi(m.width*3/5, 46, max(46, m.width-4))
	inner := w - 8 // border (2) + padding (2×3) — horizontal padding matches the other overlays
	sugs := m.cappedSuggestions()
	sel := clampi(m.palSel, 0, max(0, len(sugs)-1))

	prompt := stAccent.Bold(true).Render(": ") + stText.Render(m.palInput) + stAccent.Render("▌")
	lines := []string{truncate(prompt, inner)}
	if len(sugs) > 0 {
		lines = append(lines, stFaint.Render(strings.Repeat("╌", max(1, inner))))
		nameW := 0
		for _, s := range sugs {
			if lw := lipgloss.Width(s.label); lw > nameW {
				nameW = lw
			}
		}
		hl := lipgloss.NewStyle().Background(cSel).Foreground(cText)
		for i, s := range sugs {
			pad := strings.Repeat(" ", nameW-lipgloss.Width(s.label)+2)
			if i == sel { // reverse video under NO_COLOR, tinted row otherwise
				lines = append(lines, selStyled(hl, truncate("▸ "+s.label+pad+s.summary, inner)))
				continue
			}
			row := "  " + stAccent.Render(s.label) + pad +
				stDim.Render(truncate(s.summary, max(4, inner-nameW-4)))
			lines = append(lines, truncate(row, inner))
		}
	} else if strings.TrimSpace(m.palInput) != "" {
		lines = append(lines, stDim.Render("no matches"))
	}
	lines = append(lines, stDim.Render(truncate("↵ run · ⇥ complete · esc close", inner)))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
		Padding(0, 3).Width(w - 2).
		Render(strings.Join(lines, "\n"))
}
