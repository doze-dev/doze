// Charts: the braille line renderer, its axis gutter, and the detail card's
// memory, CPU and connection chart sections.

package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/doze-dev/doze/internal/ui"
)

// ── charts ──────────────────────────────────────────────────────────────────
// The bar: a developer glances and understands the shape in one second. So:
// a braille LINE (2×4 dots per cell — 8× the resolution of box-drawing glyphs
// in the same rows), a left axis gutter, stats in the section title, an empty
// right side, and no autoscale-amplified jitter — a steady series says so in
// words instead of drawing full-height noise.

// steadySeries reports whether a window has no movement worth charting: its
// range is under ~5% of its mean. Autoscaling such a series would amplify
// jitter into full-height noise, so callers render a flat line instead.
func steadySeries(vals []float64) bool {
	if len(vals) < 2 {
		return true
	}
	mn, mx, sum := vals[0], vals[0], 0.0
	for _, v := range vals {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
		sum += v
	}
	if mx == mn {
		return true
	}
	mean := sum / float64(len(vals))
	if mean <= 0 {
		return false
	}
	return (mx-mn)/mean < 0.05
}

// brailleDot is the bit for a dot at (dx, dy) inside one braille cell, dy 0..3
// top→bottom, dx 0 left / 1 right (the classic 2×4 dot numbering).
var brailleDot = [2][4]rune{
	{0x01, 0x02, 0x04, 0x40},
	{0x08, 0x10, 0x20, 0x80},
}

// resample fits vals onto n x-positions. Longer series are bucket-MEANED (not
// index-sampled — picking every k-th sample aliases away spikes between picks);
// shorter ones are linearly interpolated so the line stays continuous.
func resample(vals []float64, n int) []float64 {
	out := make([]float64, n)
	switch {
	case len(vals) == 0:
		return out
	case len(vals) == 1 || n == 1:
		for i := range out {
			out[i] = vals[len(vals)-1]
		}
	case len(vals) >= n: // bucket mean
		for i := range out {
			lo := i * len(vals) / n
			hi := max(lo+1, (i+1)*len(vals)/n)
			sum := 0.0
			for _, v := range vals[lo:hi] {
				sum += v
			}
			out[i] = sum / float64(hi-lo)
		}
	default: // linear interpolation
		for i := range out {
			pos := float64(i) * float64(len(vals)-1) / float64(n-1)
			j := int(pos)
			if j >= len(vals)-1 {
				out[i] = vals[len(vals)-1]
				continue
			}
			frac := pos - float64(j)
			out[i] = vals[j]*(1-frac) + vals[j+1]*frac
		}
	}
	return out
}

// brailleChart renders vals as a braille line: w×rows cells give 2w×4rows dots.
// The series is min/max-autoscaled to the dot grid (a flat series holds the
// middle), and consecutive points are connected vertically so the line never
// breaks. Returns rows plain strings, top row first (the caller colors them).
func brailleChart(vals []float64, w, rows int) []string {
	w, rows = max(1, w), max(1, rows)
	nx, ny := 2*w, 4*rows
	grid := make([][]rune, rows)
	for i := range grid {
		grid[i] = make([]rune, w)
		for j := range grid[i] {
			grid[i][j] = 0x2800 // empty braille cell, not a space: keeps columns stable
		}
	}
	if len(vals) > 0 {
		pts := resample(vals, nx)
		mn, mx := pts[0], pts[0]
		for _, v := range pts {
			if v < mn {
				mn = v
			}
			if v > mx {
				mx = v
			}
		}
		span := mx - mn
		lvl := func(v float64) int { // dot row 0 = top
			if span <= 0 {
				return ny / 2 // flat — hold the middle
			}
			return clampi(ny-1-int((v-mn)/span*float64(ny-1)+0.5), 0, ny-1)
		}
		set := func(x, y int) {
			grid[y/4][x/2] |= brailleDot[x%2][y%4]
		}
		prev := lvl(pts[0])
		set(0, prev)
		for x := 1; x < nx; x++ {
			cur := lvl(pts[x])
			set(x, cur)
			// Connect the step so the line reads as a line: fill the vertical gap
			// on this column, from just past the previous level to the new one.
			for y := min(prev, cur) + 1; y < max(prev, cur); y++ {
				set(x, y)
			}
			prev = cur
		}
	}
	out := make([]string, rows)
	for i, r := range grid {
		out[i] = string(r)
	}
	return out
}

// chartGutter builds the left axis column: the top row carries the peak label
// and '┤', the bottom row the low label and '┼', the rows between a bare '│'.
// Labels are right-aligned; pass "" to leave a row unlabelled (flat series
// label the bottom row only). All rows come back equal-width.
func chartGutter(rows int, top, bottom string) []string {
	lw := max(4, max(lipgloss.Width(top), lipgloss.Width(bottom)))
	out := make([]string, max(1, rows))
	for i := range out {
		lbl, axis := "", "│"
		switch i {
		case len(out) - 1:
			lbl, axis = bottom, "┼"
		case 0:
			lbl, axis = top, "┤"
		}
		out[i] = fmt.Sprintf("%*s %s", lw, lbl, axis)
	}
	return out
}

// chartSection assembles the shared section shape: a title row (stats left,
// window right-aligned) above `rows` of braille over the axis gutter.
func chartSection(title string, series []float64, top, bottom string, rows, inner int, line lipgloss.Style, window string) []string {
	if window != "" {
		right := stDim.Render("last " + window)
		if gap := inner - lipgloss.Width(title) - lipgloss.Width(right); gap > 0 {
			title += strings.Repeat(" ", gap) + right
		}
	}
	gut := chartGutter(rows, top, bottom)
	gw := max(8, inner-lipgloss.Width(gut[0])-1)
	g := brailleChart(series, gw, rows)
	out := []string{truncate(title, inner)}
	for i := 0; i < rows; i++ {
		out = append(out, stDim.Render(gut[i])+line.Render(g[i]))
	}
	return out
}

