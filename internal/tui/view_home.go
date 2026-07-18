package tui

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/ui"
)

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" { // quits from anywhere, however deep the mode
		return m, tea.Quit // (esc/q keep doing the graduated back-out)
	}
	if m.showHelp { // any key dismisses the help overlay
		m.showHelp = false
		return m, nil
	}
	if m.paletteMode {
		return m.handlePaletteKey(msg)
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
	if m.appsMode {
		return m.handleAppsKey(msg)
	}
	if m.dashPending != "" { // confirm a staged sleep / restart (footer prompt)
		verb, name, _ := strings.Cut(m.dashPending, ":")
		m.dashPending = ""
		switch msg.String() {
		case "y", "Y", "enter":
			m.setFlash(stDim, dashVerbLabel(verb)+" "+orAllServices(name)+"…")
			return m, do(m.client, verb, name)
		default:
			m.setFlash(stDim, "cancelled")
			return m, nil
		}
	}

	// Engine-page board focus: j/k walk the services INSIDE the selected
	// instance; enter opens that service's console page; esc hands the keys
	// back to the fleet.
	if v, ok := m.selected(); ok && hasEnginePage(v.Engine) && !m.showRawLogs {
		switch msg.String() {
		case "tab", "right":
			if !m.boardFocus {
				m.boardFocus = true
				return m, nil
			}
		case "l":
			m.showRawLogs = true
			m.boardFocus = false
			return m, nil
		// The wire's scrollback: J/K walk older/newer calls whether or not the
		// board is focused; the view is anchored by seq so live polls don't
		// shift the rows underneath the reader.
		case "J":
			m.wireScroll(v, +1)
			return m, nil
		case "K":
			m.wireScroll(v, -1)
			return m, nil
		case "esc":
			if !m.boardFocus && m.wireAnchor != 0 {
				m.wireAnchor = 0 // snap the wire back to live
				return m, nil
			}
		}
		if m.boardFocus {
			switch msg.String() {
			case "j", "down":
				if m.boardSel < m.boardLen(v)-1 {
					m.boardSel++
				}
				return m, nil
			case "k", "up":
				if m.boardSel > 0 {
					m.boardSel--
				}
				return m, nil
			case "esc", "left":
				m.boardFocus = false
				m.wireAnchor = 0
				return m, nil
			case "enter", "o":
				if v.Engine == "kafka" {
					addr := connectLine(v)
					if err := clipboard.WriteAll(addr); err == nil {
						m.setFlash(stGreen, "✓ copied "+addr+" — try: kcat -b "+addr+" -t <topic> -C")
					} else {
						m.setFlashErr("✗ copy failed: " + err.Error())
					}
					return m, nil
				}
				url := m.boardOpenURL(v)
				if url == "" {
					m.setFlash(stDim, "nothing to open")
					return m, nil
				}
				m.setFlash(stDim, "opening "+url+"…")
				return m, openInBrowser(v.Name, url)
			}
			return m, nil // the board owns the keyboard while focused
		}
	} else if m.showRawLogs {
		if v, ok := m.selected(); ok && hasEnginePage(v.Engine) && msg.String() == "l" {
			m.showRawLogs = false
			return m, nil
		}
	}

	vis := m.visible()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc": // dismiss a persistent error flash, then peel a kept filter
		if m.flashErr {
			m.clearFlash()
			return m, nil
		}
		if m.filter.Value() != "" { // a kept `/` filter clears like the console's
			m.filter.SetValue("")
			m.cursor = 0
			return m, m.onSelect()
		}
		m.clearFlash()
		return m, nil
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
	case "pgup", "ctrl+u":
		m.logVP.HalfPageUp()
		return m, nil
	case "pgdown", "ctrl+d":
		m.logVP.HalfPageDown()
		return m, nil
	case "c":
		if len(m.logLines) > 0 { // enter copy mode (line selection)
			m.copyMode, m.copyCharMode = true, false
			m.copyLines = m.logLines
			m.copyCursor = len(m.copyLines) - 1
			m.copyAnchor, m.copyAnchorColCh = -1, 0
			m.refreshCopyView()
		}
		return m, nil
	case "t":
		applyTheme(activeTheme + 1)
		saveTheme()
		m.setFlash(stAccent, "theme · "+themes[activeTheme].name)
		return m, nil
	case "?":
		m.showHelp = true
		return m, nil
	case ":", "ctrl+k": // open the command palette (Spotlight-style)
		m.paletteMode, m.palInput, m.palSel = true, "", 0
		return m, nil
	case "r":
		return m, refresh(m.client)
	case "A": // the APPS view: htop for the process fleet
		m.appsMode, m.appsSel, m.appsPending = true, 0, ""
		return m, nil
	}

	if v, ok := m.selected(); ok {
		switch msg.String() {
		case "enter": // the primary gesture: wake it if asleep, else open its web door
			if v.PID == 0 {
				m.setFlash(stDim, "waking "+v.Name+"…")
				return m, do(m.client, "boot", v.Name)
			}
			return m.openWeb(v)
		case "o": // open the web console / URL in a browser
			return m.openWeb(v)
		case "w": // wake now
			m.setFlash(stDim, "waking "+v.Name+"…")
			return m, do(m.client, "boot", v.Name)
		case "s": // sleep — stage a y/n confirm in the footer
			m.dashPending = "down:" + v.Name
			return m, nil
		case "R":
			m.dashPending = "restart:" + v.Name
			return m, nil
		case "p": // pin: toggle the idle-reaper exemption (keep awake)
			if v.KeepAwake { // was pinned → now auto-sleeps again
				m.setFlash(stDim, "○ "+v.Name+" will auto-sleep again")
			} else {
				m.setFlash(stAccent, "▲ keeping "+v.Name+" awake")
			}
			name := v.Name
			client := m.client
			return m, func() tea.Msg { // async like every other action; errors surface
				_, err := client.Do(control.Request{Op: "keepawake", DB: name})
				return actionMsg{verb: "pin", name: name, err: err}
			}
		case "y": // copy the connect line — the name-based address, never a raw IP
			url := connectLine(v)
			if url == "" {
				m.setFlash(stDim, "nothing to connect to on "+v.Name)
				return m, nil
			}
			if err := clipboard.WriteAll(url); err != nil {
				m.setFlashErr("✗ copy failed: " + err.Error())
			} else {
				m.setFlash(stGreen, "✓ copied "+v.Name+" connect")
			}
			return m, nil
		}
	}
	return m, nil
}

