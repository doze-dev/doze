package dash

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

// The meters across the top of Home: what the stack consumes, and who. Each bar
// is the whole of what is consumed, cut into one run per instance, largest
// first, and the line under it names the runs in the same colours. There is no
// "of": a bar is not how full anything is, so nothing is said to be a limit.

// share is one instance's part of a total.
type share struct {
	name string
	v    float64
}

// shareColours are the runs of a bar, largest first; the rest are dim. Red is
// not among them: it means something is wrong.
var shareColours = []string{hexGood, hexCool, hexAccent, hexWarn, "#FF9E64"}

// plainRuns tell the runs apart when there is no colour.
var plainRuns = []string{"|", "#", "=", "+", ":"}

// shown is how many instances a key names before it says "+N".
const shown = 5

func (m *Model) runStyle(i int) lipgloss.Style {
	if m.st.plain {
		return lipgloss.NewStyle()
	}
	if i >= len(shareColours) {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(hexDim))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(shareColours[i]))
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
func (m *Model) composition(name, total string, all []share, floor float64, cells int, unit func(float64) string, keyed bool) []string {
	s := m.st
	const head = 6 + 11 // "mem   " and the total's column
	inner := max(cells-head-2, 8)
	// an instance doing next to nothing has no run to show: it is counted, not drawn
	var shares []share
	idle := 0
	for _, x := range all {
		if x.v > floor {
			shares = append(shares, x)
		} else {
			idle++
		}
	}
	sort.SliceStable(shares, func(i, j int) bool { return shares[i].v > shares[j].v })
	var sum float64
	for _, x := range shares {
		sum += x.v
	}

	// the runs: every share at least a cell, the rest in proportion
	var bar strings.Builder
	if sum > 0 {
		cellsOf := make([]int, len(shares))
		left := inner
		for i, x := range shares {
			cellsOf[i] = max(int(x.v/sum*float64(inner)+0.5), 1)
			left -= cellsOf[i]
		}
		// take or give the difference from the largest, so the bar is exactly full
		for i := 0; left != 0 && i < len(cellsOf); i++ {
			d := left
			if cellsOf[i]+d < 1 {
				d = 1 - cellsOf[i]
			}
			cellsOf[i] += d
			left -= d
		}
		for i, n := range cellsOf {
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
		nm := ansiTruncate(x.name, 14)
		part := m.runStyle(i).Render(nm) + " " + s.d.Render(unit(x.v))
		w := width(nm) + 1 + width(unit(x.v))
		if s.plain {
			part = runChar(true, i) + " " + part
			w += 2
		}
		more := len(shares) - i - 1
		reserve := 0
		if more > 0 {
			reserve = 3 + len(fmt.Sprintf("+%d more", more))
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
		line2 += s.d.Render(fmt.Sprintf(" · +%d more", rest))
	}
	if idle > 0 {
		line2 += s.d.Render(fmt.Sprintf(" · %d idle", idle))
	}
	return []string{line, line2}
}

// memUnit formats a share of memory: plain megabytes while the total is under a
// gigabyte, so the key reads as a column of numbers.
func memUnit(total float64) func(float64) string {
	if total >= 1<<30 {
		return func(v float64) string { return mb(int64(v)) }
	}
	return func(v float64) string { return fmt.Sprintf("%d", (int64(v)+(1<<19))>>20) }
}

// cores says a CPU figure (ps's percent, one core being 100) as cores, which is
// what it is: 0.23 cores is a fifth of one.
func cores(pct float64) string {
	c := pct / 100
	switch {
	case c <= 0:
		return "0 cores"
	case c < 0.005:
		return "<0.01 cores"
	case c < 0.995:
		return fmt.Sprintf("%.2f cores", c)
	case c < 1.05:
		return "1 core"
	}
	return fmt.Sprintf("%.1f cores", c)
}

func coresUnit(pct float64) string {
	return strings.TrimSuffix(strings.TrimSuffix(cores(pct), " cores"), " core")
}
