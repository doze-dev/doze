package display

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The arithmetic behind the meters and the charts: who holds how much of a
// total, what a CPU figure is in cores, how a series is squeezed into a width
// without losing a spike, and what a scale runs from and to. A surface draws
// bars and blocks from these; the figures are the same on all of them.

// SampleEvery is how often the daemon samples the meters (control.Options
// leaves it at one second).
const SampleEvery = time.Second

// Share is one instance's part of a total.
type Share struct {
	Name string
	V    float64
}

// Shares is what a meter draws: the shares above floor, largest first, their
// sum, and how many were at or under the floor (counted, not drawn: an
// instance doing next to nothing has no run to show).
func Shares(all []Share, floor float64) (shares []Share, idle int, sum float64) {
	for _, x := range all {
		if x.V > floor {
			shares = append(shares, x)
		} else {
			idle++
		}
	}
	sort.SliceStable(shares, func(i, j int) bool { return shares[i].V > shares[j].V })
	for _, x := range shares {
		sum += x.V
	}
	return shares, idle, sum
}

// ShareCells divides inner cells among the shares: every share at least a
// cell, the rest in proportion, and the difference taken from or given to the
// largest so the bar is exactly full. Nothing when the sum is zero.
func ShareCells(shares []Share, sum float64, inner int) []int {
	if sum <= 0 || len(shares) == 0 {
		return nil
	}
	cells := make([]int, len(shares))
	left := inner
	for i, x := range shares {
		cells[i] = max(int(x.V/sum*float64(inner)+0.5), 1)
		left -= cells[i]
	}
	for i := 0; left != 0 && i < len(cells); i++ {
		d := left
		if cells[i]+d < 1 {
			d = 1 - cells[i]
		}
		cells[i] += d
		left -= d
	}
	return cells
}

// MemUnit formats a share of memory: plain megabytes while the total is under
// a gigabyte, so a key reads as a column of numbers.
func MemUnit(total float64) func(float64) string {
	if total >= 1<<30 {
		return func(v float64) string { return MB(int64(v)) }
	}
	return func(v float64) string { return fmt.Sprintf("%d", (int64(v)+(1<<19))>>20) }
}

// Cores says a CPU figure (ps's percent, one core being 100) as cores, which
// is what it is: 0.23 cores is a fifth of one.
func Cores(pct float64) string {
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

// CoresUnit is Cores without the word, for a key beside a bar.
func CoresUnit(pct float64) string {
	return strings.TrimSuffix(strings.TrimSuffix(Cores(pct), " cores"), " core")
}

// Stats is the highest and the mean of what a chart holds.
func Stats(vs []float64) (peak, mean float64) {
	for _, v := range vs {
		peak = max(peak, v)
		mean += v
	}
	if len(vs) > 0 {
		mean /= float64(len(vs))
	}
	return
}

// Scale is the range a chart stands for: zero to a little above the highest
// reading (no lower than floor), or, zoomed, a little either side of the
// readings so that a steady series reads as steady and a drift as a drift.
func Scale(vs []float64, floor float64, zoom bool) (lo, hi float64) {
	if len(vs) == 0 {
		return 0, max(floor, 1)
	}
	mn, mx := vs[0], vs[0]
	for _, v := range vs {
		mn, mx = min(mn, v), max(mx, v)
	}
	if zoom {
		pad := max((mx-mn)*0.25, mx*0.03)
		if pad == 0 {
			pad = 1
		}
		return max(mn-pad, 0), mx + pad
	}
	return 0, max(floor, mx*1.15, 1)
}

// Squeeze fits vs into w columns by merging neighbours by their highest, from
// the newest back, so the whole of the history is drawn and a spike is never
// lost to the width. Shorter than w it is returned as it is.
func Squeeze(vs []float64, w int) []float64 {
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

// SqueezeTimed is Squeeze for readings that carry the time they were taken:
// the merged column keeps the newest of the times it covers.
func SqueezeTimed(vs []float64, at []time.Time, w int) ([]float64, []time.Time) {
	n := len(vs)
	if n <= w || w < 1 || len(at) != n {
		return vs, at
	}
	per := (n + w - 1) / w
	cols := (n + per - 1) / per
	outV, outT := make([]float64, cols), make([]time.Time, cols)
	for i := cols - 1; i >= 0; i-- {
		end := n - per*(cols-1-i)
		start := max(end-per, 0)
		m := vs[start]
		for _, v := range vs[start:end] {
			m = max(m, v)
		}
		outV[i], outT[i] = m, at[end-1]
	}
	return outV, outT
}

// SpanWord is how long n readings cover, in the words of a chart's axis.
func SpanWord(n int) string {
	d := time.Duration(max(n, 1)) * SampleEvery
	if d >= time.Minute && d%time.Minute != 0 && d < 10*time.Minute {
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return Ago(d)
}

// Pct, Count and BytesLabel are the scales' labels.
func Pct(v float64) string        { return fmt.Sprintf("%.0f%%", v) }
func Count(v float64) string      { return fmt.Sprintf("%.0f", v) }
func BytesLabel(v float64) string { return MB(int64(v)) }
