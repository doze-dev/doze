package dash

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTheProfileReadsTheWindowOnce(t *testing.T) {
	for _, c := range []struct {
		w, h int
		wc   widthClass
		hc   heightClass
	}{
		{80, 24, widthCompact, heightShort}, {99, 27, widthCompact, heightShort},
		{100, 28, widthStandard, heightNormal}, {159, 47, widthStandard, heightNormal},
		{160, 48, widthStandard, heightTall}, {175, 50, widthStandard, heightTall},
		{176, 48, widthWide, heightTall}, {219, 60, widthWide, heightTall},
		{220, 70, widthGrand, heightTall},
	} {
		if p := profileOf(c.w, c.h); p.wc != c.wc || p.hc != c.hc {
			t.Errorf("%dx%d is %v, want width %d height %d", c.w, c.h, p, c.wc, c.hc)
		}
	}
}

// The meter key is bought after the bars and not at the expense of the table: it
// needs the rows the table wants (up to sixteen) and the logs' minimum.
func TestTheMeterKeyIsBoughtWithWhatIsLeft(t *testing.T) {
	for _, c := range []struct {
		rows, h int
		want    bool
	}{
		{15, 33, false}, {15, 34, true}, // the board's fifteen rows: 34 rows of window
		{5, 23, false}, {5, 24, true}, // a small stack gets it sooner
		{200, 34, false}, {200, 35, true}, // a big one asks no more than sixteen rows
	} {
		if got := profileOf(150, c.h).keyedMeters(c.rows); got != c.want {
			t.Errorf("%d rows in %d: key = %v, want %v", c.rows, c.h, got, c.want)
		}
	}
}

// Stacked below 176 columns; at 176 and up the selected instance is beside the
// list, with its vitals, and the list is only as wide as its content.
func TestWideHomeIsMasterAndDetail(t *testing.T) {
	m := board()
	stacked := frame(m, 150, 40)
	if !strings.Contains(stacked, "─ api · logs · following") {
		t.Errorf("below 176 the logs are under the list:\n%s", stacked)
	}
	wide := strings.Split(frame(m, 190, 54), "\n")
	var head string
	for _, l := range wide {
		if strings.Contains(l, "NAME") && strings.Contains(l, "BECAUSE") {
			head = l
		}
	}
	if head == "" {
		t.Fatalf("no table header:\n%s", strings.Join(wide, "\n"))
	}
	// the detail starts on the header's row, to the right of the list
	if i, j := strings.Index(head, "BECAUSE"), strings.Index(head, "╭"); j < i {
		t.Errorf("the detail is not beside the list: %q", head)
	}
	if got := strings.Join(wide, "\n"); !strings.Contains(got, "api · logs · following") || !strings.Contains(got, "activity") {
		t.Errorf("the detail's logs and the list's activity are missing:\n%s", got)
	}
	if m.hitX1 >= 190 || m.hitX1 < 100 {
		t.Errorf("clicks end at %d: only the list is clickable", m.hitX1)
	}
}

// A column stops at its widest cell: RUNS sits beside BECAUSE, not across the room.
func TestColumnsStopAtTheirContent(t *testing.T) {
	m := stressModel(4, func(i int) string { return fmt.Sprintf("svc%d", i) }, nil)
	for _, w := range []int{150, 190} {
		var head string
		for _, l := range strings.Split(frame(m, w, 40), "\n") {
			if strings.Contains(l, "BECAUSE") && strings.Contains(l, "RUNS") {
				head = l
			}
		}
		if head == "" {
			t.Fatalf("%d: no header with both columns", w)
		}
		if i, j := strings.Index(head, "BECAUSE"), strings.Index(head, "RUNS"); j-i > 45 {
			t.Errorf("%d: RUNS is %d columns from BECAUSE: %q", w, j-i, head)
		}
	}
}

func TestClicksOnTheDetailDoNothing(t *testing.T) {
	m := board()
	frame(m, 190, 54)
	sel := m.sel
	m.Update(tea.MouseClickMsg{X: m.hitX1 + 5, Y: m.hitY0 + 3, Button: tea.MouseLeft})
	if m.sel != sel {
		t.Error("a click on the detail moved the selection")
	}
	m.Update(tea.MouseClickMsg{X: 4, Y: m.hitY0 + 3, Button: tea.MouseLeft})
	if m.sel == sel {
		t.Error("a click on the list should select")
	}
}

func TestGrandAddsATrendAndNothingElseGrows(t *testing.T) {
	m := board()
	for i := 0; i < 20; i++ {
		m.ram["api"] = append(m.ram["api"], float64(80+i)*1048576)
	}
	if !strings.Contains(frame(m, 260, 70), "TREND") {
		t.Error("grand should show a trend column")
	}
	if strings.Contains(frame(m, 190, 54), "TREND") || strings.Contains(frame(m, 150, 40), "TREND") {
		t.Error("the trend is for 220 columns and up")
	}
}

// Every shape of stack at the sizes a laptop and a monitor give: the selected
// instance is on screen, the keys that matter are in the bar, nothing is wider
// or taller than the window.
func TestTheSizeMatrix(t *testing.T) {
	for _, sh := range shapes() {
		if len(sh.build().snap.Instances) == 0 {
			continue
		}
		for _, sz := range [][2]int{{80, 24}, {100, 30}, {140, 40}, {160, 45}, {190, 54}, {260, 70}} {
			m := sh.build()
			m.w, m.h = sz[0], sz[1]
			out := frame(m, sz[0], sz[1])
			lines := strings.Split(out, "\n")
			if len(lines) > sz[1] {
				t.Errorf("%s at %v: %d lines", sh.name, sz, len(lines))
			}
			if bar := lines[len(lines)-1]; !strings.Contains(bar, "?") || !strings.Contains(bar, "help") {
				t.Errorf("%s at %v: the key bar lost help: %q", sh.name, sz, bar)
			}
			sel := m.sel
			if sel != "" && sz[0] >= 100 && !strings.Contains(out, string([]rune(sel)[:min(4, len([]rune(sel)))])) {
				t.Errorf("%s at %v: the selected instance %q is not on screen", sh.name, sz, sel)
			}
			for _, l := range lines {
				if width(l) > sz[0] {
					t.Errorf("%s at %v: a line is %d wide", sh.name, sz, width(l))
					break
				}
			}
		}
	}
	_ = fmt.Sprint
}
