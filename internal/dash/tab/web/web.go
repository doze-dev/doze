// Package web is the tab of an instance whose console is a web page: it says
// where the page is and opens it. The address is named and proxied like any
// other, so opening it wakes the service.
package web

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/core"
)

// Tab is the web tab of one instance.
type Tab struct{ name string }

// New returns the web tab.
func New(name string) *Tab { return &Tab{name: name} }

// Title implements core.Tab.
func (t *Tab) Title() string { return "Web" }

// Show implements core.Tab.
func (t *Tab) Show(*core.Context, control.InstanceView) tea.Cmd { return nil }

// Hide implements core.Tab.
func (t *Tab) Hide() tea.Cmd { return nil }

// Keys implements core.Tab.
func (t *Tab) Keys() []core.Key {
	return []core.Key{{Keys: "o", Label: "open in browser"}, {Keys: "c", Label: "copy address"}}
}

// Update implements core.Tab.
func (t *Tab) Update(msg tea.Msg, _ *core.Context, inst control.InstanceView) (core.Tab, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return t, nil
	}
	url := Address(inst)
	switch key.String() {
	case "o", "enter":
		if url != "" {
			return t, func() tea.Msg { return core.Open{URL: url} }
		}
	case "c":
		if url != "" {
			return t, tea.Batch(tea.SetClipboard(url), func() tea.Msg { return core.Notice{Text: "copied " + url} })
		}
	}
	return t, nil
}

// Address is the page's address: what the config said (console = …), else what
// the module asked for.
func Address(inst control.InstanceView) string {
	if inst.Console != "" {
		return inst.Console
	}
	return inst.WebURL
}

// View implements core.Tab.
func (t *Tab) View(c *core.Context, inst control.InstanceView, w, h int) string {
	st := c.Styles
	out := []string{st.Dim.Render("address")}
	if url := Address(inst); url != "" {
		out = append(out, "  "+st.Accent.Render(url), "")
		if inst.State != "active" && inst.State != "idle" {
			out = append(out, st.Dim.Render("asleep — opening it wakes it."))
		}
		out = append(out, st.Dim.Render("press ")+st.Key.Render("o")+st.Dim.Render(" to open it in your browser"))
	} else {
		out = append(out, "  "+st.Dim.Render("this instance has no web console"))
	}
	for len(out) < h {
		out = append(out, "")
	}
	return strings.Join(out[:h], "\n")
}
