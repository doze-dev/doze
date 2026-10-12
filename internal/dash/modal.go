package dash

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
)

// A modal sits over the dimmed page: the command palette (Palette.dc.html) and
// the two confirmations (Confirm-Sleep.dc.html, Confirm-Reset.dc.html).

type modalKind int

const (
	modalNone modalKind = iota
	modalPalette
	modalSleep
	modalRestart // restarting something that is in use
	modalReset
	modalInput // a module's action that takes some text
	modalText  // some lines read whole, wrapped, with nothing cut off
	modalHelp  // every key of the page, and what the meters mean
	modalAsk   // a module's action that deletes something
)

type modal struct {
	kind   modalKind
	target string // the instance a confirmation is about

	// the palette
	input string
	pick  int

	// a confirmation
	btn   int
	typed string

	// the text of a modalText: its lines, and how far down it is read
	lines []string
	off   int

	// a module's action: what it is called, what it wants, and what to do with it
	title string
	hint  string
	then  func(input string) tea.Cmd
}

// action is one row of the palette: a sentence, what to say beside it, the
// command a shell would use to do the same, and what to do.
type action struct {
	rank  int // 0 the selected instance, 1 the whole stack, 2 another instance
	label string
	note  string
	bad   bool // the note is a warning
	cli   string
	run   func(m *Model) tea.Cmd
}

// actions lists everything the palette can do, the selected instance's first.
// Each has a command in the CLI: the right column is how a script does it.
func (m *Model) actions() []action {
	var out []action
	rank := 2
	add := func(a action) { a.rank = rank; out = append(out, a) }

	forInstance := func(v control.InstanceView) {
		n := v.Name
		if v.Kind == "task" {
			verb := "run task "
			if v.Task != nil {
				verb = "rerun task "
			}
			add(action{label: verb + n, cli: "doze task " + n, run: func(m *Model) tea.Cmd { return m.do(control.VerbTask, n) }})
		} else {
			if !awake(v) {
				add(action{label: "wake " + n, cli: "doze wake " + n, run: func(m *Model) tea.Cmd { return m.do(control.VerbWake, n) }})
			}
			if awake(v) {
				add(action{label: "sleep " + n, cli: "doze sleep " + n, run: func(m *Model) tea.Cmd { return m.askSleep(n) }})
				add(action{label: "restart " + n, cli: "doze restart " + n, run: func(m *Model) tea.Cmd { return m.askRestart(n) }})
			}
			pin := "pin " + n + " awake"
			if v.KeepAwake {
				pin = "unpin " + n
			}
			add(action{label: pin, cli: "doze pin " + n, run: func(m *Model) tea.Cmd { return m.do(control.VerbPin, n) }})
			add(action{label: "reset " + n, note: "wipes its data · asks first", bad: true, cli: "doze reset " + n,
				run: func(m *Model) tea.Cmd { m.modal = &modal{kind: modalReset, target: n, btn: 1}; return nil }})
		}
		add(action{label: "show the logs of " + n, cli: "doze logs " + n, run: func(m *Model) tea.Cmd {
			m.sel = n
			m.page = pageFocus
			for i, t := range m.tabs(v) {
				if t.id == tabLogs || t.id == tabLastRun {
					m.tab = i
				}
			}
			return nil
		}})
		if line := orStr(v.URL, v.Endpoint); line != "" && v.Kind != "task" {
			add(action{label: "copy the connect line of " + n, cli: "doze url " + n, run: func(m *Model) tea.Cmd { return m.out(line, "the connect line of "+n, n+"-connect", false) }})
		}
	}

	rs := rows(m.snap)
	rank = 0
	if v, ok := m.selected(); ok {
		forInstance(v)
	}
	rank = 1
	add(action{label: "wake everything", cli: "doze wake", run: func(m *Model) tea.Cmd { return m.do(control.VerbWake, "") }})
	add(action{label: "sleep everything that is awake", cli: "doze sleep", run: func(m *Model) tea.Cmd { return m.do(control.VerbSleep, "") }})
	add(action{label: "bring the stack up", cli: "doze up", run: func(m *Model) tea.Cmd { return m.do(control.VerbUp, "") }})
	add(action{label: "apply the changes in doze.hcl", cli: "doze sync", run: func(m *Model) tea.Cmd { return m.do(control.VerbSync, "") }})
	rank = 2
	for _, awakeFirst := range []bool{true, false} {
		for _, r := range rs {
			if r.v.Name != m.sel && awake(r.v) == awakeFirst {
				forInstance(r.v)
			}
		}
	}
	return out
}

