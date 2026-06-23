// Package tui is doze's live control room: an mprocs-style split view with an
// instance sidebar on the left and, on the right, the selected instance's
// telemetry (state, RAM/connection sparklines, a reap countdown) above its
// streaming logs. It refreshes continuously so the picture is always live.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/ui"
)

// ── palette / themes ────────────────────────────────────────────────────────
// The look is a dark "console": a near-neutral slate canvas lifted by one vivid
// accent, with bright state marks on small glyphs/badges and a faint accent tint
// in the chrome (borders, selection). Color stays off the large fills so it never
// reads as noisy. Themes vary the accent + chrome; cycle them with `t` (persisted
// under the doze home). The state colors (active/idle/booting/error) stay constant
// across themes so status always reads the same.
type theme struct {
	name                                        string
	accent, text, dim, faint, panel, sel, selFg lipgloss.Color
	green, gold, cyan, red                      lipgloss.Color
}

var themes = []theme{
	{"violet", "#BD93F9", "#E2E4EE", "#7C8290", "#454C5A", "#3B3550", "#2A2440", "#1C1726", "#7EE787", "#F2C879", "#82AAFF", "#FF7A93"},
	{"emerald", "#5EE0A0", "#E1EBE6", "#7B877F", "#46524C", "#2E4A40", "#1F3A30", "#0E2018", "#7EE787", "#F2C879", "#82AAFF", "#FF7A93"},
	{"amber", "#F2B765", "#ECE7DF", "#888076", "#4F4A40", "#4A3F2C", "#3A2F1E", "#241B0E", "#7EE787", "#F2C879", "#82AAFF", "#FF7A93"},
	{"cyan", "#56D4E0", "#DEEBEC", "#78878A", "#45525A", "#2E474C", "#1F373C", "#0E2024", "#7EE787", "#F2C879", "#82AAFF", "#FF7A93"},
	{"rose", "#FF8FB0", "#EFE4E8", "#8A8088", "#4F454C", "#4A2E3A", "#3A1F2C", "#24101A", "#7EE787", "#F2C879", "#82AAFF", "#FF7A93"},
}

var (
	cAccent, cText, cDim, cFaint, cPanel, cSel, cSelFg lipgloss.Color
	cGreen, cGold, cCyan, cRed                         lipgloss.Color

	stTitle, stDim, stFaint, stText, stLabel, stErr, stAccent, stGreen lipgloss.Style
)

var activeTheme int

// applyTheme makes themes[i] (wrapped to range) the active palette and rebuilds
// every derived style.
func applyTheme(i int) {
	activeTheme = ((i % len(themes)) + len(themes)) % len(themes)
	t := themes[activeTheme]
	cAccent, cText, cDim, cFaint = t.accent, t.text, t.dim, t.faint
	cPanel, cSel, cSelFg = t.panel, t.sel, t.selFg
	cGreen, cGold, cCyan, cRed = t.green, t.gold, t.cyan, t.red
	stTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stDim = lipgloss.NewStyle().Foreground(cDim)
	stFaint = lipgloss.NewStyle().Foreground(cFaint)
	stText = lipgloss.NewStyle().Foreground(cText)
	stLabel = lipgloss.NewStyle().Foreground(cDim)
	stErr = lipgloss.NewStyle().Foreground(cRed)
	stAccent = lipgloss.NewStyle().Foreground(cAccent)
	stGreen = lipgloss.NewStyle().Foreground(cGreen)
}

func init() { applyTheme(0) }

// themeFilePath is where the chosen theme name is remembered, under the doze home.
func themeFilePath() string {
	home := os.Getenv("DOZE_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(h, ".doze")
	}
	return filepath.Join(home, "tui.theme")
}

// loadTheme applies the persisted theme, if any.
func loadTheme() {
	p := themeFilePath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	name := strings.TrimSpace(string(b))
	for i, t := range themes {
		if t.name == name {
			applyTheme(i)
			return
		}
	}
}

// saveTheme remembers the active theme for next time (best-effort).
func saveTheme() {
	if p := themeFilePath(); p != "" {
		_ = os.WriteFile(p, []byte(themes[activeTheme].name), 0o644)
	}
}

func stateColor(state string) lipgloss.Color {
	switch state {
	case "active":
		return cGreen
	case "idle":
		return cGold
	case "booting":
		return cCyan
	case "error":
		return cRed
	default:
		return cDim
	}
}

const (
	histLen   = 300 // ~2.5 min of memory history at refreshMS (was 32s — too short/flat)
	detailH   = 14  // detail card height (fits the 5-row memory trace)
	refreshMS = 500 * time.Millisecond
	logsMS    = 400 * time.Millisecond
	spinMS    = 110 * time.Millisecond
)

var spinner = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

// history holds rolling samples for an instance's sparklines.
type history struct {
	ram   []float64
	conns []float64
}

func (h *history) push(ram, conns float64) {
	h.ram = pushCap(h.ram, ram)
	h.conns = pushCap(h.conns, conns)
}

func pushCap(s []float64, v float64) []float64 {
	s = append(s, v)
	if len(s) > histLen {
		s = s[len(s)-histLen:]
	}
	return s
}

// ── messages ──────────────────────────────────────────────────────────────
type (
	tickMsg     time.Time
	logsTickMsg time.Time
	spinMsg     time.Time
	statusMsg   struct {
		resp control.Response
		err  error
	}
	logsMsg struct {
		name  string
		lines []string
		err   error
	}
	actionMsg struct {
		verb, name string
		err        error
	}
)

func tick() tea.Cmd     { return tea.Tick(refreshMS, func(t time.Time) tea.Msg { return tickMsg(t) }) }
func logsTick() tea.Cmd { return tea.Tick(logsMS, func(t time.Time) tea.Msg { return logsTickMsg(t) }) }
func spin() tea.Cmd     { return tea.Tick(spinMS, func(t time.Time) tea.Msg { return spinMsg(t) }) }

func refresh(c *control.Client) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Do(control.Request{Op: "status"})
		return statusMsg{resp: resp, err: err}
	}
}

func fetchLogs(c *control.Client, name string) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Do(control.Request{Op: "logs", DB: name})
		return logsMsg{name: name, lines: resp.Lines, err: err}
	}
}

