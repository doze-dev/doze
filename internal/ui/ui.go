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
// The values are the shared light/dark pairs in colors.go — the same accent and
// state colors the TUI's default theme uses (internal/tui/theme.go) — so
// `doze status` and the dash speak the same visual vocabulary, and both stay
// readable on light terminals.
var (
	accentColor = lipgloss.AdaptiveColor{Light: AccentLight, Dark: AccentDark}
	dimColor    = lipgloss.AdaptiveColor{Light: DimLight, Dark: DimDark}
	goodColor   = lipgloss.AdaptiveColor{Light: GoodLight, Dark: GoodDark}
	badColor    = lipgloss.AdaptiveColor{Light: BadLight, Dark: BadDark}
	warnColor   = lipgloss.AdaptiveColor{Light: WarnLight, Dark: WarnDark}
	coolColor   = lipgloss.AdaptiveColor{Light: CoolLight, Dark: CoolDark}

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(dimColor)
	dimStyle    = lipgloss.NewStyle().Foreground(dimColor)
	goodStyle   = lipgloss.NewStyle().Foreground(goodColor)
	badStyle    = lipgloss.NewStyle().Foreground(badColor)
	warnStyle   = lipgloss.NewStyle().Foreground(warnColor)

	stateStyles = map[string]lipgloss.Style{
		"active":   goodStyle.Bold(true),
		"idle":     lipgloss.NewStyle().Foreground(warnColor),
		"booting":  lipgloss.NewStyle().Foreground(coolColor),
		"waking":   lipgloss.NewStyle().Foreground(coolColor), // display word for booting
		"reaped":   dimStyle,
		"asleep":   dimStyle, // display word for reaped
		"disabled": dimStyle,
		"error":    badStyle.Bold(true),
		"tainted":  badStyle.Bold(true),
		"running":  goodStyle,
	}
)

// enabled / errEnabled control whether we emit ANSI styling on stdout / stderr
// respectively. Each is off when its own stream isn't a terminal or NO_COLOR is
// set, so `doze … | grep` keeps colored stderr while `doze … 2>file` stays plain.
var (
	enabled    = colorEnabledFor(os.Stdout)
	errEnabled = colorEnabledFor(os.Stderr)
)

func colorEnabledFor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func paint(st lipgloss.Style, s string) string {
	if !enabled {
		return s
	}
	return st.Render(s)
}

func paintErr(st lipgloss.Style, s string) string {
	if !errEnabled {
		return s
	}
	return st.Render(s)
}

// Styling helpers (color-gated on stdout — use these for text printed there).
func Title(s string) string  { return paint(titleStyle, s) }
func Muted(s string) string  { return paint(dimStyle, s) }
func OK(s string) string     { return paint(goodStyle, s) }
func Fail(s string) string   { return paint(badStyle, s) }
func Warn(s string) string   { return paint(warnStyle, s) }
func Header(s string) string { return paint(headerStyle, s) }

// Stderr variants (color-gated on stderr — use these for text printed there).
func ErrMuted(s string) string { return paintErr(dimStyle, s) }
func ErrOK(s string) string    { return paintErr(goodStyle, s) }
func ErrFail(s string) string  { return paintErr(badStyle, s) }

// Width returns the visible (ANSI-stripped) width of s, for manual column layout.
func Width(s string) int { return lipgloss.Width(s) }

// State renders a lifecycle state in its color.
func State(s string) string {
	if st, ok := stateStyles[s]; ok {
		return paint(st, s)
	}
	return s
}

// StateGlyph renders the dash's state glyph for a lifecycle state, in the same
// color vocabulary as the TUI (internal/tui): active=● green, idle=○ gold,
// waking/booting=◌ cyan (the static stand-in for the dash's spinner), error=✕,
// tainted=! (bold red), disabled=⊘, asleep/anything else=· dim.
func StateGlyph(state string) string {
	switch state {
	case "active", "running":
		return paint(stateStyles["active"], "●")
	case "idle":
		return paint(stateStyles["idle"], "○")
	case "booting", "waking":
		return paint(stateStyles["waking"], "◌")
	case "error":
		return paint(stateStyles["error"], "✕")
	case "tainted":
		return paint(stateStyles["tainted"], "!")
	case "disabled":
		return paint(dimStyle, "⊘")
	default:
		return paint(dimStyle, "·") // asleep / reaped / unknown
	}
}

