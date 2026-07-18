package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/doze-dev/doze/internal/ui"
)

// ── palette / themes ────────────────────────────────────────────────────────
// The look is a dark "console": a near-neutral slate canvas lifted by one vivid
// accent, with bright state marks on small glyphs/badges and a faint accent tint
// in the chrome (borders, selection). Color stays off the large fills so it never
// reads as noisy. Themes vary the accent + chrome; cycle them with `t` (persisted
// under the doze home). The state colors (active/idle/booting/error) stay constant
// across themes so status always reads the same.
type theme struct {
	name                                        string
	accent, text, dim, faint, panel, sel, selFg lipgloss.Color
	green, gold, cyan, red                      lipgloss.Color
}

// dim must clear 4.5:1 on common dark backgrounds (#1e1e1e, not just pure
// black) — it carries real secondary text. faint is chrome only (rules, bar
// tracks, separators); never render copy in it. Accents keep ΔE > 20 from the
// constant state colors so a selected name can't read as a state mark.
// The state columns (green/gold/cyan/red) are the shared ui.*Dark constants —
// constant across themes, and the same values `doze status` paints with.
var themes = []theme{
	{"violet", ui.AccentDark, "#E2E4EE", ui.DimDark, "#454C5A", "#3B3550", "#2A2440", "#1C1726", ui.GoodDark, ui.WarnDark, ui.CoolDark, ui.BadDark},
	{"emerald", "#3FD9B8", "#E1EBE6", "#7F8B83", "#46524C", "#2E4A40", "#1F3A30", "#0E2018", ui.GoodDark, ui.WarnDark, ui.CoolDark, ui.BadDark},
	{"amber", "#F5A14B", "#ECE7DF", "#8E867D", "#4F4A40", "#4A3F2C", "#3A2F1E", "#241B0E", ui.GoodDark, ui.WarnDark, ui.CoolDark, ui.BadDark},
	{"cyan", "#56D4E0", "#DEEBEC", "#7B898C", "#45525A", "#2E474C", "#1F373C", "#0E2024", ui.GoodDark, ui.WarnDark, ui.CoolDark, ui.BadDark},
	{"rose", "#F491E8", "#EFE4E8", "#8E848C", "#4F454C", "#4A2E3A", "#3A1F2C", "#24101A", ui.GoodDark, ui.WarnDark, ui.CoolDark, ui.BadDark},
}

// defaultAdaptive is the default (violet) theme as light/dark pairs, so the dash
// stays readable on light terminals. The pairs come from the shared constants in
// internal/ui/colors.go (which the CLI paints with too); the named themes keep
// their dark-only palettes. adaptive wraps a theme color with its light variant.
func adaptive(light string, dark lipgloss.Color) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: string(dark)}
}

// noColor mirrors internal/ui's NO_COLOR gating: when set, lipgloss strips all
// styling, so selections must be indicated with reverse video / markers instead.
var noColor = os.Getenv("NO_COLOR") != ""

// reverseVideo wraps s in a raw reverse-video escape — the NO_COLOR selection
// indicator (NO_COLOR bans color, not emphasis; lipgloss drops everything).
func reverseVideo(s string) string { return "\x1b[7m" + s + "\x1b[27m" }

// selStyled renders a selection span: reverse video under NO_COLOR (so it stays
// visible), the given background style otherwise.
func selStyled(st lipgloss.Style, s string) string {
	if noColor {
		return reverseVideo(s)
	}
	return st.Render(s)
}

var (
	cAccent, cText, cDim, cFaint, cPanel, cSel, cSelFg lipgloss.TerminalColor
	cGreen, cGold, cCyan, cRed                         lipgloss.TerminalColor

	stTitle, stDim, stFaint, stText, stLabel, stErr, stAccent, stGreen lipgloss.Style
)

var activeTheme int

// applyTheme makes themes[i] (wrapped to range) the active palette and rebuilds
// every derived style.
func applyTheme(i int) {
	activeTheme = ((i % len(themes)) + len(themes)) % len(themes)
	t := themes[activeTheme]
	cAccent, cText, cDim, cFaint = t.accent, t.text, t.dim, t.faint
	cPanel, cSel, cSelFg = t.panel, t.sel, t.selFg
	cGreen, cGold, cCyan, cRed = t.green, t.gold, t.cyan, t.red
	if activeTheme == 0 { // the default theme adapts to light terminals
		cAccent, cText = adaptive(ui.AccentLight, t.accent), adaptive("#2A2D36", t.text)
		cDim, cFaint = adaptive(ui.DimLight, t.dim), adaptive("#B8BEC9", t.faint)
		cPanel, cSel = adaptive("#D8D2E8", t.panel), adaptive("#E4DEF5", t.sel)
		cSelFg = adaptive("#F8F6FE", t.selFg)
		cGreen, cGold = adaptive(ui.GoodLight, t.green), adaptive(ui.WarnLight, t.gold)
		cCyan, cRed = adaptive(ui.CoolLight, t.cyan), adaptive(ui.BadLight, t.red)
	}
	stTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stDim = lipgloss.NewStyle().Foreground(cDim)
	stFaint = lipgloss.NewStyle().Foreground(cFaint)
	stText = lipgloss.NewStyle().Foreground(cText)
	stLabel = lipgloss.NewStyle().Foreground(cDim)
	stErr = lipgloss.NewStyle().Foreground(cRed)
	stAccent = lipgloss.NewStyle().Foreground(cAccent)
	stGreen = lipgloss.NewStyle().Foreground(cGreen)
}

func init() { applyTheme(0) }

// themeFilePath is where the chosen theme name is remembered, under the doze home.
func themeFilePath() string {
	home := os.Getenv("DOZE_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(h, ".doze")
	}
	return filepath.Join(home, "tui.theme")
}

// loadTheme applies the persisted theme, if any.
func loadTheme() {
	p := themeFilePath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	name := strings.TrimSpace(string(b))
	for i, t := range themes {
		if t.name == name {
			applyTheme(i)
			return
		}
	}
}

// saveTheme remembers the active theme for next time (best-effort).
func saveTheme() {
	if p := themeFilePath(); p != "" {
		_ = os.WriteFile(p, []byte(themes[activeTheme].name), 0o644)
	}
}

func stateColor(state string) lipgloss.TerminalColor {
	switch state {
	case "active":
		return cGreen
	case "idle":
		return cGold
	case "booting":
		return cCyan
	case "error", "tainted":
		return cRed
	default:
		return cDim
	}
}