func do(c *control.Client, verb, name string) tea.Cmd {
	return func() tea.Msg {
		_, err := c.Do(control.Request{Op: verb, DB: name})
		return actionMsg{verb: verb, name: name, err: err}
	}
}

// ── model ─────────────────────────────────────────────────────────────────
type model struct {
	client *control.Client
	resp   control.Response
	err    error
	width  int
	height int

	cursor   int
	follow   bool
	logVP    viewport.Model
	logErr   string
	logLines []string // raw log lines of the selected instance (for copy mode)

	// copy mode: a frozen, keyboard-navigable selection over the logs. By default
	// the granularity is whole LINES; Tab (or any word motion) toggles to WORD,
	// where the cursor is a (line, word) — copyCursor is the line, copyCol the word.
	copyMode      bool
	copyWordMode  bool // false = line granularity (default), true = word
	copyLines     []string
	copyCursor    int
	copyCol       int
	copyAnchor    int // selection start line; -1 = no range
	copyAnchorCol int // selection start word (word mode only)

	filtering bool
	filter    textinput.Model
	showHelp  bool

	hist       map[string]*history
	frame      int
	flash      string
	flashFrame int
}

// setFlash records a transient status message (auto-cleared after ~2.5s).
func (m *model) setFlash(s string) { m.flash = s; m.flashFrame = m.frame }

// Run validates a daemon is up and launches the dashboard.
func Run(socketPath string) error {
	c := control.NewClient(socketPath)
	if !c.Available() {
		return fmt.Errorf("no daemon is running (start one with `doze start`)")
	}
	loadTheme() // restore the last-used theme
	fi := textinput.New()
	fi.Prompt = "/"
	fi.Placeholder = "filter"
	fi.CharLimit = 32
	m := model{
		client: c,
		follow: true,
		filter: fi,
		hist:   map[string]*history{},
		logVP:  viewport.New(0, 0),
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(refresh(m.client), tick(), logsTick(), spin())
}

func (m model) bodyH() int {
	if h := m.height - 3; h > 4 { // header (2) + footer (1)
		return h
	}
	return 4
}

func (m model) sidebarW() int {
	sw := 32
	if m.width < 96 {
		sw = 26
	}
	if sw > m.width/2 {
		sw = m.width / 2
	}
	if sw < 12 {
		sw = 12
	}
	return sw
}

func (m model) rightW() int {
	if w := m.width - m.sidebarW() - 3; w > 12 { // sidebar border (1) + gap (2)
		return w
	}
	return 12
}

// visible returns instance indices in display order (name-sorted, filtered).
func (m model) visible() []int {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	idx := make([]int, 0, len(m.resp.Instances))
	for i, in := range m.resp.Instances {
		if q != "" && !strings.Contains(strings.ToLower(in.Name+" "+in.Engine), q) {
			continue
		}
		idx = append(idx, i)
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return m.resp.Instances[idx[a]].Name < m.resp.Instances[idx[b]].Name
	})
	return idx
}

func (m model) selected() (control.InstanceView, bool) {
	vis := m.visible()
	if len(vis) == 0 || m.cursor < 0 || m.cursor >= len(vis) {
		return control.InstanceView{}, false
	}
	return m.resp.Instances[vis[m.cursor]], true
}

func (m *model) layout() {
	// logs box: rightW minus its rounded border (2) and horizontal padding (2×2).
	m.logVP.Width = max(4, m.rightW()-6)
	m.logVP.Height = max(3, m.bodyH()-detailH-6)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tickMsg:
		return m, tea.Batch(refresh(m.client), tick())

	case spinMsg:
		m.frame++
		if m.flash != "" && m.frame-m.flashFrame > 24 { // ~2.5s at 110ms
			m.flash = ""
		}
		return m, spin()

	case logsTickMsg:
		var cmd tea.Cmd
		if v, ok := m.selected(); ok && v.PID != 0 {
			cmd = fetchLogs(m.client, v.Name)
		}
		return m, tea.Batch(cmd, logsTick())

	case statusMsg:
		m.err = msg.err
		if msg.err == nil {
			m.resp = msg.resp
			for _, in := range m.resp.Instances {
				h := m.hist[in.Name]
				if h == nil {
					h = &history{}
					m.hist[in.Name] = h
				}
				h.push(float64(in.RAM), float64(in.Conns))
			}
		}
		if vis := m.visible(); m.cursor >= len(vis) {
			m.cursor = max(0, len(vis)-1)
		}
		return m, nil

	case logsMsg:
		if v, ok := m.selected(); ok && msg.name == v.Name {
			if msg.err != nil {
				m.logErr = msg.err.Error()
				if !m.copyMode {
					m.logVP.SetContent("")
				}
			} else {
				m.logErr = ""
				m.logLines = msg.lines
				if !m.copyMode { // freeze the view while copying
					m.logVP.SetContent(renderLogs(msg.lines))
					if m.follow {
						m.logVP.GotoBottom()
					}
				}
			}
		}
		return m, nil

	case actionMsg:
		if msg.err != nil {
			m.setFlash(stErr.Render("✗ " + msg.verb + " " + msg.name + ": " + msg.err.Error()))
		} else {
			m.setFlash(stGreen.Render("✓ " + msg.verb + " " + msg.name))
		}
		return m, refresh(m.client)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.logVP, cmd = m.logVP.Update(msg)
	return m, cmd
}

