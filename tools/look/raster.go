package main

import (
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"os"

	uv "github.com/charmbracelet/ultraviolet"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// The cell is what a terminal draws a character in: 8 x 17 px at 13.3px type,
// which is a 14px browser font's width. The page around it is the boards'.
const (
	cellW   = 8
	cellH   = 17
	padX    = 22
	padY    = 18
	fontPx  = 13.3
	faintBy = 0.45 // how much of the foreground a faint cell keeps
)

var (
	rgbBg = color.RGBA{0x15, 0x13, 0x1d, 0xff}
	rgbFg = color.RGBA{0xcf, 0xcb, 0xe3, 0xff}
)

// JetBrains Mono is the boards' font (docs/design/next/boards/term.css). It is
// embedded, so the images match the boards letter for letter on any machine.
//
//go:embed fonts/JetBrainsMono-Regular.ttf
var jbRegular []byte

//go:embed fonts/JetBrainsMono-Bold.ttf
var jbBold []byte

// family is one font in the fallback chain: a regular and a bold face.
type family struct {
	reg, bold *sfnt.Font
	regF      font.Face
	boldF     font.Face
}

func (f *family) has(r rune) bool {
	var b sfnt.Buffer
	i, err := f.reg.GlyphIndex(&b, r)
	return err == nil && i != 0
}

// fonts is the chain a glyph is looked up in: JetBrains Mono, then the system's
// monospace and symbol fonts for what it lacks, then Go Mono, which every machine
// has because it is linked in.
func fonts() []*family {
	var out []*family
	add := func(reg, bold []byte) {
		r, err := opentype.Parse(reg)
		if err != nil {
			return
		}
		fam := &family{reg: r}
		fam.regF, _ = opentype.NewFace(r, &opentype.FaceOptions{Size: fontPx, DPI: 72, Hinting: font.HintingFull})
		if bold != nil {
			if b, err := opentype.Parse(bold); err == nil {
				fam.bold = b
				fam.boldF, _ = opentype.NewFace(b, &opentype.FaceOptions{Size: fontPx, DPI: 72, Hinting: font.HintingFull})
			}
		}
		out = append(out, fam)
	}
	add(jbRegular, jbBold)
	if data, err := os.ReadFile("/System/Library/Fonts/Menlo.ttc"); err == nil {
		if c, err := opentype.ParseCollection(data); err == nil {
			fam := &family{}
			for i := 0; i < c.NumFonts(); i++ {
				f, err := c.Font(i)
				if err != nil {
					continue
				}
				var b sfnt.Buffer
				sub, _ := f.Name(&b, sfnt.NameIDSubfamily)
				face, _ := opentype.NewFace(f, &opentype.FaceOptions{Size: fontPx, DPI: 72, Hinting: font.HintingFull})
				switch sub {
				case "Regular":
					fam.reg, fam.regF = f, face
				case "Bold":
					fam.bold, fam.boldF = f, face
				}
			}
			if fam.reg != nil {
				out = append(out, fam)
			}
		}
	}
	for _, p := range []string{"/System/Library/Fonts/Apple Symbols.ttf", "/System/Library/Fonts/Supplemental/Arial Unicode.ttf"} {
		if data, err := os.ReadFile(p); err == nil {
			add(data, nil)
		}
	}
	add(gomono.TTF, gomonobold.TTF)
	return out
}

// pick is the colour of a cell's two sides as the terminal draws them.
func colours(st uv.Style) (fg, bg color.RGBA) {
	fg, bg = rgbFg, rgbBg
	if st.Fg != nil {
		fg = toRGBA(st.Fg)
	}
	if st.Bg != nil {
		bg = toRGBA(st.Bg)
	}
	if st.Attrs&uv.AttrReverse != 0 {
		fg, bg = bg, fg
		if st.Fg == nil {
			bg = rgbFg
		}
		if st.Bg == nil {
			fg = rgbBg
		}
	}
	if st.Attrs&uv.AttrFaint != 0 {
		fg = mix(fg, bg, faintBy)
	}
	return fg, bg
}

func toRGBA(c color.Color) color.RGBA {
	r, g, b, _ := c.RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0xff}
}

func mix(a, b color.RGBA, alpha float64) color.RGBA {
	m := func(x, y uint8) uint8 { return uint8(float64(x)*alpha + float64(y)*(1-alpha)) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 0xff}
}