// do runs a verb on target ("" is the whole stack) and refreshes.
func (m *Model) do(verb, target string) tea.Cmd {
	if c := m.refuse(); c != nil {
		return c
	}
	d := m.d
	return func() tea.Msg {
		return doneMsg{verb, target, d.Do(contextBackground(), control.Verb{Name: verb, Target: target})}
	}
}

// askSleep sleeps target, asking first when something is using it.
func (m *Model) askSleep(target string) tea.Cmd {
	v, ok := m.find(target)
	if ok && inUse(v) != "" {
		m.modal = &modal{kind: modalSleep, target: target}
		return nil
	}
	return m.do(control.VerbSleep, target)
}

// match is how the palette filters: each word typed must begin a word of s, in
// order. "re" finds restart, reset and rerun and not "wake worker", whose letters
// are there but nowhere a word starts; "reset d" finds reset db. It returns the
// positions in s of the letters matched.
func match(query, s string) ([]int, bool) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil, true
	}
	rs := []rune(strings.ToLower(s))
	var at []int
	pos, t := 0, 0
	for t < len(terms) {
		found := false
		for p := pos; p < len(rs); p++ {
			if p > 0 && rs[p-1] != ' ' {
				continue
			}
			term := []rune(terms[t])
			if p+len(term) <= len(rs) && string(rs[p:p+len(term)]) == terms[t] {
				for i := range term {
					at = append(at, p+i)
				}
				pos, found = p+len(term), true
				break
			}
		}
		if !found {
			return nil, false
		}
		t++
	}
	return at, true
}

// score ranks matches: letters together, and near the front, first.
func score(at []int) int {
	if len(at) == 0 {
		return 0
	}
	n := at[0]
	for i := 1; i < len(at); i++ {
		n += at[i] - at[i-1] - 1
	}
	return n
}

type hit struct {
	a  action
	at []int
}

func (m *Model) hits() []hit {
	var out []hit
	seen := map[string]bool{}
	for _, a := range m.actions() {
		// A couple of letters match one verb on every instance; the first of
		// each is enough until the person says which.
		if verb, _, _ := strings.Cut(a.label, " "); a.rank == 2 && len(m.modal.input) < 3 {
			if seen[verb] {
				continue
			}
			seen[verb] = true
		}
		// With nothing typed the palette offers what is at hand: the selected
		// instance and the stack. Typing reaches every instance.
		if m.modal.input == "" && a.rank > 1 {
			continue
		}
		if at, ok := match(m.modal.input, a.label); ok {
			out = append(out, hit{a, at})
		}
	}
	if m.modal.input != "" {
		sort.SliceStable(out, func(i, j int) bool {
			if si, sj := score(out[i].at), score(out[j].at); si != sj {
				return si < sj
			}
			return out[i].a.rank < out[j].a.rank
		})
	}
	return out
}

// --- keys ---

func (m *Model) modalKey(k, text string) tea.Cmd {
	switch m.modal.kind {
	case modalPalette:
		return m.paletteKey(k, text)
	case modalRestart:
		return m.restartKey(k)
	case modalSleep:
		return m.sleepKey(k)
	case modalReset:
		return m.resetKey(k, text)
	case modalInput, modalAsk:
		return m.askKey(k, text)
	case modalText:
		return m.textKey(k)
	case modalHelp:
		return m.helpKey(k)
	}
	return nil
}