// handleMouse routes the wheel and clicks like mprocs: wheel over the sidebar
// moves the selection, wheel over the right pane scrolls the logs, and a click
// in the sidebar selects that instance.
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	const headerRows = 2 // title + rule above the body
	if m.copyMode {      // scroll the frozen logs; drag to extend the selection
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.logVP.LineUp(2)
		case tea.MouseButtonWheelDown:
			m.logVP.LineDown(2)
		case tea.MouseButtonLeft:
			switch msg.Action {
			case tea.MouseActionPress: // position the cursor + anchor here
				ln := m.logLineAt(msg.Y)
				m.copyCursor = ln
				if m.copyWordMode {
					m.copyCol = m.logColAt(ln, msg.X)
				}
				m.copyAnchor, m.copyAnchorCol = m.copyCursor, m.copyCol
				m.refreshCopyView()
			case tea.MouseActionMotion: // drag → extend the selection
				ln := m.logLineAt(msg.Y)
				m.copyCursor = ln
				if m.copyWordMode {
					m.copyCol = m.logColAt(ln, msg.X)
				}
				m.refreshCopyView()
			case tea.MouseActionRelease:
				if m.copyAnchor != m.copyCursor || (m.copyWordMode && m.copyAnchorCol != m.copyCol) {
					return m.copySelection() // dragged → copy the span
				}
				m.copyAnchor, m.copyAnchorCol = -1, 0 // plain click → just position
				m.refreshCopyView()
			}
		}
		return m, nil
	}
	overSidebar := msg.X < m.sidebarW()
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if overSidebar {
			if m.cursor > 0 {
				m.cursor--
			}
			return m, m.onSelect()
		}
		m.follow = false
		m.logVP.LineUp(3)
		return m, nil
	case tea.MouseButtonWheelDown:
		if overSidebar {
			if m.cursor < len(m.visible())-1 {
				m.cursor++
			}
			return m, m.onSelect()
		}
		m.logVP.LineDown(3)
		if m.logVP.AtBottom() { // caught up — resume tailing
			m.follow = true
		}
		return m, nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if overSidebar {
			if row := msg.Y - headerRows; row >= 0 && row < len(m.visible()) {
				m.cursor = row
				return m, m.onSelect()
			}
		} else if m.logsRegion(msg.Y) && len(m.logLines) > 0 {
			// Enter copy mode (line granularity by default) anchored at the click.
			m.copyMode, m.copyWordMode = true, false
			m.copyLines = m.logLines
			m.copyCursor, m.copyCol = m.logLineAt(msg.Y), 0
			m.copyAnchor, m.copyAnchorCol = m.copyCursor, m.copyCol
			m.refreshCopyView()
		}
	}
	return m, nil
}

// logsTop is the screen row of the first visible log line (header + detail box +
// the logs box's top border/title/rule).
func (m model) logsTop() int { return 2 + (detailH + 2) + 3 }

// logsRegion reports whether screen row y falls inside the log viewport.
func (m model) logsRegion(y int) bool {
	return y >= m.logsTop() && y < m.logsTop()+m.logVP.Height
}

// logLineAt maps a screen row to a log line index (clamped).
func (m model) logLineAt(y int) int {
	return clampi(m.logVP.YOffset+(y-m.logsTop()), 0, max(0, len(m.copyLines)-1))
}

// logColAt maps a screen X to a word index on the given log line. The logs
// content starts after the sidebar (sidebarW), the 2-col gap, the box's left
// border, and its 2-col padding.
func (m model) logColAt(line, x int) int {
	contentX := x - (m.sidebarW() + 5)
	if contentX < 0 {
		contentX = 0
	}
	ws := m.wordsAt(line)
	for wi, sp := range ws {
		if contentX < sp.end {
			return wi
		}
	}
	return max(0, len(ws)-1)
}

// copySelection writes the selected lines to the clipboard and leaves copy mode.
func (m model) copySelection() (tea.Model, tea.Cmd) {
	var text, what string
	switch {
	case !m.copyWordMode: // whole line(s) — the default
		lo, hi := m.copyRange()
		text = strings.Join(m.copyLines[lo:hi+1], "\n")
		what = fmt.Sprintf("%d line(s)", hi-lo+1)
	case m.copyAnchor >= 0: // word-wise span
		text, what = m.selectedText(), "selection"
	default: // the single word under the cursor
		if s, e := m.curWordRange(); s >= 0 {
			r := []rune(m.copyLines[m.copyCursor])
			text = string(r[s:clampi(e, 0, len(r))])
		}
		what = "word"
	}
	err := clipboard.WriteAll(text)
	m.copyMode, m.copyAnchor, m.copyAnchorCol = false, -1, 0
	m.logVP.SetContent(renderLogs(m.logLines))
	if m.follow {
		m.logVP.GotoBottom()
	}
	if err != nil {
		m.setFlash(stErr.Render("✗ copy failed: " + err.Error()))
	} else {
		m.setFlash(stGreen.Render("✓ copied " + what + " to clipboard"))
	}
	return m, nil
}

// selectedText extracts the word-wise selected span, covering whole middle lines.
func (m model) selectedText() string {
	lL, lW, hL, hW := m.wordSel()
	loW, hiW := m.wordsAt(lL), m.wordsAt(hL)
	if len(loW) == 0 || len(hiW) == 0 {
		return ""
	}
	s := loW[clampi(lW, 0, len(loW)-1)].start
	e := hiW[clampi(hW, 0, len(hiW)-1)].end
	if lL == hL {
		r := []rune(m.copyLines[lL])
		return string(r[clampi(s, 0, len(r)):clampi(e, 0, len(r))])
	}
	rFirst := []rune(m.copyLines[lL])
	parts := []string{string(rFirst[clampi(s, 0, len(rFirst)):])}
	for i := lL + 1; i < hL; i++ {
		parts = append(parts, m.copyLines[i])
	}
	rLast := []rune(m.copyLines[hL])
	parts = append(parts, string(rLast[:clampi(e, 0, len(rLast))]))
	return strings.Join(parts, "\n")
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showHelp { // any key dismisses the help overlay
		m.showHelp = false
		return m, nil
	}
	if m.copyMode {
		return m.handleCopyKey(msg)
	}
	if m.filtering {
		switch msg.String() {
		case "enter", "esc":
			m.filtering = false
			m.filter.Blur()
			if msg.String() == "esc" {
				m.filter.SetValue("")
			}
			m.cursor = 0
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.cursor = 0
		return m, cmd
	}

	vis := m.visible()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, m.onSelect()
	case "down", "j":
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
		return m, m.onSelect()
	case "g", "home":
		m.cursor = 0
		return m, m.onSelect()
	case "G", "end":
		m.cursor = max(0, len(vis)-1)
		return m, m.onSelect()
	case "/":
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink
	case "f":
		m.follow = !m.follow
		if m.follow {
			m.logVP.GotoBottom()
		}
		return m, nil
	case "pgup", "ctrl+u":
		m.follow = false
		m.logVP.HalfViewUp()
		return m, nil
	case "pgdown", "ctrl+d":
		m.logVP.HalfViewDown()
		return m, nil
	case "c":
		if len(m.logLines) > 0 { // enter copy mode (line granularity by default)
			m.copyMode, m.copyWordMode = true, false
			m.copyLines = m.logLines
			m.copyCursor = len(m.copyLines) - 1
			m.copyCol = 0
			m.copyAnchor, m.copyAnchorCol = -1, 0
			m.refreshCopyView()
		}
		return m, nil
	case "t":
		applyTheme(activeTheme + 1)
		saveTheme()
		m.setFlash(stAccent.Render("theme · " + themes[activeTheme].name))
		return m, nil
	case "?":
		m.showHelp = true
		return m, nil
	case "r":
		return m, refresh(m.client)
	}

	if v, ok := m.selected(); ok {
		switch msg.String() {
		case "enter", "b":
			m.setFlash(stDim.Render("booting " + v.Name + "…"))
			return m, do(m.client, "boot", v.Name)
		case "d":
			m.setFlash(stDim.Render("reaping " + v.Name + "…"))
			return m, do(m.client, "down", v.Name)
		case "R":
			m.setFlash(stDim.Render("restarting " + v.Name + "…"))
			return m, do(m.client, "restart", v.Name)
		case "p": // pin: toggle the idle-reaper exemption (keep awake)
			_, _ = m.client.Do(control.Request{Op: "keepawake", DB: v.Name})
			if v.KeepAwake { // was pinned → now auto-sleeps again
				m.setFlash(stDim.Render("○ " + v.Name + " will auto-sleep again"))
			} else {
				m.setFlash(stAccent.Render("▲ keeping " + v.Name + " awake"))
			}
			return m, refresh(m.client)
		}
	}
	return m, nil
}

