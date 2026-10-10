package core

import (
	"charm.land/lipgloss/v2"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/theme"
)

// State is how an instance's state is shown: the word a person reads, the
// glyph beside it, and the colour of both. A task that has run shows how that
// went; everything else shows the daemon's state.
func State(v control.InstanceView, st theme.Styles) (word, glyph string, style lipgloss.Style) {
	switch {
	case v.Disabled:
		return "disabled", "⊘", st.Dim
	case v.Task != nil && v.State != "booting":
		if v.Task.Result == "failed" {
			return "failed", "✕", st.Bad
		}
		return "done", "✓", st.Good
	}
	switch v.State {
	case "active":
		return "awake", "●", st.Good
	case "idle":
		return "dozing", "○", st.Warn
	case "booting":
		return "waking", "◌", st.Cool
	case "crashing":
		return "crashing", "↻", st.Bad
	case "failed", "error":
		return "failed", "✕", st.Bad
	case "done":
		return "done", "✓", st.Good
	}
	return "asleep", "·", st.Dim
}