func (m *Model) paletteKey(k, text string) tea.Cmd {
	md := m.modal
	hs := m.hits()
	switch k {
	case "esc", "ctrl+c":
		m.modal = nil
	case "up", "ctrl+p":
		if md.pick > 0 {
			md.pick--
		}
	case "down", "ctrl+n":
		if md.pick < len(hs)-1 {
			md.pick++
		}
	case "backspace":
		if r := []rune(md.input); len(r) > 0 {
			md.input = string(r[:len(r)-1])
			md.pick = 0
		}
	case "ctrl+u":
		md.input, md.pick = "", 0
	case "enter":
		if md.pick < len(hs) {
			a := hs[md.pick].a
			m.modal = nil
			return a.run(m)
		}
	case "tab":
		if md.pick < len(hs) {
			return m.out(hs[md.pick].a.cli, "the command", "command", false)
		}
	case "space":
		md.input += " "
		md.pick = 0
	default:
		if text != "" && unicode.IsPrint([]rune(text)[0]) {
			md.input += text
			md.pick = 0
		}
	}
	return nil
}

// sleepButtons are the choices of Confirm-Sleep: with what depends on it, or
// without.
func (m *Model) sleepButtons(v control.InstanceView) []string {
	if deps := dependants(v); len(deps) > 0 {
		return []string{"Sleep " + v.Name + " and " + strings.Join(deps, ", "), "Sleep " + v.Name + " only", "Cancel"}
	}
	return []string{"Sleep " + v.Name, "Cancel"}
}

func dependants(v control.InstanceView) []string {
	var out []string
	for _, h := range v.Holders {
		if h.Kind == "dependant" {
			out = append(out, h.Name)
		}
	}
	return out
}

func (m *Model) sleepKey(k string) tea.Cmd {
	md := m.modal
	v, ok := m.find(md.target)
	if !ok {
		m.modal = nil
		return nil
	}
	btns := m.sleepButtons(v)
	switch k {
	case "esc":
		m.modal = nil
	case "left", "shift+tab", "h":
		if md.btn > 0 {
			md.btn--
		}
	case "right", "tab", "l":
		md.btn = (md.btn + 1) % len(btns)
	case "enter":
		m.modal = nil
		deps := dependants(v)
		switch {
		case btns[md.btn] == "Cancel":
			return nil
		case len(deps) > 0 && md.btn == 0:
			// what depends on it first, then it
			d, target := m.d, v.Name
			return func() tea.Msg {
				var err error
				for _, n := range deps {
					if err = d.Do(contextBackground(), control.Verb{Name: control.VerbSleep, Target: n}); err != nil {
						break
					}
				}
				if err == nil {
					err = d.Do(contextBackground(), control.Verb{Name: control.VerbSleep, Target: target})
				}
				return doneMsg{control.VerbSleep, target, err}
			}
		default:
			return m.do(control.VerbSleep, v.Name)
		}
	}
	return nil
}

func (m *Model) resetKey(k, text string) tea.Cmd {
	md := m.modal
	confirmed := md.typed == md.target
	switch k {
	case "esc":
		m.modal = nil
	case "tab":
		md.btn = (md.btn + 1) % 2
	case "backspace":
		if r := []rune(md.typed); len(r) > 0 {
			md.typed = string(r[:len(r)-1])
		}
	case "enter":
		switch {
		case md.btn == 1:
			m.modal = nil
		case confirmed:
			m.modal = nil
			return m.do(control.VerbReset, md.target)
		}
	default:
		if text != "" && unicode.IsPrint([]rune(text)[0]) {
			md.typed += text
			md.btn = 0
		}
	}
	return nil
}

// --- drawing ---

// overlay puts modal over base, x cells in and y lines down.
func overlay(base, modal []string, x, y int) []string {
	out := append([]string(nil), base...)
	for i, ml := range modal {
		j := y + i
		if j < 0 || j >= len(out) {
			continue
		}
		mw := width(ml)
		left := ansi.Truncate(out[j], x, "")
		if n := x - width(left); n > 0 {
			left += strings.Repeat(" ", n)
		}
		out[j] = left + ml + ansi.TruncateLeft(out[j], x+mw, "")
	}
	return out
}