// ── word-precise selection over the frozen logs ─────────────────────────────
type wordSpan struct{ start, end int } // rune indices [start,end) on a line

// lineWords splits a line into whitespace-delimited words with their rune spans.
func lineWords(s string) []wordSpan {
	r := []rune(s)
	var ws []wordSpan
	for i := 0; i < len(r); {
		for i < len(r) && unicode.IsSpace(r[i]) {
			i++
		}
		if i >= len(r) {
			break
		}
		st := i
		for i < len(r) && !unicode.IsSpace(r[i]) {
			i++
		}
		ws = append(ws, wordSpan{st, i})
	}
	return ws
}

func (m model) wordsAt(line int) []wordSpan {
	if line < 0 || line >= len(m.copyLines) {
		return nil
	}
	return lineWords(m.copyLines[line])
}

func (m model) lastWord(line int) int { return max(0, len(m.wordsAt(line))-1) }

// wordSel returns the ordered span (loLine, loWord, hiLine, hiWord) of a
// word-wise selection (anchor → cursor).
func (m model) wordSel() (lL, lW, hL, hW int) {
	if m.copyAnchor > m.copyCursor || (m.copyAnchor == m.copyCursor && m.copyAnchorCol > m.copyCol) {
		return m.copyCursor, m.copyCol, m.copyAnchor, m.copyAnchorCol
	}
	return m.copyAnchor, m.copyAnchorCol, m.copyCursor, m.copyCol
}

// curWordRange is the rune span of the word under the cursor (-1,-1 if none).
func (m model) curWordRange() (int, int) {
	ws := m.wordsAt(m.copyCursor)
	if len(ws) == 0 {
		return -1, -1
	}
	w := clampi(m.copyCol, 0, len(ws)-1)
	return ws[w].start, ws[w].end
}

// selWordRange is the selected rune span on line i for a word-wise selection
// (whole intermediate lines are fully covered).
func (m model) selWordRange(i int) (int, int, bool) {
	if !m.copyWordMode || m.copyAnchor < 0 {
		return 0, 0, false
	}
	lL, lW, hL, hW := m.wordSel()
	if i < lL || i > hL {
		return 0, 0, false
	}
	start, end := 0, len([]rune(m.copyLines[i]))
	if ws := m.wordsAt(i); len(ws) > 0 {
		if i == lL {
			start = ws[clampi(lW, 0, len(ws)-1)].start
		}
		if i == hL {
			end = ws[clampi(hW, 0, len(ws)-1)].end
		}
	}
	return start, end, true
}

// handleCopyKey drives copy mode. Granularity defaults to whole LINES; Tab (or
// any horizontal/word motion) flips to WORD. It moves the cursor, optionally
// anchors a range, then copies the selection (a line, lines, a word, or a span).
func (m model) handleCopyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	last := len(m.copyLines) - 1
	exit := func() {
		m.copyMode, m.copyAnchor, m.copyAnchorCol = false, -1, 0
		m.logVP.SetContent(renderLogs(m.logLines))
		if m.follow {
			m.logVP.GotoBottom()
		}
	}
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		exit()
		return m, nil
	case "tab": // toggle line ↔ word granularity
		m.copyWordMode = !m.copyWordMode
		m.copyAnchor = -1
	case "up", "k":
		m.copyCursor--
	case "down", "j":
		m.copyCursor++
	case "left", "h", "b": // previous word (flips to word granularity)
		m.copyWordMode = true
		if m.copyCol > 0 {
			m.copyCol--
		} else if m.copyCursor > 0 {
			m.copyCursor--
			m.copyCol = m.lastWord(m.copyCursor)
		}
	case "right", "l", "w": // next word (flips to word granularity)
		m.copyWordMode = true
		if m.copyCol < m.lastWord(m.copyCursor) {
			m.copyCol++
		} else if m.copyCursor < last {
			m.copyCursor++
			m.copyCol = 0
		}
	case "0", "^":
		m.copyWordMode, m.copyCol = true, 0
	case "$":
		m.copyWordMode, m.copyCol = true, m.lastWord(m.copyCursor)
	case "pgup", "ctrl+u":
		m.copyCursor -= 10
	case "pgdown", "ctrl+d":
		m.copyCursor += 10
	case "g", "home":
		m.copyCursor, m.copyCol = 0, 0
	case "G", "end":
		m.copyCursor = last
	case "v", " ": // toggle a selection range (line range or word span per mode)
		if m.copyAnchor < 0 {
			m.copyAnchor, m.copyAnchorCol = m.copyCursor, m.copyCol
		} else {
			m.copyAnchor, m.copyAnchorCol = -1, 0
		}
	case "a": // select all lines
		m.copyWordMode = false
		m.copyAnchor, m.copyAnchorCol, m.copyCursor = 0, 0, last
	case "c", "y", "enter":
		return m.copySelection()
	default:
		return m, nil
	}
	m.copyCursor = clampi(m.copyCursor, 0, last)
	m.copyCol = clampi(m.copyCol, 0, m.lastWord(m.copyCursor))
	m.refreshCopyView()
	return m, nil
}

