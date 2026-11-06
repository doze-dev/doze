package view

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/desk/prefs"
	"github.com/doze-dev/doze/internal/display"
)

// Palette is ⌘K, as the Palette boards draw it. With nothing typed: what
// needs you, what you opened lately, and a few things to do. A name typed
// finds services and stacks across every stack; a verb then a name finds
// what the verb can apply to, with "Did you mean" under it; nothing found
// says so and offers the closest name.
type Palette struct {
	Query    string           `json:"query"`
	Sections []PaletteSection `json:"sections"`
	// Nothing is set when the query matched nothing.
	Nothing *PaletteNothing `json:"nothing,omitempty"`
}

// PaletteSection is one heading of the list.
type PaletteSection struct {
	Title string       `json:"title"`
	Rows  []PaletteRow `json:"rows"`
}

// PaletteRow is one line: a glyph, the name in bold with the matched runes
// marked, the words after it, the key at the right, and what ⏎ does. Acts
// are the row's own actions, shown on tab and run by ⌘⏎ (the first).
type PaletteRow struct {
	// Glyph is "dot" with Tone the dot's colour class, or a character.
	Glyph string `json:"glyph"`
	Tone  string `json:"tone,omitempty"`
	Label string `json:"label"`
	At    []int  `json:"at,omitempty"`
	Note  string `json:"note,omitempty"`
	Key   string `json:"key,omitempty"`
	CLI   string `json:"cli,omitempty"`
	Bad   bool   `json:"bad,omitempty"`
	Do    Doing  `json:"do"`
	Acts  []Act  `json:"acts,omitempty"`
}

// Act is one of a row's actions: the word on the chip and what it does.
type Act struct {
	Label string `json:"label"`
	Do    Doing  `json:"do"`
}

// PaletteNothing is the no-match line and the closest name, if any.
type PaletteNothing struct {
	Name    string `json:"name"`
	Where   string `json:"where"`   // "shop or data"
	Closest string `json:"closest"` // "events"
	Why     string `json:"why"`     // "kafka, in shop"
	Key     string `json:"key,omitempty"`
	Do      *Doing `json:"do,omitempty"`
	// Fill is the corrected command a verb with a near name becomes: ⏎ puts
	// it in the box to be read and run, never runs the verb on a guess.
	Fill string `json:"fill,omitempty"`
}

// Doing is what choosing a row does. StackKey names the stack it is about.
type Doing struct {
	// Kind: "verb" runs Verb on Target (a dialog first when Ask); "page"
	// goes to Page of Target; "copy" copies Text; "open" opens Text in the
	// browser; "stack" runs Verb (up-last, down) on the stack; "folder"
	// asks for a folder.
	Kind     string `json:"kind"`
	StackKey string `json:"stackKey,omitempty"`
	Verb     string `json:"verb,omitempty"`
	Target   string `json:"target,omitempty"`
	Ask      bool   `json:"ask,omitempty"`
	Page     string `json:"page,omitempty"`
	Text     string `json:"text,omitempty"`
}

// verbs are the words a person types first. Each maps to what it does.
var verbs = []string{"sleep", "wake", "restart", "pin", "unpin", "reset", "off", "on", "logs", "copy", "open", "run", "up", "down"}

// PaletteOf lists the palette for a query across every stack. current is
// the stack on screen, whose rows come first; recent is from prefs.
func PaletteOf(stacks []hub.StackView, current string, recent []prefs.Recent, query string, now time.Time) Palette {
	query = strings.TrimSpace(query)
	p := Palette{Query: query, Sections: []PaletteSection{}}
	// The current stack first, then the rest as the Library orders them.
	sort.SliceStable(stacks, func(i, j int) bool {
		if (stacks[i].Key == current) != (stacks[j].Key == current) {
			return stacks[i].Key == current
		}
		return false
	})
	if query == "" {
		p.Sections = atHand(stacks, recent, now)
		return p
	}
	verb, rest, _ := strings.Cut(strings.ToLower(query), " ")
	if isVerb(verb) {
		p.Sections = forVerb(stacks, verb, strings.TrimSpace(rest), now)
		if len(p.Sections) == 0 {
			p.Nothing = nothing(stacks, verb, strings.TrimSpace(rest))
		}
		return p
	}
	p.Sections = byName(stacks, query, now)
	if len(p.Sections) == 0 {
		p.Nothing = nothing(stacks, "", query)
	}
	return p
}

