// Package tui is the Charm Bubble Tea dashboard: a live control room over the
// daemon's control socket. It reflects real backend state and lets you act on a
// selected instance — boot, reap, restart, and tail its logs — without leaving
// the terminal.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/ui"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	headStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#888888"))
	selStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	goodStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#43BF6D"))
	badStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E06C75"))
)

type viewMode int

const (
	modeList viewMode = iota
	modeLogs
)

type (
	tickMsg   time.Time
	statusMsg control.Response
	logsMsg   struct {
		name  string
		lines []string
	}
	actionMsg struct {
		verb, name string
		err        error
	}
	errMsg struct{ err error }
)

type model struct {
	client *control.Client
	resp   control.Response
	err    error
	width  int
	height int

	cursor int
	mode   viewMode
	logs   []string
	logName string
	logOff  int
	flash   string
}

// Run launches the dashboard against the control socket at path.
func Run(socketPath string) error {
	client := control.NewClient(socketPath)
	if !client.Available() {
		return fmt.Errorf("daemon is not running; start it with `doze start`")
	}
	m := model{client: client, width: 100, height: 24}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m model) Init() tea.Cmd { return tea.Batch(refresh(m.client), tick()) }

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refresh(c *control.Client) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Do(control.Request{Op: "status"})
		if err != nil {
			return errMsg{err}
		}
		return statusMsg(resp)
	}
}

func action(c *control.Client, verb, name string) tea.Cmd {
	return func() tea.Msg {
		_, err := c.Do(control.Request{Op: verb, DB: name})
		return actionMsg{verb: verb, name: name, err: err}
	}
}

func fetchLogs(c *control.Client, name string) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Do(control.Request{Op: "logs", DB: name})
		if err != nil {
			return errMsg{err}
		}
		return logsMsg{name: name, lines: resp.Lines}
	}
}

func (m model) selected() (control.InstanceView, bool) {
	if m.cursor >= 0 && m.cursor < len(m.resp.Instances) {
		return m.resp.Instances[m.cursor], true
	}
	return control.InstanceView{}, false
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		if m.mode == modeList {
			return m, tea.Batch(refresh(m.client), tick())
		}
		return m, tick()
	case statusMsg:
		m.resp, m.err = control.Response(msg), nil
		if m.cursor >= len(m.resp.Instances) {
			m.cursor = max(0, len(m.resp.Instances)-1)
		}
	case logsMsg:
		m.mode, m.logName, m.logs, m.logOff = modeLogs, msg.name, msg.lines, 0
	case actionMsg:
		if msg.err != nil {
			m.flash = badStyle.Render("✗ " + msg.verb + " " + msg.name + ": " + msg.err.Error())
		} else {
			m.flash = goodStyle.Render("✓ " + msg.verb + " " + msg.name)
		}
		return m, refresh(m.client)
	case errMsg:
		m.err = msg.err
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeLogs {
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.mode = modeList
		case "up", "k":
			m.logOff = max(0, m.logOff-1)
		case "down", "j":
			m.logOff++
		}
		return m, nil
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.resp.Instances)-1, m.cursor+1)
	case "r":
		return m, refresh(m.client)
	case "b", "enter":
		if v, ok := m.selected(); ok {
			m.flash = dimStyle.Render("booting " + v.Name + "…")
			return m, action(m.client, "boot", v.Name)
		}
	case "d":
		if v, ok := m.selected(); ok {
			m.flash = dimStyle.Render("reaping " + v.Name + "…")
			return m, action(m.client, "down", v.Name)
		}
	case "R":
		if v, ok := m.selected(); ok {
			m.flash = dimStyle.Render("restarting " + v.Name + "…")
			return m, action(m.client, "restart", v.Name)
		}
	case "l":
		if v, ok := m.selected(); ok {
			return m, fetchLogs(m.client, v.Name)
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.mode == modeLogs {
		return m.logsView()
	}
	return m.listView()
}

func (m model) listView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("doze") + dimStyle.Render(" — weightless local backing services") + "\n")
	if m.resp.Listen != "" {
		b.WriteString(dimStyle.Render("daemon listening on "+m.resp.Listen) + "\n")
	}
	b.WriteString("\n")
	if m.err != nil {
		b.WriteString(badStyle.Render("⚠ "+m.err.Error()) + "\n\n")
	}

	cols := []col{
		{"", 2}, {"NAME", 18}, {"ENGINE", 9}, {"STATE", 8}, {"CONNS", 6},
		{"RAM", 7}, {"UPTIME", 7}, {"ENDPOINT", 22}, {"PID", 7},
	}
	b.WriteString(headerLine(cols) + "\n")
	if len(m.resp.Instances) == 0 {
		b.WriteString(dimStyle.Render("  no instances declared") + "\n")
	}
	for i, inst := range m.resp.Instances {
		marker, name := "  ", inst.Name
		if i == m.cursor {
			marker = selStyle.Render("❯ ")
			name = selStyle.Render(truncate(inst.Name, 18))
		} else {
			name = truncate(inst.Name, 18)
		}
		ram, pid := "", ""
		if inst.PID != 0 {
			ram, pid = ui.HumanRAM(inst.PID), fmt.Sprintf("%d", inst.PID)
		}
		state := inst.State
		if inst.LastError != "" && (inst.State == "reaped" || inst.State == "") {
			state = "error"
		}
		cells := []string{
			marker, name, truncate(inst.Engine, 9), ui.State(state),
			fmt.Sprintf("%d", inst.Conns), ram, ui.Uptime(inst.StartedAt),
			truncate(inst.Endpoint, 22), pid,
		}
		b.WriteString(dataLine(cells, cols) + "\n")
	}

	if v, ok := m.selected(); ok && v.LastError != "" {
		b.WriteString("\n" + badStyle.Render("✗ "+v.Name+": "+v.LastError) + "\n")
	}
	b.WriteString("\n")
	if m.flash != "" {
		b.WriteString(m.flash + "\n")
	}
	b.WriteString(dimStyle.Render("↑/↓ select · b boot · d reap · R restart · l logs · r refresh · q quit"))
	return b.String()
}

func (m model) logsView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("logs") + dimStyle.Render(" — "+m.logName) + "\n\n")
	body := m.logs
	if len(body) == 0 {
		body = []string{dimStyle.Render("(no log output; the backend may be reaped)")}
	}
	// Window the lines to the available height.
	visible := m.height - 5
	if visible < 1 {
		visible = 1
	}
	start := 0
	if len(body) > visible {
		start = len(body) - visible - m.logOff
		start = clamp(start, 0, len(body)-visible)
	}
	end := min(len(body), start+visible)
	for _, line := range body[start:end] {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("↑/↓ scroll · esc back"))
	return b.String()
}

type col struct {
	name string
	w    int
}

func headerLine(cols []col) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = headStyle.Render(c.name) + pad(c.name, c.w)
	}
	return strings.Join(parts, " ")
}

func dataLine(cells []string, cols []col) string {
	parts := make([]string, len(cells))
	for i, cell := range cells {
		w := 0
		if i < len(cols) {
			w = cols[i].w
		}
		parts[i] = cell + pad(cell, w)
	}
	return strings.Join(parts, " ")
}

func pad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return strings.Repeat(" ", d)
	}
	return ""
}

func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return s[:w]
	}
	return s[:w-1] + "…"
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }
