// Package tui is doze's live control room: an mprocs-style split view with an
// instance sidebar on the left and, on the right, the selected instance's
// telemetry (state, CPU, a RAM/connection trace, a reap countdown) above its
// streaming logs. It refreshes continuously so the picture is always live.
// Managing a service's CONTENTS (queues, buckets, topics, tables) is the web
// consoles' job — the dash opens them in a browser (enter / o) rather than
// re-implementing them in terminal cells.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
)

const (
	histLen   = 300 // ~2.5 min of memory history at refreshMS (was 32s — too short/flat)
	refreshMS = 500 * time.Millisecond
	logsMS    = 400 * time.Millisecond
	spinMS    = 110 * time.Millisecond
)

var spinner = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

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
	resourcesMsg struct {
		name string
		res  []control.ResourceView
		err  error
	}
)

// builtinAdmin reports whether an engine exposes admin resources the detail
// card's status strip can enumerate (queue depths, bucket sizes, topic subs).
func builtinAdmin(eng string) bool {
	// aws lists the services inside it; kafka lists topics + consumer groups.
	return eng == "aws" || eng == "kafka"
}

func tick() tea.Cmd { return tea.Tick(refreshMS, func(t time.Time) tea.Msg { return tickMsg(t) }) }

func logsTick() tea.Cmd { return tea.Tick(logsMS, func(t time.Time) tea.Msg { return logsTickMsg(t) }) }

func spin() tea.Cmd { return tea.Tick(spinMS, func(t time.Time) tea.Msg { return spinMsg(t) }) }

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

func fetchResources(c *control.Client, name string) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Do(control.Request{Op: "resources", DB: name})
		return resourcesMsg{name: name, res: resp.Resources, err: err}
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
	logVP    viewport.Model
	logErr   string
	logLines []string // raw log lines of the selected instance (for copy mode)

	// copy mode: a frozen, keyboard-navigable selection over the logs. The
	// keyboard selects whole LINES; the MOUSE uses character granularity
	// (copyCharMode) so a drag selects an exact span — including within a single
	// line — like a normal terminal.
	copyMode        bool
	copyCharMode    bool // mouse drag: character-precise span (overrides lines)
	copyLines       []string
	copyCursor      int
	copyAnchor      int    // selection start line; -1 = no range
	copyColCh       int    // rune index on the cursor line (char mode)
	copyAnchorColCh int    // rune index of the anchor (char mode)
	copyFinding     bool   // the `/` find input is capturing keystrokes
	copyFind        string // kept find query — n/N step through its matches

	filtering bool
	filter    textinput.Model
	showHelp  bool

	// resource strip: a builtin's admin resources (queue depths, bucket sizes,
	// topic subscriptions), fetched on selection so the detail card can
	// enumerate them under the status row.
	adminName string // instance the fetched resources belong to
	adminRes  []control.ResourceView
	adminErr  string

	// engine page (the aws observatory): the selected instance's glance feed,
	// polled while selected; the board sub-cursor and its focus state.
	glance      *glanceData
	glanceName  string
	glanceErr   string
	glanceAt    time.Time
	boardFocus  bool
	boardSel    int
	wireAnchor  int64 // seq of the wire row pinned to the top; 0 = follow live
	showRawLogs bool // l flips an engine page back to plain logs

	// APPS view (htop for the process fleet): its own selection, sort and
	// staged confirm — the fleet cursor stays where it was.
	appsMode    bool
	appsSel     int
	appsSort    byte // 'c' cpu · 'm' mem · 'n' name · 0 declared order
	appsPending string

	// kafka page: sampled series (high-water, lag, per-partition offsets) so
	// rates and trends are computed dash-side from a dumb reporter.
	khist map[string][]ksample

	hist       map[string]*history
	frame      int
	flash      string         // flash text (raw; styled + width-fitted at render time)
	flashStyle lipgloss.Style // how to paint the flash
	flashErr   bool           // an action error: persists until dismissed (esc / next action)
	flashFrame int

	// dashPending is a destructive dash action ("down:<name>" / "restart:<name>")
	// awaiting y/n confirmation in the footer.
	dashPending string

	// connLost is set when the last status poll failed: the daemon is unreachable
	// (the tick keeps retrying). lastOK timestamps the newest good data so the
	// header can say how stale the picture is.
	connLost bool
	lastOK   time.Time

	// palette: the k9s-style `:` command prompt. The input is a plain buffer
	// (append/backspace only — no cursor), with a suggestion list overlaid above
	// the footer; palSel is the highlighted suggestion.
	paletteMode bool
	palInput    string
	palSel      int
}

// setFlash records a transient status message (auto-cleared after ~2.5s).
func (m *model) setFlash(st lipgloss.Style, s string) {
	m.flash, m.flashStyle, m.flashErr = s, st, false
	m.flashFrame = m.frame
}

// setFlashErr records an action failure that stays visible until dismissed
// (esc, or any subsequent action replacing it) — errors must not evaporate.
func (m *model) setFlashErr(s string) {
	m.flash, m.flashStyle, m.flashErr = s, stErr, true
	m.flashFrame = m.frame
}

// clearFlash drops the current flash (esc dismisses a persistent error).
func (m *model) clearFlash() { m.flash, m.flashErr = "", false }

// Run validates a daemon is up and launches the dashboard.
func Run(socketPath string) error {
	c := control.NewClient(socketPath)
	if !c.Available() {
		return fmt.Errorf("no daemon is running (wake an instance with `doze wake <name>`)")
	}
	loadTheme() // restore the last-used theme
	fi := textinput.New()
	fi.Prompt = "/"
	fi.Placeholder = "filter"
	fi.CharLimit = 32
	m := model{
		client: c,
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

func (m model) selected() (control.InstanceView, bool) {
	vis := m.visible()
	if len(vis) == 0 || m.cursor < 0 || m.cursor >= len(vis) {
		return control.InstanceView{}, false
	}
	return m.resp.Instances[vis[m.cursor]], true
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
		// Successes fade; errors persist until dismissed (esc or the next action).
		if m.flash != "" && !m.flashErr && m.frame-m.flashFrame > 24 { // ~2.5s at 110ms
			m.flash = ""
		}
		return m, spin()

	case logsTickMsg:
		if m.appsMode { // the APPS view's logs follow ITS selection
			var cmd tea.Cmd
			if v, ok := m.appsSelected(); ok && v.PID != 0 {
				cmd = fetchLogs(m.client, v.Name)
			}
			return m, tea.Batch(cmd, logsTick())
		}
		var cmds []tea.Cmd
		if v, ok := m.selected(); ok && v.PID != 0 {
			cmds = append(cmds, fetchLogs(m.client, v.Name))
			// The engine pages' feeds ride the same ticker, throttled to ~1s.
			if time.Since(m.glanceAt) > 900*time.Millisecond {
				switch v.Engine {
				case "aws":
					m.glanceAt = time.Now()
					cmds = append(cmds, fetchGlance(v))
				case "kafka":
					m.glanceAt = time.Now()
					cmds = append(cmds, fetchResources(m.client, v.Name))
				}
			}
		}
		return m, tea.Batch(append(cmds, logsTick())...)

	case glanceMsg:
		if v, ok := m.selected(); ok && v.Name == msg.name {
			if msg.err != nil {
				m.glanceErr = msg.err.Error()
			} else {
				m.glance, m.glanceName, m.glanceErr = msg.g, msg.name, ""
				if m.glance != nil && m.boardSel >= len(m.glance.Services) {
					m.boardSel = max(0, len(m.glance.Services)-1)
				}
			}
		}
		return m, nil

	case statusMsg:
		m.err = msg.err
		m.connLost = msg.err != nil // shown in the header; the tick keeps retrying
		if msg.err == nil {
			m.lastOK = time.Now()
			m.resp = msg.resp
			for _, in := range m.resp.Instances {
				h := m.hist[in.Name]
				if h == nil {
					h = &history{}
					m.hist[in.Name] = h
				}
				h.push(float64(in.RAM), in.CPU, float64(in.Conns))
			}
		}
		if vis := m.visible(); m.cursor >= len(vis) {
			m.cursor = max(0, len(vis)-1)
		}
		m.layout() // the content-sized detail card may have grown/shrunk
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
					// Auto-tail: keep pinned to the newest line when already at the
					// bottom; if the user has scrolled up to read, leave them there.
					wasBottom := m.logVP.AtBottom()
					m.logVP.SetContent(renderLogs(msg.lines, m.logVP.Width))
					if wasBottom {
						m.logVP.GotoBottom()
					}
				}
			}
		}
		return m, nil

	case actionMsg:
		if msg.err != nil {
			m.setFlashErr("✗ " + dashVerbLabel(msg.verb) + " " + msg.name + ": " + msg.err.Error())
		} else if msg.verb == "open" {
			m.setFlash(stGreen, "✓ opened "+msg.name)
		} else if msg.verb != "pin" { // pin already flashed its direction
			m.setFlash(stGreen, "✓ "+dashVerbLabel(msg.verb)+" "+msg.name)
		}
		return m, refresh(m.client)

	case resourcesMsg:
		// Only adopt resources for the instance we're currently focused on.
		if v, ok := m.selected(); ok && msg.name == v.Name {
			m.adminName = msg.name
			if msg.err != nil {
				m.adminErr = msg.err.Error()
			} else {
				m.adminErr, m.adminRes = "", msg.res
				if v.Engine == "kafka" {
					m.recordKafkaSamples(v.Name, msg.res) // feed rate/trend history
				}
			}
			// Resources feed the detail card's status strip, so its height just
			// changed; re-layout now to resize the logs pane in the same frame —
			// otherwise the taller card overflows the viewport for one tick and
			// the whole screen scrolls (the header jumps off, then returns).
			m.layout()
		}
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.logVP, cmd = m.logVP.Update(msg)
	return m, cmd
}

