package dozeversion

import "testing"

func TestOlder(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.1.1", "0.2.0", true}, {"0.2.0", "0.2.0", false}, {"v0.10.0", "0.9.9", false},
		{"0.2", "0.2.1", true}, {"dev", "0.2.0", false}, {"0.2.0", "dev", false},
	} {
		if got := Older(c.a, c.b); got != c.want {
			t.Errorf("Older(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
