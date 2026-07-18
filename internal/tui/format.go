// Formatting helpers: text truncation and wrapping, byte/duration formatting,
// and the state glyph/label vocabulary shared across every screen.

package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
)

// truncate fits s into w display columns, ellipsized. It cuts by display width
// (ANSI- and wide-character-aware), so CJK/emoji content can't overflow a column.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// wrapWidth splits a plain string into chunks of at most w display columns
// (wide-character-aware). Always returns at least one chunk.
func wrapWidth(s string, w int) []string {
	if w <= 0 || lipgloss.Width(s) <= w {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	curW := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if curW+rw > w && cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curW = 0
		}
		cur.WriteRune(r)
		curW += rw
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// abbrevHome shortens a path under the user's home dir to a leading ~.
func abbrevHome(p string) string {
	if p == "" {
		return "—"
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func compactDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d >= time.Minute {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// plural is "1 line" / "3 lines" — copy feedback reads like a sentence.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// ── helpers ───────────────────────────────────────────────────────────────

// displayState promotes a reaped instance carrying an error to "error", and a
// running-but-tainted instance (last converge failed) to "tainted" so it never
// reads as healthy.
func displayState(in control.InstanceView) string {
	if in.LastError != "" && (in.State == "reaped" || in.State == "") {
		return "error"
	}
	if in.Tainted {
		return "tainted"
	}
	if in.State == "" {
		return "reaped"
	}
	return in.State
}

// displayLabel is the user-facing name for a state. "reaped" and "booting" are
// doze's internal terms; users see the sleep metaphor everywhere, so the badge
// says ASLEEP and WAKING, never REAPED or BOOTING.
func displayLabel(state string) string {
	switch state {
	case "reaped":
		return "ASLEEP"
	case "booting":
		return "WAKING"
	}
	return strings.ToUpper(state)
}

// stateGlyph is the one-character mark for a display state (matches the
// sidebar's vocabulary).
func stateGlyph(st string) string {
	switch st {
	case "active":
		return "●"
	case "idle":
		return "○"
	case "booting":
		return "⠿"
	case "error":
		return "✕"
	case "tainted":
		return "!"
	default:
		return "·"
	}
}

// healthBadge renders a supervised process's latest liveness probe result: nil
// (not yet probed) reads as "starting".
func healthBadge(h *bool) string {
	switch {
	case h == nil:
		return lipgloss.NewStyle().Foreground(cCyan).Render("starting")
	case *h:
		return stGreen.Render("healthy")
	default:
		return stErr.Render("unhealthy")
	}
}

// memStr formats resident memory as MB (or GB at ≥1024 MB) with two decimals,
// e.g. "42.18 MB" / "1.25 GB". Empty for zero so callers can show a dash.
func memStr(b int64) string {
	if b <= 0 {
		return ""
	}
	const mb = 1024 * 1024
	if b < 1024*mb {
		return fmt.Sprintf("%.2f MB", float64(b)/mb)
	}
	return fmt.Sprintf("%.2f GB", float64(b)/(1024*mb))
}

// dashVerbLabel is the user-facing name of a staged dash action's control verb.
func dashVerbLabel(verb string) string {
	switch verb {
	case "down":
		return "sleep"
	case "destroy", "reset":
		return "reset"
	case "boot":
		return "wake"
	}
	return verb
}

// orAllServices names an empty instance argument in prompts ("sleep all services?").
func orAllServices(name string) string {
	if name == "" {
		return "all services"
	}
	return name
}

// orFallback returns a when it is non-empty, else b.
func orFallback(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