// copyRange is the inclusive selected line range (just the cursor if no anchor).
func (m model) copyRange() (int, int) {
	if m.copyAnchor < 0 {
		return m.copyCursor, m.copyCursor
	}
	lo, hi := m.copyAnchor, m.copyCursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

// refreshCopyView re-renders the frozen logs with the cursor word and the active
// selection highlighted — whole lines when line-wise, an inline character span
// when word-wise — and keeps the cursor in view.
func (m *model) refreshCopyView() {
	w := m.logVP.Width
	loL, hiL := m.copyRange()
	curFull := lipgloss.NewStyle().Background(cAccent).Foreground(cSelFg).Width(w)
	selFull := lipgloss.NewStyle().Background(cSel).Foreground(cText).Width(w)
	curSeg := lipgloss.NewStyle().Background(cAccent).Foreground(cSelFg)
	selSeg := lipgloss.NewStyle().Background(cSel).Foreground(cText)
	ccs, cce := m.curWordRange()

	var b strings.Builder
	for i, ln := range m.copyLines {
		disp := truncate(ln, w)
		if !m.copyWordMode { // line granularity — highlight whole lines
			switch {
			case i == m.copyCursor:
				b.WriteString(curFull.Render(disp))
			case m.copyAnchor >= 0 && i >= loL && i <= hiL:
				b.WriteString(selFull.Render(disp))
			default:
				b.WriteString(stText.Render(disp))
			}
			b.WriteByte('\n')
			continue
		}
		dr := []rune(disp)
		if len(dr) == 0 { // empty line — still show the cursor if it's here
			if i == m.copyCursor {
				b.WriteString(curSeg.Render(" "))
			}
			b.WriteByte('\n')
			continue
		}
		ss, se, hasSel := m.selWordRange(i)
		// Walk runes, coalescing runs of the same style: 2 = cursor word (on top),
		// 1 = selection span, 0 = plain.
		idAt := func(j int) int {
			if i == m.copyCursor && ccs >= 0 && j >= ccs && j < cce {
				return 2
			}
			if hasSel && j >= ss && j < se {
				return 1
			}
			return 0
		}
		for j := 0; j < len(dr); {
			id := idAt(j)
			k := j + 1
			for k < len(dr) && idAt(k) == id {
				k++
			}
			seg := string(dr[j:k])
			switch id {
			case 2:
				b.WriteString(curSeg.Render(seg))
			case 1:
				b.WriteString(selSeg.Render(seg))
			default:
				b.WriteString(stText.Render(seg))
			}
			j = k
		}
		b.WriteByte('\n')
	}
	m.logVP.SetContent(strings.TrimRight(b.String(), "\n"))
	// Keep the cursor in view without re-centering on every move (so dragging
	// and arrow-stepping stay smooth); only scroll when it leaves the window.
	off, h := m.logVP.YOffset, m.logVP.Height
	if m.copyCursor < off {
		off = m.copyCursor
	} else if m.copyCursor >= off+h {
		off = m.copyCursor - h + 1
	}
	m.logVP.SetYOffset(max(0, off))
}

// onSelect refetches logs immediately when the selection moves.
func (m *model) onSelect() tea.Cmd {
	m.logVP.SetContent("")
	m.logErr = ""
	if v, ok := m.selected(); ok && v.PID != 0 {
		return fetchLogs(m.client, v.Name)
	}
	return nil
}

// viewHelp is the centered keybinding overlay (toggled with `?`). It also
// documents the mouse and the state glyphs — the things the footer can't fit.
func (m model) viewHelp() string {
	k := func(keys, desc string) string {
		return stAccent.Render(fmt.Sprintf("%-11s", keys)) + " " + stDim.Render(desc)
	}
	sec := func(t string) string { return stLabel.Bold(true).Render(t) }
	col1 := strings.Join([]string{
		sec("Navigate"),
		k("↑↓ / j k", "move selection"),
		k("g / G", "first / last"),
		k("/", "filter by name"),
		"",
		sec("Instance"),
		k("b / enter", "boot (wake it)"),
		k("d", "reap — sleep, keeps data"),
		k("R", "restart"),
		k("p", "keep awake (no auto-sleep)"),
	}, "\n")
	col2 := strings.Join([]string{
		sec("Logs"),
		k("f", "follow / pause"),
		k("c", "copy mode"),
		k("pgup/pgdn", "scroll"),
		"",
		sec("Display"),
		k("t", "cycle theme"),
		k("? / q", "help / quit"),
	}, "\n")
	mouse := strings.Join([]string{
		sec("Mouse"),
		k("scroll", "sidebar move · logs scroll"),
		k("click", "select · drag selects logs"),
	}, "\n")
	states := sec("States") + "   " +
		stGreen.Render("● active") + "  " +
		lipgloss.NewStyle().Foreground(cGold).Render("○ idle") + "  " +
		stFaint.Render("· asleep") + "  " +
		lipgloss.NewStyle().Foreground(cCyan).Render("⠿ booting") + "  " +
		stErr.Render("✕ error")

	body := stTitle.Render("doze dash") + stDim.Render("  ·  keys") + "\n\n" +
		lipgloss.JoinHorizontal(lipgloss.Top, col1, "      ", col2) + "\n\n" +
		mouse + "\n\n" + states + "\n\n" +
		stFaint.Render("press any key to close")
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
		Padding(1, 3).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m model) View() string {
	if m.width == 0 {
		mm := m
		mm.width, mm.height = 110, 32
		mm.layout()
		return mm.render()
	}
	return m.render()
}

func (m model) render() string {
	if m.width < 64 || m.height < 18 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			stDim.Render("doze dash needs a larger window")+"\n"+stFaint.Render("at least 64 × 18"))
	}
	if m.showHelp {
		return m.viewHelp()
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.viewSidebar(), "  ", m.viewRight())
	return lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), body, m.viewFooter())
}

