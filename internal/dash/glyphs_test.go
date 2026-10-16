package dash

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// In the ASCII set every byte on screen is ASCII, and a column is where it was:
// a wide character becomes as many marks as it was cells.
func TestASCIISetDrawsOnlyASCIIAndKeepsColumns(t *testing.T) {
	for _, sh := range shapes() {
		m0 := sh.build()
		pgs := []struct {
			name string
			set  func(*Model)
		}{{"home", func(m *Model) {}}}
		if len(m0.snap.Instances) > 0 {
			pgs = pages(m0)
		}
		for _, pg := range pgs {
			for _, sz := range [][2]int{{260, 70}, {190, 54}, {150, 40}, {80, 24}, {60, 16}} {
				plain, ascii := sh.build(), sh.build()
				ascii.glyphs = glyphsASCII
				for _, m := range []*Model{plain, ascii} {
					pg.set(m)
					m.w, m.h = sz[0], sz[1]
				}
				got := ascii.Render(sz[0], sz[1])
				for i := 0; i < len(got); i++ {
					if got[i] >= 0x80 {
						t.Fatalf("%s / %s at %v: non-ASCII byte in %q", sh.name, pg.name, sz, ansi.Strip(got))
					}
				}
				want := strings.Split(plain.Render(sz[0], sz[1]), "\n")
				for i, l := range strings.Split(got, "\n") {
					if ansi.StringWidth(l) != ansi.StringWidth(want[i]) {
						t.Fatalf("%s / %s at %v line %d: %d cells, was %d:\n%q\n%q", sh.name, pg.name, sz, i, ansi.StringWidth(l), ansi.StringWidth(want[i]), ansi.Strip(l), ansi.Strip(want[i]))
					}
				}
			}
		}
	}
}

// The narrow set leaves no East Asian Ambiguous glyph of the dash's own, which
// is what tears a wide-ambiguous terminal's columns.
func TestNarrowSetHasNoAmbiguousDashGlyphs(t *testing.T) {
	m := stressModel(6, func(i int) string { return "svc" + string(rune('a'+i)) }, nil)
	m.glyphs = glyphsNarrow
	m.w, m.h = 150, 40
	for _, page := range []func(*Model){func(m *Model) {}, func(m *Model) { m.sel, m.page = m.snap.Instances[0].Name, pageFocus; m.follow() }, func(m *Model) { m.page = pageLogs; m.openLogs() }} {
		page(m)
		for _, r := range ansi.Strip(m.Render(150, 40)) {
			if _, ours := asciiOf[r]; ours && ambiguous(r) {
				t.Fatalf("narrow set drew %c (%U)", r, r)
			}
		}
	}
	if !ambiguous('─') || ambiguous('✓') || runewidth.RuneWidth('─') != 1 {
		t.Error("the width assumptions the set rests on changed")
	}
}

func TestEveryGlyphTheDashDrawsHasAnASCIIStandIn(t *testing.T) {
	for _, r := range "·×–—…›←↑→↓⊘⌗⏎─│└╭╮╯╰▁▂▃▄▅▆▇█▌▏▸▾◌●◐✓✗" {
		if rep, ok := asciiOf[r]; !ok || rep == "" || len(rep) != 1 {
			t.Errorf("%c (%U) has no one-cell stand-in: %q", r, r, rep)
		}
	}
}

func TestGlyphSetFollowsTheEnvironment(t *testing.T) {
	for _, c := range []struct{ term, all, ctype, lang, want string }{
		{"xterm-256color", "", "", "en_US.UTF-8", GlyphsUnicode},
		{"xterm-256color", "", "", "", GlyphsUnicode},
		{"dumb", "", "", "en_US.UTF-8", GlyphsASCII},
		{"linux", "", "", "en_US.UTF-8", GlyphsASCII},
		{"xterm", "", "", "C", GlyphsASCII},
		{"xterm", "", "", "en_US.ISO-8859-1", GlyphsASCII},
		{"xterm", "", "", "POSIX", GlyphsASCII},
		{"xterm", "ja_JP.UTF-8", "", "en_US.UTF-8", GlyphsNarrow},
		{"xterm", "", "zh_CN.UTF-8", "", GlyphsNarrow},
		{"xterm", "", "", "ko_KR.utf8", GlyphsNarrow},
		{"xterm", "en_US.UTF-8", "", "ja_JP.UTF-8", GlyphsUnicode},
	} {
		if got := detectGlyphs(c.term, c.all, c.ctype, c.lang); got != c.want {
			t.Errorf("%+v: %s", c, got)
		}
	}
	t.Setenv("DOZE_ASCII", "1")
	if DetectGlyphs() != GlyphsASCII {
		t.Error("DOZE_ASCII")
	}
}

func TestBrailleBecomesDensityMarks(t *testing.T) {
	if got, want := substitute("⠀⠁⠃⠇⡇⣿", glyphsASCII), " ..::#"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
