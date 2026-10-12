package dash

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func plain(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r)
	}
	return out
}

// A chart is rows tall and cells wide however much it holds, newest at the
// right edge, and the highest reading reaches the top row.
func TestPlotShape(t *testing.T) {
	st := lipgloss.NewStyle()
	vs := []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	rows := plain(plot(vs, 10, 3, 0, 10, st, st))
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	for i, r := range rows {
		if n := width(r); n != 10 {
			t.Errorf("row %d is %d cells", i, n)
		}
	}
	if strings.TrimSpace(rows[0]) == "" {
		t.Error("the highest reading does not reach the top row")
	}
	// the body under the line is solid
	if !strings.Contains(strings.Join(rows, ""), "█") {
		t.Error("no solid body under the line")
	}
}

// Two readings to a cell: a chart of w cells holds 2w readings.
func TestPlotHoldsTwoReadingsToACell(t *testing.T) {
	st := lipgloss.NewStyle()
	vs := make([]float64, 40)
	for i := range vs {
		vs[i] = float64(i % 7)
	}
	a := plain(plot(vs, 20, 2, 0, 7, st, st))
	b := plain(plot(append([]float64{99, 99}, vs...), 20, 2, 0, 7, st, st))
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Error("a reading older than the chart's width changed what is drawn")
	}
}

// A zero reading draws nothing, and a reading above zero is never invisible.
func TestPlotNeverHidesAReading(t *testing.T) {
	st := lipgloss.NewStyle()
	zero := strings.TrimSpace(strings.Join(plain(plot([]float64{0, 0, 0, 0}, 2, 2, 0, 100, st, st)), ""))
	if zero != "" {
		t.Errorf("zero drew %q", zero)
	}
	tiny := strings.TrimSpace(strings.Join(plain(plot([]float64{0.001, 0.001, 0.001, 0.001}, 2, 2, 0, 100, st, st)), ""))
	if tiny == "" {
		t.Error("a small reading vanished")
	}
}

// A zoomed series is scaled to its own range: a memory line that moves by a
// couple of MB is seen to move, and a flat one sits mid-chart, not on the floor.
func TestZoomShowsDriftAndSitsFlatInTheMiddle(t *testing.T) {
	tr := trace{zoom: true}
	lo, hi := tr.scale([]float64{210, 210, 210})
	if !(lo < 210 && hi > 210) || lo < 0 {
		t.Errorf("flat series scale = %v..%v", lo, hi)
	}
	lo, hi = tr.scale([]float64{200, 210, 220})
	if lo > 200 || hi < 220 || hi-lo > 60 {
		t.Errorf("drifting series scale = %v..%v", lo, hi)
	}
	zero := trace{floor: 5}
	if lo, hi := zero.scale([]float64{0.4}); lo != 0 || hi != 5 {
		t.Errorf("a quiet cpu is %v..%v, want 0..5", lo, hi)
	}
}

// A process with no history yet, an outside one the daemon has not sampled, is
// drawn from the figure it has now rather than as an empty box; a short history
// ends at the right edge and is not stretched across the width.
func TestChartsWithLittleOrNoHistory(t *testing.T) {
	m := board()
	m.areaCharts = true // blocks, where an empty chart is plainly empty
	empty := plain(m.charts([]trace{{name: "mem", unit: bytesLabel, now: "148 MB", cur: 148 << 20, zoom: true, st: lipgloss.NewStyle()}}, 60, 3))
	if !strings.Contains(strings.Join(empty, "\n"), "█") && !strings.ContainsAny(strings.Join(empty, ""), "▁▂▃▄▅▆▇") {
		t.Errorf("no history drew an empty chart:\n%s", strings.Join(empty, "\n"))
	}
	short := plain(plot([]float64{1, 2, 3}, 20, 2, 0, 3, lipgloss.NewStyle(), lipgloss.NewStyle()))
	for _, row := range short {
		if strings.TrimSpace(row[:2]) != "" {
			t.Errorf("a three-reading history reaches the left edge: %q", row)
		}
		if strings.TrimSpace(row[len(row)-3:]) == "" && row == short[len(short)-1] {
			t.Errorf("a three-reading history does not end at the right edge: %q", row)
		}
	}
}

// The line chart is rows tall and cells wide, ends at the right edge, joins a
// jump into a stroke, and keeps its scale marks out of the line's colour.
func TestPlotLine(t *testing.T) {
	st := lipgloss.NewStyle()
	vs := make([]float64, 40)
	vs[20] = 10 // a spike in a flat line
	rows := plain(plotLine(vs, 20, 3, 0, 10, st, st))
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	for i, r := range rows {
		if n := width(r); n != 20 {
			t.Errorf("row %d is %d cells", i, n)
		}
	}
	// the spike reaches the top row and the stroke is drawn down its column
	col := 20 / 2
	for i, r := range rows {
		if []rune(r)[col] == ' ' {
			t.Errorf("row %d has no dot in the spike's column: %q", i, r)
		}
	}
	// the flat part is on the bottom row only (apart from the scale's rules)
	if !strings.ContainsAny(rows[2], "⣀⡀⢀⠀") && strings.TrimSpace(rows[2]) == "" {
		t.Error("the flat reading is not drawn on the bottom row")
	}
}

