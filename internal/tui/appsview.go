// The APPS view: htop for the stack's supervised processes. One screen — totals
// up top, a selectable table with CPU/MEM bars, each app's own child processes
// indented under it, health and restart columns, an attention line when the
// supervisor has been earning its keep, and the selected app's logs below.
// Opened with A (or :apps); the dash's own verbs (R restart · s sleep · p pin)
// work unchanged on the selection.
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/ui"
)

// apps returns the fleet's process instances in the current sort order.
func (m model) apps() []control.InstanceView {
	var out []control.InstanceView
	for _, v := range m.resp.Instances {
		if v.Engine == "process" {
			out = append(out, v)
		}
	}
	switch m.appsSort {
	case 'c':
		sort.SliceStable(out, func(i, j int) bool { return out[i].CPU > out[j].CPU })
	case 'm':
		sort.SliceStable(out, func(i, j int) bool { return out[i].RAM > out[j].RAM })
	case 'n':
		sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	}
	return out
}

// appsSelected is the APPS view's current selection.
func (m model) appsSelected() (control.InstanceView, bool) {
	apps := m.apps()
	if len(apps) == 0 {
		return control.InstanceView{}, false
	}
	i := min(m.appsSel, len(apps)-1)
	return apps[i], true
}

// handleAppsKey owns the keyboard while the APPS view is up.
func (m model) handleAppsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.appsPending != "" { // staged confirm, same shape as the fleet's
		verb, name, _ := strings.Cut(m.appsPending, ":")
		m.appsPending = ""
		switch msg.String() {
		case "y", "Y", "enter":
			m.setFlash(stDim, dashVerbLabel(verb)+" "+name+"…")
			return m, do(m.client, verb, name)
		default:
			m.setFlash(stDim, "cancelled")
			return m, nil
		}
	}
	apps := m.apps()
	switch msg.String() {
	case "esc", "q", "A":
		m.appsMode = false
		return m, m.onSelect()
	case "j", "down":
		if m.appsSel < len(apps)-1 {
			m.appsSel++
		}
		return m, nil
	case "k", "up":
		if m.appsSel > 0 {
			m.appsSel--
		}
		return m, nil
	case "c", "m", "n":
		m.appsSort = msg.String()[0]
		m.appsSel = 0
		return m, nil
	case "R":
		if v, ok := m.appsSelected(); ok {
			m.appsPending = "restart:" + v.Name
		}
		return m, nil
	case "s":
		if v, ok := m.appsSelected(); ok {
			m.appsPending = "down:" + v.Name
		}
		return m, nil
	case "p":
		if v, ok := m.appsSelected(); ok {
			name := v.Name
			client := m.client
			return m, func() tea.Msg {
				_, err := client.Do(control.Request{Op: "keepawake", DB: name})
				return actionMsg{verb: "pin", name: name, err: err}
			}
		}
		return m, nil
	case "enter", "o":
		if v, ok := m.appsSelected(); ok {
			if url := m.webURL(v); url != "" {
				m.setFlash(stDim, "opening "+url+"…")
				return m, openInBrowser(v.Name, url)
			}
			m.setFlash(stDim, v.Name+" has no HTTP endpoint")
		}
		return m, nil
	case "?":
		m.showHelp = true
		return m, nil
	}
	return m, nil
}

// bar renders a meter of the given cell width: filled ▮, empty ▯.
func bar(frac float64, width int) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	fill := int(frac*float64(width) + 0.5)
	return lipgloss.NewStyle().Foreground(cCyan).Render(strings.Repeat("▮", fill)) +
		stFaint.Render(strings.Repeat("▯", width-fill))
}

