package dash

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
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

// sampleEvery is how often the daemon samples the meters (control.Options
// leaves it at one second).
const sampleEvery = time.Second

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

// scale is the range the chart stands for: zero to a little above the highest
// reading (no lower than floor), or, zoomed, a little either side of the
// readings so that a steady series reads as steady and a drift as a drift.
func (t trace) scale(vs []float64) (lo, hi float64) {
	if len(vs) == 0 {
		return 0, max(t.floor, 1)
	}
	mn, mx := vs[0], vs[0]
	for _, v := range vs {
		mn, mx = min(mn, v), max(mx, v)
	}
	if t.zoom {
		pad := max((mx-mn)*0.25, mx*0.03)
		if pad == 0 {
			pad = 1
		}
		return max(mn-pad, 0), mx + pad
	}
	return 0, max(t.floor, mx*1.15, 1)
}

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
func dimmed(st lipgloss.Style) lipgloss.Style {
	fg := st.GetForeground()
	if _, none := fg.(lipgloss.NoColor); none || fg == nil {
		return st
	}
	hex := func(c color.Color) string {
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(hex(fg), hexBg, 0.38)))
}

// chartH is how many rows each chart can have when n of them and a time axis
// must fit in room lines: 0 when they cannot be drawn as charts at all.
func chartH(room, n int) int {
	for r := 8; r >= 2; r-- {
		if n*(r+1)+(n-1)+1 <= room { // each a heading and r rows, a blank between, one axis
			return r
		}
	}
	return 0
}

// charts draws the traces w cells wide, each rows high, with one age axis
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
		peak, mean := stats(t.vals)
		head := s.w.Render(t.name) + "  " + s.b.Render(t.now)
		if len(t.vals) > 1 {
			head += s.d.Render("   peak " + t.unit(peak) + " · avg " + t.unit(mean))
		}
		out = append(out, head)
		vals := t.vals
		if len(vals) == 0 {
			vals = []float64{t.cur} // nothing sampled yet: the figure it has now
		}
		if len(vals) > 2*cw {
			vals = vals[len(vals)-2*cw:]
		}
		n = max(n, len(vals))
		lo, hi := t.scale(vals)
		lines := plot(vals, cw, rows, lo, hi, t.st, dimmed(t.st))
		for j, l := range lines {
			g := ""
			switch j {
			case 0:
				g = t.unit(hi)
			case len(lines) - 1:
				g = t.unit(lo)
			}
			out = append(out, s.d.Render(right(g, gutter))+" "+l)
		}
	}
	// the age of the oldest reading, under where the line begins
	span := time.Duration(max(n, 1)) * sampleEvery
	left := ago(span) + " ago"
	indent := 0
	if n < cw {
		indent = cw - n
	}
	fill := max(cw-indent-width(left)-5, 0)
	out = append(out, s.d.Render(strings.Repeat(" ", gutter+1+indent)+left+" "+strings.Repeat("┈", fill)+" now"))
	return out
}

// pct and count are the scales' labels.
func pct(v float64) string { return fmt.Sprintf("%.0f%%", v) }
func count(v float64) string {
	return fmt.Sprintf("%.0f", v)
}
func bytesLabel(v float64) string { return mb(int64(v)) }