// renderScreen draws the emulator's screen the way a terminal would: each cell
// a rectangle of its background with its character on it.
func renderScreen(s *session) image.Image {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, h := s.emu.Width(), s.emu.Height()
	img := image.NewRGBA(image.Rect(0, 0, w*cellW+2*padX, h*cellH+2*padY))
	draw.Draw(img, img.Bounds(), &image.Uniform{rgbBg}, image.Point{}, draw.Src)
	chain := fonts()

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := s.emu.CellAt(x, y)
			if c == nil || c.Width == 0 {
				continue
			}
			fg, bg := colours(c.Style)
			px, py := padX+x*cellW, padY+y*cellH
			span := max(c.Width, 1)
			if bg != rgbBg {
				fill(img, px, py, span*cellW, cellH, bg)
			}
			r, _ := firstRune(c.Content)
			if r == 0 || r == ' ' {
				continue
			}
			bold := c.Style.Attrs&uv.AttrBold != 0
			if !blockOrLine(img, px, py, span*cellW, cellH, r, fg, bg) {
				text(img, chain, px, py, r, fg, bold)
			}
			if c.Style.Underline != 0 {
				fill(img, px, py+cellH-3, span*cellW, 1, fg)
			}
		}
	}
	return img
}

func firstRune(s string) (rune, int) {
	for i, r := range s {
		return r, i
	}
	return 0, 0
}

func fill(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	draw.Draw(img, image.Rect(x, y, x+w, y+h), &image.Uniform{c}, image.Point{}, draw.Src)
}

// text draws r with the first font in the chain that has it, centred in its
// cell and sitting on a common baseline.
func text(img *image.RGBA, chain []*family, x, y int, r rune, fg color.RGBA, bold bool) {
	for _, fam := range chain {
		if !fam.has(r) {
			continue
		}
		face := fam.regF
		if bold && fam.boldF != nil {
			face = fam.boldF
		}
		if face == nil {
			continue
		}
		m := face.Metrics()
		asc, desc := m.Ascent.Ceil(), m.Descent.Ceil()
		base := y + (cellH-(asc+desc))/2 + asc
		adv, _ := face.GlyphAdvance(r)
		dx := (cellW - adv.Round()) / 2 // a symbol font's glyph is not a cell wide
		d := font.Drawer{Dst: img, Src: &image.Uniform{fg}, Face: face, Dot: fixed.P(x+dx, base)}
		d.DrawString(string(r))
		return
	}
}

// blockOrLine draws the characters a terminal draws itself, as geometry, so
// borders join and bars reach the cell's edge whatever the font does.
func blockOrLine(img *image.RGBA, x, y, w, h int, r rune, fg, bg color.RGBA) bool {
	switch {
	case r >= 0x2581 && r <= 0x2588: // lower one eighth to full block
		n := int(r-0x2580) * h / 8
		fill(img, x, y+h-n, w, n, fg)
		return true
	case r == 0x258C: // left half block
		fill(img, x, y, w/2, h, fg)
		return true
	case r == 0x2591: // light shade
		fill(img, x, y, w, h, mix(fg, bg, 0.25))
		return true
	case r == 0x2592:
		fill(img, x, y, w, h, mix(fg, bg, 0.5))
		return true
	case r == 0x2593:
		fill(img, x, y, w, h, mix(fg, bg, 0.75))
		return true
	}
	// box drawing: which arms leave the centre
	var l, rt, u, d bool
	switch r {
	case '─':
		l, rt = true, true
	case '│':
		u, d = true, true
	case '┌', '╭':
		rt, d = true, true
	case '┐', '╮':
		l, d = true, true
	case '└', '╰':
		rt, u = true, true
	case '┘', '╯':
		l, u = true, true
	case '├':
		u, d, rt = true, true, true
	case '┤':
		u, d, l = true, true, true
	case '┬':
		l, rt, d = true, true, true
	case '┴':
		l, rt, u = true, true, true
	case '┼':
		l, rt, u, d = true, true, true, true
	default:
		return false
	}
	cx, cy := x+w/2, y+h/2
	if l {
		fill(img, x, cy, cx-x+1, 1, fg)
	}
	if rt {
		fill(img, cx, cy, x+w-cx, 1, fg)
	}
	if u {
		fill(img, cx, y, 1, cy-y+1, fg)
	}
	if d {
		fill(img, cx, cy, 1, y+h-cy, fg)
	}
	return true
}
