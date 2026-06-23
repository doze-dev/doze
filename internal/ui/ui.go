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
var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#888888"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	goodStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#43BF6D"))
	badStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E06C75"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E0A82E"))

	stateStyles = map[string]lipgloss.Style{
		"active":  goodStyle.Bold(true),
		"idle":    lipgloss.NewStyle().Foreground(lipgloss.Color("#E0A82E")),
		"booting": lipgloss.NewStyle().Foreground(lipgloss.Color("#3C9DD0")),
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
func Title(s string) string { return paint(titleStyle, s) }
func Muted(s string) string { return paint(dimStyle, s) }
func OK(s string) string    { return paint(goodStyle, s) }
func Fail(s string) string  { return paint(badStyle, s) }
func Warn(s string) string  { return paint(warnStyle, s) }

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
