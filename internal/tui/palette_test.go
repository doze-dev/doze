package tui

import (
	"testing"
)

func TestFuzzyScoreRanking(t *testing.T) {
	// Match classes: exact > prefix > word-start > substring > subsequence.
	ordered := []struct{ q, hi, lo string }{
		{"wake", "wake", "wakeall"},        // exact beats prefix
		{"co", "console", "orders-copy"},   // prefix beats word-start
		{"pg", "orders-pg", "shipping"},    // word-start beats subsequence
		{"end", "sender", "e-n-d-queue"},   // substring beats subsequence
		{"send", "send", "sendmail-queue"}, // exact beats prefix (long)
	}
	for _, c := range ordered {
		hi, lo := fuzzyScore(c.q, c.hi), fuzzyScore(c.q, c.lo)
		if hi < 0 || lo < 0 {
			t.Fatalf("fuzzyScore(%q): unexpected non-match hi=%d lo=%d", c.q, hi, lo)
		}
		if hi <= lo {
			t.Fatalf("fuzzyScore(%q): %q (%d) should outrank %q (%d)", c.q, c.hi, hi, c.lo, lo)
		}
	}
	// Out-of-order letters don't match; case is ignored.
	if fuzzyScore("xzy", "console") >= 0 {
		t.Fatal("no-match should score -1")
	}
	if fuzzyScore("CON", "Console") < 0 {
		t.Fatal("matching should be case-insensitive")
	}
	// Subsequence still matches (the whole point of fuzzy).
	if fuzzyScore("cnsl", "console") < 0 {
		t.Fatal("subsequence should match")
	}
}

func TestPaletteFuzzyVerbMatch(t *testing.T) {
	m := threeInstances()
	// A subsequence like "thm" reaches the theme verb — prefix-only never did.
	m.palInput = "thm"
	found := false
	for _, s := range m.paletteSuggestions() {
		if s.label == "theme" {
			found = true
		}
	}
	if !found {
		t.Fatal("fuzzy 'thm' should surface theme")
	}
	// Locals still lead the list on empty input, in curated order (`open` — the
	// web-console door — first).
	m.palInput = ""
	sugs := m.paletteSuggestions()
	if len(sugs) < 2 || sugs[0].label != "open" || sugs[1].label != "theme" {
		t.Fatalf("empty input should lead with the local verbs, got %+v", sugs[:min(3, len(sugs))])
	}
}

func TestPaletteStaysBulkOnly(t *testing.T) {
	m := threeInstances()
	// Per-item console verbs never appear — the palette is for fleet/view moves;
	// send/put/publish/purge live inside each service's manager.
	m.palInput = ""
	labels := map[string]bool{}
	for _, s := range m.paletteSuggestions() {
		labels[s.label] = true
	}
	for _, banned := range []string{"send", "put", "publish", "peek", "purge"} {
		if labels[banned] {
			t.Fatalf("palette must not list :%s", banned)
		}
	}
	for _, want := range []string{"wake", "sleep", "restart", "reset", "theme", "open"} {
		if !labels[want] {
			t.Fatalf("palette should list :%s, got %v", want, labels)
		}
	}
}
