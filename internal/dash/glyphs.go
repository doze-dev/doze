package dash

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// The characters the dash draws with. Most terminals draw box lines and status
// dots one cell wide. Two kinds do not: one that treats East Asian Ambiguous
// characters as two cells wide (an iTerm2 option, and the default of some CJK
// locales), where every border and column shifts a cell per glyph; and one that
// cannot draw them at all (LANG=C over ssh, the Linux console, TERM=dumb).
//
// Every glyph the dash draws is one cell, so a set is a substitution made on
// the finished screen and layout is not touched: "narrow" replaces only the
// ambiguous ones, "ascii" replaces everything outside ASCII.

type glyphSet int

const (
	glyphsUnicode glyphSet = iota
	glyphsNarrow
	glyphsASCII
)

// Glyph sets by name, as Options.Glyphs and DOZE_GLYPHS spell them.
const (
	GlyphsUnicode = "unicode"
	GlyphsNarrow  = "narrow"
	GlyphsASCII   = "ascii"
)

func parseGlyphs(s string) glyphSet {
	switch strings.ToLower(s) {
	case GlyphsNarrow:
		return glyphsNarrow
	case GlyphsASCII:
		return glyphsASCII
	}
	return glyphsUnicode
}

// DetectGlyphs picks the set for this terminal from the environment: DOZE_GLYPHS
// or DOZE_ASCII say so; a locale that is not UTF-8, or TERM of dumb or linux,
// get ascii; a Chinese, Japanese or Korean locale, where ambiguous characters
// are commonly wide, gets narrow.
func DetectGlyphs() string {
	if g := os.Getenv("DOZE_GLYPHS"); g != "" {
		return g
	}
	if os.Getenv("DOZE_ASCII") != "" {
		return GlyphsASCII
	}
	return detectGlyphs(os.Getenv("TERM"), os.Getenv("LC_ALL"), os.Getenv("LC_CTYPE"), os.Getenv("LANG"))
}

func detectGlyphs(term, lcAll, lcCtype, lang string) string {
	switch term {
	case "dumb", "linux":
		return GlyphsASCII
	}
	loc := lcAll
	if loc == "" {
		loc = lcCtype
	}
	if loc == "" {
		loc = lang
	}
	if loc == "" {
		return GlyphsUnicode // nothing said: most terminals that start doze are UTF-8
	}
	l := strings.ToLower(loc)
	if l == "c" || l == "posix" || (strings.Contains(l, ".") && !strings.Contains(l, "utf-8") && !strings.Contains(l, "utf8")) {
		return GlyphsASCII
	}
	for _, p := range []string{"ja", "zh", "ko"} {
		if strings.HasPrefix(l, p+"_") || l == p {
			return GlyphsNarrow
		}
	}
	return GlyphsUnicode
}

// asciiOf is each glyph the dash draws and what stands for it in ASCII.
var asciiOf = map[rune]string{
	'·': ".", '×': "x", '–': "-", '—': "-", '…': "~", '›': ">", '←': "<", '↑': "^", '→': ">", '↓': "v",
	'⊘': "x", '⌗': "#", '⏎': ">", '─': "-", '│': "|", '└': "+", '┌': "+", '┐': "+", '┘': "+",
	'├': "+", '┤': "|", '┬': "+", '┴': "+", '┼': "+", '╭': "+", '╮': "+", '╯': "+", '╰': "+",
	'▁': ".", '▂': ".", '▃': ":", '▄': ":", '▅': "=", '▆': "=", '▇': "#", '█': "#", '▌': "|", '▏': "|",
	'▸': ">", '▾': "v", '◌': "o", '●': "*", '◐': "o", '✓': "+", '✗': "x", '≈': "~", '�': "?",
	'╴': "-", '╵': "|", '╶': "-", '╷': "|", '╌': "-", '┈': "-",
}

var eastAsian = func() *runewidth.Condition {
	c := runewidth.NewCondition()
	c.EastAsianWidth = true
	return c
}()

// ambiguous says whether a terminal that treats East Asian Ambiguous as wide
// would draw r two cells wide where this one counts it one.
func ambiguous(r rune) bool { return eastAsian.RuneWidth(r) == 2 && runewidth.RuneWidth(r) == 1 }

// braille is a plotted cell of a chart: more dots, a denser mark.
func brailleMark(r rune) string {
	n := 0
	for b := uint32(r - 0x2800); b != 0; b &= b - 1 {
		n++
	}
	switch {
	case n == 0:
		return " "
	case n <= 2:
		return "."
	case n <= 4:
		return ":"
	case n <= 6:
		return "+"
	}
	return "#"
}

// substitute rewrites a finished screen for the set. Text is what the programs
// printed as well as what the dash drew, so a character it has no ASCII for
// becomes a ? per cell it took, and the columns stay where they were.
func substitute(s string, set glyphSet) string {
	if set == glyphsUnicode || s == "" {
		return s
	}
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		case r >= 0x2800 && r <= 0x28FF:
			b.WriteString(brailleMark(r))
		default:
			if rep, ok := asciiOf[r]; ok && (set == glyphsASCII || ambiguous(r)) {
				b.WriteString(rep)
			} else if set == glyphsASCII {
				b.WriteString(strings.Repeat("?", runewidth.RuneWidth(r)))
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
