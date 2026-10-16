package dash

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The screen of a folder with no doze.hcl yet (boards/Empty.dc.html): what
// doze found there, and three ways to start. It chooses and returns the choice;
// the command that does the work is the CLI's, so this stays a menu.

// Start is what the person picked on the empty screen.
type Start struct {
	// Action is "init" (write a doze.hcl from the project), "add" (one thing,
	// named in Arg), "empty" (a starter file), or "" when they left.
	Action string
	Arg    string
}

// EmptyOptions describe the folder.
type EmptyOptions struct {
	Dir   string   // shown with ~ for the home
	Found []string // what the project says: "app api", "module postgres", …
	Plain bool
	Theme string
}

type emptyModel struct {
	o      EmptyOptions
	st     styles
	sel    int
	typing bool   // naming the thing to add
	name   string // what has been typed
	pick   Start
	w, h   int
}

var emptyChoices = [3]struct{ title, what string }{
	{"Set it up from this repo", "reads go.mod, package.json, compose files and writes a doze.hcl"},
	{"Add one thing", "name a module or a service: postgres, valkey, mongo, …"},
	{"Start with an empty file", "writes a short doze.hcl, with comments, to fill in"},
}

// Empty runs the screen and returns what was chosen.
func Empty(o EmptyOptions) (Start, error) {
	m := &emptyModel{o: o, st: newThemed(o.Plain, themeNamed(o.Theme))}
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return Start{}, fmt.Errorf("the dash: %w", err)
	}
	return m.pick, nil
}

func (m *emptyModel) Init() tea.Cmd { return nil }

func (m *emptyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return m.key(msg.String(), msg.Text)
	}
	return m, nil
}

func (m *emptyModel) key(k, text string) (tea.Model, tea.Cmd) {
	if m.typing {
		switch k {
		case "esc":
			m.typing, m.name = false, ""
		case "enter":
			if n := strings.TrimSpace(m.name); n != "" {
				m.pick = Start{Action: "add", Arg: n}
				return m, tea.Quit
			}
		case "backspace":
			if r := []rune(m.name); len(r) > 0 {
				m.name = string(r[:len(r)-1])
			}
		case "ctrl+c":
			return m, tea.Quit
		default:
			if text != "" {
				m.name += text
			}
		}
		return m, nil
	}
	switch k {
	case "up", "k":
		m.sel = (m.sel + len(emptyChoices) - 1) % len(emptyChoices)
	case "down", "j", "tab":
		m.sel = (m.sel + 1) % len(emptyChoices)
	case "1", "2", "3":
		m.sel = int(k[0] - '1')
		return m.key("enter", "")
	case "enter":
		switch m.sel {
		case 0:
			m.pick = Start{Action: "init"}
			return m, tea.Quit
		case 1:
			m.typing = true
		case 2:
			m.pick = Start{Action: "empty"}
			return m, tea.Quit
		}
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m *emptyModel) View() tea.View {
	v := tea.NewView(m.render(max(m.w, 60), max(m.h, 16)))
	v.AltScreen = true
	if !m.st.plain {
		v.BackgroundColor = lipglossColor(m.st.pal.bg)
		v.ForegroundColor = lipglossColor(m.st.pal.text)
	}
	return v
}

func (m *emptyModel) render(w, h int) string {
	s := m.st
	bw := min(w-4, 100)
	var out []string
	out = append(out, "")
	out = append(out, " "+s.a.Bold(true).Render("doze")+"  "+s.d.Render("z z z"))
	out = append(out, " "+s.text.Render("No doze.hcl in ")+s.w.Render(m.o.Dir)+s.text.Render(" yet."))
	if len(m.o.Found) > 0 {
		out = append(out, " "+s.d.Render("found "+strings.Join(m.o.Found, ", ")))
	}
	out = append(out, "")
	var lines []string
	for i, c := range emptyChoices {
		mark, title := "  ", s.w.Render(c.title)
		if i == m.sel {
			mark = s.a.Render("▸ ")
			title = s.b.Render(c.title)
		}
		row := mark + title + strings.Repeat(" ", max(30-width(c.title), 1)) + s.d.Render(fit(c.what, bw-4-34))
		if i == 0 && len(m.o.Found) == 0 {
			row = mark + title + strings.Repeat(" ", max(30-width(c.title), 1)) + s.d.Render(fit("nothing recognised here; it asks instead", bw-4-34))
		}
		if i == 1 && m.typing {
			row = s.a.Render("▸ ") + s.b.Render("Add") + "  " + s.w.Render(m.name) + s.a.Render("▏")
		}
		lines = append(lines, row)
	}
	out = append(out, strings.Split(strings.Join(box(s.borderOn, s.a.Bold(true).Render("start here"), "", lines, bw, len(lines)+2), "\n"), "\n")...)
	out = append(out, "")
	var keys string
	if m.typing {
		keys = s.cap("⏎", "add it") + "  " + s.cap("esc", "back")
	} else {
		keys = s.cap("↑↓", "move") + "  " + s.cap("⏎", "choose") + "  " + s.cap("1-3", "pick") + "  " + s.cap("q", "quit")
	}
	for len(out) < h-1 {
		out = append(out, "")
	}
	out = append(out[:h-1], " "+keys)
	for i, l := range out {
		if width(l) > w {
			out[i] = ansi.Truncate(l, w, "…")
		}
	}
	return strings.Join(out, "\n")
}
