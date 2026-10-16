package dash

import (
	"os"
	"path/filepath"
	"strings"
)

// palette is every colour the dash draws with. The meaning of a colour stays
// the same in every theme (good is a healthy thing, warn is a waiting one, bad is
// a failure), so a theme changes the ground, the accent and the tint of the
// greys, and moves the others only far enough to stay apart from its accent.
type palette struct {
	name  string
	about string // one line, for the palette

	bg, panel, sel, pick string // the page, a box, the selection, a table's selected row
	text, bright         string
	dim, dimOn           string // dim, and dim as it is drawn on a selection
	border               string
	accent               string
	good, warn, cool     string
	bad                  string
	extra                string // the fifth run of a meter
}

// violet is the palette of boards/term.css, and the one the dash starts in.
var violet = palette{
	name: "violet", about: "the default: violet on a dark plum",
	bg: hexBg, panel: hexPanel, sel: hexSel, pick: hexPick,
	text: hexText, bright: hexBright, dim: hexDim, dimOn: hexDimOn,
	border: hexBorder, accent: hexAccent,
	good: hexGood, warn: hexWarn, cool: hexCool, bad: hexBad, extra: "#FF9E64",
}

var themes = []palette{
	violet,
	{
		name: "teal", about: "teal and green on a deep sea ground",
		bg: "#0e1816", panel: "#14201e", sel: "#1d3330", pick: "#1f4540",
		text: "#c6dcd8", bright: "#effaf8", dim: "#84a39e", dimOn: "#a9c7c2",
		border: "#4f8a82", accent: "#4FD6C4",
		good: "#86E08A", warn: "#F2C879", cool: "#8FBDFF", bad: "#FF8497", extra: "#FFB070",
	},
	{
		name: "burgundy", about: "rose on a burgundy ground",
		bg: "#1a0f13", panel: "#22151a", sel: "#3a1d27", pick: "#4d2030",
		text: "#e0cdd2", bright: "#fff1f4", dim: "#a88a93", dimOn: "#cdb3bb",
		border: "#9a6676", accent: "#F08FA6",
		good: "#8FE0A0", warn: "#F2C879", cool: "#8DB4F5", bad: "#FF6F5E", extra: "#C9A0FF",
	},
	{
		name: "amber", about: "gold on a dark olive ground",
		bg: "#16140b", panel: "#1d1a0f", sel: "#332d12", pick: "#3b3412",
		text: "#e0dcc2", bright: "#fffbe6", dim: "#a39e7e", dimOn: "#c7c29f",
		border: "#8a7f3a", accent: "#F2E04A",
		good: "#9BE07A", warn: "#FF9A3C", cool: "#7FC4F5", bad: "#FF7E7E", extra: "#C9A0FF",
	},
	{
		name: "slate", about: "cool grey with a steel-blue accent",
		bg: "#12161b", panel: "#181d23", sel: "#28313c", pick: "#2d3b4b",
		text: "#c9d1da", bright: "#f1f5f9", dim: "#8592a0", dimOn: "#aab6c3",
		border: "#667a90", accent: "#9DB7D5",
		good: "#8FD694", warn: "#E8C27A", cool: "#5FD0E0", bad: "#F7808F", extra: "#F0B27A",
	},
}

// themeNamed is the theme called name, ignoring case; violet when there is none.
func themeNamed(name string) palette {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, t := range themes {
		if t.name == name {
			return t
		}
	}
	return violet
}

// ThemeNames are the themes there are, in the order the palette lists them.
func ThemeNames() []string {
	var out []string
	for _, t := range themes {
		out = append(out, t.name)
	}
	return out
}

// themeFile is where the chosen theme is kept between runs.
func themeFile() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "doze", "dash-theme")
}

// DetectTheme is the theme to start in: DOZE_THEME, else the one chosen last.
func DetectTheme() string {
	if t := os.Getenv("DOZE_THEME"); t != "" {
		return t
	}
	if f := themeFile(); f != "" {
		if b, err := os.ReadFile(f); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

// rememberTheme keeps the choice for the next run. A var so tests do not write
// to the user's config.
var rememberTheme = func(name string) {
	f := themeFile()
	if f == "" || os.Getenv("DOZE_THEME") != "" {
		return // DOZE_THEME wins, so a saved choice would never show
	}
	if os.MkdirAll(filepath.Dir(f), 0o755) == nil {
		_ = os.WriteFile(f, []byte(name+"\n"), 0o644)
	}
}

// setTheme draws the dash in the theme called name and keeps the choice.
func (m *Model) setTheme(name string) {
	p := themeNamed(name)
	m.st = newThemed(m.st.plain, p)
	rememberTheme(p.name)
	m.say("theme · " + p.name)
}