// instanceByName finds an instance in the current status snapshot.
func (m model) instanceByName(name string) (control.InstanceView, bool) {
	for _, in := range m.resp.Instances {
		if in.Name == name {
			return in, true
		}
	}
	return control.InstanceView{}, false
}

// selectInstance moves the sidebar cursor to the named instance, clearing the
// filter when it hides it; false when the name isn't in the fleet.
func (m *model) selectInstance(name string) bool {
	find := func() bool {
		for di, i := range m.visible() {
			if m.resp.Instances[i].Name == name {
				m.cursor = di
				return true
			}
		}
		return false
	}
	if find() {
		return true
	}
	if m.filter.Value() != "" {
		m.filter.SetValue("")
		return find()
	}
	return false
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
			stDim.Render("doze dash needs a larger window")+"\n"+stDim.Render("at least 64 × 18"))
	}
	if m.showHelp {
		return m.viewHelp()
	}
	if m.appsMode {
		return lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), m.viewApps(), m.viewFooter())
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.viewSidebar(), "  ", m.viewRight())
	out := lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), body, m.viewFooter())
	if m.paletteMode { // Spotlight-style floating panel in the upper third
		out = m.overlayAt(out, m.paletteView(), max(1, m.height/6))
	}
	if m.dashPending != "" { // destructive confirm — a centered modal, unmissable
		modal := m.confirmView()
		top := max(1, (m.height-lipgloss.Height(modal))/2)
		out = m.overlayAt(out, modal, top)
	}
	return out
}

// overlayAt composites a floating block (palette, modal) centered over the base
// frame starting at row `top`, splicing each block line INTO the base row so the
// content on either side — crucially the sidebar — stays visible rather than
// being wiped by a full-width blank line.
func (m model) overlayAt(frame, block string, top int) string {
	lines := strings.Split(frame, "\n")
	blockLines := strings.Split(block, "\n")
	blockW := 0
	for _, b := range blockLines {
		if w := ansi.StringWidth(b); w > blockW {
			blockW = w
		}
	}
	left := max(0, (m.width-blockW)/2)
	for i, b := range blockLines {
		r := top + i
		if r < 0 || r >= len(lines) {
			continue
		}
		base := lines[r]
		// Keep the base's first `left` cells (the sidebar lives here), padded out
		// if the row is short, then the block, then whatever base sits to its right.
		lseg := ansi.Truncate(base, left, "")
		if pad := left - ansi.StringWidth(lseg); pad > 0 {
			lseg += strings.Repeat(" ", pad)
		}
		if bw := ansi.StringWidth(b); bw < blockW {
			b += strings.Repeat(" ", blockW-bw)
		}
		rseg := ansi.TruncateLeft(base, left+blockW, "")
		lines[r] = lseg + "\x1b[0m" + b + "\x1b[0m" + rseg
	}
	return strings.Join(lines, "\n")
}

// renderLogs paints log lines truncated to the pane width — an overlong line
// must never soft-wrap and grow the box past its height budget (which would
// desync the mouse math). Copy mode gives access to the full line.
func renderLogs(lines []string, w int) string {
	if len(lines) == 0 {
		return stDim.Render("(no output yet)")
	}
	if w <= 0 {
		w = 1 << 20 // viewport not laid out yet — leave lines whole
	}
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString(stText.Render(truncate(ln, w)))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func clampi(v, lo, hi int) int { return max(lo, min(hi, v)) }
