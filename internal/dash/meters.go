package dash

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/doze-dev/doze/internal/display"
)

// The meters across the top of Home: what the stack consumes, and who. Each bar
// is the whole of what is consumed, cut into one run per instance, largest
// first, and the line under it names the runs in the same colours. There is no
// "of": a bar is not how full anything is, so nothing is said to be a limit.

// share is one instance's part of a total, as display works the shares out.
type share = display.Share

// shareColours are the runs of a bar, largest first; the rest are dim. Red is
// not among them: it means something is wrong.
// shareColours are the palette's, in that order.
func shareColours(p palette) []string { return []string{p.good, p.cool, p.accent, p.warn, p.extra} }

// plainRuns tell the runs apart when there is no colour.
var plainRuns = []string{"|", "#", "=", "+", ":"}

// shown is the most instances a key names before it says "+N"; the width
// usually stops it sooner.
const shown = 8

func (m *Model) runStyle(i int) lipgloss.Style {
	if m.st.plain {
		return lipgloss.NewStyle()
	}
	cols := shareColours(m.st.pal)
	if i >= len(cols) {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(m.st.pal.dim))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(cols[i]))
}

func runChar(plain bool, i int) string {
	if !plain {
		return "|"
	}
	if i >= len(plainRuns) {
		return "."
	}
	return plainRuns[i]
}

// composition draws a meter: its name and total, the bar, and (when keyed) the
// line that says whose each colour is. unit formats one instance's part.
func (m *Model) composition(name, total string, all []share, floor float64, cells int, unit, sumOf func(float64) string, keyed bool) []string {
	s := m.st
	const head = 6 + 11 // "mem   " and the total's column
	inner := max(cells-head-2, 8)
	// an instance doing next to nothing has no run to show: it is counted, not drawn
	shares, idle, sum := display.Shares(all, floor)

	// the runs: every share at least a cell, the rest in proportion
	var bar strings.Builder
	if sum > 0 {
		for i, n := range display.ShareCells(shares, sum, inner) {
			bar.WriteString(m.runStyle(i).Render(strings.Repeat(runChar(s.plain, i), n)))
		}
	} else {
		bar.WriteString(strings.Repeat(" ", inner))
	}
	line := s.a.Bold(true).Render(fit(name, 6)) + s.w.Render(fit(total, 11)) + s.d.Render("[") + bar.String() + s.d.Render("]")
	if !keyed {
		return []string{line}
	}

	// the key: the biggest few by name, then how many more
	key := strings.Repeat(" ", head)
	if sum == 0 {
		return []string{line, key + s.d.Render(fmt.Sprintf("%d awake, none using any", idle))}
	}
	room := inner + 2
	var parts []string
	used := 0
	for i, x := range shares {
		if i >= shown {
			break
		}
		nm := ansiTruncate(x.Name, 14)
		part := m.runStyle(i).Render(nm) + " " + s.d.Render(unit(x.V))
		w := width(nm) + 1 + width(unit(x.V))
		if s.plain {
			part = runChar(true, i) + " " + part
			w += 2
		}
		more := len(shares) - i - 1
		reserve := 0
		if more > 0 {
			reserve = 3 + len(fmt.Sprintf("+%d more · 000.0 GB", more))
			if idle > 0 {
				reserve += 3 + len(fmt.Sprintf("%d idle", idle))
			}
		}
		if used+w+reserve+3*min(len(parts), 1) > room && len(parts) > 0 {
			break
		}
		parts = append(parts, part)
		used += w + 3*min(len(parts)-1, 1)
	}
	line2 := key + strings.Join(parts, s.d.Render(" · "))
	if rest := len(shares) - len(parts); rest > 0 {
		var restSum float64
		for _, x := range shares[len(parts):] {
			restSum += x.V
		}
		line2 += s.d.Render(fmt.Sprintf(" · +%d more · %s", rest, sumOf(restSum)))
	}
	if idle > 0 {
		line2 += s.d.Render(fmt.Sprintf(" · %d idle", idle))
	}
	return []string{line, line2}
}