// viewApps renders the whole APPS screen (replacing sidebar + detail).
func (m model) viewApps() string {
	w := m.width - 2
	apps := m.apps()

	// ── totals ──
	var cpu float64
	var mem int64
	healthy, probed := 0, 0
	for _, v := range apps {
		cpu += v.CPU
		mem += v.RAM
		if v.Healthy != nil {
			probed++
			if *v.Healthy {
				healthy++
			}
		}
	}
	title := stTitle.Render("APPS") + stDim.Render(fmt.Sprintf(" · %d supervised", len(apps)))
	if probed > 0 {
		frag := fmt.Sprintf(" · %d/%d healthy", healthy, probed)
		if healthy == probed {
			title += stGreen.Render(frag)
		} else {
			title += lipgloss.NewStyle().Foreground(cGold).Render(frag)
		}
	}
	title += stDim.Render(" · sort " + map[byte]string{'c': "cpu", 'm': "mem", 'n': "name", 0: "declared"}[m.appsSort])
	meters := stDim.Render(fmt.Sprintf("cpu %.0f%% · mem %s", cpu, ui.HumanBytes(mem)))
	head := title + strings.Repeat(" ", max(1, w-lipgloss.Width(title)-lipgloss.Width(meters))) + meters

	// ── table ──
	var maxRAM int64 = 1
	for _, v := range apps {
		if v.RAM > maxRAM {
			maxRAM = v.RAM
		}
	}
	cols := stDim.Render(fmt.Sprintf("  %-18s %-9s %-12s %-14s %-16s %-6s %-3s %s",
		"NAME", "STATE", "HEALTH", "CPU", "MEM", "UP", "↻", "ENDPOINT"))
	lines := []string{head, "", cols}
	var attention []string
	for i, v := range apps {
		sel := i == min(m.appsSel, len(apps)-1)
		barCh := "  "
		if sel {
			barCh = stAccent.Render("▌ ")
		}
		state := stDim.Render("· asleep")
		if v.PID != 0 {
			state = stGreen.Render("● run")
		}
		health := stDim.Render("— no probe")
		switch {
		case v.Healthy != nil && *v.Healthy:
			health = stGreen.Render("✓ healthy")
		case v.Healthy != nil:
			health = stErr.Render("✗ failing")
		}
		up := "—"
		if v.PID != 0 && !v.StartedAt.IsZero() {
			up = shortDurTUI(time.Since(v.StartedAt))
		}
		restarts := stDim.Render("0")
		if v.RestartCount > 0 {
			restarts = lipgloss.NewStyle().Foreground(cGold).Render(fmt.Sprintf("%d", v.RestartCount))
		}
		row := fmt.Sprintf("%s%-18s %s %s %s %s %s %s %s",
			barCh, truncate(v.Name, 18),
			padANSI(state, 9), padANSI(health, 12),
			bar(v.CPU/100, 5)+stDim.Render(fmt.Sprintf(" %3.0f%%", v.CPU)),
			bar(float64(v.RAM)/float64(maxRAM), 5)+stDim.Render(fmt.Sprintf(" %8s", ui.HumanBytes(v.RAM))),
			stDim.Render(fmt.Sprintf("%-6s", up)), padANSI(restarts, 3),
			stDim.Render(truncate(connectLine(v), max(16, w-82))))
		if sel {
			row = lipgloss.NewStyle().Background(cSel).Render(row)
		}
		lines = append(lines, truncate(row, w))
		for _, c := range v.Children {
			child := fmt.Sprintf("      %s %s %s %s",
				stFaint.Render("└─ "+truncate(c.Cmd, 24)+fmt.Sprintf(" %d", c.PID)),
				strings.Repeat(" ", max(0, 24-len(truncate(c.Cmd, 24)))),
				bar(c.CPU/100, 5)+stDim.Render(fmt.Sprintf(" %3.0f%%", c.CPU)),
				bar(float64(c.RSS)/float64(maxRAM), 5)+stDim.Render(fmt.Sprintf(" %8s", ui.HumanBytes(c.RSS))))
			lines = append(lines, truncate(child, w))
		}
		if v.RestartCount > 0 || (v.Healthy != nil && !*v.Healthy) || v.LastError != "" {
			note := v.Name
			switch {
			case v.LastError != "":
				note += " — " + v.LastError
			case v.RestartCount > 0:
				note += fmt.Sprintf(" restarted %d× recently", v.RestartCount)
			default:
				note += " — health probe failing"
			}
			attention = append(attention, note)
		}
	}
	if len(apps) == 0 {
		lines = append(lines, "", stDim.Render("  no process blocks declared — add one to processes.doze.hcl"))
	}

	// ── attention ──
	for _, a := range attention {
		lines = append(lines, lipgloss.NewStyle().Foreground(cGold).Render("  ⚠ "+truncate(a, w-4)))
	}

	// ── logs of the selection ──
	logBudget := m.height - len(lines) - 5
	if v, ok := m.appsSelected(); ok && logBudget > 2 {
		t := stLabel.Render("logs") + stDim.Render(" · "+v.Name+" · following")
		lines = append(lines, "", stFaint.Render("╌╌ ")+t+" "+stFaint.Render(strings.Repeat("╌", max(1, w-6-lipgloss.Width(t)))))
		logs := m.logLines
		if len(logs) > logBudget {
			logs = logs[len(logs)-logBudget:]
		}
		for _, l := range logs {
			lines = append(lines, truncate("  "+l, w))
		}
	}

	// staged confirm, footer-style
	if m.appsPending != "" {
		verb, name, _ := strings.Cut(m.appsPending, ":")
		lines = append(lines, "", lipgloss.NewStyle().Foreground(cGold).Render(
			fmt.Sprintf("  %s %s? y/n", dashVerbLabel(verb), name)))
	}

	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(m.width).Height(m.height - 2).Render(body)
}

// padANSI pads a styled string to the given VISIBLE width.
func padANSI(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// shortDurTUI renders an uptime compactly: 26m, 3h12m, 2d.
func shortDurTUI(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