// atHand is the empty palette: Needs you, Recent, Do.
func atHand(stacks []hub.StackView, recent []prefs.Recent, now time.Time) []PaletteSection {
	var out []PaletteSection
	var needs []PaletteRow
	for _, st := range stacks {
		if !st.Running {
			continue
		}
		snap := snapshot(st)
		for _, v := range st.Instances {
			if display.Troubled(v) {
				needs = append(needs, instanceRow(st, snap, v, now, "⏎ open"))
			}
		}
	}
	if len(needs) > 0 {
		out = append(out, PaletteSection{Title: "Needs you", Rows: needs})
	}
	var rec []PaletteRow
	for _, r := range recent {
		for _, st := range stacks {
			if st.Key != r.Key || !st.Running {
				continue
			}
			snap := snapshot(st)
			if v, ok := display.Find(snap, r.Name); ok {
				rec = append(rec, recentRow(st, snap, v, now))
			}
		}
		if len(rec) == 4 {
			break
		}
	}
	if len(rec) > 0 {
		out = append(out, PaletteSection{Title: "Recent", Rows: rec})
	}
	var do []PaletteRow
	for _, st := range stacks {
		if st.Running {
			if n := awakeCount(st); n > 0 {
				do = append(do, sleepAllRow(st, "wakes on connect; nothing is lost"))
			}
		} else {
			do = append(do, upRow(st))
		}
	}
	do = append(do, PaletteRow{Glyph: "⌘O", Tone: "dim", Label: "Open folder…", Note: "add a project to the Library", Key: "open", Do: Doing{Kind: "folder"}})
	out = append(out, PaletteSection{Title: "Do", Rows: do})
	return out
}

// byName finds services and stacks whose names match, best match first,
// and the Do rows that match too.
func byName(stacks []hub.StackView, query string, now time.Time) []PaletteSection {
	type hit struct {
		row   PaletteRow
		score int
		order int
	}
	var hits []hit
	add := func(r PaletteRow) {
		at, ok := display.FuzzyMatch(query, r.Label)
		if !ok {
			return
		}
		r.At = at
		hits = append(hits, hit{row: r, score: display.FuzzyScore(at), order: len(hits)})
	}
	for _, st := range stacks {
		if !st.Running {
			// A name under Open opens: a stopped stack's page, where Up is.
			// Starting it is "up acme", said as a verb.
			add(PaletteRow{Glyph: "dot", Tone: "dim", Label: st.Name, Note: display.Tilde(st.Dir) + " · stopped", Key: "⏎ open", Do: Doing{Kind: "page", StackKey: st.Key, Page: "home"}})
			continue
		}
		snap := snapshot(st)
		for _, r := range display.Rows(snap) {
			add(instanceRow(st, snap, r.V, now, "⏎ open"))
		}
		add(PaletteRow{Glyph: "dot", Tone: "g", Label: st.Name, Note: display.Tilde(st.Dir) + " · " + stackFigure(st), Key: "⏎ open", Do: Doing{Kind: "page", StackKey: st.Key, Page: "home"}})
		if awakeCount(st) > 0 {
			add(sleepAllRow(st, "wakes on connect; nothing is lost"))
		}
	}
	add(PaletteRow{Glyph: "⌘O", Tone: "dim", Label: "Open folder…", Note: "add a project to the Library", Key: "open", Do: Doing{Kind: "folder"}})
	if len(hits) == 0 {
		return nil
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score < hits[j].score
		}
		return hits[i].order < hits[j].order
	})
	rows := make([]PaletteRow, 0, len(hits))
	for _, h := range hits {
		rows = append(rows, h.row)
	}
	return []PaletteSection{{Title: "Open", Rows: rows}}
}

