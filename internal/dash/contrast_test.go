package dash

import (
	"fmt"
	"math"
	"testing"
)

// luminance and ratio are WCAG 2.1's.
func luminance(hex string) float64 {
	var c [3]int
	fmt.Sscanf(hex, "#%02x%02x%02x", &c[0], &c[1], &c[2])
	f := func(v int) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c[0]) + 0.7152*f(c[1]) + 0.0722*f(c[2])
}

func ratio(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Every text colour is read on the grounds it is drawn on: 4.5 for text
// (WCAG 1.4.3), 3.0 for the lines of a box (1.4.11). The faded backdrop behind
// a modal is meant to be faint and is not in the list.
func TestTextAndBordersAreReadable(t *testing.T) {
	for _, p := range themes {
		p := p
		t.Run(p.name, func(t *testing.T) { readable(t, p) })
	}
}

func readable(t *testing.T, p palette) {
	text := map[string]string{"text": p.text, "accent": p.accent, "good": p.good, "warn": p.warn, "cool": p.cool, "bad": p.bad, "bright": p.bright, "dim": p.dim, "extra": p.extra}
	for fgName, fg := range text {
		for bgName, bg := range map[string]string{"page": p.bg, "panel": p.panel} {
			if r := ratio(fg, bg); r < 4.5 {
				t.Errorf("%s on %s is %.2f", fgName, bgName, r)
			}
		}
	}
	// on the selection, dim is drawn as dimOn, and the rest are as they are
	for fgName, fg := range text {
		if fgName == "dim" {
			fg = p.dimOn
		}
		for bgName, bg := range map[string]string{"selected pill": p.sel, "selected row": p.pick} {
			if r := ratio(fg, bg); r < 4.5 {
				t.Errorf("%s on %s is %.2f", fgName, bgName, r)
			}
		}
	}
	for bgName, bg := range map[string]string{"page": p.bg, "panel": p.panel} {
		if r := ratio(p.border, bg); r < 3.0 {
			t.Errorf("border on %s is %.2f", bgName, r)
		}
	}
	// and the style the dash draws a dim cell with on a selection is that one
	s := newThemed(false, p)
	for name, got := range map[string]string{"on": fmt.Sprint(s.on(s.d).GetForeground()), "onPick": fmt.Sprint(s.onPick(s.d).GetForeground())} {
		if want := fmt.Sprint(newThemed(false, p).d.Foreground(lipglossColor(p.dimOn)).GetForeground()); got != want {
			t.Errorf("%s draws dim as %s, want %s", name, got, want)
		}
	}
}

// Colours that mean different things stay apart in every theme: the accent is
// not the failure colour, and a warning is not the accent.
func TestThemeColoursStayApart(t *testing.T) {
	dist := func(a, b string) float64 {
		var x, y [3]int
		fmt.Sscanf(a, "#%02x%02x%02x", &x[0], &x[1], &x[2])
		fmt.Sscanf(b, "#%02x%02x%02x", &y[0], &y[1], &y[2])
		d := 0.0
		for i := range x {
			d += math.Abs(float64(x[i] - y[i]))
		}
		return d
	}
	for _, p := range themes {
		for _, pair := range [][2]string{{p.accent, p.bad}, {p.accent, p.warn}, {p.good, p.warn}, {p.good, p.bad}, {p.warn, p.bad}, {p.accent, p.good}} {
			if d := dist(pair[0], pair[1]); d < 90 {
				t.Errorf("%s: %s and %s are too alike (%.0f)", p.name, pair[0], pair[1], d)
			}
		}
	}
}
