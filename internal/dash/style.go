// Package dash is the terminal dashboard. It is built from the boards in
// docs/design/next/boards: the colours below are term.css, the layout of each
// screen is its board's markup, and nothing is on a screen that is not on its
// board or marked in the commit that added it.
//
// It depends on control.Daemon and nothing under it, so the same screens run
// against a live daemon and against fakedaemon.
package dash

import (
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
	plain bool

	text, a, g, y, c, r, d, w, b lipgloss.Style
	border, borderOn, borderBad  lipgloss.Style
	sel                          lipgloss.Style
	pill, pillG, pillY, pillR    lipgloss.Style
	pillC, pillQ, pillOn         lipgloss.Style
	key                          lipgloss.Style
}

func newStyles(plain bool) styles {
	fg := func(hex string) lipgloss.Style {
		if plain {
			return lipgloss.NewStyle()
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
	}
	pill := func(bg, fgHex string) lipgloss.Style {
		s := lipgloss.NewStyle().Bold(true).Padding(0, 1)
		if plain {
			return s.Reverse(true)
		}
		return s.Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(fgHex))
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
		s.pillOn = s.pillOn.Background(lipgloss.Color(hexSel)).Foreground(lipgloss.Color(hexAccent))
		s.pillQ = s.pillQ.Background(lipgloss.Color(hexSel)).Foreground(lipgloss.Color(hexText))
		s.sel = lipgloss.NewStyle().Background(lipgloss.Color(hexSel)).Foreground(lipgloss.Color(hexBright))
		s.key = lipgloss.NewStyle().Background(lipgloss.Color(hexSel)).Foreground(lipgloss.Color(hexBright)).Padding(0, 1)
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
	return b.String() + strings.Repeat(" ", w-len(vs))
}