// ── header ────────────────────────────────────────────────────────────────
func (m model) viewHeader() string {
	live := stGreen // steady (a blink reads as a fault); the data updating shows it's live
	var up int
	for _, in := range m.resp.Instances {
		if in.PID != 0 {
			up++
		}
	}
	sub := stFaint.Render("mission control")
	if m.flash != "" {
		sub = m.flash
	}
	left := stTitle.Render("◆ doze") + "  " + sub
	listen := m.resp.Listen
	if listen == "" {
		listen = "—"
	}
	// Total RSS lives in the sidebar footer (with the fleet counts); keep the
	// header focused on endpoint / up-count / liveness so memory isn't shown twice.
	right := strings.Join([]string{
		stDim.Render(listen),
		stText.Render(fmt.Sprintf("%d up", up)) + stDim.Render("/"+fmt.Sprint(len(m.resp.Instances))),
		live.Render("●") + stDim.Render(" live"),
	}, stFaint.Render("  ·  "))
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	line := left + strings.Repeat(" ", gap) + right
	rule := stFaint.Render(strings.Repeat("─", max(1, m.width)))
	return line + "\n" + rule
}

// ── sidebar ───────────────────────────────────────────────────────────────
func (m model) viewSidebar() string {
	w := m.sidebarW()
	bodyH := m.bodyH()
	vis := m.visible()

	var maxRAM int64 = 1
	for _, i := range vis {
		if r := m.resp.Instances[i].RAM; r > maxRAM {
			maxRAM = r
		}
	}
	var rows []string
	for di, i := range vis {
		rows = append(rows, m.sidebarRow(m.resp.Instances[i], di == m.cursor, w, maxRAM))
	}
	if len(rows) == 0 {
		rows = append(rows, stDim.Render("  (no instances)"))
	}
	footer := m.sidebarTotals(w)
	avail := max(0, bodyH-len(footer))
	for len(rows) < avail {
		rows = append(rows, "")
	}
	all := append(rows[:avail], footer...)
	return lipgloss.NewStyle().Width(w).
		Border(lipgloss.NormalBorder(), false, true, false, false). // right edge only
		BorderForeground(cPanel).
		Render(strings.Join(all, "\n"))
}

func (m model) sidebarRow(in control.InstanceView, selected bool, w int, maxRAM int64) string {
	st := displayState(in)
	// The name is the primary element of the row: bold, and given most of the
	// width. The precise RAM figure lives in the detail pane and the footer total,
	// so the sidebar stays uncluttered and the names read large.
	nameStyle := stText.Bold(true)
	if selected {
		nameStyle = stAccent.Bold(true)
	}
	// A small bar proportional to the heaviest instance keeps relative memory
	// glanceable without spending a column on digits.
	const bw = 5
	var bar string
	if in.RAM > 0 {
		filled := clampi(int(float64(in.RAM)/float64(maxRAM)*float64(bw)+0.5), 0, bw)
		bar = lipgloss.NewStyle().Foreground(stateColor(st)).Render(strings.Repeat("▰", filled)) +
			stFaint.Render(strings.Repeat("▱", bw-filled))
	} else {
		bar = stFaint.Render(strings.Repeat("▱", bw))
	}
	nameMax := w - bw - 7
	if in.KeepAwake {
		nameMax -= 2 // room for the ▲ keep-awake marker
	}
	left := m.glyph(in) + " " + nameStyle.Render(truncate(in.Name, max(3, nameMax)))
	if in.KeepAwake {
		left += stAccent.Render(" ▲") // pinned: exempt from auto-sleep
	}
	// Reserve the 2-char row prefix AND a 1-col right margin so the bar sits just
	// inside the panel's right border instead of flush against it.
	gap := max(1, w-lipgloss.Width(left)-bw-3)
	inner := left + strings.Repeat(" ", gap) + bar + " "
	if selected {
		return lipgloss.NewStyle().Background(cSel).Width(w).Render(stAccent.Render("▌") + " " + inner)
	}
	return lipgloss.NewStyle().Width(w).Render("  " + inner)
}

// sidebarTotals is the at-a-glance resource summary pinned to the bottom.
func (m model) sidebarTotals(w int) []string {
	var act, idle, asleep, errc int
	var total int64
	for _, in := range m.resp.Instances {
		switch displayState(in) {
		case "active", "booting":
			act++
		case "idle":
			idle++
		case "error":
			errc++
		default:
			asleep++
		}
		if in.PID != 0 {
			total += in.RAM
		}
	}
	counts := stGreen.Render(fmt.Sprintf("●%d", act)) + "  " +
		lipgloss.NewStyle().Foreground(cGold).Render(fmt.Sprintf("○%d", idle)) + "  " +
		stFaint.Render(fmt.Sprintf("·%d", asleep))
	if errc > 0 {
		counts += "  " + stErr.Render(fmt.Sprintf("✕%d", errc))
	}
	mem := stLabel.Render("mem ") + stAccent.Render(orDash(memStr(total)))
	return []string{
		stFaint.Render(strings.Repeat("─", max(1, w))),
		" " + counts,
		" " + mem,
	}
}

func (m model) glyph(in control.InstanceView) string {
	st := displayState(in)
	s := lipgloss.NewStyle().Foreground(stateColor(st))
	switch st {
	case "booting":
		return s.Render(string(spinner[m.frame%len(spinner)]))
	case "active":
		return s.Render("●") // filled
	case "idle":
		return s.Render("○") // hollow
	case "error":
		return s.Render("✕")
	default:
		return stFaint.Render("·") // asleep — small + faint
	}
}

// ── right pane ────────────────────────────────────────────────────────────
func (m model) viewRight() string {
	w := m.rightW()
	v, ok := m.selected()
	if !ok {
		return lipgloss.NewStyle().Width(w).Height(m.bodyH()).
			Align(lipgloss.Center, lipgloss.Center).Foreground(cDim).
			Render("nothing selected")
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.viewDetail(v, w), m.viewLogs(v, w))
}

