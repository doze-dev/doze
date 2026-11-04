package display

import (
	"fmt"
	"strings"

	"github.com/doze-dev/doze/internal/control"
)

// The words of the confirmations (Confirm-Sleep, Confirm-Reset and the
// restart question): who is using the instance, what the buttons offer, what
// a reset deletes. A surface draws them in its own boxes; the sentences and
// the order of the buttons are the same everywhere.

// Dependants are the instances holding v awake because they need it.
func Dependants(v control.InstanceView) []string {
	var out []string
	for _, h := range v.Holders {
		if h.Kind == "dependant" {
			out = append(out, h.Name)
		}
	}
	return out
}

// Connections is how many connections are open to v right now.
func Connections(v control.InstanceView) int {
	for _, h := range v.Holders {
		if h.Kind == "connection" {
			return h.Count
		}
	}
	return 0
}

// SleepButtons are the choices of Confirm-Sleep, in order, the first being the
// default: with what depends on it, or without. The last is always Cancel.
func SleepButtons(v control.InstanceView) []string {
	if deps := Dependants(v); len(deps) > 0 {
		return []string{"Sleep " + v.Name + " and " + strings.Join(deps, ", "), "Sleep " + v.Name + " only", "Cancel"}
	}
	return []string{"Sleep " + v.Name, "Cancel"}
}

// RestartButtons are the choices of the restart question, Restart first.
func RestartButtons(v control.InstanceView) []string {
	return []string{"Restart " + v.Name, "Cancel"}
}

// Span is a run of a sentence, and whether it is the part to draw bright: a
// name or a figure the sentence is about.
type Span struct {
	Text   string
	Strong bool
}

// HeldSentence says who is using v, for the sleep and restart questions:
// "api is running and needs it, and 3 connections are open to it right now."
// The names and the count are strong.
func HeldSentence(v control.InstanceView) []Span {
	deps, conns := Dependants(v), Connections(v)
	count := fmt.Sprintf("%d connection%s", conns, Plural(conns))
	switch {
	case len(deps) > 0 && conns > 0:
		return []Span{{strings.Join(deps, ", "), true}, {" is running and needs it, and ", false}, {count, true}, {" are open to it right now.", false}}
	case len(deps) > 0:
		return []Span{{strings.Join(deps, ", "), true}, {" is running and needs it.", false}}
	}
	return []Span{{count, true}, {" open to it right now.", false}}
}

// Fact is one row of a confirmation: a key down the left and what it says,
// in the tone it is drawn in.
type Fact struct {
	Key, Text string
	Tone      Tone
}

// ResetFacts are the rows of Confirm-Reset: what it deletes, what it stops,
// what happens then, and that there is no way back.
func ResetFacts(v control.InstanceView) []Fact {
	folder := Tilde(v.DataDir)
	if folder == "" {
		folder = "its data folder"
	}
	return []Fact{
		{"deletes", "everything in " + folder, ToneBright},
		{"stops", v.Name + " if it is awake", ToneText},
		{"then", v.Name + " starts empty on its next wake, and its setup runs again", ToneText},
		{"way back", "none. There is no snapshot.", ToneWarn},
	}
}

// ResetCommand is how a script resets an instance without being asked.
func ResetCommand(name string) string { return "doze reset " + name + " --yes" }
