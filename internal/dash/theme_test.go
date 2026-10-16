package dash

import (
	"strings"
	"testing"
)

// Home's detail shows cpu and memory as charts; connections are counted once, in
// the meters. An instance's own page still charts them.
func TestHomeDetailHasNoConnectionsChart(t *testing.T) {
	m := board()
	wide := frame(m, 190, 54)
	if !strings.Contains(wide, "vitals") {
		t.Fatalf("the detail has no vitals:\n%s", wide)
	}
	if n := strings.Count(wide, "conns"); n != 1 {
		t.Errorf("conns is on a wide Home %d times, want the meter's line only:\n%s", n, wide)
	}
	m.page = pageFocus
	if got := frame(m, 190, 54); !strings.Contains(got, "conns") {
		t.Errorf("an instance's page lost its connections chart:\n%s", got)
	}
}

func TestThemesChangeTheDrawingAndKeepTheLayout(t *testing.T) {
	if len(themes) < 5 {
		t.Fatalf("%d themes", len(themes))
	}
	m := board()
	// the notice that names the theme is in the key bar; the rest is the same
	body := func() []string { return strings.Split(frame(m, 150, 40), "\n") }
	base := body()
	for _, th := range themes {
		m.setTheme(th.name)
		if m.st.pal.name != th.name {
			t.Errorf("setTheme(%q) is %q", th.name, m.st.pal.name)
		}
		got := body()
		if len(got) != len(base) {
			t.Fatalf("%s: %d lines, want %d", th.name, len(got), len(base))
		}
		for i := 0; i < len(got)-1; i++ { // all but the key bar
			if got[i] != base[i] {
				t.Errorf("%s moved the layout on line %d:\n%q\n%q", th.name, i, got[i], base[i])
				break
			}
		}
		if !strings.Contains(got[len(got)-1], "theme · "+th.name) {
			t.Errorf("%s: the key bar does not say so: %q", th.name, got[len(got)-1])
		}
	}
	m.setTheme("no such theme")
	if m.st.pal.name != "violet" {
		t.Errorf("an unknown theme is %q, want violet", m.st.pal.name)
	}
}

func TestThemesAreOnThePalette(t *testing.T) {
	m := board()
	m.modal = nil
	var got []string
	for _, a := range m.actions() {
		if strings.HasPrefix(a.label, "theme · ") {
			got = append(got, strings.TrimPrefix(a.label, "theme · "))
		}
	}
	if strings.Join(got, ",") != strings.Join(ThemeNames(), ",") {
		t.Errorf("palette lists %v, want %v", got, ThemeNames())
	}
}