// dim draws a page as the board dims it behind a modal: colour gone.
func (m *Model) dim(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = m.st.d.Render(ansi.Strip(l))
	}
	return out
}

// withModal draws the open modal over the page lines.
func (m *Model) withModal(base []string, w, h int) []string {
	if m.modal == nil {
		return base
	}
	var box []string
	switch m.modal.kind {
	case modalPalette:
		box = m.paletteBox(w, h)
	case modalSleep:
		box = m.sleepBox(w)
	case modalRestart:
		box = m.restartBox(w)
	case modalReset:
		box = m.resetBox(w)
	case modalInput, modalAsk:
		box = m.askBox(w)
	case modalText:
		box = m.textBox(w, h)
	case modalHelp:
		box = m.helpBox(w, h)
	}
	if len(box) == 0 {
		return base
	}
	x := (w - width(box[0])) / 2
	if x < 0 {
		x = 0
	}
	y := (h - len(box)) / 3
	if m.modal.kind == modalPalette {
		y = 4
	}
	if y+len(box) > h-1 {
		y = h - 1 - len(box)
	}
	if y < 0 {
		y = 0
	}
	out := overlay(base, box, x, y)
	// A confirmation's keys replace the page's in the bar along the bottom, dim,
	// as on the boards.
	if m.modal.kind != modalPalette && len(out) > 0 {
		f := fadedStyles(m.st.plain)
		out[len(out)-1] = f.cap("←→", "choose") + "  " + f.cap("⏎", "confirm") + "  " + f.cap("esc", "cancel")
		if m.modal.kind == modalReset {
			out[len(out)-1] = f.cap("tab", "buttons") + "  " + f.cap("⏎", "confirm") + "  " + f.cap("esc", "cancel")
		}
		if m.modal.kind == modalText {
			out[len(out)-1] = f.cap("↑↓", "read") + "  " + f.cap("y", "copy") + "  " + f.cap("esc", "close")
		}
		if m.modal.kind == modalHelp {
			out[len(out)-1] = f.cap("↑↓", "read") + "  " + f.cap("esc", "close")
		}
	}
	return out
}

func (m *Model) paletteBox(w, h int) []string {
	s := m.st
	bw := w * 69 / 100
	if bw > 110 {
		bw = 110
	}
	if bw < 50 {
		bw = w
	}
	inner := bw - 4
	hs := m.hits()
	md := m.modal
	if md.pick >= len(hs) {
		md.pick = 0
	}
	room := h - 10
	if room > 8 {
		room = 8
	}
	if room < 3 {
		room = 3
	}
	first := 0
	if md.pick >= room {
		first = md.pick - room + 1
	}

	lines := []string{s.a.Bold(true).Render(":") + " " + s.w.Render(md.input) + s.a.Render("▌"), s.border.Render(strings.Repeat("─", inner))}
	cliW := 0
	for _, h := range hs {
		if n := width(h.a.cli); n > cliW {
			cliW = n
		}
	}
	if cliW > inner/2 {
		cliW = inner / 2
	}
	if len(hs) == 0 {
		lines = append(lines, s.d.Render("nothing does that"))
	}
	for i := first; i < len(hs) && i < first+room; i++ {
		h := hs[i]
		picked := i == md.pick
		paint := func(st lipgloss.Style) lipgloss.Style {
			if picked {
				return s.on(st)
			}
			return st
		}
		label := highlight(s, h.a.label, h.at, picked)
		if h.a.note != "" {
			ns := s.d
			if h.a.bad {
				ns = s.r
			}
			label += paint(lipgloss.NewStyle()).Render(" ") + paint(ns).Render(h.a.note)
		}
		cli := paint(s.d).Render(fit(h.a.cli, cliW))
		mark := paint(lipgloss.NewStyle()).Render("  ")
		if picked {
			mark = s.on(s.a).Render("▸") + s.on(lipgloss.NewStyle()).Render(" ")
		}
		gap := inner - 2 - width(label) - cliW
		if gap < 1 {
			gap = 1
		}
		line := mark + label + paint(lipgloss.NewStyle()).Render(strings.Repeat(" ", gap)) + cli
		lines = append(lines, line)
	}
	lines = append(lines, s.border.Render(strings.Repeat("─", inner)))
	foot := s.d.Render("Every row is a shell command. The right column shows it.")
	keys := s.cap("⏎", "run") + "  " + s.cap("tab", "copy command")
	gap := inner - width(foot) - width(keys)
	if gap < 1 {
		gap = 1
	}
	lines = append(lines, foot+strings.Repeat(" ", gap)+keys)
	return box(s.borderOn, "", "", lines, bw, len(lines)+2)
}

