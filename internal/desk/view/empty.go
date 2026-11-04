package view

import (
	"strings"

	"github.com/doze-dev/doze/internal/display"
)

// Empty is the page of a folder with no doze.hcl yet (the NewTab board):
// what doze found there, and the three ways to start, in the words bare
// doze uses in an empty folder.
type Empty struct {
	Dir   string `json:"dir"`
	Name  string `json:"name"`
	Title string `json:"title"`
	// Found is what the project says: "found 2 apps, 1 module · nothing was
	// started", or "" when nothing was recognised.
	Found   string   `json:"found,omitempty"`
	Choices []Choice `json:"choices"`
	Note    string   `json:"note"`
	// Add is the dialog "Add one thing" opens: the name to add.
	Add Dialog `json:"add"`
}

// Choice is one way to start.
type Choice struct {
	N      int    `json:"n"`
	Action string `json:"action"` // init, add, empty
	Title  string `json:"title"`
	What   string `json:"what"`
}

// EmptyOf is the page for a folder, given what the detector found there
// ("2 apps", "1 module", …).
func EmptyOf(dir string, found []string) Empty {
	name := dir
	if i := strings.LastIndexAny(dir, "/\\"); i >= 0 && i < len(dir)-1 {
		name = dir[i+1:]
	}
	out := Empty{
		Dir:   dir,
		Name:  name,
		Title: "No doze.hcl in " + display.Tilde(dir) + " yet.",
		Note:  "The same three choices as a bare doze in an empty folder.",
	}
	if len(found) > 0 {
		out.Found = "found " + strings.Join(found, ", ") + " · nothing was started"
	}
	actions := [3]string{"init", "add", "empty"}
	for i, c := range display.EmptyChoices {
		what := c.What
		if i == 0 && len(found) == 0 {
			what = display.EmptyNothingFound
		}
		out.Choices = append(out.Choices, Choice{N: i + 1, Action: actions[i], Title: c.Title, What: what})
	}
	out.Add = Dialog{
		Kind: "setup", Needed: true, Title: "Add one thing", Setup: dir, Input: true, Hint: "postgres, valkey, mongo, …",
		Sentence: []Span{{Text: "name a module or a service; "}, {Text: "doze add", Mono: true, Strong: true}, {Text: " writes doze.hcl with it"}},
		Buttons:  []Button{{Label: "Add", Verb: "add", Primary: true}, {Label: "Cancel", Cancel: true}},
	}
	return out
}