// memShort is the axis-gutter form of a memory value: one decimal, unit-less
// for MB (the section title carries the unit), "G"-suffixed above 1 GB.
func memShort(b int64) string {
	const mb = 1024 * 1024
	if b <= 0 {
		return "0"
	}
	if b < 1024*mb {
		return strconv.FormatFloat(float64(b)/mb, 'f', 1, 64)
	}
	return strconv.FormatFloat(float64(b)/(1024*mb), 'f', 1, 64) + "G"
}

// memBounds returns the min and max resident memory across the history window,
// which anchor the bottom and top of the auto-scaled trace (its y-axis).
func memBounds(h *history) (lo, hi int64) {
	if h == nil || len(h.ram) == 0 {
		return 0, 0
	}
	mn, mx := h.ram[0], h.ram[0]
	for _, v := range h.ram {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	return int64(mn), int64(mx)
}

// histWindow is the time span a history series currently covers (its x-extent).
func histWindow(n int) string {
	if n == 0 {
		return ""
	}
	return compactDur(time.Duration(n) * refreshMS)
}

// memorySection is the card's memory block: a section-title row carrying the
// stats ("memory · 10.58 MB now · peak 14.66 MB", the window right-aligned as
// "last 2m30s") above a braille line over a left axis gutter. The chart's right
// side stays empty. now==peak prints once; a steady series (<5% range of mean)
// is never autoscale-amplified — it draws flat with a "steady" title.
func memorySection(h *history, ramNow int64, inner int) []string {
	const rows = 4
	var series []float64
	if h != nil {
		series = h.ram
	}
	lo, hi := memBounds(h)
	cur := memStr(ramNow)
	steady := len(series) < 2 || steadySeries(series)

	title := stLabel.Render("memory") + stDim.Render(" · ")
	switch {
	case steady:
		title += stText.Render("steady") + stDim.Render(" · ") + stText.Bold(true).Render(orDash(cur))
	case memStr(hi) == cur:
		title += stText.Bold(true).Render(cur+" now") + stDim.Render(" (the peak)")
	default:
		title += stText.Bold(true).Render(cur+" now") + stDim.Render(" · peak "+memStr(hi))
	}

	topLbl, botLbl := memShort(hi), memShort(lo)
	if steady || topLbl == botLbl {
		topLbl, botLbl = "", memShort(max(lo, ramNow)) // flat: one value, bottom row
	}
	if steady {
		series = []float64{0, 0} // force the clean mid-height flat line
	}
	line := lipgloss.NewStyle().Foreground(cAccent)
	return chartSection(title, series, topLbl, botLbl, rows, inner, line, histWindow(len(seriesOr(h))))
}

// seriesOr is h.ram when h exists (the window length all sections share).
func seriesOr(h *history) []float64 {
	if h == nil {
		return nil
	}
	return h.ram
}

// cpuSection is the card's CPU block: one quiet row while usage is basically
// flat ("cpu · 2% for 2m30s"), or a title row plus a 2-row braille line once it
// moved. The threshold is ABSOLUTE (1 percentage point), not relative — an idle
// process jittering between 0.1% and 0.3% must not chart as full-height noise.
func cpuSection(h *history, now float64, inner int) []string {
	var series []float64
	if h != nil {
		series = h.cpu
	}
	if len(series) == 0 {
		return nil
	}
	mn, mx := series[0], series[0]
	for _, v := range series {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	if mx-mn < 1.0 { // never moved a whole point — quiet words, no chart
		if mx < 0.5 { // truly idle: the title fact already says "cpu 0%"
			return nil
		}
		return []string{stLabel.Render("cpu") + stDim.Render(" · ") +
			stText.Bold(true).Render(orDash(ui.CPUStr(now))) + stDim.Render(" for "+histWindow(len(series)))}
	}
	const rows = 2
	// The instantaneous reading may outrun the sampled window; the shown peak
	// must never read lower than "now".
	title := stLabel.Render("cpu") + stDim.Render(" · ") +
		stText.Bold(true).Render(ui.CPUStr(now)+" now") +
		stDim.Render(" · peak "+ui.CPUStr(max(mx, now)))
	line := lipgloss.NewStyle().Foreground(cGreen)
	return chartSection(title, series, ui.CPUStr(mx), ui.CPUStr(mn), rows, inner, line, "")
}

// connsSection is the card's connection block: a single quiet row while the
// count holds ("conns · 4 for 2m30s"), or — once it varied in the window — a
// title row plus a 2-row braille step line in the same gutter style.
func connsSection(h *history, now, inner int) []string {
	var series []float64
	if h != nil {
		series = h.conns
	}
	if len(series) == 0 {
		return nil
	}
	mn, mx := series[0], series[0]
	for _, v := range series {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	if int(mn) == int(mx) { // constant — one row of words, no chart
		return []string{stLabel.Render("conns") + stDim.Render(" · ") +
			stText.Bold(true).Render(fmt.Sprint(now)) + stDim.Render(" for "+histWindow(len(series)))}
	}
	const rows = 2
	title := stLabel.Render("conns") + stDim.Render(" · ") +
		stText.Bold(true).Render(fmt.Sprintf("now %d", now)) +
		stDim.Render(fmt.Sprintf(" · peak %d", int(mx)))
	line := lipgloss.NewStyle().Foreground(cCyan)
	return chartSection(title, series, strconv.Itoa(int(mx)), strconv.Itoa(int(mn)), rows, inner, line, "")
}
