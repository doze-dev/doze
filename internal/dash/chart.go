package dash

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// A chart as the dash draws it: the metric's name and figure over it, the scale
// down its left edge (the top of the chart and zero), the newest reading at the
// right edge, and a time axis under the last of a group. Each row holds eighths
// of a block, so a chart of r rows has 8r levels; nothing else in the terminal
// draws a solid area at that resolution on every emulator.

// gutter is the width of the scale at a chart's left edge.
const gutter = 8

// sampleEvery is how often the daemon samples the meters (control.Options
// leaves it at one second).
const sampleEvery = time.Second

// trace is one metric to draw.
type trace struct {
	name  string
	vals  []float64
	floor float64 // the smallest top of the scale, so a quiet series is not stretched
	unit  func(float64) string
	now   string // the current figure, as the box would say it
	st    lipgloss.Style
}

// stats is the highest and the mean of what the chart holds.
func stats(vs []float64) (peak, mean float64) {
	for _, v := range vs {
		peak = max(peak, v)
		mean += v
	}
	if len(vs) > 0 {
		mean /= float64(len(vs))
	}
	return
}

// chartH is how many rows each chart can have when n of them and a time axis
// must fit in room lines: 0 when they cannot be drawn as charts at all.
func chartH(room, n int) int {
	for r := 6; r >= 2; r-- {
		if n*(r+1)+(n-1)+1 <= room { // each a heading and r rows, a blank between, one axis
			return r
		}
	}
	return 0
}

// charts draws the traces w cells wide, each rows high, with one time axis
// under the last.
func (m *Model) charts(ts []trace, w, rows int) []string {
	s := m.st
	cw := w - gutter - 1
	var out []string
	n := 0
	for i, t := range ts {
		if i > 0 {
			out = append(out, "")
		}
		if len(t.vals) > n {
			n = len(t.vals)
		}
		peak, mean := stats(t.vals)
		head := s.w.Render(t.name) + "  " + s.b.Render(t.now)
		if len(t.vals) > 1 {
			head += s.d.Render("   peak " + t.unit(peak) + " · avg " + t.unit(mean))
		}
		out = append(out, head)
		lines, top := areaScaled(t.vals, cw, rows, t.floor)
		for i, l := range lines {
			g := ""
			switch i {
			case 0:
				g = t.unit(top)
			case len(lines) - 1:
				g = "0"
			}
			out = append(out, s.d.Render(right(g, gutter))+" "+t.st.Render(l))
		}
	}
	span := time.Duration(min(max(n, 1), cw)) * sampleEvery
	left := ago(span) + " ago"
	fill := cw - width(left) - 5
	if fill < 0 {
		fill = 0
	}
	out = append(out, s.d.Render(strings.Repeat(" ", gutter)+" "+left+" "+strings.Repeat("┈", fill)+" now"))
	return out
}

// pct and count are the scales' labels.
func pct(v float64) string { return fmt.Sprintf("%.0f%%", v) }
func count(v float64) string {
	return fmt.Sprintf("%.0f", v)
}
func bytesLabel(v float64) string { return mb(int64(v)) }
