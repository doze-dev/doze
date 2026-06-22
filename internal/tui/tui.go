// Package tui is doze's live control room: an mprocs-style split view with an
// instance sidebar on the left and, on the right, the selected instance's
// telemetry (state, RAM/connection sparklines, a reap countdown) above its
// streaming logs. It refreshes continuously so the picture is always live.
package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/ui"
)

// ── palette ───────────────────────────────────────────────────────────────
// A "quiet instrument" theme: a near-monochrome slate console where color is
// rare and meaningful. One restrained accent (slate-cyan), desaturated state
// marks used only on small glyphs/badges — never on large fills.
var (
	cAccent = lipgloss.Color("#8FB8C9") // slate-cyan — the single accent (name, url, graph, selection)
	cText   = lipgloss.Color("#C5C8D0")
	cDim    = lipgloss.Color("#6E7480")
	cFaint  = lipgloss.Color("#3A3F4A")
	cPanel  = lipgloss.Color("#2A2F3A") // borders
	cSel    = lipgloss.Color("#222732") // selected row fill
	cGreen  = lipgloss.Color("#8FB89B") // active  (sage)
	cGold   = lipgloss.Color("#C6A878") // idle    (sand)
	cCyan   = lipgloss.Color("#88A6C4") // booting (slate-blue)
	cRed    = lipgloss.Color("#C98B8B") // error   (dusty)

	stTitle  = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stDim    = lipgloss.NewStyle().Foreground(cDim)
	stFaint  = lipgloss.NewStyle().Foreground(cFaint)
	stText   = lipgloss.NewStyle().Foreground(cText)
	stLabel  = lipgloss.NewStyle().Foreground(cDim)
	stErr    = lipgloss.NewStyle().Foreground(cRed)
	stAccent = lipgloss.NewStyle().Foreground(cAccent)
	stGreen  = lipgloss.NewStyle().Foreground(cGreen)
)

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
	histLen   = 64
	detailH   = 12
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

	cursor  int
	follow  bool
	logVP   viewport.Model
	logErr  string

	filtering bool
	filter    textinput.Model

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
	if w := m.width - m.sidebarW() - 2; w > 12 { // sidebar border (1) + gap (1)
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
	m.logVP.Width = max(4, m.rightW()-4)
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
				m.logVP.SetContent("")
			} else {
				m.logErr = ""
				m.logVP.SetContent(renderLogs(msg.lines))
				if m.follow {
					m.logVP.GotoBottom()
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
		if msg.Action == tea.MouseActionPress && overSidebar {
			if row := msg.Y - headerRows; row >= 0 && row < len(m.visible()) {
				m.cursor = row
				return m, m.onSelect()
			}
		}
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		}
	}
	return m, nil
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
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.viewSidebar(), " ", m.viewRight())
	return lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), body, m.viewFooter())
}

