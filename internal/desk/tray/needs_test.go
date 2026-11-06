package tray

import (
	"testing"

	"github.com/doze-dev/doze/internal/desk/view"
)

func TestNeedsOf(t *testing.T) {
	red := func(id string) view.Item {
		return view.Item{ID: id, Red: true, Title: id + " keeps crashing", Stack: "shop", Fixes: []view.Fix{{Label: "Logs", Kind: "show"}, {Label: "Restart", Kind: "do", Verb: "restart"}}}
	}
	for _, c := range []struct {
		name   string
		a      view.Attention
		header string
		lines  int
		worth  string
	}{
		{"nothing", view.Attention{}, "", 0, ""},
		{"yellow only", view.Attention{Yellow: 1, Items: []view.Item{{ID: "y"}}}, "", 0, "1 thing worth doing   ·   Open"},
		{"red and yellow", view.Attention{Red: 1, Yellow: 2, Items: []view.Item{red("api"), {ID: "y"}}}, "Needs you · 1", 1, "and 2 things worth doing   ·   Open"},
		{"four red, three shown", view.Attention{Red: 4, Items: []view.Item{red("a"), red("b"), red("c"), red("d")}}, "Needs you · 4", 3, ""},
		{"fixed is not listed", view.Attention{Items: []view.Item{{ID: "f", Red: true, Fixed: true}}}, "", 0, ""},
	} {
		n := NeedsOf(c.a)
		if n.Header != c.header || len(n.Lines) != c.lines || n.Worth != c.worth {
			t.Errorf("%s: got %q, %d lines, %q", c.name, n.Header, len(n.Lines), n.Worth)
		}
		if c.lines > 0 {
			l := n.Lines[0]
			if l.Fix == nil || l.Fix.Label != "Restart" || l.Title != l.Item.Title+"   ·   shop" {
				t.Errorf("%s: first line = %+v", c.name, l)
			}
		}
	}
}
