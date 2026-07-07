// Package ui holds the shared visual vocabulary for doze's CLI and TUI: the
// color palette, state styling, an ANSI-aware table renderer, and small
// formatters (RAM, uptime). Color is auto-disabled when stdout is not a terminal
// or NO_COLOR is set, so piped output stays plain.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Palette (unexported; render through the helpers below so color can be gated).
// The values are light/dark pairs matching the TUI's default theme (the accent
// and state colors in internal/tui — the source of truth), so `doze status` and
// the dash speak the same visual vocabulary, and both stay readable on light
// terminals.
var (
	accentColor = lipgloss.AdaptiveColor{Light: "#6F42C1", Dark: "#BD93F9"}
	dimColor    = lipgloss.AdaptiveColor{Light: "#666C78", Dark: "#7C8290"}
	goodColor   = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#7EE787"}
	badColor    = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#FF7A93"}
	warnColor   = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#F2C879"}
	coolColor   = lipgloss.AdaptiveColor{Light: "#0969DA", Dark: "#82AAFF"}

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(dimColor)
	dimStyle    = lipgloss.NewStyle().Foreground(dimColor)
	goodStyle   = lipgloss.NewStyle().Foreground(goodColor)
	badStyle    = lipgloss.NewStyle().Foreground(badColor)
	warnStyle   = lipgloss.NewStyle().Foreground(warnColor)

	stateStyles = map[string]lipgloss.Style{
		"active":  goodStyle.Bold(true),
		"idle":    lipgloss.NewStyle().Foreground(warnColor),
		"booting": lipgloss.NewStyle().Foreground(coolColor),
		"reaped":  dimStyle,
		"error":   badStyle.Bold(true),
		"tainted": badStyle.Bold(true),
		"running": goodStyle,
	}
)

// enabled controls whether we emit ANSI styling. Off when stdout isn't a
// terminal or NO_COLOR is set, so piped/redirected output stays plain.
var enabled = colorEnabled()

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func paint(st lipgloss.Style, s string) string {
	if !enabled {
		return s
	}
	return st.Render(s)
}

// Styling helpers (color-gated).
func Title(s string) string  { return paint(titleStyle, s) }
func Muted(s string) string  { return paint(dimStyle, s) }
func OK(s string) string     { return paint(goodStyle, s) }
func Fail(s string) string   { return paint(badStyle, s) }
func Warn(s string) string   { return paint(warnStyle, s) }
func Header(s string) string { return paint(headerStyle, s) }

// Width returns the visible (ANSI-stripped) width of s, for manual column layout.
func Width(s string) int { return lipgloss.Width(s) }

// State renders a lifecycle state in its color.
func State(s string) string {
	if st, ok := stateStyles[s]; ok {
		return paint(st, s)
	}
	return s
}

// Table renders header + rows as aligned columns, accounting for ANSI width so
// colored cells don't break alignment. Columns are sized to their widest cell.
func Table(header []string, rows [][]string) string {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && lipgloss.Width(cell) > widths[i] {
				widths[i] = lipgloss.Width(cell)
			}
		}
	}
	var b strings.Builder
	b.WriteString(row(header, widths, true))
	for _, r := range rows {
		b.WriteString("\n")
		b.WriteString(row(r, widths, false))
	}
	return b.String()
}

// row renders one line: header cells get the header style; data cells keep any
// styling the caller already applied (e.g. a colored state). Padding is computed
// on the visible (ANSI-stripped) width so colored cells stay aligned.
func row(cells []string, widths []int, header bool) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		w := 0
		if i < len(widths) {
			w = widths[i]
		}
		val := c
		if header {
			val = paint(headerStyle, c)
		}
		parts[i] = val + pad(c, w)
	}
	return strings.Join(parts, "   ")
}

func pad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return strings.Repeat(" ", d)
	}
	return ""
}

// Uptime is a compact human duration since t ("-" for the zero time).
func Uptime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// HumanRAM returns a human RSS string for a pid, or "" if unavailable.
func HumanRAM(pid int) string { return HumanBytes(rssBytes(pid)) }

// RSSBytes returns resident memory for a pid in bytes (0 if unavailable).
func RSSBytes(pid int) int64 { return rssBytes(pid) }

// ProcStat is a process's resident memory and CPU usage.
type ProcStat struct {
	RSS int64   // root process RSS in bytes
	CPU float64 // CPU percent (one core = 100), summed across the process subtree
}

// ProcStats returns, for each root pid, its resident memory (the root's own RSS)
// and CPU usage summed across the root plus all descendant processes — so
// multi-process engines like Postgres, whose query work runs in per-connection
// child backends, report the work happening under them rather than just the idle
// parent. One `ps` call covers every root. CPU is the kernel's recent-usage value
// on macOS and ps's lifetime average on Linux.
func ProcStats(roots []int) map[int]ProcStat {
	out := make(map[int]ProcStat, len(roots))
	if len(roots) == 0 {
		return out
	}
	raw, err := exec.Command("ps", "-Ao", "pid=,ppid=,rss=,%cpu=").Output()
	if err != nil {
		for _, pid := range roots { // fall back to RAM-only, no tree
			out[pid] = ProcStat{RSS: rssBytes(pid)}
		}
		return out
	}
	type proc struct {
		rss int64
		cpu float64
	}
	procs := map[int]proc{}
	children := map[int][]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		kb, _ := strconv.ParseInt(f[2], 10, 64)
		cpu, _ := strconv.ParseFloat(f[3], 64)
		procs[pid] = proc{rss: kb * 1024, cpu: cpu}
		children[ppid] = append(children[ppid], pid)
	}
	for _, root := range roots {
		rp, ok := procs[root]
		if !ok {
			continue
		}
		cpu, seen, stack := 0.0, map[int]bool{}, []int{root}
		for len(stack) > 0 {
			pid := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[pid] {
				continue
			}
			seen[pid] = true
			cpu += procs[pid].cpu
			stack = append(stack, children[pid]...)
		}
		out[root] = ProcStat{RSS: rp.rss, CPU: cpu}
	}
	return out
}

// CPUStr formats a CPU percentage as a compact column value ("" when negative;
// otherwise e.g. "12%" or "0%").
func CPUStr(pct float64) string {
	if pct < 0 {
		return ""
	}
	return fmt.Sprintf("%.0f%%", pct)
}

// HumanBytes formats a byte count as a compact K/M/G string ("" for <= 0).
func HumanBytes(b int64) string {
	if b <= 0 {
		return ""
	}
	const unit = 1024
	switch {
	case b < unit*unit:
		return fmt.Sprintf("%dK", b/unit)
	case b < unit*unit*unit:
		return fmt.Sprintf("%dM", b/(unit*unit))
	default:
		return fmt.Sprintf("%.1fG", float64(b)/float64(unit*unit*unit))
	}
}

// rssBytes reads resident memory for a pid (Linux via /proc, macOS via ps).
func rssBytes(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid)); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) >= 2 {
			if pages, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				return pages * int64(syscall.Getpagesize())
			}
		}
		return 0
	}
	// macOS / BSD: ps reports RSS in kilobytes.
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	kb, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}
