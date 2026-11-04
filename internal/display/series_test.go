package display

import "testing"

func TestCoresSaysWhatTheNumberIs(t *testing.T) {
	for pct, want := range map[float64]string{0: "0 cores", 0.1: "<0.01 cores", 22.6: "0.23 cores", 100: "1 core", 250: "2.5 cores", 1020: "10.2 cores"} {
		if got := Cores(pct); got != want {
			t.Errorf("Cores(%v) = %q, want %q", pct, got, want)
		}
	}
}

// The whole of the history is drawn, whatever the width: readings are merged by
// their highest, from the newest back, so a spike survives and the right edge
// is now.
func TestSqueezeKeepsTheSpikeAndTheNewest(t *testing.T) {
	vs := make([]float64, 240)
	vs[100] = 9
	vs[239] = 5
	out := Squeeze(vs, 60)
	if len(out) > 60 {
		t.Fatalf("%d columns, want at most 60", len(out))
	}
	hi := 0.0
	for _, v := range out {
		hi = max(hi, v)
	}
	if hi != 9 {
		t.Errorf("the spike was lost: highest is %v", hi)
	}
	if out[len(out)-1] != 5 {
		t.Errorf("the newest reading is not at the right edge: %v", out[len(out)-1])
	}
	if got := Squeeze([]float64{1, 2, 3}, 60); len(got) != 3 {
		t.Errorf("a short series was changed: %v", got)
	}
}

// The bar is the whole of what is consumed: every share gets at least a cell
// and the cells add up to the width exactly.
func TestShareCellsFillTheBar(t *testing.T) {
	shares, idle, sum := Shares([]Share{{"api", 84}, {"web", 212}, {"quiet", 0}}, 0)
	if idle != 1 || len(shares) != 2 || shares[0].Name != "web" || sum != 296 {
		t.Fatalf("shares = %v idle=%d sum=%v", shares, idle, sum)
	}
	cells := ShareCells(shares, sum, 40)
	if total := cells[0] + cells[1]; total != 40 || cells[1] < 1 {
		t.Errorf("cells %v do not fill 40", cells)
	}
}