// forVerb lists what a verb can apply to, narrowed by the name after it.
func forVerb(stacks []hub.StackView, verb, name string, now time.Time) []PaletteSection {
	type hit struct {
		row   PaletteRow
		score int
		order int
	}
	var hits []hit
	add := func(label string, r PaletteRow) {
		if name != "" {
			at, ok := display.FuzzyMatch(name, label)
			if !ok {
				return
			}
			r.At = at
			hits = append(hits, hit{row: r, score: display.FuzzyScore(at), order: len(hits)})
			return
		}
		hits = append(hits, hit{row: r, order: len(hits)})
	}
	for _, st := range stacks {
		switch verb {
		case "up":
			if !st.Running {
				add(st.Name, upRow(st))
			} else {
				// Said, not missing: "up shop" on a running shop.
				add(st.Name, PaletteRow{Glyph: "dot", Tone: "g", Label: st.Name + " is up", Note: "already running; ⏎ opens it", Key: "⏎ open", Do: Doing{Kind: "page", StackKey: st.Key, Page: "home"}})
			}
			continue
		case "down":
			if st.Running {
				add(st.Name, PaletteRow{Glyph: "↓", Tone: "dim", Label: "Down " + st.Name, Note: "stops everything; the set is remembered", Key: "down " + st.Name, CLI: "doze down", Do: Doing{Kind: "stack", StackKey: st.Key, Verb: "down"}})
			} else {
				add(st.Name, PaletteRow{Glyph: "dot", Tone: "dim", Label: st.Name + " is stopped", Note: "already down; ⏎ opens it", Key: "⏎ open", Do: Doing{Kind: "page", StackKey: st.Key, Page: "home"}})
			}
			continue
		case "restart":
			if st.Running {
				// A stack's name after restart restarts the stack: down, then up
				// as last time.
				add(st.Name, PaletteRow{Glyph: "↻", Tone: "dim", Label: "Restart " + st.Name, Note: "the whole stack: down, then up as last time", Key: "restart " + st.Name, CLI: "doze down && doze up --last", Do: Doing{Kind: "stack", StackKey: st.Key, Verb: "restart"}})
			}
		}
		if !st.Running {
			continue
		}
		snap := snapshot(st)
		for _, r := range display.Rows(snap) {
			if row, ok := verbRow(st, snap, r.V, verb, now); ok {
				add(r.V.Name, row)
			}
		}
	}
	if len(hits) == 0 {
		return nil
	}
	if name != "" {
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].score != hits[j].score {
				return hits[i].score < hits[j].score
			}
			return hits[i].order < hits[j].order
		})
	}
	rows := make([]PaletteRow, 0, len(hits))
	for _, h := range hits {
		rows = append(rows, h.row)
	}
	out := []PaletteSection{{Title: strings.ToUpper(verb[:1]) + verb[1:], Rows: rows}}
	// Did you mean: the whole-stack form of sleep and wake.
	if verb == "sleep" || verb == "wake" {
		var meant []PaletteRow
		for _, st := range stacks {
			if !st.Running {
				continue
			}
			if verb == "sleep" {
				if n := awakeCount(st); n > 0 {
					meant = append(meant, sleepAllRow(st, fmt.Sprintf("%d awake", n)))
				}
			} else {
				meant = append(meant, PaletteRow{Glyph: "↑", Tone: "dim", Label: "Wake everything in " + st.Name, Note: "every service, with what it needs", Key: "wake all", CLI: "doze wake", Do: Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbWake}})
			}
		}
		if len(meant) > 0 {
			out = append(out, PaletteSection{Title: "Did you mean", Rows: meant})
		}
	}
	return out
}

