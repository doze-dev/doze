package view

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/display"
	"github.com/doze-dev/doze/internal/journal"
)

// LogsQuery is what a page asks for: an instance's lines ("" is every
// instance merged), filtered, then one window of them for a virtual list.
type LogsQuery struct {
	Instance string `json:"instance"`
	// Show, on the merged page, keeps only these instances' lines; nil
	// keeps every instance. "doze" is doze's own events.
	Show []string `json:"show,omitempty"`
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
	// Counts is how many lines each level has, by rank, over the whole
	// unfiltered list: the "levels" box.
	Counts [4]int `json:"counts"`
	// Matches is how many lines the search matched, over every level.
	Matches int `json:"matches"`
	// Instances names the instances with lines, in first-seen order, so the
	// page colours each the same way every time.
	Instances []string `json:"instances,omitempty"`
}

// Line is one line as the page draws it.
type Line struct {
	Instance string `json:"instance,omitempty"`
	Time     string `json:"time"` // clock, local
	Rel      string `json:"rel"`  // relative to now: "-3m 05s"
	Line     string `json:"line"`
	Level    string `json:"level,omitempty"` // INF, WRN, ERR, DBG, or zzz for doze's own
	Rank     int    `json:"rank"`
	Run      int    `json:"run,omitempty"`
	// Doze marks one of doze's own events, drawn as the Logs board draws
	// them: the bat's zzz, in the accent.
	Doze bool `json:"doze,omitempty"`
}

// Levels names the four filters, as the dash did.
var Levels = []string{"debug and up", "info and up", "warnings and up", "errors only"}

// LogsOf filters and windows what the hub kept. events are doze's own
// journal, merged in as lines from "doze" when the query shows it.
func LogsOf(q LogsQuery, kept []control.LogFrame, events []journal.Event, now time.Time) Logs {
	out := Logs{Instance: q.Instance, Levels: Levels}
	show := map[string]bool{}
	for _, s := range q.Show {
		show[s] = true
	}
	showing := func(inst string) bool { return len(show) == 0 || show[inst] }
	type raw struct {
		f    control.LogFrame
		doze bool
	}
	var all []raw
	for _, f := range kept {
		all = append(all, raw{f: f})
	}
	if q.Instance == "" && showing("doze") {
		for _, e := range events {
			all = append(all, raw{f: control.LogFrame{Instance: "doze", Line: e.Instance + " " + display.Event(e), Time: e.Time}, doze: true})
		}
		sort.SliceStable(all, func(i, j int) bool { return all[i].f.Time.Before(all[j].f.Time) })
	}
	var filtered []Line
	runs := map[int]bool{}
	seen := map[string]bool{}
	search := strings.ToLower(q.Search)
	for _, r := range all {
		f := r.f
		if !seen[f.Instance] {
			seen[f.Instance] = true
			out.Instances = append(out.Instances, f.Instance)
		}
		if f.Run > 0 {
			runs[f.Run] = true
		}
		rank, tag := display.Rank(f.Line)
		if r.doze {
			rank, tag = display.LevelInfo, "zzz"
		}
		if rank >= 0 && rank < 4 {
			out.Counts[rank]++
		}
		if !showing(f.Instance) {
			continue
		}
		if search != "" && strings.Contains(strings.ToLower(f.Line), search) {
			out.Matches++
		}
		if rank < q.Level {
			continue
		}
		if q.Run > 0 && f.Run != q.Run {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(f.Line), search) {
			continue
		}
		l := Line{Time: f.Time.Local().Format("15:04:05"), Rel: rel(now.Sub(f.Time)), Line: withoutLevelWord(f.Line, tag), Level: tag, Rank: rank, Run: f.Run, Doze: r.doze}
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

// rel is a time before now as the AppLogs board shows it: "-3m 05s" within
// the hour, "-2h" and "-3d" beyond it.
func rel(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("-%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("-%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return "-" + display.Ago(d)
}

// withoutLevelWord drops a leading level word the tag already says, as the
// Logs board draws lines: "INFO GET /products" becomes "GET /products"
// beside its INF tag. Only a bare word at the very start is dropped.
func withoutLevelWord(line, tag string) string {
	if tag == "" || tag == "zzz" {
		return line
	}
	trimmed := strings.TrimLeft(line, " ")
	for _, w := range []string{"INFO", "WARN", "WARNING", "ERROR", "DEBUG", "INF", "WRN", "ERR", "DBG"} {
		if len(trimmed) > len(w) && strings.EqualFold(trimmed[:len(w)], w) && (trimmed[len(w)] == ' ' || trimmed[len(w)] == '\t') {
			return strings.TrimLeft(trimmed[len(w):], " \t")
		}
	}
	return line
}
