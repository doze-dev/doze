package view

import (
	"sort"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/display"
)

// LogsQuery is what a page asks for: an instance's lines ("" is every
// instance merged), filtered, then one window of them for a virtual list.
type LogsQuery struct {
	Instance string `json:"instance"`
	// Level keeps lines at this rank or above: 0 everything, 1 info and up,
	// 2 warnings and up, 3 errors only (display.Rank).
	Level int `json:"level"`
	// Search keeps lines containing it, case-insensitively.
	Search string `json:"search,omitempty"`
	// Run keeps one run's lines; 0 is every run.
	Run int `json:"run,omitempty"`
	// From and N are the window: N lines from index From of the filtered
	// list. N of 0 means the whole list.
	From int `json:"from"`
	N    int `json:"n"`
}

// Logs is the answer: the window, and what the whole filtered list is like.
type Logs struct {
	Instance string `json:"instance"`
	Total    int    `json:"total"`
	From     int    `json:"from"`
	Lines    []Line `json:"lines"`
	// Runs are the run numbers seen, oldest first, for stepping through them.
	Runs []int `json:"runs,omitempty"`
	// Levels are the level filter's names, index by LogsQuery.Level.
	Levels []string `json:"levels"`
}

// Line is one line as the page draws it.
type Line struct {
	Instance string `json:"instance,omitempty"`
	Time     string `json:"time"`
	Ago      string `json:"ago"`
	Line     string `json:"line"`
	// Level is the line's level tag ("", "info", "warn", "error"…) and Rank
	// its number, for colour and the filter.
	Level string `json:"level,omitempty"`
	Rank  int    `json:"rank"`
	Run   int    `json:"run,omitempty"`
}

// Levels names the four filters, as the dash did.
var Levels = []string{"everything", "info and up", "warnings and up", "errors only"}

// LogsOf filters and windows what the hub kept.
func LogsOf(q LogsQuery, kept []control.LogFrame, now time.Time) Logs {
	out := Logs{Instance: q.Instance, Levels: Levels}
	var filtered []Line
	runs := map[int]bool{}
	search := strings.ToLower(q.Search)
	for _, f := range kept {
		if f.Run > 0 {
			runs[f.Run] = true
		}
		rank, tag := display.Rank(f.Line)
		if rank < q.Level {
			continue
		}
		if q.Run > 0 && f.Run != q.Run {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(f.Line), search) {
			continue
		}
		l := Line{Time: f.Time.UTC().Format(time.RFC3339), Ago: ago(f.Time, now), Line: f.Line, Level: tag, Rank: rank, Run: f.Run}
		if q.Instance == "" {
			l.Instance = f.Instance
		}
		filtered = append(filtered, l)
	}
	for r := range runs {
		out.Runs = append(out.Runs, r)
	}
	sort.Ints(out.Runs)
	out.Total = len(filtered)
	from := max(0, min(q.From, len(filtered)))
	n := q.N
	if n <= 0 || from+n > len(filtered) {
		n = len(filtered) - from
	}
	out.From = from
	out.Lines = filtered[from : from+n]
	if out.Lines == nil {
		out.Lines = []Line{}
	}
	return out
}
