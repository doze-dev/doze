package view

import (
	"fmt"
	"math"
	"strings"
)

// The map's lines are drawn by the page as SVG paths, but placed here: a
// line is only drawn where it passes behind no card, and that is a fact
// about the layout, checked against every card before the path is chosen.

// box is a card's rectangle in the map's pixels.
type box struct{ x, y, w, h int }

func (b box) right() int  { return b.x + b.w }
func (b box) bottom() int { return b.y + b.h }
func (b box) cx() int     { return b.x + b.w/2 }
func (b box) cy() int     { return b.y + b.h/2 }

// inside says the point is in the box, short of a margin, so a line that
// runs along a card's edge, or starts on it, is not counted as behind it.
func (b box) inside(x, y float64) bool {
	const m = 2
	return x > float64(b.x)+m && x < float64(b.right())-m && y > float64(b.y)+m && y < float64(b.bottom())-m
}

type pt struct{ x, y float64 }

// path is a drawable line: a start and cubic segments, or a polyline whose
// corners are rounded, kept as points so it can be checked and written.
type path struct {
	d   string
	pts []pt // samples along it, for the collision check
}

// curve is one cubic from a to b with control points c1 and c2.
func curve(a, c1, c2, b pt) path {
	p := path{d: fmt.Sprintf("M%s %s C %s %s, %s %s, %s %s", n(a.x), n(a.y), n(c1.x), n(c1.y), n(c2.x), n(c2.y), n(b.x), n(b.y))}
	p.pts = cubicSamples(a, c1, c2, b)
	return p
}

func cubicSamples(a, c1, c2, b pt) []pt {
	const steps = 48
	out := make([]pt, 0, steps+1)
	for i := 0; i <= steps; i++ {
		t := float64(i) / steps
		u := 1 - t
		out = append(out, pt{
			u*u*u*a.x + 3*u*u*t*c1.x + 3*u*t*t*c2.x + t*t*t*b.x,
			u*u*u*a.y + 3*u*u*t*c1.y + 3*u*t*t*c2.y + t*t*t*b.y,
		})
	}
	return out
}

// elbow is a polyline through the points, its corners rounded, the way a
// line is routed along the gaps between cards when no curve is clear.
func elbow(points ...pt) path {
	points = dedupe(points)
	var b strings.Builder
	var samples []pt
	fmt.Fprintf(&b, "M%s %s", n(points[0].x), n(points[0].y))
	cur := points[0]
	for i := 1; i < len(points); i++ {
		p := points[i]
		if i == len(points)-1 {
			fmt.Fprintf(&b, " L %s %s", n(p.x), n(p.y))
			samples = append(samples, line(cur, p)...)
			break
		}
		// Round the corner at p: stop short of it, curve through it.
		next := points[i+1]
		r := math.Min(10, math.Min(dist(cur, p), dist(p, next))/2)
		in := toward(p, cur, r)
		out := toward(p, next, r)
		fmt.Fprintf(&b, " L %s %s Q %s %s, %s %s", n(in.x), n(in.y), n(p.x), n(p.y), n(out.x), n(out.y))
		samples = append(samples, line(cur, in)...)
		samples = append(samples, line(in, out)...)
		cur = out
	}
	return path{d: b.String(), pts: samples}
}

func dedupe(points []pt) []pt {
	out := []pt{points[0]}
	for _, p := range points[1:] {
		if last := out[len(out)-1]; last == p {
			continue
		}
		// A point on the straight line between its neighbours is no corner.
		if len(out) >= 2 {
			a, b := out[len(out)-2], out[len(out)-1]
			if (a.x == b.x && b.x == p.x) || (a.y == b.y && b.y == p.y) {
				out[len(out)-1] = p
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

func line(a, b pt) []pt {
	steps := int(dist(a, b)/6) + 1
	out := make([]pt, 0, steps+1)
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		out = append(out, pt{a.x + (b.x-a.x)*t, a.y + (b.y-a.y)*t})
	}
	return out
}

func dist(a, b pt) float64 { return math.Hypot(b.x-a.x, b.y-a.y) }

func toward(from, to pt, r float64) pt {
	d := dist(from, to)
	if d == 0 {
		return from
	}
	return pt{from.x + (to.x-from.x)*r/d, from.y + (to.y-from.y)*r/d}
}

// n writes a coordinate without a needless fraction.
func n(f float64) string {
	if f == math.Trunc(f) {
		return fmt.Sprint(int(f))
	}
	return fmt.Sprintf("%.1f", f)
}

// clear says no point of the path is behind a card other than its ends'.
func (p path) clear(cards []box, ends ...box) bool {
	for _, q := range p.pts {
		for _, c := range cards {
			if isEnd(c, ends) {
				continue
			}
			if c.inside(q.x, q.y) {
				return false
			}
		}
	}
	return true
}

func isEnd(c box, ends []box) bool {
	for _, e := range ends {
		if e == c {
			return true
		}
	}
	return false
}

// gaps are the free lines between the cards: the middles of the runs no
// card covers, across (xs) and down (ys), and one beyond each side.
func gaps(cards []box, horizontal bool) []float64 {
	type span struct{ a, b int }
	var spans []span
	for _, c := range cards {
		if horizontal {
			spans = append(spans, span{c.x, c.right()})
		} else {
			spans = append(spans, span{c.y, c.bottom()})
		}
	}
	if len(spans) == 0 {
		return nil
	}
	// merge
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j].a < spans[j-1].a; j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}
	merged := []span{spans[0]}
	for _, s := range spans[1:] {
		last := &merged[len(merged)-1]
		if s.a <= last.b {
			if s.b > last.b {
				last.b = s.b
			}
			continue
		}
		merged = append(merged, s)
	}
	out := []float64{float64(merged[0].a) - 10}
	for i := 1; i < len(merged); i++ {
		out = append(out, float64(merged[i-1].b+merged[i].a)/2)
	}
	return append(out, float64(merged[len(merged)-1].b)+12)
}