// Charts sit side by side while each can have minChart cells, and stack when
// they cannot: the rule of boards/W-Charts.dc.html.
func TestChartsAcrossWhenThereIsRoom(t *testing.T) {
	for _, c := range []struct{ w, n, want int }{
		{180, 3, 3}, // three across
		{120, 3, 3},
		{100, 3, 2}, // two across, the third under
		{80, 3, 2},
		{70, 3, 1}, // stacked
		{40, 2, 1},
		{100, 1, 1},
	} {
		if got := chartsAcross(c.w, c.n); got != c.want {
			t.Errorf("chartsAcross(%d, %d) = %d, want %d", c.w, c.n, got, c.want)
		}
	}
	if g := chartGroups(100, 3); g != 2 {
		t.Errorf("100 cells, 3 charts: %d groups, want 2", g)
	}
}

// Side by side is side by side on the screen: the three headings share a line,
// and no line is wider than the space it was given.
func TestChartsDrawnAcross(t *testing.T) {
	m := board()
	st := lipgloss.NewStyle()
	ts := []trace{
		{"cpu", []float64{1, 2, 3}, 5, false, pct, "1%", 1, st},
		{"mem", []float64{1, 2, 3}, 0, true, bytesLabel, "84 MB", 1, st},
		{"conns", []float64{1, 2, 3}, 8, false, count, "4", 1, st},
	}
	out := plain(m.charts(ts, 180, 3))
	if !strings.Contains(out[0], "cpu") || !strings.Contains(out[0], "mem") || !strings.Contains(out[0], "conns") {
		t.Errorf("headings are not on one line: %q", out[0])
	}
	for i, l := range out {
		if width(l) > 180 {
			t.Errorf("line %d is %d cells, over 180", i, width(l))
		}
	}
	stacked := plain(m.charts(ts, 70, 3))
	if strings.Contains(stacked[0], "mem") {
		t.Errorf("70 cells should stack, got %q", stacked[0])
	}
}

// The whole of the history is drawn, whatever the width: readings are merged by
// their highest, from the newest back, so a spike survives and the right edge
// is now.
func TestSqueezeKeepsTheSpikeAndTheNewest(t *testing.T) {
	vs := make([]float64, 240)
	vs[100] = 9
	vs[239] = 5
	out := squeeze(vs, 60)
	if len(out) > 60 {
		t.Fatalf("%d columns, want at most 60", len(out))
	}
	hi := 0.0
	for _, v := range out {
		hi = max(hi, v)
	}
	if hi != 9 {
		t.Errorf("the spike was lost: highest is %v", hi)
	}
	if out[len(out)-1] != 5 {
		t.Errorf("the newest reading is not at the right edge: %v", out[len(out)-1])
	}
	if got := squeeze([]float64{1, 2, 3}, 60); len(got) != 3 {
		t.Errorf("a short series was changed: %v", got)
	}
}

// The metrics view is ntcharts' time-series chart: a value axis, a time axis
// ending at now, and every line the width asked for.
func TestNTChartHasAxesAndTheWidthAsked(t *testing.T) {
	m := board()
	m.ntCharts = true
	vals := make([]float64, 240)
	for i := range vals {
		vals[i] = 80 + float64(i%9)
	}
	tr := trace{name: "mem", vals: vals, zoom: true, unit: bytesLabel, now: "84 MB", cur: 84 << 20, st: lipgloss.NewStyle()}
	for i := range tr.vals {
		tr.vals[i] *= 1 << 20
	}
	lines := m.ntChart(tr, 50, 5)
	if len(lines) != 5+2+1 {
		t.Fatalf("%d lines, want head + plot + axis + labels = 8", len(lines))
	}
	all := strings.Join(plain(lines), "\n")
	for _, want := range []string{"mem", "84 MB", "now", "-", "MB"} {
		if !strings.Contains(all, want) {
			t.Errorf("chart lacks %q:\n%s", want, all)
		}
	}
	for i, l := range plain(lines[1:]) {
		if width(l) != gutter+1+50 {
			t.Errorf("line %d is %d cells, want %d", i, width(l), gutter+1+50)
		}
	}
	m.cycleCharts()
	if m.ntCharts || !m.areaCharts {
		t.Errorf("c from ntcharts should go to the filled area: nt=%v area=%v", m.ntCharts, m.areaCharts)
	}
}
