// Engine pages: some engines earn a custom body below their card — the aws
// instance is a whole cloud, so instead of an (empty) logs pane it gets an
// observatory: a service board, an attention line, and the live wire of API
// calls. The page is read-only by design — managing things stays the web
// console's job; enter deep-links straight to the right console page.
//
// Data arrives via the instance's own glance API (one JSON call, designed for
// this page), polled only while the row is selected.
package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/doze-dev/doze/internal/control"
)

// hasEnginePage reports whether an engine renders a custom body.
func hasEnginePage(engine string) bool { return engine == "aws" || engine == "kafka" }

// ── glance data (the aws page's feed) ────────────────────────────────────────

type glanceService struct {
	Svc   string `json:"svc"`
	Label string `json:"label"`
	State string `json:"state"`
	Warn  bool   `json:"warn"`
	Spark []int  `json:"spark"`
	Calls int    `json:"calls"`
}

type glanceAttention struct {
	Text string `json:"text"`
	Slug string `json:"slug"`
}

type glanceWire struct {
	Seq    int64   `json:"seq"`
	T      string  `json:"t"`
	Svc    string  `json:"svc"`
	Action string  `json:"action"`
	Res    string  `json:"res"`
	Code   int     `json:"code"`
	Millis float64 `json:"ms"`
	Err    bool    `json:"err"`
}

type glanceData struct {
	Services  []glanceService   `json:"services"`
	Attention []glanceAttention `json:"attention"`
	Wire      []glanceWire      `json:"wire"`
	Rate      string            `json:"rate"`
	Rate60    []int             `json:"rate60"`
	Recorder  bool              `json:"recorder"`
}

type glanceMsg struct {
	name string
	g    *glanceData
	err  error
}

// fetchGlance pulls the instance's glance feed over its raw bind address (no
// DNS dependency; works even before dns-setup).
func fetchGlance(v control.InstanceView) tea.Cmd {
	bind := v.Bind
	if bind == "" {
		bind = v.Endpoint
	}
	name := v.Name
	return func() tea.Msg {
		client := &http.Client{Timeout: 900 * time.Millisecond}
		resp, err := client.Get("http://" + bind + "/_console/api/glance")
		if err != nil {
			return glanceMsg{name: name, err: err}
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return glanceMsg{name: name, err: err}
		}
		var g glanceData
		if err := json.Unmarshal(body, &g); err != nil {
			return glanceMsg{name: name, err: err}
		}
		return glanceMsg{name: name, g: &g}
	}
}

// consoleSlug is each service's page path in the web console.
var consoleSlug = map[string]string{
	"s3": "s3", "sqs": "sqs", "sns": "sns", "ddb": "ddb",
	"eb": "eb", "lambda": "lambda", "kms": "kms", "ssm": "ssm", "sm": "sm",
}

// svcTitle is the board's display name per glance key.
var svcTitle = map[string]string{
	"s3": "s3", "sqs": "sqs", "sns": "sns", "ddb": "dynamodb",
	"eb": "eventbridge", "lambda": "lambda", "kms": "kms",
	"ssm": "ssm", "sm": "secretsmanager",
}

// boardLen is how many rows the selected instance's board has (aws: services
// from the glance; kafka: topics from the admin snapshot).
func (m model) boardLen(v control.InstanceView) int {
	switch v.Engine {
	case "aws":
		if m.glance != nil {
			return len(m.glance.Services)
		}
	case "kafka":
		n := 0
		for _, r := range m.adminRes {
			if r.Kind == "topic" {
				n++
			}
		}
		return n
	}
	return 0
}

// wireStart is the index into the glance wire the view starts at: the entry
// the anchor pins, or 0 (newest) when following live. If the anchored entry
// slid out of the buffer, the next older survivor takes its place.
func (m model) wireStart() int {
	if m.wireAnchor == 0 || m.glance == nil {
		return 0
	}
	for i, e := range m.glance.Wire {
		if e.Seq <= m.wireAnchor {
			return i
		}
	}
	return max(0, len(m.glance.Wire)-1)
}

// wireScroll moves the wire scrollback: dir +1 shows older calls, -1 newer.
// Reaching the newest entry drops the anchor and the view follows live again.
func (m *model) wireScroll(v control.InstanceView, dir int) {
	if v.Engine != "aws" || m.glance == nil || len(m.glance.Wire) == 0 {
		return
	}
	idx := m.wireStart() + dir
	if idx <= 0 {
		m.wireAnchor = 0
		return
	}
	if idx > len(m.glance.Wire)-1 {
		idx = len(m.glance.Wire) - 1
	}
	m.wireAnchor = m.glance.Wire[idx].Seq
}

// boardOpenURL is the console page the board's selected row opens.
func (m model) boardOpenURL(v control.InstanceView) string {
	base := m.webURL(v) // …/_console
	if base == "" || m.glance == nil || m.boardSel >= len(m.glance.Services) {
		return base
	}
	if slug, ok := consoleSlug[m.glance.Services[m.boardSel].Svc]; ok {
		return base + "/" + slug
	}
	return base
}

// ── rendering ────────────────────────────────────────────────────────────────

