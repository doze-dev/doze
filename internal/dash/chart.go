package dash

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/doze-dev/doze/internal/display"
)

// A chart as the dash draws it: the metric's name and figure over it, the scale
// down its left edge, the newest reading at the right, and an age axis under the
// last of a group.
//
// The body is solid blocks in a dim tone and the top cell of each column, the
// cap, is the bright one, in eighths of a block: r rows have 8r levels. A chart
// of w cells holds 2w readings, two to a cell, merged by their highest so a
// spike is never lost to the width. Nothing here needs a graphics protocol.

// gutter is the width of the scale at a chart's left edge.
const gutter = 8

// trace is one metric to draw.
type trace struct {
	name  string
	vals  []float64
	floor float64 // the smallest top of the scale, so a quiet series is not stretched
	zoom  bool    // scale from just under the lowest reading, not from zero
	unit  func(float64) string
	now   string  // the current figure, as the box would say it
	cur   float64 // and as a number, for a series with no history yet
	st    lipgloss.Style
}

// scale is the range the chart stands for, as display works it out from the
// trace's floor and whether it zooms.
func (t trace) scale(vs []float64) (lo, hi float64) { return display.Scale(vs, t.floor, t.zoom) }

// plot draws vs cells wide and rows tall between lo and hi, newest at the right
// edge, and returns the rows top first. More than a reading to a cell are merged
// by their highest, so a spike is never lost to the width; fewer are spread.
func plot(vs []float64, cells, rows int, lo, hi float64, bright, dim lipgloss.Style) []string {
	if cells < 1 || rows < 1 {
		return nil
	}
	if len(vs) > 2*cells {
		vs = vs[len(vs)-2*cells:]
	}
	col := make([]float64, cells)
	switch n := len(vs); {
	case n == 0:
		for i := range col {
			col[i] = lo
		}
	case n <= cells:
		// a history that has only just begun is drawn as it is, at a reading to a
		// cell and ending at the right edge, not stretched into wide steps
		for i := range col {
			col[i] = math.NaN()
		}
		copy(col[cells-n:], vs)
	default:
		// two readings to a cell, counted from the newest so the right edge is now
		for i := cells - 1; i >= 0; i-- {
			end := n - 2*(cells-1-i)
			a, b := max(end-2, 0), max(end, 0)
			m := lo
			for _, v := range vs[a:b] {
				m = max(m, v)
			}
			col[i] = m
		}
	}
	levels := rows * 8
	level := make([]int, cells)
	for i, v := range col {
		if math.IsNaN(v) {
			continue // before the history began
		}
		l := int((v-lo)/(hi-lo)*float64(levels) + 0.5)
		if v > lo && l == 0 {
			l = 1 // a reading is never invisible
		}
		level[i] = min(max(l, 0), levels)
	}
	steps := []rune(" ▁▂▃▄▅▆▇█")
	out := make([]string, rows)
	for r := rows - 1; r >= 0; r-- { // top row first
		var b strings.Builder
		for c := 0; c < cells; c++ {
			l := level[c]
			n := l - 8*r
			switch {
			case l == 0 || n <= 0:
				b.WriteByte(' ')
			case (l-1)/8 == r: // the cap: the cell the line ends in
				b.WriteString(bright.Render(string(steps[min(n, 8)])))
			default: // the body under it
				b.WriteString(dim.Render("█"))
			}
		}
		out[rows-1-r] = b.String()
	}
	return out
}

// dimmed is st's colour a good part of the way to the ground, for the body of a
// chart under its line. A plain style has no colour to dim.
func (s styles) dimmed(st lipgloss.Style) lipgloss.Style {
	fg := st.GetForeground()
	if _, none := fg.(lipgloss.NoColor); none {
		return st
	}
	hex := func(c color.Color) string {
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(hex(fg), s.pal.bg, 0.38)))
}

// A chart is never drawn wider than maxChart cells, which is as wide as a
// line gets before it only grows thinner, and never narrower than minChart.
const (
	maxChart = 60
	minChart = 28
	chartGap = 3 // blank cells between charts that sit side by side
)

// chartsAcross is how many of n charts sit side by side in w cells: as many as
// can have minChart cells each, and one per row when not even two can. This is
// the rule of boards/W-Charts.dc.html.
func chartsAcross(w, n int) int {
	for k := n; k > 1; k-- {
		if (w-(k-1)*chartGap)/k-gutter-1 >= minChart {
			return k
		}
	}
	return 1
}

// chartGroups is how many rows of charts n of them make in w cells.
func chartGroups(w, n int) int {
	per := chartsAcross(w, n)
	return (n + per - 1) / per
}

// chartH is how many rows each chart can have when groups rows of charts must
// fit in room lines: 0 when they cannot be drawn as charts at all. A group is a
// heading, the rows, the time axis and its labels, with a blank line between
// groups.
func chartH(room, groups int) int {
	for r := 8; r >= 2; r-- {
		if groups*(r+3)+(groups-1) <= room {
			return r
		}
	}
	return 0
}

// charts draws the traces in w cells, each rows high: side by side where they
// fit, and the rest under them.
func (m *Model) charts(ts []trace, w, rows int) []string {
	per := chartsAcross(w, len(ts))
	cw := min(maxChart, (w-(per-1)*chartGap)/per-gutter-1)
	one := gutter + 1 + cw
	var out []string
	for g := 0; g < len(ts); g += per {
		if g > 0 {
			out = append(out, "")
		}
		var cols [][]string
		for _, t := range ts[g:min(g+per, len(ts))] {
			cols = append(cols, m.chart(t, cw, rows))
		}
		for l := 0; l < len(cols[0]); l++ {
			var b strings.Builder
			for c, col := range cols {
				if c > 0 {
					b.WriteString(m.st.d.Render(" │ ")) // chartGap cells: a rule between them
				}
				b.WriteString(fit(col[l], one))
			}
			out = append(out, strings.TrimRight(b.String(), " "))
		}
	}
	return out
}