func highlight(s styles, label string, at []int, picked bool) string {
	set := map[int]bool{}
	for _, p := range at {
		set[p] = true
	}
	hit, rest := s.a.Bold(true), s.text
	if picked {
		hit, rest = s.on(hit), s.on(rest)
	}
	var b strings.Builder
	for i, r := range []rune(label) {
		if set[i] {
			b.WriteString(hit.Render(string(r)))
		} else {
			b.WriteString(rest.Render(string(r)))
		}
	}
	return b.String()
}

func (m *Model) buttons(names []string, cur int, off int) string {
	s := m.st
	var parts []string
	for i, n := range names {
		switch {
		case i == off:
			parts = append(parts, s.pillQ.Foreground(s.d.GetForeground()).Render(n))
		case i == cur:
			parts = append(parts, s.pill.Render(n))
		default:
			parts = append(parts, s.pillQ.Render(n))
		}
	}
	return strings.Join(parts, " ")
}

// sleepBox is Confirm-Sleep.dc.html.
func (m *Model) sleepBox(w int) []string {
	s := m.st
	v, ok := m.find(m.modal.target)
	if !ok {
		return nil
	}
	bw := w * 66 / 100
	if bw > 100 {
		bw = 100
	}
	if bw < 56 {
		bw = w
	}
	label := func(k string) string { return s.d.Render(fit(k, 13)) + " " }
	var lines []string
	conns := 0
	for _, h := range v.Holders {
		if h.Kind == "connection" {
			conns = h.Count
		}
	}
	deps := dependants(v)
	var sentence string
	switch {
	case len(deps) > 0 && conns > 0:
		sentence = s.w.Render(strings.Join(deps, ", ")) + " is running and needs it, and " + s.w.Render(fmt.Sprintf("%d connection%s", conns, plural(conns))) + " are open to it right now."
	case len(deps) > 0:
		sentence = s.w.Render(strings.Join(deps, ", ")) + " is running and needs it."
	default:
		sentence = s.w.Render(fmt.Sprintf("%d connection%s", conns, plural(conns))) + " open to it right now."
	}
	lines = append(lines, "", s.text.Render(sentence), "",
		label("what happens")+s.text.Render("Connections drop. The next connection wakes it again,"),
		label("")+s.text.Render("so it is back within seconds unless what uses it sleeps too."),
		label("data")+s.text.Render("Kept. Nothing is deleted."),
		"",
		m.buttons(m.sleepButtons(v), m.modal.btn, -1),
		"",
		s.border.Render(strings.Repeat("─", bw-4)),
		s.d.Render("doze only asks when something else is affected. Sleeping an idle service never asks."),
		"",
	)
	return box(s.borderOn, s.a.Bold(true).Render("sleep "+v.Name+"?"), "", lines, bw, len(lines)+2)
}

