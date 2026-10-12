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
	text := map[string]string{"text": hexText, "accent": hexAccent, "good": hexGood, "warn": hexWarn, "cool": hexCool, "bad": hexBad, "bright": hexBright, "dim": hexDim}
	for fgName, fg := range text {
		for bgName, bg := range map[string]string{"page": hexBg, "panel": hexPanel} {
			if r := ratio(fg, bg); r < 4.5 {
				t.Errorf("%s on %s is %.2f", fgName, bgName, r)
			}
		}
	}
	// on the selection, dim is drawn as dimOn, and the rest are as they are
	for fgName, fg := range text {
		if fgName == "dim" {
			fg = hexDimOn
		}
		for bgName, bg := range map[string]string{"selected pill": hexSel, "selected row": hexPick} {
			if r := ratio(fg, bg); r < 4.5 {
				t.Errorf("%s on %s is %.2f", fgName, bgName, r)
			}
		}
	}
	for bgName, bg := range map[string]string{"page": hexBg, "panel": hexPanel} {
		if r := ratio(hexBorder, bg); r < 3.0 {
			t.Errorf("border on %s is %.2f", bgName, r)
		}
	}
	// and the style the dash draws a dim cell with on a selection is that one
	s := newStyles(false)
	for name, got := range map[string]string{"on": fmt.Sprint(s.on(s.d).GetForeground()), "onPick": fmt.Sprint(s.onPick(s.d).GetForeground())} {
		if want := fmt.Sprint(newStyles(false).d.Foreground(lipglossColor(hexDimOn)).GetForeground()); got != want {
			t.Errorf("%s draws dim as %s, want %s", name, got, want)
		}
	}
}
