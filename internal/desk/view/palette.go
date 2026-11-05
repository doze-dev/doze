package view

import (
	"sort"
	"strings"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Palette is the ⌘K list, as the Search board's Actions pane and the dash's
// palette lay it out: every action with the shell command beside it, the
// selected instance's first, then the stack's, then every other instance,
// awake first. Matching and ranking are the dash's (display.FuzzyMatch).
type Palette struct {
	Query string       `json:"query"`
	Rows  []PaletteRow `json:"rows"`
}

// PaletteRow is one action: its label with the matched runes marked, the
// command a script would run, a note, and what the page does when chosen.
type PaletteRow struct {
	Label string `json:"label"`
	// At are the rune positions of the label that matched the query.
	At   []int  `json:"at,omitempty"`
	CLI  string `json:"cli,omitempty"`
	Note string `json:"note,omitempty"`
	Bad  bool   `json:"bad,omitempty"`
	Do   Doing  `json:"do"`
}

// Doing is what choosing a row does.
type Doing struct {
	// Kind: "verb" runs Verb on Target (a dialog first when Ask); "page"
	// goes to Page (an instance's logs); "copy" copies Text; "recent" opens
	// recent messages; "theme" sets Text.
	Kind   string `json:"kind"`
	Verb   string `json:"verb,omitempty"`
	Target string `json:"target,omitempty"`
	Ask    bool   `json:"ask,omitempty"`
	Page   string `json:"page,omitempty"`
	Text   string `json:"text,omitempty"`
}

// PaletteOf lists and ranks the actions for a query. selected is the
// instance the page has picked, whose actions come first; theme is the
// theme in use.
func PaletteOf(st hub.StackView, selected, theme, query string) Palette {
	snap := snapshot(st)
	type ranked struct {
		row   PaletteRow
		rank  int
		score int
		order int
	}
	var all []ranked
	rank := 0
	seenVerb := map[string]bool{}
	add := func(r PaletteRow) {
		// With nothing typed the palette offers what is at hand: the selected
		// instance and the stack. Typing reaches every instance; and a couple
		// of letters match one verb on every instance, so the first of each
		// is enough until the person says which. Both as the dash's hits did.
		if query == "" && rank > 1 {
			return
		}
		if verb, _, _ := strings.Cut(r.Label, " "); rank == 2 && len(query) < 3 {
			if seenVerb[verb] {
				return
			}
			seenVerb[verb] = true
		}
		at, ok := display.FuzzyMatch(query, r.Label)
		if !ok {
			return
		}
		r.At = at
		all = append(all, ranked{row: r, rank: rank, score: display.FuzzyScore(at), order: len(all)})
	}
	forInstance := func(v control.InstanceView) {
		n := v.Name
		if v.Kind == "task" {
			verb := "run task "
			if v.Task != nil {
				verb = "rerun task "
			}
			add(PaletteRow{Label: verb + n, CLI: "doze task " + n, Do: Doing{Kind: "verb", Verb: control.VerbTask, Target: n}})
		} else {
			if !display.AwakeNow(v) {
				add(PaletteRow{Label: "wake " + n, CLI: "doze wake " + n, Do: Doing{Kind: "verb", Verb: control.VerbWake, Target: n}})
			} else {
				add(PaletteRow{Label: "sleep " + n, CLI: "doze sleep " + n, Do: Doing{Kind: "verb", Verb: control.VerbSleep, Target: n, Ask: true}})
				add(PaletteRow{Label: "restart " + n, CLI: "doze restart " + n, Do: Doing{Kind: "verb", Verb: control.VerbRestart, Target: n, Ask: true}})
			}
			pin, cli := "pin "+n+" awake", "doze pin "+n
			if v.KeepAwake {
				pin, cli = "unpin "+n, "doze pin --off "+n
			}
			add(PaletteRow{Label: pin, CLI: cli, Do: Doing{Kind: "verb", Verb: control.VerbPin, Target: n}})
			add(PaletteRow{Label: "reset " + n, Note: "wipes its data · asks first", Bad: true, CLI: "doze reset " + n, Do: Doing{Kind: "verb", Verb: control.VerbReset, Target: n, Ask: true}})
		}
		add(PaletteRow{Label: "show the logs of " + n, CLI: "doze logs " + n, Do: Doing{Kind: "page", Page: "logs", Target: n}})
		if line := firstOf(v.URL, v.Endpoint); line != "" && v.Kind != "task" {
			add(PaletteRow{Label: "copy the connect line of " + n, CLI: "doze url " + n, Do: Doing{Kind: "copy", Text: line, Target: n}})
		}
	}
	rows := display.Rows(snap)
	if v, ok := display.Find(snap, selected); ok {
		forInstance(v)
	}
	rank = 1
	add(PaletteRow{Label: "recent messages", Do: Doing{Kind: "recent"}})
	for _, t := range []struct{ name, about string }{{"dark", "the dash's violet palette"}, {"light", "for a bright room"}, {"system", "follows the system setting"}} {
		note := t.about
		if t.name == theme {
			note = "in use · " + t.about
		}
		add(PaletteRow{Label: "theme · " + t.name, Note: note, Do: Doing{Kind: "theme", Text: t.name}})
	}
	add(PaletteRow{Label: "wake everything", CLI: "doze wake", Do: Doing{Kind: "verb", Verb: control.VerbWake}})
	add(PaletteRow{Label: "sleep everything that is awake", CLI: "doze sleep", Do: Doing{Kind: "verb", Verb: control.VerbSleep}})
	add(PaletteRow{Label: "bring the stack up", CLI: "doze up", Do: Doing{Kind: "verb", Verb: control.VerbUp}})
	add(PaletteRow{Label: "apply the changes in doze.hcl", CLI: "doze sync", Do: Doing{Kind: "verb", Verb: control.VerbSync}})
	rank = 2
	for _, awakeFirst := range []bool{true, false} {
		for _, r := range rows {
			if r.V.Name != selected && display.AwakeNow(r.V) == awakeFirst {
				forInstance(r.V)
			}
		}
	}
	// With a query, the best match first; without, the dash's order.
	if query != "" {
		sort.SliceStable(all, func(i, j int) bool {
			if all[i].score != all[j].score {
				return all[i].score < all[j].score
			}
			return all[i].rank < all[j].rank
		})
	}
	out := Palette{Query: query, Rows: []PaletteRow{}}
	for _, r := range all {
		out.Rows = append(out.Rows, r.row)
	}
	return out
}

func firstOf(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