func (m model) viewDetail(v control.InstanceView, w int) string {
	st := displayState(v)
	title := stTitle.Render(v.Name) + stDim.Render("  "+v.Engine)
	if v.Version != "" {
		title += stDim.Render(" " + v.Version)
	}
	if v.KeepAwake {
		title += stAccent.Render("   ▲ kept awake")
	}
	badge := lipgloss.NewStyle().Foreground(stateColor(st)).Bold(true).Render(displayLabel(st))

	vit := func(label, val string) string { return stLabel.Render(label) + " " + stText.Render(orDash(val)) }
	row1 := strings.Join([]string{
		stLabel.Render("state") + " " + badge,
		vit("conns", fmt.Sprint(v.Conns)),
		vit("pid", pidStr(v.PID)),
		vit("up", ui.Uptime(v.StartedAt)),
	}, stFaint.Render("   "))
	row2 := stLabel.Render("endpoint ") + stText.Render(orDash(v.Endpoint))
	urlLine := stLabel.Render(orEnv(v.EnvVar)+" ") + stAccent.Render(truncate(orDash(v.URL), w-len(orEnv(v.EnvVar))-7))

	dataLine := stLabel.Render("data ") + stDim.Render(truncate(abbrevHome(v.DataDir), w-8))

	// Memory: a filled braille area trace with a right-edge y-axis. When the
	// instance is asleep there's nothing to plot, so say so plainly rather than
	// drawing a dash-filled flatline that reads as "broken".
	const lbl = 7 // width of the "memory" gutter
	const rows = 5
	h := m.hist[v.Name]
	pad := strings.Repeat(" ", lbl)
	mem := make([]string, rows)
	if v.PID == 0 {
		mem[1] = stLabel.Render("memory ") + stFaint.Render("— asleep, no live trace")
	} else {
		// Keep the pixel count (gw*2) below the sample count so the trace is dense
		// (downsampled) rather than upsampled into long straight runs.
		gw := clampi(w-lbl-18, 16, 110)
		graph := lipgloss.NewStyle().Foreground(cAccent)
		varying := h != nil && varies(h.ram)
		var g []string
		if varying {
			g = brailleGraph(h.ram, gw, rows, true) // filled area
		} else {
			g = make([]string, rows)
			for i := range g {
				g[i] = strings.Repeat(" ", gw)
			}
			g[1] = strings.Repeat("⠒", gw) // running but flat — a steady midline
			graph = stFaint
		}
		// Right-edge y-axis: peak anchors the TOP, current (bright) the label row,
		// low + time span the BOTTOM. Bounds appear only with a varying trace.
		lo, hi := memBounds(h)
		peakLbl, lowLbl, span := "", "", ""
		if varying {
			peakLbl, lowLbl, span = memStr(hi), memStr(lo), memWindow(h)
		}
		for i := range g {
			gutter, suffix := pad, ""
			switch i {
			case 0:
				suffix = "  " + stDim.Render(orDash(peakLbl)) // peak (top of scale)
			case 1:
				gutter = stLabel.Render("memory ")
				suffix = "  " + stText.Render(orDash(memStr(v.RAM))) // current (bright)
			case rows - 1:
				tail := orDash(lowLbl) // low (bottom of scale)
				if span != "" {
					tail += stFaint.Render(" · " + span)
				}
				suffix = "  " + stFaint.Render(tail)
			}
			mem[i] = gutter + graph.Render(g[i]) + suffix
		}
	}

	var status string
	switch {
	case v.LastError != "":
		status = stErr.Render("✕ " + truncate(v.LastError, w-6))
	case st == "active":
		status = stGreen.Render("● serving " + fmt.Sprint(v.Conns) + " connection(s)")
	case st == "booting":
		status = lipgloss.NewStyle().Foreground(cCyan).Render(string(spinner[m.frame%len(spinner)]) + " booting…")
	case st == "idle":
		// Running, zero connections — not asleep. A subtle countdown to reap, unless
		// it's pinned awake.
		switch {
		case v.KeepAwake:
			status = stAccent.Render("▲ kept awake — won't auto-sleep")
		case !v.IdleSince.IsZero() && m.resp.IdleTimeout > 0:
			status = m.reapHint(v)
		default:
			status = lipgloss.NewStyle().Foreground(cGold).Render("○ idle — up, 0 connections")
		}
	default: // reaped
		status = stDim.Render("· asleep — connect to wake it")
	}

	lines := []string{
		title,
		stFaint.Render(strings.Repeat("╌", max(1, w-6))),
		row1, row2, urlLine, dataLine, "",
	}
	lines = append(lines, mem...)
	lines = append(lines, "", status)
	card := strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(w).Height(detailH).
		Border(lipgloss.RoundedBorder()).BorderForeground(cPanel).
		Padding(0, 2).Render(card)
}

// reapHint is a quiet, compact idle countdown — a thin baseline that empties.
func (m model) reapHint(v control.InstanceView) string {
	remain := m.resp.IdleTimeout - time.Since(v.IdleSince)
	if remain < 0 {
		remain = 0
	}
	const track = 16
	filled := clampi(int(float64(remain)/float64(m.resp.IdleTimeout)*float64(track)+0.5), 0, track)
	bar := lipgloss.NewStyle().Foreground(cGold).Render(strings.Repeat("▔", filled)) +
		stFaint.Render(strings.Repeat("▔", track-filled))
	idle := lipgloss.NewStyle().Foreground(cGold).Render("○")
	return idle + stDim.Render(" idle · sleeps in "+compactDur(remain)+"  ") + bar
}

// memStr formats resident memory as MB (or GB at ≥1024 MB) with two decimals,
// e.g. "42.18 MB" / "1.25 GB". Empty for zero so callers can show a dash.
func memStr(b int64) string {
	if b <= 0 {
		return ""
	}
	const mb = 1024 * 1024
	if b < 1024*mb {
		return fmt.Sprintf("%.2f MB", float64(b)/mb)
	}
	return fmt.Sprintf("%.2f GB", float64(b)/(1024*mb))
}

