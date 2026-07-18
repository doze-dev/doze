// Shared color constants — the single source of truth for the hex values the
// CLI palette (this package) and the TUI dash's default theme (internal/tui)
// have in common, so a palette change lands in one place.
package ui

// Light/dark pairs for the shared vocabulary: the default violet accent, muted
// text, and the four state colors (green/gold/cyan/red), which stay constant
// across every TUI theme.
const (
	AccentLight = "#6F42C1"
	AccentDark  = "#BD93F9"
	DimLight    = "#666C78"
	DimDark     = "#828895"
	GoodLight   = "#1A7F37"
	GoodDark    = "#7EE787" // state green
	WarnLight   = "#9A6700"
	WarnDark    = "#F2C879" // state gold
	CoolLight   = "#0969DA"
	CoolDark    = "#82AAFF" // state cyan
	BadLight    = "#CF222E"
	BadDark     = "#FF7A93" // state red
)
