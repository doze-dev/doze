package brand

import (
	"math"
	"strconv"
	"testing"
)

// luminance is WCAG's relative luminance of a #rrggbb colour.
func luminance(hex string) float64 {
	c := func(s string) float64 {
		v, _ := strconv.ParseUint(s, 16, 8)
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*c(hex[1:3]) + 0.7152*c(hex[3:5]) + 0.0722*c(hex[5:7])
}

func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Every theme's words read on its panels: body, dim and accent text at
// 4.5:1 (WCAG AA), and the words on a filled accent button too.
func TestThemesReadable(t *testing.T) {
	for _, th := range Themes {
		mode := "dark"
		if th.Pale {
			mode = "light"
		}
		for _, ground := range []struct{ name, hex string }{{"bg", th.Bg}, {"panel", th.Panel}} {
			for _, fg := range []struct{ name, hex string }{{"text", th.Text}, {"dim", th.Dim}, {"accent", th.Accent}, {"bad", th.Bad}, {"good", th.Good}, {"warn", th.Warn}, {"cool", th.Cool}} {
				if r := contrast(fg.hex, ground.hex); r < 4.5 {
					t.Errorf("%s %s: %s on %s is %.2f:1, under 4.5", th.Name, mode, fg.name, ground.name, r)
				}
			}
		}
		// Text on a selected row (dim takes the text colour there).
		if r := contrast(th.Text, th.Pick); r < 4.5 {
			t.Errorf("%s %s: text on the selection is %.2f:1, under 4.5", th.Name, mode, r)
		}
		// Controls: an off switch's and a box's edge (dim) at 3:1.
		if r := contrast(th.Dim, th.Track); r < 3 {
			t.Errorf("%s %s: an off switch's edge is %.2f:1, under 3", th.Name, mode, r)
		}
		if r := contrast(th.ink(), th.Accent); r < 4.5 {
			t.Errorf("%s %s: text on the accent is %.2f:1, under 4.5", th.Name, mode, r)
		}
	}
}