const sparkGlyphs = " ▁▂▃▄▅▆▇█"

// sparkline renders buckets as block glyphs, scaled to the given max.
func sparkline(buckets []int, n, maxV int) string {
	if maxV < 1 {
		maxV = 1
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		v := 0
		if i < len(buckets) {
			v = buckets[i]
		}
		g := 0
		if v > 0 {
			g = 1 + v*(len([]rune(sparkGlyphs))-2)/maxV
			if g > len([]rune(sparkGlyphs))-1 {
				g = len([]rune(sparkGlyphs)) - 1
			}
		}
		b.WriteRune([]rune(sparkGlyphs)[g])
	}
	return b.String()
}

// viewEnginePage renders the aws body: board · attention · wire. It fills the
// same region the logs pane would occupy (height rows, width w).
func (m model) viewEnginePage(v control.InstanceView, w int) string {
	height := max(3, m.bodyH()-m.detailBoxH()-1)
	head := func(title, extra string) string {
		t := stLabel.Render(title)
		if extra != "" {
			t += stDim.Render(" · " + extra)
		}
		fill := max(1, w-4-lipgloss.Width(t))
		return stFaint.Render("╌╌ ") + t + " " + stFaint.Render(strings.Repeat("╌", fill))
	}
	var lines []string

	g := m.glance
	if g == nil || m.glanceName != v.Name {
		msg := "reading the stack…"
		if v.PID == 0 {
			msg = "asleep — enter wakes it, then the board fills in"
		} else if m.glanceErr != "" {
			msg = "glance unavailable (" + m.glanceErr + ") — l shows raw logs"
		}
		lines = append(lines, head("aws", ""), stDim.Render("  "+msg))
		return pad(lines, height)
	}

	// ── the board ──
	focusHint := "console " + m.webURL(v) + " · o opens it · tab focuses"
	if m.boardFocus {
		focusHint = "j/k move · enter opens that service's console page · esc back"
	}
	lines = append(lines, head("services", focusHint))
	maxCall := 1
	for _, s := range g.Services {
		for _, b := range s.Spark {
			if b > maxCall {
				maxCall = b
			}
		}
	}
	board := min(len(g.Services), max(3, height-8)) // leave room for wire
	for i := 0; i < board; i++ {
		s := g.Services[i]
		bar := "  "
		if m.boardFocus && i == m.boardSel {
			bar = stAccent.Render("▌ ")
		}
		calls := "—"
		if s.Calls > 0 {
			calls = plural(s.Calls, "call")
		}
		stateStyle := stDim
		if s.Warn {
			stateStyle = lipgloss.NewStyle().Foreground(cGold)
		} else if strings.HasPrefix(s.State, "warm") {
			stateStyle = stGreen
		}
		row := fmt.Sprintf("%s%-14s %-18s %s %-16s %s",
			bar, svcTitle[s.Svc], s.Label,
			lipgloss.NewStyle().Foreground(cCyan).Render(sparkline(s.Spark, 8, maxCall)), calls,
			stateStyle.Render(s.State))
		if m.boardFocus && i == m.boardSel {
			row = lipgloss.NewStyle().Background(cSel).Render(row)
		}
		lines = append(lines, truncate(row, w-2))
	}

	// ── attention ──
	for _, a := range g.Attention {
		lines = append(lines, lipgloss.NewStyle().Foreground(cGold).Render("  ⚠ ")+lipgloss.NewStyle().Foreground(cGold).Render(a.Text)+stDim.Render(" — enter on its service opens the console"))
	}

	// ── the wire ──
	rate := g.Rate
	if !g.Recorder {
		rate = "capture off in this topology"
	} else if rate == "" || rate == "0/min" {
		rate = "quiet"
	}
	start := m.wireStart()
	if start > 0 {
		rate += fmt.Sprintf(" · ↑%d of %d · K newer · esc live", start, len(g.Wire))
	} else if len(g.Wire) > 0 {
		rate += fmt.Sprintf(" · %d buffered · J/K scroll", len(g.Wire))
	}
	lines = append(lines, head("wire", rate))
	room := height - len(lines)
	if len(g.Wire) == 0 {
		hint := "no calls yet — point an SDK at " + connectLine(v)
		if !g.Recorder {
			hint = "the recorder is not in this request path"
		}
		lines = append(lines, stDim.Render("  "+hint))
	}
	for i := start; i < len(g.Wire) && i-start < room; i++ {
		e := g.Wire[i]
		code := stGreen.Render(fmt.Sprint(e.Code))
		if e.Err {
			code = stErr.Render(fmt.Sprint(e.Code))
		}
		row := fmt.Sprintf("  %s  %s %-16s %-28s %s %7.1fms",
			stDim.Render(e.T), lipgloss.NewStyle().Foreground(cCyan).Bold(true).Render(fmt.Sprintf("%-7s", e.Svc)),
			e.Action, stDim.Render(truncate(e.Res, 28)), code, e.Millis)
		lines = append(lines, truncate(row, w-2))
	}
	return pad(lines, height)
}

func pad(lines []string, height int) string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:height], "\n")
}
