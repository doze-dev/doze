// Package brand is where doze's colours and type live, once, for every
// surface that draws them: the CLI (internal/ui), the dash, the app and the
// site. The app's tokens.css is generated from here (go generate), and a test
// keeps the values the CLI holds in step.
//
// The app's themes are the dash's five palettes (docs/design/desktop/boards/
// app.css draws every board in them) and a light one chosen for a window.
package brand

import (
	"fmt"
	"sort"
	"strings"

	"github.com/doze-dev/doze/internal/ui"
)

//go:generate go run ./gen -o ../desk/frontend/src/tokens.css

// Theme is one set of colours, named as the dash names them.
type Theme struct {
	// Name is the theme's name: violet, teal, burgundy, amber, slate, light.
	Name string
	// About is the one line Settings shows under the swatch.
	About string
	// Surfaces: the window, the sidebar and title bar, cards and tables,
	// the two line weights, and the well (an input, a log, a code block).
	Bg, Side, Panel, Line, Line2, Well string
	// Text, dim text, and the brightest text (names, figures, headings).
	Text, Dim, Bright string
	// The accent and the four state colours, shared with the CLI and the
	// dash, plus the fifth run of a meter.
	Accent, Good, Warn, Cool, Bad, Extra string
	// Selection, the picked row, and a bar's track.
	Sel, Pick, Track string
	// Pale is a light theme: text on a filled colour is white, not the ground.
	Pale bool
}

