package dash

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func emptyFor(found ...string) *emptyModel {
	return &emptyModel{o: EmptyOptions{Dir: "~/code/shop", Found: found, Plain: true}, st: newStyles(true), w: 100, h: 24}
}

// The empty screen says where it is, what it found, and the three ways in.
func TestEmptyScreenSaysWhatItFound(t *testing.T) {
	out := frame2(emptyFor("2 apps", "1 module").render(100, 24))
	for _, want := range []string{"No doze.hcl in ~/code/shop yet.", "found 2 apps, 1 module", "start here",
		"Set it up from this repo", "Add one thing", "Start with an empty file", "choose"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty screen lacks %q:\n%s", want, out)
		}
	}
	if out := frame2(emptyFor().render(100, 24)); !strings.Contains(out, "nothing recognised") {
		t.Errorf("with nothing found it should say so:\n%s", out)
	}
}

// Enter on the first choice sets the project up; on the second it asks for a
// name and then adds it; the third writes the starter file; q leaves.
func TestEmptyScreenChoices(t *testing.T) {
	press := func(m *emptyModel, k, text string) tea.Cmd {
		_, cmd := m.key(k, text)
		return cmd
	}
	m := emptyFor()
	if cmd := press(m, "enter", ""); cmd == nil || m.pick.Action != "init" {
		t.Errorf("first choice: pick=%+v", m.pick)
	}
	m = emptyFor()
	press(m, "down", "")
	press(m, "enter", "")
	if !m.typing {
		t.Fatal("the second choice should ask for a name")
	}
	for _, r := range "postgres" {
		press(m, string(r), string(r))
	}
	if cmd := press(m, "enter", ""); cmd == nil || m.pick != (Start{Action: "add", Arg: "postgres"}) {
		t.Errorf("add: pick=%+v", m.pick)
	}
	m = emptyFor()
	if press(m, "3", ""); m.pick.Action != "empty" {
		t.Errorf("third choice: pick=%+v", m.pick)
	}
	m = emptyFor()
	press(m, "q", "")
	if m.pick.Action != "" {
		t.Errorf("q chose %+v", m.pick)
	}
}

func frame2(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return strings.Join(out, "\n")
}
