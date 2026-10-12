package dash

import (
	"fmt"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/v2/linechart"
	"github.com/NimbleMarkets/ntcharts/v2/linechart/timeserieslinechart"
)

// A chart is ntcharts' time-series line chart: braille dots on a canvas with a
// value axis in the metric's own units and a time axis ending at now, each
// reading stamped with the time it was taken. Tiles and sparklines, which are
// too small for axes, are the dash's own (style.go, spark).

// chart draws t in cw+gutter+1 cells and rows+2 lines (the plot, the axis and
// its labels), and returns the lines.
func (m *Model) chart(t trace, cw, rows int) []string {
	s := m.st
	vals := t.vals
	if len(vals) == 0 {
		vals = []float64{t.cur}
	}
	vals = squeeze(vals, 2*cw)
	lo, hi := t.scale(vals)
	// readings are a second apart and the newest is now, so a reading's time is
	// known without the daemon stamping it
	span := time.Duration(max(len(t.vals), 1)) * sampleEvery
	now := m.now
	if now.IsZero() {
		now = time.Now()
	}
	start := now.Add(-span)
	epoch := func(t time.Time) float64 { return float64(t.UnixMilli()) / 1e3 }

	xfmt := func(_ int, v float64) string {
		ago := time.Duration((epoch(now) - v) * float64(time.Second))
		if ago < time.Second {
			return "now"
		}
		// whole units, so neighbouring labels do not run together
		switch {
		case ago >= 2*time.Minute:
			return fmt.Sprintf("-%dm", int((ago+30*time.Second)/time.Minute))
		case ago >= time.Minute:
			secs := int(ago.Round(5*time.Second) / time.Second)
			return fmt.Sprintf("-%dm%02ds", secs/60, secs%60)
		}
		return fmt.Sprintf("-%ds", int(ago.Round(5*time.Second)/time.Second))
	}
	yfmt := func(_ int, v float64) string { return t.unit(v) }

	w := gutter + 1 + cw
	lc := linechart.New(w, rows+2, epoch(start), epoch(now), lo, hi,
		linechart.WithXYSteps(4, 2),
		linechart.WithXLabelFormatter(xfmt),
		linechart.WithYLabelFormatter(yfmt),
		linechart.WithStyles(s.d, s.d, t.st),
	)
	c := timeserieslinechart.New(w, rows+2,
		timeserieslinechart.WithLineChart(&lc),
		timeserieslinechart.WithStyle(t.st),
	)
	// the readings, oldest first, spread over the span they cover
	step := span / time.Duration(max(len(vals), 1))
	for i, v := range vals {
		c.Push(timeserieslinechart.TimePoint{Time: start.Add(time.Duration(i+1) * step), Value: v})
	}
	c.DrawBraille()

	peak, mean := stats(t.vals)
	head := s.w.Render(t.name) + "  " + s.b.Render(t.now)
	if len(t.vals) > 1 {
		head += s.d.Render(fmt.Sprintf("   peak %s · avg %s", t.unit(peak), t.unit(mean)))
	}
	return append([]string{head}, strings.Split(c.View(), "\n")...)
}