// The themes, in the order Settings lists them. Violet is the default and
// the dash's own; its state colours and accent are the CLI's dark ones.
var (
	Violet = Theme{
		Name: "violet", About: "the default: violet on a dark plum",
		Bg: "#15131d", Side: "#110f18", Panel: "#1b1927", Line: "#2c2840", Line2: "#37324b", Well: "#120f19",
		Text: "#cfcbe3", Dim: "#8a85a8", Bright: "#f4f1ff",
		Accent: ui.AccentDark, Good: ui.GoodDark, Warn: ui.WarnDark, Cool: ui.CoolDark, Bad: ui.BadDark, Extra: "#FF9E64",
		Sel: "#2b2542", Pick: "#3a3170", Track: "#262236",
	}
	Teal = Theme{
		Name: "teal", About: "teal and green on a deep sea ground",
		Bg: "#0e1816", Side: "#0b1312", Panel: "#14201e", Line: "#1d3330", Line2: "#27423e", Well: "#0b1312",
		Text: "#c6dcd8", Dim: "#84a39e", Bright: "#effaf8",
		Accent: "#4FD6C4", Good: "#86E08A", Warn: "#F2C879", Cool: "#8FBDFF", Bad: "#FF8497", Extra: "#FFB070",
		Sel: "#1d3330", Pick: "#1f4540", Track: "#1d2b29",
	}
	Burgundy = Theme{
		Name: "burgundy", About: "rose on a burgundy ground",
		Bg: "#1a0f13", Side: "#150b0f", Panel: "#22151a", Line: "#3a1d27", Line2: "#4a2733", Well: "#150b0f",
		Text: "#e0cdd2", Dim: "#a88a93", Bright: "#fff1f4",
		Accent: "#F08FA6", Good: "#8FE0A0", Warn: "#F2C879", Cool: "#8DB4F5", Bad: "#FF6F5E", Extra: "#C9A0FF",
		Sel: "#3a1d27", Pick: "#4d2030", Track: "#2e1a21",
	}
	Amber = Theme{
		Name: "amber", About: "gold on a dark olive ground",
		Bg: "#16140b", Side: "#111009", Panel: "#1d1a0f", Line: "#332d12", Line2: "#443c18", Well: "#111009",
		Text: "#e0dcc2", Dim: "#a39e7e", Bright: "#fffbe6",
		Accent: "#F2E04A", Good: "#9BE07A", Warn: "#FF9A3C", Cool: "#7FC4F5", Bad: "#FF7E7E", Extra: "#C9A0FF",
		Sel: "#332d12", Pick: "#3b3412", Track: "#2a2612",
	}
	Slate = Theme{
		Name: "slate", About: "cool grey with a steel-blue accent",
		Bg: "#12161b", Side: "#0e1115", Panel: "#181d23", Line: "#28313c", Line2: "#34404d", Well: "#0e1115",
		Text: "#c9d1da", Dim: "#8592a0", Bright: "#f1f5f9",
		Accent: "#9DB7D5", Good: "#8FD694", Warn: "#E8C27A", Cool: "#5FD0E0", Bad: "#F7808F", Extra: "#F0B27A",
		Sel: "#28313c", Pick: "#2d3b4b", Track: "#222a33",
	}
	// The light ones: the same hue on a pale ground, the accent and state
	// colours darkened until text in them reads (4.5:1 on the panel).
	Light = Theme{
		Name: "violet", About: "violet on white",
		Bg: "#fbfaff", Side: "#f3f1fa", Panel: "#ffffff", Line: "#e4e0f0", Line2: "#d4cde6", Well: "#f6f4fb",
		Text: "#3a3550", Dim: "#6f6888", Bright: "#17141f",
		Accent: "#7a4fe0", Good: "#17773f", Warn: "#9a6400", Cool: "#2f62d9", Bad: "#cc3550", Extra: "#b85c00",
		Sel: "#ece6fb", Pick: "#e3d9ff", Track: "#ece8f5",
		Pale: true,
	}
	TealLight = Theme{
		Name: "teal", About: "teal on a pale sea ground",
		Bg: "#f6fbfa", Side: "#edf6f4", Panel: "#ffffff", Line: "#d9e9e6", Line2: "#c4dcd8", Well: "#f0f7f6",
		Text: "#2d4744", Dim: "#5a7470", Bright: "#0f1f1d",
		Accent: "#0f7f73", Good: "#17773f", Warn: "#9a6400", Cool: "#2f62d9", Bad: "#c8364b", Extra: "#b85c00",
		Sel: "#dff0ed", Pick: "#c9e7e2", Track: "#e3efed",
		Pale: true,
	}
	BurgundyLight = Theme{
		Name: "burgundy", About: "rose on a blush ground",
		Bg: "#fdf8f9", Side: "#f8eef1", Panel: "#ffffff", Line: "#efdde2", Line2: "#e3c8d0", Well: "#faf2f4",
		Text: "#4a3038", Dim: "#7a5d65", Bright: "#22121a",
		Accent: "#b5345a", Good: "#17773f", Warn: "#9a6400", Cool: "#2f62d9", Bad: "#c43c2c", Extra: "#7a4fe0",
		Sel: "#f6e3e9", Pick: "#f0d0da", Track: "#f3e6ea",
		Pale: true,
	}
	AmberLight = Theme{
		Name: "amber", About: "gold on a parchment ground",
		Bg: "#fcfbf3", Side: "#f6f3e3", Panel: "#ffffff", Line: "#e9e4c8", Line2: "#dcd4ae", Well: "#f8f6ea",
		Text: "#444026", Dim: "#6d6849", Bright: "#1c1a0c",
		Accent: "#836700", Good: "#3f7d1f", Warn: "#b04e00", Cool: "#2563a8", Bad: "#c43c3c", Extra: "#7a4fe0",
		Sel: "#f3edcc", Pick: "#ebe2b0", Track: "#efebd8",
		Pale: true,
	}
	SlateLight = Theme{
		Name: "slate", About: "steel blue on a cool white",
		Bg: "#f8fafc", Side: "#eff3f7", Panel: "#ffffff", Line: "#dfe5ec", Line2: "#cbd4de", Well: "#f2f5f8",
		Text: "#33404d", Dim: "#5f6c7a", Bright: "#0f172a",
		Accent: "#3d6a99", Good: "#237a3b", Warn: "#946200", Cool: "#0e7490", Bad: "#c2334a", Extra: "#b45309",
		Sel: "#e3eaf2", Pick: "#d3dfec", Track: "#e6ebf1",
		Pale: true,
	}

	// Dark is the default dark theme, by its older name.
	Dark = Violet
)

// Palette is a colour theme as Settings offers it: one name, drawn dark or
// light as the mode says.
type Palette struct {
	Name        string
	Dark, Light Theme
}

// Palettes are the colour themes, in Settings' order. Violet is the default.
var Palettes = []Palette{
	{"violet", Violet, Light},
	{"teal", Teal, TealLight},
	{"burgundy", Burgundy, BurgundyLight},
	{"amber", Amber, AmberLight},
	{"slate", Slate, SlateLight},
}

