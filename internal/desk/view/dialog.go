package view

import (
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Dialog is a confirmation, as the Confirm, Dialogs and Reset boards lay
// them out and in the dash's words: the question, who is using it, what
// happens, the buttons in the dash's order, the footer. Needed is false
// when nothing asks: sleeping or restarting an idle service never does.
type Dialog struct {
	Kind   string `json:"kind"` // "sleep", "restart", "reset", "ask"
	Needed bool   `json:"needed"`
	Title  string `json:"title"`
	Danger bool   `json:"danger"`
	// Sentence is who is using it, with the names and figures strong.
	Sentence []Span `json:"sentence,omitempty"`
	// Rows are "what happens" and "data" on a sleep or restart.
	Rows []KV `json:"rows,omitempty"`
	// Facts are a reset's rows: deletes, stops, then, way back.
	Facts []Fact `json:"facts,omitempty"`
	// TypeName is the name a reset asks to be typed; Command is how a
	// script does it without being asked.
	TypeName string `json:"typeName,omitempty"`
	Command  string `json:"command,omitempty"`
	// Buttons in the dash's order; Default is the index Enter presses.
	Buttons []Button `json:"buttons"`
	Default int      `json:"default"`
	Foot    []string `json:"foot,omitempty"`
}

// Span is a run of the sentence, strong when it is a name or a figure.
type Span struct {
	Text   string `json:"text"`
	Strong bool   `json:"strong,omitempty"`
	Mono   bool   `json:"mono,omitempty"`
}

// Fact is one row of a reset dialog.
type Fact struct {
	K     string `json:"k"`
	Spans []Span `json:"spans"`
	Tone  string `json:"tone"`
}

// Button is one choice: the verb it runs on which targets, in order, or a
// cancel with no verb.
type Button struct {
	Label   string   `json:"label"`
	Verb    string   `json:"verb,omitempty"`
	Targets []string `json:"targets,omitempty"`
	Primary bool     `json:"primary,omitempty"`
	Danger  bool     `json:"danger,omitempty"`
	Cancel  bool     `json:"cancel,omitempty"`
}

// DialogOf is the confirmation for a verb on an instance. For sleep and
// restart the dash asks only when something is using it; a reset always
// asks, and asks for the name.
func DialogOf(st hub.StackView, name, verb string, now time.Time) Dialog {
	snap := snapshot(st)
	v, ok := display.Find(snap, name)
	if !ok {
		return Dialog{Kind: verb}
	}
	switch verb {
	case control.VerbSleep:
		return sleepDialog(v)
	case control.VerbRestart:
		return restartDialog(v)
	case control.VerbReset:
		return resetDialog(v)
	}
	_ = now
	return Dialog{Kind: verb}
}

func sleepDialog(v control.InstanceView) Dialog {
	d := Dialog{Kind: "sleep", Title: "Sleep " + v.Name + "?"}
	if display.InUse(v) == "" {
		return d
	}
	d.Needed = true
	d.Sentence = spans(display.HeldSentence(v))
	d.Rows = []KV{
		{K: "What happens", V: "Connections drop. The next connection wakes it again, so it is back within seconds unless what uses it sleeps too."},
		{K: "Data", V: "Kept. Nothing is deleted."},
	}
	deps := display.Dependants(v)
	for i, label := range display.SleepButtons(v) {
		b := Button{Label: label, Primary: i == 0}
		switch {
		case label == "Cancel":
			b.Cancel = true
		case len(deps) > 0 && strings.Contains(label, " and "):
			// Dependants first, then the target, as the dash's sleepKey did.
			b.Verb, b.Targets = control.VerbSleep, append(append([]string{}, deps...), v.Name)
		default:
			b.Verb, b.Targets = control.VerbSleep, []string{v.Name}
		}
		d.Buttons = append(d.Buttons, b)
	}
	d.Foot = []string{"doze only asks when something else is affected. Sleeping an idle service never asks."}
	return d
}

func restartDialog(v control.InstanceView) Dialog {
	d := Dialog{Kind: "restart", Title: "Restart " + v.Name + "?"}
	if display.InUse(v) == "" || !display.Awake(v.State) || v.Kind == "task" {
		return d
	}
	d.Needed = true
	d.Sentence = spans(display.HeldSentence(v))
	d.Rows = []KV{
		{K: "What happens", V: "It stops and starts again. Every connection drops; clients that reconnect find it back in a few seconds, those that do not stay down."},
		{K: "Data", V: "Kept. Nothing is deleted."},
	}
	for i, label := range display.RestartButtons(v) {
		b := Button{Label: label, Primary: i == 0}
		if label == "Cancel" {
			b.Cancel = true
		} else {
			b.Verb, b.Targets = control.VerbRestart, []string{v.Name}
		}
		d.Buttons = append(d.Buttons, b)
	}
	d.Foot = []string{"doze only asks when something is using it. Restarting an idle one never asks."}
	return d
}

func resetDialog(v control.InstanceView) Dialog {
	d := Dialog{Kind: "reset", Needed: true, Danger: true, Title: "reset " + v.Name + " · this deletes data", TypeName: v.Name, Command: display.ResetCommand(v.Name)}
	for _, f := range display.ResetFacts(v) {
		d.Facts = append(d.Facts, resetFact(f))
	}
	d.Buttons = []Button{
		{Label: "Reset " + v.Name, Verb: control.VerbReset, Targets: []string{v.Name}, Danger: true},
		{Label: "Cancel", Cancel: true},
	}
	d.Default = 1 // Cancel is the default; Reset is disabled until the name is typed
	d.Foot = []string{
		"Typing the name is only asked when data is deleted. Cancel is the default button.",
		"From a script: " + d.Command,
	}
	return d
}

func spans(in []display.Span) []Span {
	out := make([]Span, 0, len(in))
	for _, s := range in {
		out = append(out, Span{Text: s.Text, Strong: s.Strong})
	}
	return out
}

// resetFact draws one of the dash's reset facts as the Reset board does:
// the folder in mono, "none." in bold, the rest plain.
func resetFact(f display.Fact) Fact {
	switch f.Key {
	case "deletes":
		if rest, ok := strings.CutPrefix(f.Text, "everything in "); ok {
			return Fact{K: f.Key, Tone: "text", Spans: []Span{{Text: "everything in "}, {Text: rest, Strong: true, Mono: true}}}
		}
	case "way back":
		if first, rest, ok := strings.Cut(f.Text, " "); ok {
			return Fact{K: f.Key, Tone: "text", Spans: []Span{{Text: first, Strong: true}, {Text: " " + rest}}}
		}
	}
	return Fact{K: f.Key, Tone: string(f.Tone), Spans: []Span{{Text: f.Text}}}
}