// memBounds returns the min and max resident memory across the history window,
// which anchor the bottom and top of the auto-scaled trace (its y-axis).
func memBounds(h *history) (lo, hi int64) {
	if h == nil || len(h.ram) == 0 {
		return 0, 0
	}
	mn, mx := h.ram[0], h.ram[0]
	for _, v := range h.ram {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	return int64(mn), int64(mx)
}

// memWindow is the time span the history currently covers (its x-extent).
func memWindow(h *history) string {
	if h == nil || len(h.ram) == 0 {
		return ""
	}
	return compactDur(time.Duration(len(h.ram)) * refreshMS)
}

// varies reports whether a series has any movement worth plotting as a line.
func varies(vals []float64) bool {
	if len(vals) < 2 {
		return false
	}
	mn, mx := vals[0], vals[0]
	for _, v := range vals {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	return mx > 0
}

// brailleGraph plots vals using braille dots (each cell packs 2×4 dots). With
// fill, every column is shaded from the line down to the baseline — an area chart
// whose silhouette reads as a solid shape; otherwise it's a thin connected line.
// Returns hRows plain strings (top to bottom); the caller colors them. Scaled to
// the window's own min..max so small movements are visible.
func brailleGraph(vals []float64, w, hRows int, fill bool) []string {
	cols, rowsPx := w*2, hRows*4
	cell := make([][]uint8, hRows)
	for i := range cell {
		cell[i] = make([]uint8, w)
	}
	if len(vals) > 0 {
		mn, mx := vals[0], vals[0]
		for _, v := range vals {
			if v < mn {
				mn = v
			}
			if v > mx {
				mx = v
			}
		}
		span := mx - mn
		yOf := func(v float64) int {
			f := 0.5
			if span > 0 {
				f = (v - mn) / span
			}
			return clampi(int((1-f)*float64(rowsPx-1)+0.5), 0, rowsPx-1) // 0 = top
		}
		prev := -1
		for x := 0; x < cols; x++ {
			idx := 0
			if cols > 1 {
				idx = x * (len(vals) - 1) / (cols - 1)
			}
			y := yOf(vals[idx])
			lo, hi := y, y
			if fill {
				hi = rowsPx - 1 // shade from the line down to the baseline (area)
			} else if prev >= 0 { // bridge to the previous point so the line is continuous
				if prev < lo {
					lo = prev
				}
				if prev > hi {
					hi = prev
				}
			}
			for yy := lo; yy <= hi; yy++ {
				setDot(cell, x, yy)
			}
			prev = y
		}
	}
	out := make([]string, hRows)
	for r := 0; r < hRows; r++ {
		var b strings.Builder
		for c := 0; c < w; c++ {
			b.WriteRune(rune(0x2800 + int(cell[r][c])))
		}
		out[r] = b.String()
	}
	return out
}

// setDot lights the braille dot at pixel (x,y) within the cell grid.
func setDot(cell [][]uint8, x, y int) {
	// Braille bit layout per cell (2 cols × 4 rows of dots).
	bits := [4][2]uint8{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}
	cell[y/4][x/2] |= bits[y%4][x%2]
}

func (m model) viewLogs(v control.InstanceView, w int) string {
	mode := stDim.Render("paused")
	if m.follow {
		mode = stGreen.Render("following")
	}
	if m.copyMode {
		// A clear, always-visible toggle so the tab switch is never a mystery: the
		// active granularity is emphasized, the other dimmed, with the key spelled out.
		on, off := stAccent.Bold(true), stFaint
		lineLbl, wordLbl := on.Render("LINE"), off.Render("word")
		if m.copyWordMode {
			lineLbl, wordLbl = off.Render("line"), on.Render("WORD")
		}
		mode = stDim.Render("copy ") + lineLbl + stFaint.Render(" / ") + wordLbl + stDim.Render("  ·  tab switches")
	}
	title := stLabel.Render("logs ") + stFaint.Render("· "+v.Name)
	gap := max(1, w-6-lipgloss.Width(title)-lipgloss.Width(mode))
	head := title + strings.Repeat(" ", gap) + mode

	var bodyTxt string
	switch {
	case v.PID == 0:
		bodyTxt = stFaint.Render("(asleep — no live log stream)") + strings.Repeat("\n", max(0, m.logVP.Height-1))
	case m.logErr != "":
		bodyTxt = stDim.Render(m.logErr) + strings.Repeat("\n", max(0, m.logVP.Height-1))
	default:
		bodyTxt = m.logVP.View()
	}
	inner := head + "\n" + stFaint.Render(strings.Repeat("╌", max(1, w-6))) + "\n" + bodyTxt
	return lipgloss.NewStyle().Width(w).
		Border(lipgloss.RoundedBorder()).BorderForeground(cPanel).
		Padding(0, 2).Render(inner)
}

// ── footer ────────────────────────────────────────────────────────────────
func (m model) viewFooter() string {
	key := func(k, label string) string { return stAccent.Render(k) + stDim.Render(" "+label) }
	sep := stFaint.Render("  ·  ")
	if m.filtering {
		return stAccent.Render(m.filter.View()) + stFaint.Render("   enter/esc")
	}
	if m.copyMode {
		toggle := key("tab", "word mode")
		motion := key("↑↓", "line")
		if m.copyWordMode {
			toggle = key("tab", "line mode")
			motion = key("←→", "word") + sep + key("↑↓", "line")
		}
		return strings.Join([]string{
			toggle, motion, key("v", "select"), key("y", "copy"), key("esc", "exit"),
		}, sep)
	}
	return strings.Join([]string{
		key("↑↓", "select"), key("b", "boot"), key("d", "reap"), key("f", "follow"),
		key("c", "copy"), key("/", "filter"), key("t", "theme"), key("?", "more"), key("q", "quit"),
	}, sep)
}

// ── helpers ───────────────────────────────────────────────────────────────

// displayState promotes a reaped instance carrying an error to "error".
func displayState(in control.InstanceView) string {
	if in.LastError != "" && (in.State == "reaped" || in.State == "") {
		return "error"
	}
	if in.State == "" {
		return "reaped"
	}
	return in.State
}

// displayLabel is the user-facing name for a state. "reaped" is doze's internal
// term; users see "asleep" everywhere, so the badge says ASLEEP, not REAPED.
func displayLabel(state string) string {
	if state == "reaped" {
		return "ASLEEP"
	}
	return strings.ToUpper(state)
}

func renderLogs(lines []string) string {
	if len(lines) == 0 {
		return stFaint.Render("(no output yet)")
	}
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString(stText.Render(ln))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	if w <= 1 || len(r) == 0 {
		return "…"
	}
	if w-1 > len(r) {
		return s
	}
	return string(r[:w-1]) + "…"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// abbrevHome shortens a path under the user's home dir to a leading ~.
func abbrevHome(p string) string {
	if p == "" {
		return "—"
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
func orEnv(s string) string {
	if s == "" {
		return "url"
	}
	return s
}
func pidStr(p int) string {
	if p == 0 {
		return "—"
	}
	return fmt.Sprint(p)
}
func compactDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d >= time.Minute {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
func clampi(v, lo, hi int) int { return max(lo, min(hi, v)) }