// Themes is every theme, dark ones first.
var Themes = []Theme{Violet, Teal, Burgundy, Amber, Slate, Light, TealLight, BurgundyLight, AmberLight, SlateLight}

// Named is the dark theme called name, ignoring case; Violet when there is
// none.
func Named(name string) Theme {
	for _, p := range Palettes {
		if strings.EqualFold(p.Name, name) {
			return p.Dark
		}
	}
	return Violet
}

// Bat colours, from website/brand: the bat on its tile.
const (
	BatInk  = "#3A2448"
	BatTile = "#F0A24B"
)

// Sizes and type, in CSS, as the boards set them: Inter 13/20 for text,
// JetBrains Mono 12 for anything a machine wrote, a 42 px title bar, a
// 224 px sidebar, 48 px list rows.
var sizes = map[string]string{
	"font-ui":     `"Inter Variable", Inter, -apple-system, system-ui, sans-serif`,
	"font-mono":   `"JetBrains Mono Variable", "JetBrains Mono", ui-monospace, Menlo, monospace`,
	"text-xs":     "11px",
	"text-sm":     "12px",
	"text-base":   "13px",
	"text-md":     "14px",
	"text-lg":     "16px",
	"text-xl":     "20px",
	"text-figure": "22px",
	"lh":          "20px", // line height; not --line, which is the border colour
	"space-1":     "4px",
	"space-2":     "8px",
	"space-3":     "12px",
	"space-4":     "16px",
	"space-5":     "20px",
	"space-6":     "24px",
	"space-7":     "28px",
	"radius":      "7px",
	"radius-lg":   "10px",
	"radius-xl":   "12px",
	"titlebar":    "42px",
	"sidebar":     "224px",
	"pane":        "372px",
	"row":         "48px",
	"tile":        "112px",
	"keys":        "34px",
}

// vars is a theme as CSS custom properties.
func (t Theme) vars() []string {
	return []string{
		"--bg: " + t.Bg, "--side: " + t.Side, "--panel: " + t.Panel, "--line: " + t.Line, "--line2: " + t.Line2, "--well: " + t.Well,
		"--text: " + t.Text, "--dim: " + t.Dim, "--bright: " + t.Bright,
		"--accent: " + t.Accent, "--good: " + t.Good, "--warn: " + t.Warn, "--cool: " + t.Cool, "--bad: " + t.Bad, "--extra: " + t.Extra,
		"--sel: " + t.Sel, "--pick: " + t.Pick, "--track: " + t.Track,
		// Text on a filled accent or state colour: the ground on a dark
		// theme, white on the light one.
		"--ink: " + t.ink(), "--on-accent: " + t.ink(),
	}
}

func (t Theme) ink() string {
	if t.Pale {
		return "#ffffff"
	}
	return t.Bg
}

// CSS renders tokens.css: the sizes; violet, dark or light as macOS is,
// until the page says otherwise; then every colour theme in each mode,
// under [data-theme] (violet, teal, …) and [data-mode] (dark or light),
// which the page sets from Settings, resolving "system" itself.
func CSS() string {
	var b strings.Builder
	b.WriteString("/* Generated by go generate ./internal/brand; do not edit. */\n:root {\n")
	keys := make([]string, 0, len(sizes))
	for k := range sizes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  --%s: %s;\n", k, sizes[k])
	}
	for _, v := range Violet.vars() {
		fmt.Fprintf(&b, "  %s;\n", v)
	}
	b.WriteString("}\n@media (prefers-color-scheme: light) {\n  :root:not([data-mode]) {\n")
	for _, v := range Light.vars() {
		fmt.Fprintf(&b, "    %s;\n", v)
	}
	b.WriteString("  }\n}\n")
	for _, p := range Palettes {
		for _, m := range []struct {
			mode string
			t    Theme
		}{{"dark", p.Dark}, {"light", p.Light}} {
			fmt.Fprintf(&b, ":root[data-theme=%q][data-mode=%q] {\n", p.Name, m.mode)
			for _, v := range m.t.vars() {
				fmt.Fprintf(&b, "  %s;\n", v)
			}
			b.WriteString("}\n")
		}
	}
	return b.String()
}