// verbRow is one instance under a verb, when the verb applies to it.
func verbRow(st hub.StackView, snap control.Snapshot, v control.InstanceView, verb string, now time.Time) (PaletteRow, bool) {
	n := v.Name
	row := instanceRow(st, snap, v, now, "")
	awake := display.AwakeNow(v)
	switch verb {
	case "sleep":
		if v.Kind == "task" || !awake {
			return row, false
		}
		row.Do = Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbSleep, Target: n}
		row.Key, row.CLI = "⏎ sleep now", "doze sleep "+n
		if DialogOf(st, n, control.VerbSleep, now).Needed {
			row.Do.Ask, row.Key = true, "⏎ asks first"
		}
	case "wake":
		if v.Kind == "task" || awake {
			return row, false
		}
		row.Do = Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbWake, Target: n}
		row.Key, row.CLI = "⏎ wake now", "doze wake "+n
	case "restart":
		if v.Kind == "task" {
			return row, false
		}
		row.Do = Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbRestart, Target: n}
		row.Key, row.CLI = "⏎ restart", "doze restart "+n
		if DialogOf(st, n, control.VerbRestart, now).Needed {
			row.Do.Ask, row.Key = true, "⏎ asks first"
		}
	case "pin", "unpin":
		if v.Kind == "task" || (verb == "pin") == v.KeepAwake {
			return row, false
		}
		row.Do = Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbPin, Target: n}
		if verb == "pin" {
			row.Key, row.CLI = "⏎ pin awake", "doze pin "+n
		} else {
			row.Key, row.CLI = "⏎ unpin", "doze pin --off "+n
		}
	case "reset":
		if v.Kind != "module" {
			return row, false
		}
		row.Do = Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbReset, Target: n, Ask: true}
		row.Key, row.CLI, row.Bad = "⏎ asks first · wipes its data", "doze reset "+n, true
	case "off":
		if v.Kind == "task" || v.Disabled {
			return row, false
		}
		row.Do = Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbOff, Target: n}
		row.Key, row.CLI = "⏎ off", "doze off "+n
		if display.InUse(v) != "" {
			// Something uses it, and nothing will wake it: ask, as sleep does.
			row.Do.Ask, row.Key = true, "⏎ asks first"
		}
	case "on":
		if v.Kind == "task" || !v.Disabled {
			return row, false
		}
		row.Do = Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbOn, Target: n}
		row.Key, row.CLI = "⏎ on", "doze on "+n
	case "logs":
		row.Do = Doing{Kind: "page", StackKey: st.Key, Page: "logs", Target: n}
		row.Key, row.CLI = "⏎ logs", "doze logs "+n
	case "copy":
		line := firstOf(v.HTTPS, v.URL, v.Endpoint)
		if line == "" || v.Kind == "task" {
			return row, false
		}
		row.Do = Doing{Kind: "copy", StackKey: st.Key, Text: line, Target: n}
		row.Key, row.CLI = "⏎ copy "+line, "doze url "+n
	case "open":
		url := firstOf(v.HTTPS, webURL(v))
		if url == "" {
			return row, false
		}
		row.Do = Doing{Kind: "open", StackKey: st.Key, Text: url, Target: n}
		row.Key, row.CLI = "⏎ open "+url, "open "+url
	case "run":
		if v.Kind != "task" {
			return row, false
		}
		row.Do = Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbTask, Target: n}
		row.Key, row.CLI = "⏎ run now", "doze task "+n
	default:
		return row, false
	}
	return row, true
}

// instanceRow is an instance as a palette line: its dot, name, where it
// is and how it is, with ⏎ opening its page and its actions on tab.
func instanceRow(st hub.StackView, snap control.Snapshot, v control.InstanceView, now time.Time, key string) PaletteRow {
	look := display.LookOf(v)
	row := PaletteRow{Glyph: "dot", Tone: dotOf(look), Label: v.Name, Key: key, Do: Doing{Kind: "page", StackKey: st.Key, Page: "instance", Target: v.Name}}
	if v.Kind == "task" {
		row.Glyph, row.Tone, row.Label = "▸", "acc", "Run "+v.Name
		row.Do = Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbTask, Target: v.Name}
	}
	parts := []string{"in " + st.Name}
	if v.Kind == "task" {
		if v.Task != nil {
			parts = append(parts, "last run "+display.Ago(now.Sub(v.Task.RanAt))+" ago")
		} else {
			parts = append(parts, "never run")
		}
	} else {
		state := look.Word
		if display.AwakeNow(v) && v.RAM > 0 {
			state += ", " + display.MB(v.RAM)
		}
		parts = append(parts, state)
		if b := display.Because(v, snap, now); b != "" {
			parts = append(parts, b)
		}
	}
	row.Note = strings.Join(parts, " · ")
	row.Acts = actsOf(st, v, now)
	return row
}

// recentRow is a recent service: the same line, with its actions named
// after the stack, as the board writes "copy URL, psql, sleep".
func recentRow(st hub.StackView, snap control.Snapshot, v control.InstanceView, now time.Time) PaletteRow {
	row := instanceRow(st, snap, v, now, "⏎")
	switch {
	case v.Kind == "task":
	case v.HTTPS != "":
		row.Note = "in " + st.Name + " · " + v.HTTPS
	default:
		var names []string
		for _, a := range row.Acts {
			names = append(names, a.Label)
			if len(names) == 3 {
				break
			}
		}
		if len(names) > 0 {
			row.Note = "in " + st.Name + " · " + strings.Join(names, ", ")
		}
	}
	return row
}