// resetBox is Confirm-Reset.dc.html.
func (m *Model) resetBox(w int) []string {
	s := m.st
	v, ok := m.find(m.modal.target)
	if !ok {
		return nil
	}
	bw := w * 66 / 100
	if bw > 100 {
		bw = 100
	}
	if bw < 56 {
		bw = w
	}
	label := func(k string) string { return s.d.Render(fit(k, 9)) + " " }
	folder := tilde(v.DataDir)
	if folder == "" {
		folder = "its data folder"
	}
	lines := []string{
		label("deletes") + s.w.Render("everything in "+folder),
		label("stops") + s.text.Render(v.Name+" if it is awake"),
		label("then") + s.text.Render(v.Name+" starts empty on its next wake, and its setup runs again"),
		label("way back") + s.y.Render("none. There is no snapshot."),
		"",
		s.text.Render("Type ") + s.b.Render(v.Name) + s.text.Render(" to confirm"),
	}
	typed := s.w.Render(m.modal.typed)
	if m.modal.typed == v.Name {
		typed = s.g.Render(m.modal.typed)
	}
	cursor := s.r.Render("▌")
	lines = append(lines, box(s.border, "", "", []string{typed + cursor}, 30, 3)...)
	reset := "Reset " + v.Name
	btns := []string{reset, "Cancel"}
	cur, off := m.modal.btn, -1
	if m.modal.typed != v.Name {
		off = 0
		cur = 1
	}
	lines = append(lines, "", m.buttons(btns, cur, off), "",
		s.border.Render(strings.Repeat("─", bw-4)),
		s.d.Render("Typing the name is only asked when data is deleted. Cancel is the default button."),
		s.d.Render("From a script: ")+s.a.Render("doze reset "+v.Name+" --yes"),
	)
	return box(s.borderBad, s.r.Bold(true).Render("reset "+v.Name+" · this deletes data"), "", lines, bw, len(lines)+2)
}

// askKey is the input box of a module's action, and the question before a
// destructive one.
func (m *Model) askKey(k, text string) tea.Cmd {
	md := m.modal
	switch k {
	case "esc":
		m.modal = nil
	case "backspace":
		if r := []rune(md.typed); len(r) > 0 {
			md.typed = string(r[:len(r)-1])
		}
	case "enter":
		if md.kind == modalInput && strings.TrimSpace(md.typed) == "" {
			return nil
		}
		m.modal = nil
		return md.then(md.typed)
	case "space":
		md.typed += " "
	default:
		if md.kind == modalInput && text != "" && unicode.IsPrint([]rune(text)[0]) {
			md.typed += text
		}
	}
	return nil
}

func (m *Model) askBox(w int) []string {
	s := m.st
	md := m.modal
	bw := w * 55 / 100
	if bw < 50 {
		bw = min(w, 60)
	}
	var lines []string
	if md.target != "" {
		lines = append(lines, s.d.Render("for ")+s.w.Render(md.target), "")
	}
	if md.kind == modalInput {
		lines = append(lines, s.d.Render(md.hint), s.w.Render(md.typed)+s.a.Render("▌"), "",
			s.cap("⏎", "do it")+"  "+s.cap("esc", "cancel"))
		return box(s.borderOn, s.a.Bold(true).Render(md.title), "", lines, bw, len(lines)+2)
	}
	lines = append(lines, s.text.Render("This deletes data and cannot be undone."), "",
		s.cap("⏎", md.title)+"  "+s.cap("esc", "cancel"))
	return box(s.borderBad, s.r.Bold(true).Render(md.title+"?"), "", lines, bw, len(lines)+2)
}

// askRestart restarts target, or asks first when something is using it: a
// restart drops every connection, which is what sleeping does too and is asked
// about by the same rule. An instance nobody holds, a task or a crashed one
// restarts at once.
func (m *Model) askRestart(target string) tea.Cmd {
	v, ok := m.find(target)
	if !ok {
		return nil
	}
	if c := m.refuse(); c != nil {
		return c
	}
	if awake(v) && v.Kind != "task" && inUse(v) != "" {
		m.modal = &modal{kind: modalRestart, target: target}
		return nil
	}
	return m.do(control.VerbRestart, target)
}

