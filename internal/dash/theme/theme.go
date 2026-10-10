// Package theme is the dash's look, as a value. Nothing in the dash reads a
// global palette: a screen is handed the Styles it draws with, so two themes
// can be on screen at once (the picker's live preview) and a golden test can
// choose its own.
//
// The look is a near-neutral canvas lifted by one accent, with the state
// colours — awake, dozing, waking, failed — constant across themes so a state
// always reads the same.
package theme

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/doze-dev/doze/internal/ui"
)

// Palette is the colours of one theme in one variant.
type Palette struct {
	Accent, Text, Dim, Faint color.Color
	Panel, Sel, SelFg        color.Color
	Good, Warn, Cool, Bad    color.Color
}

// Theme is a named accent with a dark and a light variant.
type Theme struct {
	Name        string
	Dark, Light Palette
}

func hex(s string) color.Color { return lipgloss.Color(s) }

// The state colours are shared with `doze status`, so the CLI and the dash
// paint a state alike.
func states(dark bool) (good, warn, cool, bad color.Color) {
	if dark {
		return hex(string(ui.GoodDark)), hex(string(ui.WarnDark)), hex(string(ui.CoolDark)), hex(string(ui.BadDark))
	}
	return hex(ui.GoodLight), hex(ui.WarnLight), hex(ui.CoolLight), hex(ui.BadLight)
}

func dark(accent, text, dim, faint, panel, sel, selFg string) Palette {
	g, w, c, b := states(true)
	return Palette{hex(accent), hex(text), hex(dim), hex(faint), hex(panel), hex(sel), hex(selFg), g, w, c, b}
}

func light(accent, text, dim, faint, panel, sel, selFg string) Palette {
	g, w, c, b := states(false)
	return Palette{hex(accent), hex(text), hex(dim), hex(faint), hex(panel), hex(sel), hex(selFg), g, w, c, b}
}

// All is every theme, the default first.
func All() []Theme {
	return []Theme{
		{"violet",
			dark(string(ui.AccentDark), "#E2E4EE", string(ui.DimDark), "#454C5A", "#3B3550", "#2A2440", "#1C1726"),
			light(ui.AccentLight, "#2A2D36", ui.DimLight, "#B8BEC9", "#D8D2E8", "#E4DEF5", "#F8F6FE")},
		{"emerald",
			dark("#3FD9B8", "#E1EBE6", "#7F8B83", "#46524C", "#2E4A40", "#1F3A30", "#0E2018"),
			light("#0E8F73", "#1F2A25", "#56655D", "#B5C4BC", "#CFE5DB", "#DDEEE6", "#F3FAF6")},
		{"amber",
			dark("#F5A14B", "#ECE7DF", "#8E867D", "#4F4A40", "#4A3F2C", "#3A2F1E", "#241B0E"),
			light("#B25F00", "#2D2820", "#665D50", "#CFC6B5", "#EBDDC4", "#F3E8D3", "#FBF6EC")},
		{"cyan",
			dark("#56D4E0", "#DEEBEC", "#7B898C", "#45525A", "#2E474C", "#1F373C", "#0E2024"),
			light("#0A7C8A", "#1D2A2C", "#52646A", "#B3C7CB", "#CCE4E8", "#DCEEF1", "#F2FAFB")},
		{"rose",
			dark("#F491E8", "#EFE4E8", "#8E848C", "#4F454C", "#4A2E3A", "#3A1F2C", "#24101A"),
			light("#B3399E", "#2D2229", "#665A62", "#CFBFC8", "#EBD3E0", "#F3E2EC", "#FBF3F8")},
	}
}

// Default is the theme a new install starts with.
func Default() Theme { return All()[0] }

// ByName finds a theme, or reports false.
func ByName(name string) (Theme, bool) {
	for _, t := range All() {
		if t.Name == name {
			return t, true
		}
	}
	return Theme{}, false
}

// Styles are the lipgloss styles a screen draws with, built from a palette.
type Styles struct {
	Name string

	Title, Text, Dim, Faint, Accent lipgloss.Style
	Good, Warn, Cool, Bad           lipgloss.Style
	Selected                        lipgloss.Style // a selected row
	Key, KeyLabel                   lipgloss.Style // the key bar

	// Plain turns colour off: NO_COLOR, or a terminal that has none.
	Plain bool

	sources []lipgloss.Style
}

// Styles builds the styles for a variant. plain removes colour and keeps
// emphasis, so a selection is still visible as reverse video.
func (t Theme) Styles(darkVariant, plain bool) Styles {
	p := t.Light
	if darkVariant {
		p = t.Dark
	}
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	s := Styles{
		Name:     t.Name,
		Title:    fg(p.Accent).Bold(true),
		Text:     fg(p.Text),
		Dim:      fg(p.Dim),
		Faint:    fg(p.Faint),
		Accent:   fg(p.Accent),
		Good:     fg(p.Good),
		Warn:     fg(p.Warn),
		Cool:     fg(p.Cool),
		Bad:      fg(p.Bad),
		Selected: lipgloss.NewStyle().Background(p.Sel).Foreground(p.Text),
		Key:      fg(p.Accent).Bold(true),
		KeyLabel: fg(p.Dim),
		Plain:    plain,
	}
	for _, c := range []color.Color{p.Accent, p.Cool, p.Text, p.Good, p.Dim} {
		s.sources = append(s.sources, lipgloss.NewStyle().Foreground(c))
	}
	if plain {
		bare := lipgloss.NewStyle()
		s.Title, s.Text, s.Dim, s.Faint, s.Accent = bare.Bold(true), bare, bare, bare, bare
		s.Good, s.Warn, s.Cool, s.Bad = bare, bare, bare, bare
		s.Selected = bare.Reverse(true)
		s.Key, s.KeyLabel = bare.Bold(true), bare
	}
	return s
}

// Pick returns a stable colour for a name — a log source, say — out of the
// theme's own: the same name is the same colour on every screen, and none of
// them is a state colour, so a source never reads as "failed".
func (s Styles) Pick(name string) lipgloss.Style {
	if s.Plain || len(s.sources) == 0 {
		return lipgloss.NewStyle()
	}
	h := 0
	for _, r := range name {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return s.sources[h%len(s.sources)]
}

// Choice is a remembered theme: which, in which variant, with or without colour.
type Choice struct {
	Name  string
	Dark  bool
	Plain bool
}

func choicePath(home string) string { return filepath.Join(home, "dash.theme") }

// Load reads the remembered choice. ok is false when there is none.
func Load(home string) (Choice, bool) {
	raw, err := os.ReadFile(choicePath(home))
	if err != nil {
		return Choice{}, false
	}
	f := strings.Fields(string(raw))
	if len(f) == 0 {
		return Choice{}, false
	}
	c := Choice{Name: f[0], Dark: true}
	for _, w := range f[1:] {
		switch w {
		case "light":
			c.Dark = false
		case "plain":
			c.Plain = true
		}
	}
	if _, ok := ByName(c.Name); !ok {
		return Choice{}, false
	}
	return c, true
}

// Save remembers a choice, best effort: a theme is not worth an error.
func Save(home string, c Choice) {
	words := []string{c.Name, "dark"}
	if !c.Dark {
		words[1] = "light"
	}
	if c.Plain {
		words = append(words, "plain")
	}
	_ = os.MkdirAll(home, 0o755)
	_ = os.WriteFile(choicePath(home), []byte(strings.Join(words, " ")+"\n"), 0o644)
}
