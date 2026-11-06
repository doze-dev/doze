package view

import (
	"fmt"
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
	// Input asks for a line of text, with Hint in the box: a module action
	// that takes input. Action says the buttons run a module action (their
	// Verb is its id, their first Target the row) on Instance.
	Input    bool   `json:"input,omitempty"`
	Hint     string `json:"hint,omitempty"`
	Action   bool   `json:"action,omitempty"`
	Instance string `json:"instance,omitempty"`
	// Stack says the buttons run a stack action (down) on the stack, not a
	// verb on its services.
	Stack bool `json:"stack,omitempty"`
	// Setup is the folder the buttons set up (Verb is the way: init, add,
	// empty; the input is the name to add).
	Setup string `json:"setup,omitempty"`
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
	if name == "" && verb == "down" {
		return stopDialog(st)
	}
	if name == "" && (verb == "forget" || verb == "remove") {
		return forgetDialog(st)
	}
	snap := snapshot(st)
	v, ok := display.Find(snap, name)
	if !ok {
		return Dialog{Kind: verb}
	}
	switch verb {
	case control.VerbOff:
		return offDialog(v)
	case control.VerbSleep:
		return sleepDialog(v, snap)
	case control.VerbRestart:
		return restartDialog(v)
	case control.VerbReset:
		return resetDialog(v)
	}
	_ = now
	return Dialog{Kind: verb}
}

func sleepDialog(v control.InstanceView, snap control.Snapshot) Dialog {
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
	// What uses it, less what is pinned awake: a pin is the person's word
	// that it stays up, so the default never sleeps it unasked.
	var deps, pinned []string
	for _, n := range display.Dependants(v) {
		if o, ok := display.Find(snap, n); ok && o.KeepAwake {
			pinned = append(pinned, n)
			continue
		}
		deps = append(deps, n)
	}
	if len(pinned) > 0 {
		d.Rows = append(d.Rows, KV{K: "Pinned", V: andWords(pinned) + " stays up, pinned awake, and wakes " + v.Name + " again when it connects. Turn " + v.Name + " off to keep it down."})
	}
	if len(deps) > 0 {
		// Dependants first, then the target, as the dash's sleepKey did.
		d.Buttons = append(d.Buttons, Button{Label: "Sleep " + v.Name + " and " + strings.Join(deps, ", "), Verb: control.VerbSleep, Targets: append(append([]string{}, deps...), v.Name), Primary: true})
	}
	d.Buttons = append(d.Buttons,
		Button{Label: "Sleep " + v.Name + " only", Verb: control.VerbSleep, Targets: []string{v.Name}, Primary: len(deps) == 0},
		// Off keeps it down whatever connects, until it is switched on.
		Button{Label: "Turn " + v.Name + " off", Verb: control.VerbOff, Targets: []string{v.Name}},
		Button{Label: "Cancel", Cancel: true},
	)
	d.Foot = []string{"doze only asks when something else is affected. Sleeping an idle service never asks."}
	return d
}

// andWords joins names as a sentence does.
func andWords(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func restartDialog(v control.InstanceView) Dialog {
	d := Dialog{Kind: "restart", Title: "Restart " + v.Name + "?"}
	if display.InUse(v) == "" || !display.AwakeNow(v) || v.Kind == "task" {
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

// stopDialog is the question before a whole stack goes down: asked only
// when something in it is in use, as sleeping one service is.
func stopDialog(st hub.StackView) Dialog {
	name := st.Name
	if name == "" {
		name = "the stack"
	}
	d := Dialog{Kind: "down", Title: "Stop " + name + "?", Stack: true}
	t := display.TallyOf(st.Instances)
	if t.Conns == 0 {
		return d
	}
	d.Needed = true
	d.Sentence = []Span{
		{Text: fmt.Sprintf("%d client connection%s", t.Conns, display.Plural(t.Conns)), Strong: true},
		{Text: " are open to it, and "},
		{Text: fmt.Sprintf("%d service%s", t.Awake+t.Dozing+t.Waking, display.Plural(t.Awake+t.Dozing+t.Waking)), Strong: true},
		{Text: " are up."},
	}
	d.Rows = []KV{
		{K: "What happens", V: "Every service stops and every connection drops. Nothing wakes until the stack is up again; Up as last time brings back this set."},
		{K: "Data", V: "Kept. Nothing is deleted."},
	}
	d.Buttons = []Button{{Label: "Stop " + name, Verb: "down", Primary: true}, {Label: "Cancel", Cancel: true}}
	d.Foot = []string{"From a script: doze down"}
	return d
}

// forgetDialog is the question before a stack is removed from doze, running
// or not: its services stop, its files stay. Always asked: a stack vanishing
// from a key press should not happen unseen.
func forgetDialog(st hub.StackView) Dialog {
	name := st.Name
	if name == "" {
		name = "this stack"
	}
	what := "doze stops listing it: the Library, its tab and the menu bar."
	if st.Running {
		what = "Its services stop, then doze stops listing it: the Library, its tab and the menu bar."
	}
	return Dialog{
		Kind: "remove", Title: "Remove " + name + " from doze?", Stack: true, Needed: true,
		Rows: []KV{
			{K: "What happens", V: what},
			{K: "Files", V: "Kept: the folder, its doze.hcl and its data stay on disk. Open the folder again (⌘O, or drop it on the window) to bring it back, data and all."},
		},
		Buttons: []Button{{Label: "Remove " + name, Verb: "remove", Primary: true}, {Label: "Cancel", Cancel: true}},
		Default: 1,
	}
}

// offDialog asks before a service in use is turned off: unlike sleep, a
// connection will not bring it back, so what uses it fails until it is
// switched on again.
func offDialog(v control.InstanceView) Dialog {
	d := Dialog{Kind: "off", Title: "Turn " + v.Name + " off?"}
	if display.InUse(v) == "" {
		return d
	}
	d.Needed = true
	d.Sentence = spans(display.HeldSentence(v))
	d.Rows = []KV{
		{K: "What happens", V: "It stops, and nothing wakes it: what uses it fails to connect until you switch it on."},
		{K: "Data", V: "Kept. Nothing is deleted."},
	}
	d.Buttons = []Button{
		{Label: "Turn " + v.Name + " off", Verb: control.VerbOff, Targets: []string{v.Name}},
		{Label: "Sleep it instead", Verb: control.VerbSleep, Targets: []string{v.Name}, Primary: true},
		{Label: "Cancel", Cancel: true},
	}
	d.Default = 1
	d.Foot = []string{"From a script: doze off " + v.Name}
	return d
}
