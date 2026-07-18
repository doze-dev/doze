// Layout math: pane sizing and the sidebar's grouped, windowed line layout —
// the single source both the renderers and the mouse hit-testing derive from.

package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/doze-dev/doze/internal/control"
)

func (m *model) layout() {
	// logs region: an open area under the content-sized detail card — 1 chrome
	// row (the inline-title rule) and a 1-col right margin.
	m.logVP.Width = max(4, m.rightW()-1)
	m.logVP.Height = max(3, m.bodyH()-m.detailBoxH()-1)
}

func (m model) bodyH() int {
	if h := m.height - 3; h > 4 { // header (2) + footer (1)
		return h
	}
	return 4
}

func (m model) sidebarW() int {
	sw := 32
	if m.width < 96 {
		sw = 26
	}
	if sw > m.width/2 {
		sw = m.width / 2
	}
	if sw < 12 {
		sw = 12
	}
	return sw
}

// rightW is the CONTENT width handed to the right pane's boxes: lipgloss draws
// the detail card's rounded border OUTSIDE .Width(), so the budget subtracts
// the sidebar (its right border included in sidebarW rendering +1), the 2-col
// gap, and the card's own 2 border cols — otherwise every body line lands 2
// cols past the window and tears the frame.
func (m model) rightW() int {
	if w := m.width - m.sidebarW() - 5; w > 12 { // sidebar border (1) + gap (2) + card border (2)
		return w
	}
	return 12
}

// detailBoxH is the rendered height of the detail card including its border.
// The card is content-sized, so the logs pane and the mouse math derive from
// the same builder the renderer uses.
func (m model) detailBoxH() int {
	v, ok := m.selected()
	if !ok {
		return 2
	}
	return len(m.detailLines(v, m.rightW())) + 2
}

// visible returns instance indices in display order (name-sorted, filtered).
func (m model) visible() []int {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	idx := make([]int, 0, len(m.resp.Instances))
	for i, in := range m.resp.Instances {
		if q != "" && !strings.Contains(strings.ToLower(in.Name+" "+in.Engine), q) {
			continue
		}
		idx = append(idx, i)
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ia, ib := m.resp.Instances[idx[a]], m.resp.Instances[idx[b]]
		ga, gb := groupOf(ia), groupOf(ib)
		if ra, rb := groupRank(ga), groupRank(gb); ra != rb {
			return ra < rb
		}
		if ga != gb { // same rank (custom headings): keep each group contiguous
			return ga < gb
		}
		return ia.Name < ib.Name
	})
	return idx
}

// groupOf is the display heading an instance falls under: its engine type —
// buckets sit with buckets, postgres with postgres — so the sidebar reads as
// the stack's architecture. An explicit `group=` heading from config
// (InstanceView.Group) wins; supervised apps stay under "processes".
func groupOf(in control.InstanceView) string {
	if in.Group != "" {
		return in.Group
	}
	if in.Engine == "process" {
		return "processes"
	}
	return in.Engine
}

// groupOrder ranks the headings by role, top to bottom: relational and
// document stores, then caches, then the AWS trio, then everything custom,
// with the user's own processes anchoring the bottom — architecture order,
// not alphabetical order.
var groupOrder = map[string]int{
	"postgres": 0, "mariadb": 1, "ferret": 2, "documentdb": 3,
	"valkey": 10, "kvrocks": 11,
	"temporal": 20,
	"s3":       30, "sqs": 31, "sns": 32,
	"aws-console": 40, // the opt-in web console rides right under the services it fronts
	"processes":   100,
}

// groupRank orders the group headings; unknown engines and custom `group=`
// headings sit between the builtins and the processes.
func groupRank(cat string) int {
	if r, ok := groupOrder[cat]; ok {
		return r
	}
	return 50
}

// sbLine is one rendered sidebar line: a group header, or an instance at display
// index di (into visible()). The cursor only ever lands on instance lines.
type sbLine struct {
	header string
	di     int
}

// sidebarLines lays out the visible instances with a header line inserted wherever
// the group changes. Both the renderer and the click handler use it so headers and
// selection stay in sync.
func (m model) sidebarLines() []sbLine {
	vis := m.visible()
	counts := map[string]int{}
	for _, i := range vis {
		counts[groupOf(m.resp.Instances[i])]++
	}
	out := make([]sbLine, 0, len(vis)+8)
	prev := ""
	for di, i := range vis {
		if g := groupOf(m.resp.Instances[i]); g != prev {
			h := g
			if counts[g] > 1 {
				h += fmt.Sprintf(" · %d", counts[g])
			}
			out = append(out, sbLine{header: h})
			prev = g
		}
		out = append(out, sbLine{di: di})
	}
	return out
}

// sidebarAvail is how many sidebar body rows fit above the pinned totals
// footer (sidebarTotals is always 3 lines: rule + counts + cpu/mem).
func (m model) sidebarAvail() int { return max(0, m.bodyH()-3) }

// sidebarWindow returns the sidebar lines actually rendered — at most rows of
// them, slid so the cursor's line stays on screen — plus how many lines sit
// hidden above and below the window. The renderer and the mouse hit-testing
// both map screen rows through this, so they can never disagree.
func (m model) sidebarWindow(rows int) ([]sbLine, int, int) {
	lines := m.sidebarLines()
	if rows <= 0 || len(lines) <= rows {
		return lines, 0, 0
	}
	cur := 0
	for i, ln := range lines {
		if ln.header == "" && ln.di == m.cursor {
			cur = i
			break
		}
	}
	start := clampi(cur-rows/2, 0, len(lines)-rows)
	return lines[start : start+rows], start, len(lines) - start - rows
}