// connectLine is the address a user pastes to reach a service: the connection
// string, else the shared-endpoint resource path, else the DNS-name endpoint —
// never a raw 127.0.0.x:port (that's the bind line's job). Falls back to
// whatever Endpoint holds (a unix socket path) last. It is both the card's
// `connect` row and what `y` copies, so the two always agree.
func connectLine(v control.InstanceView) string {
	switch {
	case v.URL != "":
		return v.URL
	case v.Resource != "":
		return v.Resource
	case v.Domain != "" && v.Endpoint != "":
		if i := strings.LastIndex(v.Endpoint, ":"); i >= 0 && !strings.HasPrefix(v.Endpoint, "unix:") {
			return v.Domain + v.Endpoint[i:]
		}
		return v.Domain
	default:
		return v.Endpoint
	}
}

// ── web hand-off (enter / o) ─────────────────────────────────────────────────

// webURL is the browser destination for an instance: the aws engine opens its
// web console; anything else that answers HTTP (an ingress process, an HTTP
// engine) opens its connect line directly. Empty when there is nothing
// sensible to open.
func (m model) webURL(v control.InstanceView) string {
	// The aws engine serves its own console; open that, not the bare gateway
	// (whose root answers as S3 ListBuckets XML).
	if v.Engine == "aws" {
		if base := connectLine(v); strings.HasPrefix(base, "http") {
			return strings.TrimRight(base, "/") + "/_console"
		}
	}
	// The kafka module serves its web console one port above the broker (the
	// Kafka wire protocol can't share a port with HTTP).
	if v.Engine == "kafka" {
		if host, port, err := net.SplitHostPort(connectLine(v)); err == nil {
			if p, perr := strconv.Atoi(port); perr == nil {
				return fmt.Sprintf("http://%s:%d", host, p+1)
			}
		}
	}
	if c := connectLine(v); strings.HasPrefix(c, "http://") || strings.HasPrefix(c, "https://") {
		return c
	}
	return ""
}

// openWeb opens the instance's web destination in the default browser.
func (m model) openWeb(v control.InstanceView) (tea.Model, tea.Cmd) {
	url := m.webURL(v)
	if url == "" {
		m.setFlash(stDim, "nothing to open on "+v.Name+" — y copies the connect line")
		return m, nil
	}
	m.setFlash(stDim, "opening "+url+"…")
	return m, openInBrowser(v.Name, url)
}

// openInBrowser launches the OS URL opener detached; failures surface like any
// other action error.
func openInBrowser(name, url string) tea.Cmd {
	return func() tea.Msg {
		bin := "xdg-open"
		if runtime.GOOS == "darwin" {
			bin = "open"
		}
		err := exec.Command(bin, url).Start()
		return actionMsg{verb: "open", name: name, err: err}
	}
}

// ── command palette (`:`) ────────────────────────────────────────────────────

