// Package dash is the terminal dashboard. It is built from the boards in
// docs/design/next/boards: the colours below are term.css, the layout of each
// screen is its board's markup, and nothing is on a screen that is not on its
// board or marked in the commit that added it.
//
// It depends on control.Daemon and nothing under it, so the same screens run
// against a live daemon and against fakedaemon.
package dash

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The palette, from docs/design/next/boards/term.css.
const (
	hexBg     = "#15131d"
	hexText   = "#cfcbe3"
	hexAccent = "#BD93F9"
	hexGood   = "#7EE787"
	hexWarn   = "#F2C879"
	hexCool   = "#82AAFF"
	hexBad    = "#FF7A93"
	hexDim    = "#8a85a8"
	hexBright = "#f4f1ff"
	hexBorder = "#37324b"
	hexSel    = "#2b2542"
)

// styles are the board's classes: a g y c r d w are the colour classes, b is
// bold bright, and the pills and key caps are their own.
type styles struct {
	plain  bool
	selHex string

	text, a, g, y, c, r, d, w, b lipgloss.Style
	border, borderOn, borderBad  lipgloss.Style
	sel                          lipgloss.Style
	pill, pillG, pillY, pillR    lipgloss.Style
	pillC, pillQ, pillOn         lipgloss.Style
	key                          lipgloss.Style
}

func newStyles(plain bool) styles { return build(plain, func(h string) string { return h }) }

// fadedStyles are the same colours at a third of their strength, for the page
// behind a modal: the board dims it, it does not grey it out.
func fadedStyles(plain bool) styles {
	return build(plain, func(h string) string { return mix(h, hexBg, 0.32) })
}

// mix is fg at alpha over bg, as hex.
func mix(fg, bg string, alpha float64) string {
	var f, b [3]int
	fmt.Sscanf(fg, "#%02x%02x%02x", &f[0], &f[1], &f[2])
	fmt.Sscanf(bg, "#%02x%02x%02x", &b[0], &b[1], &b[2])
	var o [3]int
	for i := range o {
		o[i] = int(float64(f[i])*alpha + float64(b[i])*(1-alpha))
	}
	return fmt.Sprintf("#%02x%02x%02x", o[0], o[1], o[2])
}

func build(plain bool, tint func(string) string) styles {
	fg := func(hex string) lipgloss.Style {
		if plain {
			return lipgloss.NewStyle()
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(tint(hex)))
	}
	pill := func(bg, fgHex string) lipgloss.Style {
		s := lipgloss.NewStyle().Bold(true).Padding(0, 1)
		if plain {
			return s.Reverse(true)
		}
		return s.Background(lipgloss.Color(tint(bg))).Foreground(lipgloss.Color(tint(fgHex)))
	}
	s := styles{plain: plain}
	s.text, s.a, s.g, s.y, s.c = fg(hexText), fg(hexAccent), fg(hexGood), fg(hexWarn), fg(hexCool)
	s.r, s.d, s.w = fg(hexBad), fg(hexDim), fg(hexBright)
	s.b = s.w.Bold(true)
	s.border, s.borderOn, s.borderBad = fg(hexBorder), fg(hexAccent), fg(hexBad)
	s.pill = pill(hexAccent, hexBg)
	s.pillG, s.pillY, s.pillR = pill(hexGood, hexBg), pill(hexWarn, hexBg), pill(hexBad, hexBg)
	s.pillC = pill(hexCool, hexBg)
	s.pillQ = lipgloss.NewStyle().Padding(0, 1)
	s.pillOn = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	if !plain {
		s.pillOn = s.pillOn.Background(lipgloss.Color(tint(hexSel))).Foreground(lipgloss.Color(tint(hexAccent)))
		s.pillQ = s.pillQ.Background(lipgloss.Color(tint(hexSel))).Foreground(lipgloss.Color(tint(hexText)))
		s.selHex = tint(hexSel)
		s.sel = lipgloss.NewStyle().Background(lipgloss.Color(s.selHex)).Foreground(lipgloss.Color(tint(hexBright)))
		s.key = lipgloss.NewStyle().Background(lipgloss.Color(tint(hexSel))).Foreground(lipgloss.Color(tint(hexBright))).Padding(0, 1)
	} else {
		s.sel = lipgloss.NewStyle().Reverse(true)
		s.key = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	}
	return s
}

// --- cells ---

func width(s string) int { return ansi.StringWidth(s) }

// fit cuts s to w cells with an ellipsis, and pads it to w.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if n := w - width(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// right pads s on the left to w cells.
func right(s string, w int) string {
	if width(s) > w {
		return ansi.Truncate(s, w, "…")
	}
	return strings.Repeat(" ", w-width(s)) + s
}

