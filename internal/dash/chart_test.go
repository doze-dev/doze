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
