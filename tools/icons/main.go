// Command icons renders doze's icons from website/brand: the app icon as
// an .iconset (every size macOS wants, then `iconutil` makes the .icns) and
// the menu bar bat as a template image, black on transparent, at 1x and 2x.
//
//	go run ./tools/icons -brand website/brand -out build/darwin
//
// Then: iconutil -c icns build/darwin/doze.iconset -o build/darwin/doze.icns
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

func main() {
	brand := flag.String("brand", "website/brand", "the brand folder with doze.svg")
	out := flag.String("out", "build/darwin", "where to write doze.iconset and the menu bar images")
	flag.Parse()
	src, err := os.ReadFile(filepath.Join(*brand, "doze.svg"))
	if err != nil {
		fail(err)
	}
	// The app icon: the whole tile, at every size the iconset wants. oksvg
	// ignores a viewBox origin, so the drawing is moved to 0,0 instead.
	tile := atOrigin(inlineFills(oneTransform(string(src))))
	set := filepath.Join(*out, "doze.iconset")
	if err := os.MkdirAll(set, 0o755); err != nil {
		fail(err)
	}
	for _, s := range []struct {
		name string
		px   int
	}{
		{"icon_16x16", 16}, {"icon_16x16@2x", 32}, {"icon_32x32", 32}, {"icon_32x32@2x", 64},
		{"icon_128x128", 128}, {"icon_128x128@2x", 256}, {"icon_256x256", 256}, {"icon_256x256@2x", 512},
		{"icon_512x512", 512}, {"icon_512x512@2x", 1024},
	} {
		img, err := render(tile, s.px, s.px, 0, 0, s.px, s.px)
		if err != nil {
			fail(err)
		}
		write(filepath.Join(set, s.name+".png"), img)
	}
	// The menu bar bat: the black group of the icon, alone, centred on a
	// square as tall as the bar, four fifths of its height.
	g := regexp.MustCompile(`<g fill="#3A2448">([\s\S]*?)</g>`).FindStringSubmatch(string(src))
	if g == nil {
		fail(fmt.Errorf("doze.svg has no bat group (fill #3A2448)"))
	}
	paths := strings.ReplaceAll(strings.TrimSpace(g[1]), "<path ", `<path fill="#000000" `)
	bat := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 340 620"><g transform="translate(-350 -170)">%s</g></svg>`, paths)
	for _, side := range []int{22, 44} {
		h := side * 82 / 100
		w := h * 340 / 620
		img, err := render(bat, side, side, (side-w)/2, (side-h)/2, w, h)
		if err != nil {
			fail(err)
		}
		write(filepath.Join(*out, fmt.Sprintf("bat-%d.png", side)), img)
		// The bat with a dot, for "something needs you": a template image too,
		// so macOS draws it right on a light or a dark menu bar.
		write(filepath.Join(*out, fmt.Sprintf("bat-alert-%d.png", side)), withDot(img, color.RGBA{0, 0, 0, 255}, side))
	}
}

// oneTransform folds the brand icon's "translate scale translate" chain into
// a single matrix, which oksvg reads where it does not read the chain.
func oneTransform(svg string) string {
	chain := regexp.MustCompile(`transform="translate\((-?[\d.]+) (-?[\d.]+)\) scale\((-?[\d.]+)\) translate\((-?[\d.]+) (-?[\d.]+)\)"`)
	return chain.ReplaceAllStringFunc(svg, func(t string) string {
		m := chain.FindStringSubmatch(t)
		f := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
		tx, ty, s, ux, uy := f(m[1]), f(m[2]), f(m[3]), f(m[4]), f(m[5])
		return fmt.Sprintf(`transform="matrix(%g 0 0 %g %g %g)"`, s, s, tx+s*ux, ty+s*uy)
	})
}

// inlineFills copies a group's fill onto the paths inside it: oksvg does not
// inherit a fill from a group, so without this the bat (a filled group of
// paths) is left out of the icon.
func inlineFills(svg string) string {
	group := regexp.MustCompile(`(?s)<g fill="(#[0-9A-Fa-f]+)">(.*?)</g>`)
	return group.ReplaceAllStringFunc(svg, func(g string) string {
		m := group.FindStringSubmatch(g)
		inner := strings.ReplaceAll(m[2], "<path d=", `<path fill="`+m[1]+`" d=`)
		return `<g fill="` + m[1] + `">` + inner + "</g>"
	})
}

// atOrigin rewrites an SVG whose viewBox starts away from 0,0 so that it
// starts there: the content is wrapped in a translate of minus the origin.
func atOrigin(svg string) string {
	m := regexp.MustCompile(`<svg([^>]*?)viewBox="(-?[\d.]+) (-?[\d.]+) ([\d.]+) ([\d.]+)"([^>]*)>`).FindStringSubmatchIndex(svg)
	if m == nil {
		return svg
	}
	minX, minY, w, h := svg[m[4]:m[5]], svg[m[6]:m[7]], svg[m[8]:m[9]], svg[m[10]:m[11]]
	if minX == "0" && minY == "0" {
		return svg
	}
	head := fmt.Sprintf(`<svg%sviewBox="0 0 %s %s"%s><g transform="translate(-%s -%s)">`, svg[m[2]:m[3]], w, h, svg[m[12]:m[13]], minX, minY)
	body := svg[m[1]:]
	body = strings.TrimSuffix(strings.TrimSpace(body), "</svg>") + "</g></svg>"
	return head + body
}

// render draws an SVG into a canvas of cw×ch, placed at x,y with size w×h.
// withDot puts a dot at the bat's top right in ink, cut out from the bat
// by a ring of transparency so the two read apart.
func withDot(src image.Image, ink color.RGBA, side int) image.Image {
	b := src.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := src.At(x, y).RGBA()
			out.Set(x, y, color.NRGBA{ink.R, ink.G, ink.B, uint8(a >> 8)})
		}
	}
	r := float64(side) * 0.17
	cx, cy := float64(side)-r-1, r+1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			switch {
			case d <= r:
				out.Set(x, y, color.NRGBA{ink.R, ink.G, ink.B, 0xFF})
			case d <= r+float64(side)/22:
				out.Set(x, y, color.NRGBA{})
			}
		}
	}
	return out
}

func render(svg string, cw, ch, x, y, w, h int) (image.Image, error) {
	icon, err := oksvg.ReadIconStream(strings.NewReader(svg))
	if err != nil {
		return nil, err
	}
	icon.SetTarget(float64(x), float64(y), float64(w), float64(h))
	img := image.NewRGBA(image.Rect(0, 0, cw, ch))
	scanner := rasterx.NewScannerGV(cw, ch, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(cw, ch, scanner), 1)
	return img, nil
}

func write(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "icons:", err)
	os.Exit(1)
}
