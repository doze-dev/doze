package display

import "testing"

// The palette matches word starts, not scattered letters.
func TestPaletteMatchesWordStarts(t *testing.T) {
	cases := []struct {
		query, label string
		want         bool
	}{
		{"re", "restart api", true},
		{"re", "reset db", true},
		{"re", "rerun task migrate", true},
		{"re", "wake worker", false},
		{"reset d", "reset db", true},
		{"reset db", "reset db", true},
		{"db", "reset db", true},
		{"rdb", "reset db", false},
		{"", "anything", true},
		{"res", "restart api", true},
		{"resx", "restart api", false},
	}
	for _, c := range cases {
		if _, got := FuzzyMatch(c.query, c.label); got != c.want {
			t.Errorf("FuzzyMatch(%q, %q) = %v, want %v", c.query, c.label, got, c.want)
		}
	}
}
