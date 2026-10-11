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

// braille bits for a cell's two dot columns, top row first.
var (
	dotsL = [4]rune{0x01, 0x02, 0x04, 0x40}
	dotsR = [4]rune{0x08, 0x10, 0x20, 0x80}
)

// plotLine draws vs as a single line of braille dots: two readings to a cell
// across and four levels to a row down, each reading joined to the one before it
// so a spike is a stroke and not a dot. A faint dotted rule marks the top and
// the middle of the scale. Returns the rows top first.
func plotLine(vs []float64, cells, rows int, lo, hi float64, bright, dim lipgloss.Style) []string {
	if cells < 1 || rows < 1 {
		return nil
	}
	w, h := cells*2, rows*4
	if len(vs) > w {
		vs = vs[len(vs)-w:]
	}
	on := make([][]bool, h) // [y from the bottom][x]
	for i := range on {
		on[i] = make([]bool, w)
	}
	line := make([][]bool, h)
	for i := range line {
		line[i] = make([]bool, w)
	}
	// the scale's rules, every fourth dot
	for x := 0; x < w; x += 4 {
		on[h-1][x] = true
		on[h/2][x] = true
	}
	prev := -1
	for i, v := range vs {
		x := w - len(vs) + i // ends at the right edge
		y := int((v-lo)/(hi-lo)*float64(h-1) + 0.5)
		y = min(max(y, 0), h-1)
		a, b := y, y
		if prev >= 0 {
			a, b = min(prev, y), max(prev, y)
		}
		for yy := a; yy <= b; yy++ {
			on[yy][x], line[yy][x] = true, true
		}
		prev = y
	}
	out := make([]string, rows)
	for r := rows - 1; r >= 0; r-- {
		var b strings.Builder
		for c := 0; c < cells; c++ {
			g, ink := rune(0x2800), false
			for j := 0; j < 4; j++ { // j up from the cell's bottom
				y := r*4 + j
				if on[y][2*c] {
					g |= dotsL[3-j]
				}
				if on[y][2*c+1] {
					g |= dotsR[3-j]
				}
				ink = ink || line[y][2*c] || line[y][2*c+1]
			}
			switch {
			case g == 0x2800:
				b.WriteByte(' ')
			case ink:
				b.WriteString(bright.Render(string(g)))
			default:
				b.WriteString(dim.Render(string(g)))
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

// squeeze fits vs into w columns by merging neighbours by their highest, from
// the newest back, so the whole of the history is drawn and a spike is never
// lost to the width. Shorter than w it is returned as it is.
func squeeze(vs []float64, w int) []float64 {
	n := len(vs)
	if n <= w || w < 1 {
		return vs
	}
	per := (n + w - 1) / w
	cols := (n + per - 1) / per
	out := make([]float64, cols)
	for i := cols - 1; i >= 0; i-- {
		end := n - per*(cols-1-i)
		start := max(end-per, 0)
		m := vs[start]
		for _, v := range vs[start:end] {
			m = max(m, v)
		}
		out[i] = m
	}
	return out
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
// heading, the rows and the axis, with a blank line between groups.
func chartH(room, groups int) int {
	for r := 8; r >= 2; r-- {
		if groups*(r+2)+(groups-1) <= room {
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

// chart is one trace: its name and figure, the plot with its scale at the left,
// and the age of its oldest reading under it. Every line is at most one wide.
func (m *Model) chart(t trace, cw, rows int) []string {
	s := m.st
	peak, mean := stats(t.vals)
	head := s.w.Render(t.name) + "  " + s.b.Render(t.now)
	if len(t.vals) > 1 {
		head += s.d.Render("   peak " + t.unit(peak) + " · avg " + t.unit(mean))
	}
	vals := t.vals
	if len(vals) == 0 {
		vals = []float64{t.cur} // nothing sampled yet: the figure it has now
	}
	span := len(vals) // readings the chart stands for, however many dots they come to
	vals = squeeze(vals, 2*cw)
	lo, hi := t.scale(vals)
	dim := dimmed(t.st)
	var lines []string
	if m.areaCharts {
		lines = plot(vals, cw, rows, lo, hi, t.st, dim)
	} else {
		lines = plotLine(vals, cw, rows, lo, hi, t.st, dim)
	}
	out := []string{head}
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
	// the age of the oldest reading, under where the line begins
	left := ago(time.Duration(max(span, 1))*sampleEvery) + " ago"
	indent := 0
	if n := (len(vals) + 1) / 2; n < cw {
		indent = cw - n // a dot column is half a cell
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

// tiles draws the traces as one tile each: its name in the border, the figure
// and what it has been doing on one line, and under that a filled chart of it.
// As many sit across as can have tileMin cells, the rest under them
// (boards/V-A.dc.html).
const tileMin = 30

func (m *Model) tiles(ts []trace, w, rows int) []string {
	s := m.st
	per := 1
	for k := len(ts); k > 1; k-- {
		if (w-(k-1))/k >= tileMin {
			per = k
			break
		}
	}
	tw := (w - (per - 1)) / per
	th := rows + 3
	var out []string
	for g := 0; g < len(ts); g += per {
		var boxes [][]string
		for _, t := range ts[g:min(g+per, len(ts))] {
			vals := t.vals
			if len(vals) == 0 {
				vals = []float64{t.cur}
			}
			inner := tw - 4
			span := len(vals)
			peak, mean := stats(vals)
			sum := "peak " + t.unit(peak) + " · avg " + t.unit(mean)
			if t.zoom && span > 1 {
				d := vals[len(vals)-1] - vals[0]
				arrow := "→"
				switch {
				case d > 0:
					arrow = "↑"
				case d < 0:
					arrow, d = "↓", -d
				}
				sum = arrow + " " + t.unit(d) + " in " + ago(time.Duration(span)*sampleEvery)
			}
			vals = squeeze(vals, inner) // a column to a cell, so the whole history fits
			lo, hi := t.scale(vals)
			head := s.b.Render(t.now) + "  " + s.d.Render(fit(sum, max(inner-width(t.now)-2, 4)))
			lines := append([]string{head}, plot(vals, inner, rows, lo, hi, t.st, dimmed(t.st))...)
			boxes = append(boxes, box(s.border, s.d.Render(t.name), "", lines, tw, th))
		}
		for l := 0; l < th; l++ {
			var b strings.Builder
			for i, bx := range boxes {
				if i > 0 {
					b.WriteString(" ")
				}
				b.WriteString(bx[l])
			}
			out = append(out, b.String())
		}
	}
	return out
}