// actsOf is what a row can do, first the most likely.
func actsOf(st hub.StackView, v control.InstanceView, now time.Time) []Act {
	n := v.Name
	var acts []Act
	if v.Kind == "task" {
		acts = append(acts, Act{Label: "run", Do: Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbTask, Target: n}})
		acts = append(acts, Act{Label: "logs", Do: Doing{Kind: "page", StackKey: st.Key, Page: "logs", Target: n}})
		return acts
	}
	if line := firstOf(v.HTTPS, v.URL, v.Endpoint); line != "" {
		acts = append(acts, Act{Label: "copy URL", Do: Doing{Kind: "copy", StackKey: st.Key, Text: line, Target: n}})
	}
	if url := firstOf(v.HTTPS, webURL(v)); url != "" {
		acts = append(acts, Act{Label: "open", Do: Doing{Kind: "open", StackKey: st.Key, Text: url, Target: n}})
	}
	if display.AwakeNow(v) {
		acts = append(acts, Act{Label: "sleep", Do: Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbSleep, Target: n, Ask: DialogOf(st, n, control.VerbSleep, now).Needed}})
		acts = append(acts, Act{Label: "restart", Do: Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbRestart, Target: n, Ask: DialogOf(st, n, control.VerbRestart, now).Needed}})
	} else if !v.Disabled {
		acts = append(acts, Act{Label: "wake", Do: Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbWake, Target: n}})
	}
	acts = append(acts, Act{Label: "logs", Do: Doing{Kind: "page", StackKey: st.Key, Page: "logs", Target: n}})
	return acts
}

func sleepAllRow(st hub.StackView, note string) PaletteRow {
	return PaletteRow{Glyph: "↓", Tone: "dim", Label: "Sleep everything in " + st.Name, Note: note, Key: "sleep all", CLI: "doze sleep", Do: Doing{Kind: "verb", StackKey: st.Key, Verb: control.VerbSleep}}
}

func upRow(st hub.StackView) PaletteRow {
	note := "stopped · as last time: everything"
	if len(st.LastSet) > 0 {
		note = "stopped · as last time: " + strings.Join(st.LastSet, ", ")
	}
	return PaletteRow{Glyph: "↑", Tone: "dim", Label: "Up " + st.Name, Note: note, Key: "up " + st.Name, CLI: "doze up --last", Do: Doing{Kind: "stack", StackKey: st.Key, Verb: "up-last"}}
}

// nothing is the no-match line: where it looked, and the closest name by
// edit distance over services (and their engines) and stacks.
func nothing(stacks []hub.StackView, verb, name string) *PaletteNothing {
	var wheres []string
	for _, st := range stacks {
		wheres = append(wheres, st.Name)
	}
	out := &PaletteNothing{Name: name, Where: orList(wheres)}
	best, bestD := "", 1<<30
	var bestSt hub.StackView
	var bestV control.InstanceView
	for _, st := range stacks {
		for _, v := range st.Instances {
			for _, cand := range []string{v.Name, v.Engine} {
				if cand == "" {
					continue
				}
				if d := editDistance(strings.ToLower(name), strings.ToLower(cand)); d < bestD {
					best, bestD, bestSt, bestV = v.Name, d, st, v
				}
			}
		}
	}
	if best == "" || bestD > max(3, len(name)/2) {
		return out
	}
	out.Closest = best
	why := "in " + bestSt.Name
	if bestV.Engine != "" && !strings.EqualFold(bestV.Engine, best) {
		why = bestV.Engine + ", in " + bestSt.Name
	}
	out.Why = why
	if verb != "" {
		snap := snapshot(bestSt)
		if _, ok := verbRow(bestSt, snap, bestV, verb, time.Now()); ok {
			out.Key = "⏎ use " + best
			out.Fill = verb + " " + best
			return out
		}
	}
	out.Key = "⏎ open " + best
	do := Doing{Kind: "page", StackKey: bestSt.Key, Page: "instance", Target: best}
	out.Do = &do
	return out
}

func isVerb(w string) bool {
	for _, v := range verbs {
		if v == w {
			return true
		}
	}
	return false
}

func awakeCount(st hub.StackView) int {
	n := 0
	for _, v := range st.Instances {
		if v.Kind != "task" && display.AwakeNow(v) {
			n++
		}
	}
	return n
}

func stackFigure(st hub.StackView) string {
	if !st.Running {
		return "stopped"
	}
	return fmt.Sprintf("%d awake", awakeCount(st))
}

func dotOf(look display.Look) string {
	switch look.Tone {
	case display.ToneGood:
		return "g"
	case display.ToneWarn:
		return "y"
	case display.ToneBad:
		return "r"
	case display.ToneCool:
		return "b"
	}
	return ""
}

func webURL(v control.InstanceView) string {
	if v.Console != "" {
		return v.Console
	}
	if v.Kind == "app" && v.URL != "" && strings.HasPrefix(v.URL, "http") {
		return v.URL
	}
	return ""
}

func orList(names []string) string {
	switch len(names) {
	case 0:
		return "any stack"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// editDistance is the Levenshtein distance between two strings.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func firstOf(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
