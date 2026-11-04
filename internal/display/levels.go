package display

// Level ranks, for a logs view's "info and up". A line that says no level
// counts as info: it is shown by default and hidden by "warnings and up".
const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
)

// LevelNames are the ranks as a filter reads them, from the least strict.
var LevelNames = []string{"debug and up", "info and up", "warnings and up", "errors only"}

// Rank is a log line's level rank and its three-letter tag ("ERR", "WRN",
// "INF", "DBG"), or LevelInfo and "" for a line that says nothing.
func Rank(line string) (int, string) {
	switch Level(line) {
	case "error":
		return LevelError, "ERR"
	case "warn":
		return LevelWarn, "WRN"
	case "info":
		return LevelInfo, "INF"
	case "debug":
		return LevelDebug, "DBG"
	}
	return LevelInfo, ""
}