func (m *Model) restartBox(w int) []string {
	s := m.st
	v, ok := m.find(m.modal.target)
	if !ok {
		return nil
	}
	bw := min(w*66/100, 100)
	if bw < 56 {
		bw = w
	}
	label := func(k string) string { return s.d.Render(fit(k, 13)) + " " }
	conns := 0
	for _, h := range v.Holders {
		if h.Kind == "connection" {
			conns = h.Count
		}
	}
	deps := dependants(v)
	var sentence string
	switch {
	case len(deps) > 0 && conns > 0:
		sentence = s.w.Render(strings.Join(deps, ", ")) + " is running and needs it, and " + s.w.Render(fmt.Sprintf("%d connection%s", conns, plural(conns))) + " are open to it right now."
	case len(deps) > 0:
		sentence = s.w.Render(strings.Join(deps, ", ")) + " is running and needs it."
	default:
		sentence = s.w.Render(fmt.Sprintf("%d connection%s", conns, plural(conns))) + " open to it right now."
	}
	lines := []string{"", s.text.Render(sentence), "",
		label("what happens") + s.text.Render("It stops and starts again. Every connection drops; clients that"),
		label("") + s.text.Render("reconnect find it back in a few seconds, those that do not stay down."),
		label("data") + s.text.Render("Kept. Nothing is deleted."),
		"",
		m.buttons([]string{"Restart " + v.Name, "Cancel"}, m.modal.btn, -1),
		"",
		s.border.Render(strings.Repeat("─", bw-4)),
		s.d.Render("doze only asks when something is using it. Restarting an idle one never asks."),
		"",
	}
	return box(s.borderOn, s.a.Bold(true).Render("restart "+v.Name+"?"), "", lines, bw, len(lines)+2)
}

func (m *Model) restartKey(k string) tea.Cmd {
	md := m.modal
	v, ok := m.find(md.target)
	if !ok {
		m.modal = nil
		return nil
	}
	switch k {
	case "esc", "n":
		m.modal = nil
	case "left", "shift+tab", "h":
		if md.btn > 0 {
			md.btn--
		}
	case "right", "tab", "l":
		md.btn = (md.btn + 1) % 2
	case "enter", "y":
		m.modal = nil
		if md.btn == 0 || k == "y" {
			return m.do(control.VerbRestart, v.Name)
		}
	}
	return nil
}

// openInstanceLogs goes to the selected instance's logs: its logs tab, or what a
// task has instead.
func (m *Model) openInstanceLogs() {
	v, ok := m.selected()
	if !ok {
		return
	}
	m.open()
	for i, t := range m.tabs(v) {
		if t.id == tabLogs || t.id == tabLastRun {
			m.tab = i
		}
	}
}

func (m *Model) helpBox(w, h int) []string {
	s := m.st
	md := m.modal
	bw := min(w-4, 104)
	if bw < 24 {
		bw = max(w, 4)
	}
	lines := m.helpLines(bw - 4)
	room := max(h-6, 3)
	md.off = min(max(md.off, 0), max(len(lines)-room, 0))
	end := min(md.off+room, len(lines))
	shown := make([]string, 0, room)
	for _, l := range lines[md.off:end] {
		shown = append(shown, fit(l, bw-4))
	}
	rcap := ""
	if len(lines) > room {
		rcap = s.d.Render(fmt.Sprintf("%d–%d of %d", md.off+1, end, len(lines)))
	}
	return box(s.borderOn, s.a.Bold(true).Render("keys"), rcap, shown, bw, len(shown)+2)
}

func (m *Model) helpKey(k string) tea.Cmd {
	md := m.modal
	switch k {
	case "esc", "?", "q", "enter":
		m.modal = nil
	case "up", "k":
		md.off--
	case "down", "j":
		md.off++
	case "pgup", "ctrl+u":
		md.off -= 8
	case "pgdown", "ctrl+d", " ":
		md.off += 8
	case "g", "home":
		md.off = 0
	case "G", "end":
		md.off = 1 << 20
	case "ctrl+c":
		return tea.Quit
	}
	return nil
}