// ── header ────────────────────────────────────────────────────────────────
func (m model) viewHeader() string {
	live := stGreen
	if m.frame%2 == 0 {
		live = live.Faint(true)
	}
	var up int
	var total int64
	for _, in := range m.resp.Instances {
		if in.PID != 0 {
			up++
			total += in.RAM
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
	right := strings.Join([]string{
		stDim.Render(listen),
		stText.Render(fmt.Sprintf("%d up", up)) + stDim.Render("/"+fmt.Sprint(len(m.resp.Instances))),
		stAccent.Render(orDash(ui.HumanBytes(total))) + stDim.Render(" rss"),
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
	nameStyle := stText
	if selected {
		nameStyle = stAccent.Bold(true)
	}
	// RAM value + a bar proportional to the heaviest instance, so relative
	// consumption is legible at a glance without selecting anything.
	const bw = 5
	var ram, bar string
	if in.RAM > 0 {
		ram = stText.Render(fmt.Sprintf("%5s", ui.HumanBytes(in.RAM)))
		filled := clampi(int(float64(in.RAM)/float64(maxRAM)*float64(bw)+0.5), 0, bw)
		bar = lipgloss.NewStyle().Foreground(stateColor(st)).Render(strings.Repeat("▰", filled)) +
			stFaint.Render(strings.Repeat("▱", bw-filled))
	} else {
		ram = stFaint.Render("    ·")
		bar = stFaint.Render(strings.Repeat("▱", bw))
	}
	left := m.glyph(in) + " " + nameStyle.Render(truncate(in.Name, w-16))
	right := ram + " " + bar
	gap := max(1, w-lipgloss.Width(left)-lipgloss.Width(right)-2)
	inner := left + strings.Repeat(" ", gap) + right
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
	rss := stLabel.Render("rss ") + stAccent.Render(orDash(ui.HumanBytes(total)))
	return []string{
		stFaint.Render(strings.Repeat("─", max(1, w))),
		" " + counts,
		" " + rss,
	}
}

func (m model) glyph(in control.InstanceView) string {
	st := displayState(in)
	s := lipgloss.NewStyle().Foreground(stateColor(st))
	switch st {
	case "booting":
		return s.Render(string(spinner[m.frame%len(spinner)]))
	case "active":
		if m.frame%2 == 0 {
			return s.Render("●")
		}
		return s.Faint(true).Render("●")
	case "idle":
		return s.Render("○")
	case "error":
		return s.Render("✕")
	default:
		return stFaint.Render("·")
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
	badge := lipgloss.NewStyle().Foreground(stateColor(st)).Bold(true).Render(strings.ToUpper(st))

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

	// Memory as a thin braille line trace (an oscilloscope, not a filled bar).
	const lbl = 7 // width of the "memory" gutter
	gw := clampi(w-lbl-16, 12, 48)
	h := m.hist[v.Name]
	graph := lipgloss.NewStyle().Foreground(cAccent)
	var g []string
	if h != nil && v.PID != 0 && varies(h.ram) {
		g = brailleGraph(h.ram, gw, 3)
	} else {
		// a faint, flat midline when there's nothing to plot
		g = []string{strings.Repeat(" ", gw), strings.Repeat("⠒", gw), strings.Repeat(" ", gw)}
		graph = stFaint
	}
	pad := strings.Repeat(" ", lbl)
	memTop := pad + graph.Render(g[0])
	memMid := stLabel.Render("memory ") + graph.Render(g[1]) + "  " + stText.Render(orDash(ui.HumanBytes(v.RAM)))
	memBot := pad + graph.Render(g[2]) + "  " + stFaint.Render(memCaption(h))

	var status string
	switch {
	case v.LastError != "":
		status = stErr.Render("✕ " + truncate(v.LastError, w-6))
	case st == "active":
		status = stGreen.Render("● serving " + fmt.Sprint(v.Conns) + " connection(s)")
	case st == "booting":
		status = lipgloss.NewStyle().Foreground(cCyan).Render(string(spinner[m.frame%len(spinner)]) + " booting…")
	case st == "idle":
		// Running, zero connections — not asleep. A subtle countdown to reap.
		if !v.IdleSince.IsZero() && m.resp.IdleTimeout > 0 {
			status = m.reapHint(v)
		} else {
			status = lipgloss.NewStyle().Foreground(cGold).Render("○ idle — up, 0 connections")
		}
	default: // reaped
		status = stDim.Render("· asleep — connect to wake it")
	}

	card := strings.Join([]string{
		title,
		stFaint.Render(strings.Repeat("╌", max(1, w-4))),
		row1, row2, urlLine, dataLine, "",
		memTop, memMid, memBot, "",
		status,
	}, "\n")
	return lipgloss.NewStyle().Width(w).Height(detailH).
		Border(lipgloss.RoundedBorder()).BorderForeground(cPanel).
		Padding(0, 1).Render(card)
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

// memCaption summarizes the memory window: peak value and the time span shown.
func memCaption(h *history) string {
	if h == nil || len(h.ram) == 0 {
		return ""
	}
	var peak float64
	for _, v := range h.ram {
		if v > peak {
			peak = v
		}
	}
	win := compactDur(time.Duration(len(h.ram)) * refreshMS)
	if p := ui.HumanBytes(int64(peak)); p != "" {
		return "peak " + p + " · " + win
	}
	return win
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

// brailleGraph plots vals as a thin connected line using braille dots: each cell
// packs 2×4 dots, so a few rows give a smooth trace. Returns hRows plain strings
// (top to bottom); the caller colors them. The line is scaled to the window's
// own min..max so small movements are visible.
func brailleGraph(vals []float64, w, hRows int) []string {
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
			if prev >= 0 { // bridge to the previous point so the line is continuous
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
	title := stLabel.Render("logs ") + stFaint.Render("· "+v.Name)
	gap := max(1, w-4-lipgloss.Width(title)-lipgloss.Width(mode))
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
	inner := head + "\n" + stFaint.Render(strings.Repeat("╌", max(1, w-4))) + "\n" + bodyTxt
	return lipgloss.NewStyle().Width(w).
		Border(lipgloss.RoundedBorder()).BorderForeground(cPanel).
		Padding(0, 1).Render(inner)
}

// ── footer ────────────────────────────────────────────────────────────────
func (m model) viewFooter() string {
	if m.filtering {
		return stAccent.Render(m.filter.View()) + stFaint.Render("   enter/esc")
	}
	key := func(k, label string) string { return stAccent.Render(k) + stDim.Render(" "+label) }
	return strings.Join([]string{
		key("↑↓", "select"), key("b", "boot"), key("d", "reap"), key("R", "restart"),
		key("f", "follow"), key("/", "filter"), key("r", "refresh"), key("q", "quit"),
	}, stFaint.Render("  ·  "))
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