// box draws a rounded box w cells wide, with a caption in the top border on
// the left and optionally another on the right, and lines inside, one cell of
// padding either side. It is h cells tall; lines past that are cut, and short
// content is padded with blank lines.
func box(border lipgloss.Style, left, rt string, lines []string, w, h int) []string {
	if w < 4 || h < 2 {
		return nil
	}
	inner := w - 4
	top, sep := border.Render("╭─")+" "+left, " "
	if left == "" {
		top, sep = border.Render("╭"), ""
	}
	used := width(top)
	tail := ""
	if rt != "" {
		tail = " " + rt + " " + border.Render("─╮")
	} else {
		tail = border.Render("─╮")
	}
	fill := w - used - width(sep) - width(tail)
	if fill < 0 {
		fill = 0
	}
	out := []string{top + sep + border.Render(strings.Repeat("─", fill)) + tail}
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, border.Render("│")+" "+fit(l, inner)+" "+border.Render("│"))
	}
	out = append(out, border.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return out
}

// spark draws values as block glyphs, tallest the largest, w cells wide: the
// last w samples, left-aligned while there are fewer.
func spark(vs []float64, w int) string {
	if len(vs) == 0 {
		return strings.Repeat(" ", w)
	}
	if len(vs) > w {
		vs = vs[len(vs)-w:]
	}
	// Fewer readings than cells: spread what there is across the width, as the
	// boards draw a chart that fills its box.
	if len(vs) < w {
		filled := make([]float64, w)
		for i := range filled {
			filled[i] = vs[i*len(vs)/w]
		}
		vs = filled
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	lo, hi := vs[0], vs[0]
	for _, v := range vs {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	var b strings.Builder
	for _, v := range vs {
		i := 0
		if hi > lo {
			i = int((v - lo) / (hi - lo) * float64(len(blocks)-1))
		}
		b.WriteRune(blocks[i])
	}
	return strings.Repeat(" ", w-len(vs)) + b.String()
}

// on draws st on the selection's background, so a selected row keeps each
// part's colour across the whole row (a reset inside a styled string would
// otherwise end the highlight there).
func (s styles) on(st lipgloss.Style) lipgloss.Style {
	if s.plain {
		return st.Reverse(true)
	}
	return st.Background(lipgloss.Color(s.selHex))
}

// cap is a key cap and its label, a cell apart as on the boards.
func (s styles) cap(k, label string) string {
	return s.key.Render(k) + " " + s.d.Render(label)
}

// area draws a series h rows tall and w cells wide, newest at the right edge.
// It starts at zero and ends a little above the highest reading (or at floor,
// when the readings are all lower), so a steady line looks steady and a spike
// looks like one. Each row holds eighths of a block, so h rows have 8h levels.
func area(vs []float64, w, h int, floor float64) []string {
	rows, _ := areaScaled(vs, w, h, floor)
	return rows
}

// areaScaled is area and the value the top of the chart stands for.
func areaScaled(vs []float64, w, h int, floor float64) ([]string, float64) {
	if w < 1 || h < 1 {
		return nil, 0
	}
	if len(vs) > w {
		vs = vs[len(vs)-w:]
	}
	if len(vs) > 0 && len(vs) < w {
		// fewer readings than cells: spread them, as spark does
		filled := make([]float64, w)
		for i := range filled {
			filled[i] = vs[i*len(vs)/w]
		}
		vs = filled
	}
	top := floor
	for _, v := range vs {
		if v*1.15 > top {
			top = v * 1.15
		}
	}
	if top <= 0 {
		top = 1
	}
	// a steady series sits at half height rather than filling the box, so the
	// eye reads "level" and not "full"
	if len(vs) > 0 {
		lo, hi := vs[0], vs[0]
		for _, v := range vs {
			lo, hi = min(lo, v), max(hi, v)
		}
		if hi > 0 && (hi-lo)/hi < 0.1 && top < hi*2 {
			top = hi * 2
		}
	}
	steps := []rune(" ▁▂▃▄▅▆▇█")
	rows := make([]string, h)
	var cols [][]rune
	for _, v := range vs {
		level := int(v / top * float64(h*8))
		if v > 0 && level == 0 {
			level = 1 // a reading is never invisible
		}
		col := make([]rune, h)
		for r := 0; r < h; r++ { // r = 0 is the bottom row
			n := level - r*8
			switch {
			case n >= 8:
				col[r] = steps[8]
			case n > 0:
				col[r] = steps[n]
			default:
				col[r] = ' '
			}
		}
		cols = append(cols, col)
	}
	pad := w - len(cols)
	for r := 0; r < h; r++ {
		var b strings.Builder
		b.WriteString(strings.Repeat(" ", pad))
		for _, c := range cols {
			b.WriteRune(c[r])
		}
		rows[h-1-r] = b.String()
	}
	return rows, top
}