// onSelect refetches logs immediately when the selection moves, and (for a
// running builtin) its resources so the detail card's strip is ready.
func (m *model) onSelect() tea.Cmd {
	m.logVP.SetContent("")
	m.logErr = ""
	// Moving off the previous instance invalidates its cached resources and
	// its engine page's feed.
	m.adminRes, m.adminName, m.adminErr = nil, "", ""
	m.glance, m.glanceName, m.glanceErr = nil, "", ""
	m.boardFocus, m.boardSel, m.showRawLogs, m.wireAnchor = false, 0, false, 0
	m.glanceAt = time.Time{}
	m.layout() // the new selection's card height sizes the logs pane
	v, ok := m.selected()
	if !ok || v.PID == 0 {
		return nil
	}
	cmds := []tea.Cmd{fetchLogs(m.client, v.Name)}
	if builtinAdmin(v.Engine) {
		cmds = append(cmds, fetchResources(m.client, v.Name))
	}
	return tea.Batch(cmds...)
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
		k("r", "refresh now"),
		"",
		sec("Instance"),
		k("enter", "wake if asleep, else open web console"),
		k("o", "open web console / URL in browser"),
		k("w", "wake it now"),
		k("s", "sleep — keeps data (confirms)"),
		k("R", "restart (confirms)"),
		k("p", "keep awake (no auto-sleep)"),
		k("y", "copy connect line"),
	}, "\n")
	col2 := strings.Join([]string{
		sec("Logs"),
		k("c", "copy mode (lines; drag for chars)"),
		k("/ n N", "find in copy mode · repeat"),
		k("pgup/pgdn", "scroll"),
		"",
		sec("Palette"),
		k(": / ctrl+k", "command palette — fleet moves:"),
		k("", ":wake, :sleep, :restart, :reset, :sync,"),
		k("", ":logs, :pin, :theme, :filter, …"),
		"",
		sec("Display"),
		k("t", "cycle theme"),
		k("esc", "dismiss error message"),
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
		stDim.Render("· asleep") + "  " +
		lipgloss.NewStyle().Foreground(cCyan).Render("⠿ waking") + "  " +
		stErr.Render("✕ error") + "  " +
		stErr.Render("! tainted")

	return m.helpOverlay("doze dash", col1, col2, mouse+"\n\n"+states)
}

// helpOverlay assembles a bordered key-reference overlay: title row, two key
// columns, an optional tail block. On terminals too narrow for two columns the
// columns stack — but only when the taller box still fits; otherwise the wide
// layout clipped at the right edge loses less than a stacked one clipped at
// the bottom. The clip is the true last resort: an oversized overlay tears the
// frame and scrolls the terminal.
func (m model) helpOverlay(title, col1, col2, tail string) string {
	build := func(cols string) string {
		body := stTitle.Render(title) + stDim.Render("  ·  keys") + "\n\n" + cols
		if tail != "" {
			body += "\n\n" + tail
		}
		body += "\n\n" + stDim.Render("press any key to close")
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
			Padding(1, 3).Render(body)
	}
	box := build(lipgloss.JoinHorizontal(lipgloss.Top, col1, "      ", col2))
	if lipgloss.Width(box) > m.width {
		if alt := build(col1 + "\n\n" + col2); lipgloss.Width(alt) <= m.width &&
			lipgloss.Height(alt) <= m.height {
			box = alt
		}
	}
	if lines := strings.Split(box, "\n"); len(lines) > m.height || lipgloss.Width(box) > m.width {
		if len(lines) > m.height {
			lines = lines[:max(1, m.height)]
		}
		for i := range lines {
			lines[i] = truncate(lines[i], m.width)
		}
		box = strings.Join(lines, "\n")
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// confirmView is the centered modal for a staged destructive action: the verb
// and target read large, with a one-line consequence underneath.
func (m model) confirmView() string {
	verb, name, _ := strings.Cut(m.dashPending, ":")
	target := name
	if target == "" {
		target = "ALL services"
	}
	// The modal floats over the frame, so it must clamp itself — overlayAt will
	// happily splice lines wider than the window (a long name would tear it).
	head := stErr.Bold(true).Render(truncate("⚠ "+dashVerbLabel(verb)+" "+target+"?", max(16, m.width-10)))
	var why string
	switch verb {
	case "down":
		why = "stops it now; data is kept and a connection re-wakes it"
		if name == "" {
			why = "sleeps every awake service; connections re-wake them"
		}
	case "restart":
		why = "stops and re-wakes the backend in place"
	case "destroy":
		why = "wipes its data; the next wake re-provisions fresh"
	}
	keys := stAccent.Render("enter") + stDim.Render("/") + stAccent.Render("y") + stDim.Render(" confirm") +
		stFaint.Render("   ·   ") + stDim.Render("any other key cancels")
	body := head + "\n" + stDim.Render(truncate(why, max(16, m.width-10))) + "\n\n" + keys
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(cRed).
		Padding(1, 3).Render(body)
}

// ── header ────────────────────────────────────────────────────────────────
func (m model) viewHeader() string {
	var up int
	for _, in := range m.resp.Instances {
		if in.PID != 0 {
			up++
		}
	}
	listen := m.resp.Listen
	if listen == "" {
		listen = "—"
	}
	// The right cluster is joined verbatim below; an unbounded listen string
	// would push the line past the window and pad every joined row to it.
	listen = truncate(listen, max(12, m.width/3))
	// Liveness is truthful: green "live" only while status polls succeed; a failed
	// poll flips the whole header state to a red "lost" until a poll lands again.
	liveDot := stGreen.Render("●") + stDim.Render(" live")
	if m.connLost {
		liveDot = stErr.Render("● lost")
	}
	// Total RSS lives in the sidebar footer (with the fleet counts); keep the
	// header focused on endpoint / up-count / liveness so memory isn't shown twice.
	right := strings.Join([]string{
		stDim.Render(listen),
		stText.Render(fmt.Sprintf("%d up", up)) + stDim.Render("/"+fmt.Sprint(len(m.resp.Instances))),
		liveDot,
	}, stFaint.Render("  ·  "))

	title := stTitle.Render("◆ doze")
	// The sub slot next to the title: flash > connection-lost banner > tagline.
	// Whatever it is, it is fitted to the width left over so a long message can
	// never blow the header line out of the terminal.
	avail := max(8, m.width-lipgloss.Width(title)-lipgloss.Width(right)-3)
	var sub string
	switch {
	case m.flash != "":
		sub = m.flashStyle.Render(truncate(m.flash, avail))
	case m.connLost:
		banner := "● lost — daemon unreachable, retrying…"
		age := ""
		if !m.lastOK.IsZero() { // how stale the picture on screen is
			age = "  data " + compactDur(time.Since(m.lastOK)) + " old"
		}
		sub = stErr.Render(truncate(banner, max(8, avail-lipgloss.Width(age)))) + stDim.Render(age)
	}
	left := title
	if sub != "" {
		left += "  " + sub
	}
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	line := truncate(left+strings.Repeat(" ", gap)+right, m.width)
	rule := stFaint.Render(strings.Repeat("─", max(1, m.width)))
	return line + "\n" + rule
}

// ── sidebar ───────────────────────────────────────────────────────────────
func (m model) viewSidebar() string {
	w := m.sidebarW()
	vis := m.visible()

	var maxRAM int64 = 1
	for _, i := range vis {
		if r := m.resp.Instances[i].RAM; r > maxRAM {
			maxRAM = r
		}
	}
	avail := m.sidebarAvail()
	lines, above, below := m.sidebarWindow(avail)
	var rows []string
	for _, ln := range lines {
		if ln.header != "" {
			rows = append(rows, m.sidebarHeader(ln.header, w))
			continue
		}
		rows = append(rows, m.sidebarRow(m.resp.Instances[vis[ln.di]], ln.di == m.cursor, w, maxRAM))
	}
	// When the fleet outgrows the pane the window slides with the cursor and the
	// edge rows become overflow markers, so the selection can never leave the
	// screen and hidden instances are never a surprise.
	if above > 0 && len(rows) > 0 {
		rows[0] = stDim.Render(fmt.Sprintf("  ↑ %d more", above+1))
	}
	if below > 0 && len(rows) > 0 {
		rows[len(rows)-1] = stDim.Render(fmt.Sprintf("  ↓ %d more", below+1))
	}
	if len(rows) == 0 {
		if len(m.resp.Instances) == 0 { // truly empty — point at the way forward
			rows = append(rows,
				stDim.Render("  no instances yet"),
				stDim.Render("  declare some in doze.hcl,"),
				stDim.Render("  then run `doze up`"))
		} else { // instances exist, the filter hides them all
			rows = append(rows, stDim.Render("  (no matches)"))
		}
	}
	footer := m.sidebarTotals(w)
	for len(rows) < avail {
		rows = append(rows, "")
	}
	all := append(rows[:avail], footer...)
	return lipgloss.NewStyle().Width(w).
		Border(lipgloss.NormalBorder(), false, true, false, false). // right edge only
		BorderForeground(cPanel).
		Render(strings.Join(all, "\n"))
}

// sidebarHeader renders a group section label (a faint, upper-case heading that
// reads distinctly from the indented instance rows below it).
func (m model) sidebarHeader(label string, w int) string {
	return lipgloss.NewStyle().Width(w).Render(stDim.Render(strings.ToUpper(label)))
}

func (m model) sidebarRow(in control.InstanceView, selected bool, w int, maxRAM int64) string {
	st := displayState(in)
	// The selected row's tint must ride on EVERY span: each styled span ends in
	// an ANSI reset, so a single wrapping Background would only paint the gaps
	// before the first span and after the last — the name and bar would sit on
	// the default background with an oddly highlighted tail.
	var bg lipgloss.TerminalColor
	if selected && !noColor {
		bg = cSel
	}
	on := func(s lipgloss.Style) lipgloss.Style {
		if bg != nil {
			return s.Background(bg)
		}
		return s
	}
	sp := func(n int) string { return on(lipgloss.NewStyle()).Render(strings.Repeat(" ", max(0, n))) }
	// The name is the primary element of the row: bold, and given most of the
	// width. The precise RAM figure lives in the detail pane and the footer total,
	// so the sidebar stays uncluttered and the names read large.
	nameStyle := on(stText.Bold(true))
	if selected {
		nameStyle = on(stAccent.Bold(true))
	}
	// A small bar proportional to the heaviest instance keeps relative memory
	// glanceable without spending a column on digits.
	const bw = 5
	var bar string
	if in.RAM > 0 {
		filled := clampi(int(float64(in.RAM)/float64(maxRAM)*float64(bw)+0.5), 0, bw)
		bar = on(lipgloss.NewStyle().Foreground(stateColor(st))).Render(strings.Repeat("▰", filled)) +
			on(stFaint).Render(strings.Repeat("▱", bw-filled))
	} else {
		bar = on(stFaint).Render(strings.Repeat("▱", bw))
	}
	nameMax := w - bw - 7
	if in.KeepAwake {
		nameMax -= 2 // room for the ▲ keep-awake marker
	}
	left := m.glyphOn(in, bg) + sp(1) + nameStyle.Render(truncate(in.Name, max(3, nameMax)))
	if in.KeepAwake {
		left += on(stAccent).Render(" ▲") // pinned: exempt from auto-sleep
	}
	// Reserve the 2-char row prefix AND a 1-col right margin so the bar sits just
	// inside the panel's right border instead of flush against it.
	gap := max(1, w-lipgloss.Width(left)-bw-3)
	inner := left + sp(gap) + bar + sp(1)
	if selected {
		if noColor { // the tinted row is invisible without color — reverse it instead
			return lipgloss.NewStyle().Width(w).Render(reverseVideo("▌ " + inner))
		}
		return lipgloss.NewStyle().Background(cSel).Width(w).Render(on(stAccent).Render("▌") + sp(1) + inner)
	}
	return lipgloss.NewStyle().Width(w).Render("  " + inner)
}

// sidebarTotals is the at-a-glance resource summary pinned to the bottom.
func (m model) sidebarTotals(w int) []string {
	var act, idle, asleep, errc, taint int
	var total int64
	var cpuTotal float64
	for _, in := range m.resp.Instances {
		switch displayState(in) {
		case "active", "booting":
			act++
		case "idle":
			idle++
		case "error":
			errc++
		case "tainted":
			taint++
		default:
			asleep++
		}
		if in.PID != 0 {
			total += in.RAM
			cpuTotal += in.CPU
		}
	}
	counts := stGreen.Render(fmt.Sprintf("●%d", act)) + "  " +
		lipgloss.NewStyle().Foreground(cGold).Render(fmt.Sprintf("○%d", idle)) + "  " +
		stDim.Render(fmt.Sprintf("·%d", asleep))
	if errc > 0 {
		counts += "  " + stErr.Render(fmt.Sprintf("✕%d", errc))
	}
	if taint > 0 {
		counts += "  " + stErr.Render(fmt.Sprintf("!%d", taint))
	}
	mem := stLabel.Render("cpu ") + stAccent.Render(orDash(ui.CPUStr(cpuTotal))) +
		stFaint.Render("  ") + stLabel.Render("mem ") + stAccent.Render(orDash(memStr(total)))
	return []string{
		stFaint.Render(strings.Repeat("─", max(1, w))),
		" " + counts,
		" " + mem,
	}
}

func (m model) glyph(in control.InstanceView) string { return m.glyphOn(in, nil) }

// glyphOn renders the state mark, optionally on a background (the selected
// row's tint) — the span carries the background itself so its ANSI reset can't
// punch a hole in the row band.
func (m model) glyphOn(in control.InstanceView, bg lipgloss.TerminalColor) string {
	st := displayState(in)
	s := lipgloss.NewStyle().Foreground(stateColor(st))
	dim := stDim
	if bg != nil {
		s, dim = s.Background(bg), dim.Background(bg)
	}
	switch st {
	case "booting":
		return s.Render(string(spinner[m.frame%len(spinner)]))
	case "active":
		return s.Render("●") // filled
	case "idle":
		return s.Render("○") // hollow
	case "error":
		return s.Render("✕")
	case "tainted":
		return s.Bold(true).Render("!") // converge failed — must not read as asleep
	default:
		return dim.Render("·") // asleep — quiet, but the mark must stay visible
	}
}

// ── right pane ────────────────────────────────────────────────────────────
func (m model) viewRight() string {
	w := m.rightW()
	v, ok := m.selected()
	if !ok {
		msg := "nothing selected"
		if len(m.resp.Instances) == 0 {
			msg = "no instances yet\n\ndeclare some in doze.hcl, then run `doze up`\nto wake the fleet"
		}
		return lipgloss.NewStyle().Width(w).Height(m.bodyH()).
			Align(lipgloss.Center, lipgloss.Center).Foreground(cDim).
			Render(msg)
	}
	body := m.viewLogs(v, w)
	if !m.showRawLogs {
		switch v.Engine {
		case "aws":
			body = m.viewEnginePage(v, w)
		case "kafka":
			body = m.viewKafkaPage(v, w)
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.viewDetail(v, w), body)
}

// viewDetail is the instance card. It is content-sized: a sleeping instance
// yields a shorter card (no padding), and the logs pane below takes the rest.
func (m model) viewDetail(v control.InstanceView, w int) string {
	return lipgloss.NewStyle().Width(w).
		Border(lipgloss.RoundedBorder()).BorderForeground(cPanel).
		Padding(0, 2).Render(strings.Join(m.detailLines(v, w), "\n"))
}

// detailLines builds the card's content rows — the single source for rendering
// AND the layout math (logs pane height, mouse hit-testing). The design rule:
// one blank row between sections, facts consolidated into section-title rows,
// charts carry a left axis gutter and keep their right side empty.
//
// The card must leave room for the logs pane below it (its rule + the 3-row
// minimum the layout floors at), so near the minimum window the optional
// sections drop in priority order — conns chart first, then cpu, then the
// memory chart — rather than pushing the frame past the terminal height.
func (m model) detailLines(v control.InstanceView, w int) []string {
	budget := m.bodyH() - 4 - 2 // logs rule + 3 log rows, minus the card border
	if budget < 1 {
		budget = 1
	}
	lines := m.detailLinesWith(v, w, true, true, true)
	if len(lines) > budget {
		lines = m.detailLinesWith(v, w, true, true, false)
	}
	if len(lines) > budget {
		lines = m.detailLinesWith(v, w, true, false, false)
	}
	if len(lines) > budget {
		lines = m.detailLinesWith(v, w, false, false, false)
	}
	if len(lines) > budget {
		lines = lines[:budget]
	}
	return lines
}

func (m model) detailLinesWith(v control.InstanceView, w int, withMem, withCPU, withConns bool) []string {
	st := displayState(v)
	inner := max(24, w-6) // border (2) + padding (2×2)
	isProc := v.Engine == "process"

	// Title row: identity on the left, the live facts cluster right-aligned.
	// "builtin"/"0" are versionless pseudo-versions (a process, an AWS built-in) —
	// hide them, matching `doze status`; a process is not a "builtin".
	name := stTitle.Render(v.Name) + stDim.Render("  "+v.Engine)
	if v.Version != "" && v.Version != "builtin" && v.Version != "0" {
		name += stDim.Render(" " + v.Version)
	}
	if v.KeepAwake {
		name += stAccent.Render("  ▲ kept awake")
	}
	facts := m.titleFacts(v, st)
	gap := max(1, inner-lipgloss.Width(name)-lipgloss.Width(facts))
	titleRow := truncate(name+strings.Repeat(" ", gap)+facts, inner)

	// Connection facts: a fixed label column. `connect` is the address you paste
	// to reach the service (URL > resource path > DNS name — never a raw IP);
	// `bind` is the dialable truth underneath it: the real IP:port the backend
	// occupies, with pid and the conventional env-var name riding dim. A plain
	// process has neither, so both rows drop rather than showing bare dashes.
	lbl := func(s string) string { return stLabel.Render(fmt.Sprintf("%-8s  ", s)) }
	var connRows []string
	if c := connectLine(v); c != "" {
		avail := max(12, inner-10)
		chunks := wrapWidth(c, avail)
		connRows = []string{lbl("connect") + stAccent.Bold(true).Render(chunks[0])}
		if len(chunks) > 1 { // 2 lines max; anything longer ends in an ellipsis
			rest := strings.Join(chunks[1:], "")
			connRows = append(connRows, strings.Repeat(" ", 10)+stAccent.Bold(true).Render(truncate(rest, avail)))
		}
	}
	if bind := orFallback(v.Bind, v.Endpoint); bind != "" {
		row := lbl("bind") + stDim.Render(bind)
		if v.PID != 0 {
			row += stDim.Render(" · pid " + fmt.Sprint(v.PID))
		}
		// The env hint answers "what do I export?" in one token; it is the first
		// thing to go when the row runs out of room, then the pid.
		if v.EnvVar != "" && lipgloss.Width(row)+7+len(v.EnvVar) <= inner {
			row += stDim.Render(" · env " + v.EnvVar)
		}
		connRows = append(connRows, truncate(row, inner))
	}
	dataRow := lbl("data") + stDim.Render(truncate(abbrevHome(v.DataDir), max(8, inner-12)))

	lines := []string{titleRow, ""}
	lines = append(lines, connRows...)
	lines = append(lines, truncate(dataRow, inner))

	// Charts — only while running; a sleeping card simply ends here (short is fine).
	if v.PID != 0 {
		h := m.hist[v.Name]
		if withMem {
			lines = append(lines, "")
			lines = append(lines, memorySection(h, v.RAM, inner)...)
		}
		if withCPU {
			if cs := cpuSection(h, v.CPU, inner); len(cs) > 0 {
				lines = append(lines, "")
				lines = append(lines, cs...)
			}
		}
		if withConns && !isProc {
			if cs := connsSection(h, v.Conns, inner); len(cs) > 0 {
				lines = append(lines, "")
				lines = append(lines, cs...)
			}
		}
	}

	// Status strip: only what the title row can't say — failures, a builtin's
	// resources, and the web-console door. Always a blank row above.
	var status, resLine string
	switch {
	case v.LastError != "":
		status = stErr.Render("✕ " + truncate(v.LastError, inner-2))
	case v.Tainted:
		status = stErr.Render("✕ structure incomplete — run `doze sync` to re-converge")
	case isProc && v.PID != 0 && v.Healthy != nil && !*v.Healthy:
		status = stErr.Render("✕ running but health probe failing")
	}
	if status == "" && v.PID != 0 {
		var hint string
		if m.webURL(v) != "" {
			hint = stAccent.Render("o") + stDim.Render(" opens the web console")
		}
		if builtinAdmin(v.Engine) && m.adminName == v.Name && len(m.adminRes) > 0 {
			kind := m.adminRes[0].Kind + "s"
			status = stGreen.Render("● ") + stAccent.Render(fmt.Sprintf("%d %s", len(m.adminRes), kind))
			if hint != "" {
				status += stDim.Render(" · ") + hint
			}
			names := make([]string, 0, len(m.adminRes))
			for _, r := range m.adminRes {
				n := r.Name
				// Engine rows carry their one-line status inline — the point of
				// the strip (service counts, topic watermarks, group lag).
				if r.Status != "" {
					n += " " + stDim.Render(r.Status)
				}
				if b := resBadges(r); b != "" {
					n += " " + b
				}
				names = append(names, n)
			}
			resLine = stDim.Render(truncate(strings.Join(names, "  ·  "), inner-2))
		} else if hint != "" {
			status = stGreen.Render("● serving") + stDim.Render(" · ") + hint
		}
	}
	if status != "" {
		// Fitted like every other row — an overlong strip would soft-wrap inside
		// the card and desync the layout math by a row.
		lines = append(lines, "", truncate(status, inner))
		if resLine != "" {
			lines = append(lines, resLine)
		}
	}
	return lines
}

// resBadges renders a resource's salient traits (FIFO, dead-letter protection)
// from its engine Info, so a queue's nature is visible in the strip.
func resBadges(r control.ResourceView) string {
	var bs []string
	if r.Info["fifo"] == "true" {
		bs = append(bs, "FIFO")
	}
	if r.Info["redrive"] != "" {
		bs = append(bs, "DLQ↩")
	}
	return strings.Join(bs, " ")
}

// titleFacts is the card title's right-aligned facts cluster: state badge,
// conns/health, cpu, uptime, and the reap countdown when one is running.
func (m model) titleFacts(v control.InstanceView, st string) string {
	badge := lipgloss.NewStyle().Foreground(stateColor(st)).Bold(true).
		Render(stateGlyph(st) + " " + displayLabel(st))
	sep := stDim.Render(" · ")
	parts := []string{badge}
	if v.PID == 0 {
		if st == "booting" {
			parts = append(parts, stDim.Render("waking…"))
		} else {
			parts = append(parts, stDim.Render("wakes on connect"))
		}
		return strings.Join(parts, sep)
	}
	if v.Engine == "process" {
		parts = append(parts, healthBadge(v.Healthy))
	} else {
		parts = append(parts, stText.Render(fmt.Sprintf("%d conns", v.Conns)))
	}
	if c := ui.CPUStr(v.CPU); c != "" {
		parts = append(parts, stDim.Render("cpu "+c))
	}
	parts = append(parts, stDim.Render("up "+ui.Uptime(v.StartedAt)))
	if v.RestartCount > 0 {
		parts = append(parts, stDim.Render(fmt.Sprintf("%d restarts", v.RestartCount)))
	}
	// The idle reaper exempts supervised processes (they boot eagerly and stay
	// up), so they never sleep on idle — don't tease a countdown that won't fire.
	if st == "idle" && v.Engine != "process" && !v.KeepAwake && !v.IdleSince.IsZero() && m.resp.IdleTimeout > 0 {
		remain := m.resp.IdleTimeout - time.Since(v.IdleSince)
		if remain < 0 {
			remain = 0
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(cGold).Render("sleeps in "+compactDur(remain)))
	}
	return strings.Join(parts, sep)
}

func (m model) viewLogs(v control.InstanceView, w int) string {
	mode := "" // logs auto-tail; the only header status is copy-mode selection
	if m.copyMode {
		// Live selection feedback: the header always says what a copy would take.
		var sel string
		switch {
		case m.copyCharMode && m.copyAnchor >= 0: // mouse drag
			sel = plural(len([]rune(m.selectedCharText())), "char")
		case m.copyAnchor >= 0:
			lo, hi := m.copyRange()
			sel = plural(hi-lo+1, "line") + " selected"
		default:
			sel = "line " + fmt.Sprint(m.copyCursor+1) + " of " + fmt.Sprint(len(m.copyLines))
		}
		mode = stAccent.Bold(true).Render("COPY") + stDim.Render(" · "+sel)
	}
	// An open region, not a box: one faint rule carrying the title inline —
	// the second nested border read as clutter and cost 2 rows + 4 cols of logs.
	title := stLabel.Render("logs") + stDim.Render(" · "+v.Name)
	fill := max(1, w-4-lipgloss.Width(title)-lipgloss.Width(mode))
	head := stFaint.Render("╌╌ ") + title + " " + stFaint.Render(strings.Repeat("╌", fill))
	if mode != "" {
		head += mode
	}

	var bodyTxt string
	switch {
	case v.PID == 0:
		bodyTxt = stDim.Render("(asleep — no live log stream)") + strings.Repeat("\n", max(0, m.logVP.Height-1))
	case m.logErr != "":
		bodyTxt = stDim.Render(m.logErr) + strings.Repeat("\n", max(0, m.logVP.Height-1))
	default:
		bodyTxt = m.logVP.View()
	}
	return lipgloss.NewStyle().Width(w).Render(truncate(head, w) + "\n" + bodyTxt)
}

// ── footer ────────────────────────────────────────────────────────────────
func (m model) viewFooter() string {
	key := func(k, label string) string { return stAccent.Render(k) + stDim.Render(" "+label) }
	sep := stFaint.Render("  ·  ")
	if m.appsMode { // the APPS view has its own verbs — the fleet footer would lie
		hs := []string{
			key("j/k", "move"), key("o", "open"), key("R", "restart"), key("s", "sleep"),
			key("p", "pin"), key("c/m/n", "sort"), key("esc", "back"), key("q", "quit"),
		}
		return truncate(strings.Join(hs, sep), m.width)
	}
	if m.filtering {
		return stAccent.Render(m.filter.View()) + stDim.Render("   enter/esc")
	}
	if m.copyMode {
		if m.copyFinding { // the find input owns the footer while it captures keys
			return truncate(stAccent.Render("/"+m.copyFind+"▌")+stDim.Render("   enter jump · esc cancel"), m.width)
		}
		mv, find, next := key("↑↓", "move"), key("/", "find"), key("n/N", "next")
		selLines, all := key("v", "select lines"), key("a", "all")
		cp, exit := key("y", "copy"), key("esc", "exit")
		// Trim like the other footers: the selection niceties drop before the
		// find keys, and movement/copy/exit always survive.
		variants := [][]string{
			{mv, find, next, selLines, all, cp, exit},
			{mv, find, next, cp, exit},
			{mv, find, cp, exit},
			{mv, cp, exit},
		}
		for _, v := range variants {
			if line := strings.Join(v, sep); lipgloss.Width(line) <= m.width {
				return line
			}
		}
		return truncate(strings.Join(variants[len(variants)-1], sep), m.width)
	}
	// Every dash key, in display order, each with a drop priority (higher drops
	// first). The line is trimmed to the window: least-important hints go before
	// core actions ever do, and "? help" / "q quit" (prio 0) always survive.
	type hint struct {
		text string
		prio int
	}
	// A deliberately short line: the core verbs plus the palette, which reaches
	// everything else (:restart, :pin, :filter, :theme, :logs — see `?`).
	hints := []hint{
		{key("↑↓", "move"), 1},
	}
	if q := m.filter.Value(); q != "" {
		// A kept filter must stay visible — it narrows the fleet, and esc is its
		// escape hatch (the console footer makes the same promise).
		hints = append(hints, hint{stAccent.Render("/"+q) +
			stDim.Render(fmt.Sprintf(" %d of %d · esc clears", len(m.visible()), len(m.resp.Instances))), 0})
	}
	// enter adapts to the selection — wake when asleep, open when serving a web
	// console — and the footer says which it is rather than surprising.
	wake := hint{key("enter/w", "wake"), 2}
	if v, ok := m.selected(); ok && v.PID != 0 {
		wake = hint{key("w", "wake"), 3}
		if m.webURL(v) != "" {
			hints = append(hints, hint{key("o", "console"), 2})
		}
	}
	hints = append(hints,
		wake,
		hint{key("s", "sleep"), 2},
		hint{key("y", "connect"), 3},
		hint{key(":", "commands"), 1}, // the palette reaches everything — keep it visible
		hint{key("?", "help"), 0},
		hint{key("q", "quit"), 0},
	)
	line := func(hs []hint) string {
		parts := make([]string, len(hs))
		for i, h := range hs {
			parts[i] = h.text
		}
		return strings.Join(parts, sep)
	}
	for lipgloss.Width(line(hints)) > m.width {
		drop, at := 0, -1
		for i, h := range hints {
			if h.prio > drop {
				drop, at = h.prio, i
			}
		}
		if at < 0 { // only prio-0 hints left — nothing more can go
			break
		}
		hints = append(hints[:at], hints[at+1:]...)
	}
	return line(hints)
}