// Table accumulates rows and renders them as aligned columns, accounting for
// ANSI width so colored cells don't break alignment. It carries doze's table
// conventions: a 2-space indent, a 3-space gutter, a muted header, no trailing
// padding after the last column, and optional muted group labels between rows.
type Table struct {
	header []string
	lines  []tableLine
}

type tableLine struct {
	cells []string // nil for a label line
	label string
}

// NewTable starts a table with the given header cells (empty strings render as
// blank, headerless columns).
func NewTable(header ...string) *Table {
	return &Table{header: header}
}

// Row appends one data row. Cells may carry their own ANSI styling.
func (t *Table) Row(cells ...string) {
	t.lines = append(t.lines, tableLine{cells: cells})
}

// Label appends a full-width muted group label (rendered unindented, so it
// reads as a section heading between row groups).
func (t *Table) Label(s string) {
	t.lines = append(t.lines, tableLine{label: s})
}

// String renders the table. Column widths are shared across the whole table so
// every group lines up.
func (t *Table) String() string {
	widths := make([]int, len(t.header))
	for i, h := range t.header {
		widths[i] = lipgloss.Width(h)
	}
	for _, ln := range t.lines {
		for i, cell := range ln.cells {
			if i < len(widths) && lipgloss.Width(cell) > widths[i] {
				widths[i] = lipgloss.Width(cell)
			}
		}
	}
	var b strings.Builder
	b.WriteString(tableRow(t.header, widths, true))
	for _, ln := range t.lines {
		b.WriteString("\n")
		if ln.cells == nil {
			b.WriteString(Muted(ln.label))
			continue
		}
		b.WriteString(tableRow(ln.cells, widths, false))
	}
	return b.String()
}

// tableRow lays out one line with shared column widths. The final column is
// left unpadded (no trailing whitespace) so simple line parsers stay reliable.
func tableRow(cells []string, widths []int, header bool) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		val := c
		if header {
			val = paint(headerStyle, c)
		}
		if i == len(cells)-1 {
			parts[i] = val // last column: no trailing pad
			continue
		}
		w := 0
		if i < len(widths) {
			w = widths[i]
		}
		if d := w - lipgloss.Width(c); d > 0 {
			val += strings.Repeat(" ", d)
		}
		parts[i] = val
	}
	return strings.TrimRight("  "+strings.Join(parts, "   "), " ")
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

// ProcNode is one descendant process of a supervised root — the app's own
// children (a `go run` build's compiled child, a shell's workers), each with
// its own meters so a dashboard can render the tree htop-style.
type ProcNode struct {
	PID   int
	RSS   int64
	CPU   float64
	Cmd   string
	Depth int // 1 = direct child of the root
}

// ProcTree returns each root's descendant processes (depth-first, one ps pass).
func ProcTree(roots []int) map[int][]ProcNode {
	out := make(map[int][]ProcNode, len(roots))
	if len(roots) == 0 {
		return out
	}
	raw, err := exec.Command("ps", "-Ao", "pid=,ppid=,rss=,%cpu=,comm=").Output()
	if err != nil {
		return out
	}
	type proc struct {
		rss int64
		cpu float64
		cmd string
	}
	procs := map[int]proc{}
	children := map[int][]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		kb, _ := strconv.ParseInt(f[2], 10, 64)
		cpu, _ := strconv.ParseFloat(f[3], 64)
		cmd := strings.Join(f[4:], " ")
		if i := strings.LastIndexByte(cmd, '/'); i >= 0 {
			cmd = cmd[i+1:] // comm can be a full path; the basename reads better
		}
		procs[pid] = proc{rss: kb * 1024, cpu: cpu, cmd: cmd}
		children[ppid] = append(children[ppid], pid)
	}
	for _, root := range roots {
		var walk func(pid, depth int)
		seen := map[int]bool{root: true}
		walk = func(pid, depth int) {
			for _, c := range children[pid] {
				if seen[c] {
					continue
				}
				seen[c] = true
				cp := procs[c]
				out[root] = append(out[root], ProcNode{PID: c, RSS: cp.rss, CPU: cp.cpu, Cmd: cp.cmd, Depth: depth})
				walk(c, depth+1)
			}
		}
		walk(root, 1)
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
