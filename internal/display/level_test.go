package display

import "testing"

// Level reads the shapes programs commonly log in, and says nothing about a
// line that does not carry one, so the dash can tell "info" from "unknown".
func TestLevel(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{"INF listening addr=:8080", "info"},
		{"[error] upstream timed out", "error"},
		{"ERROR: relation \"x\" does not exist", "error"},
		{"2026-10-11 14:04:00.123 UTC [612] LOG:  checkpoint complete", "info"},
		{"2026-10-11 14:04:00.123 UTC [612] WARNING:  too many", "warn"},
		{"level=warn msg=\"slow\" ms=212", "warn"},
		{"ts=1 lvl=debug msg=x", "debug"},
		{`{"level":"error","msg":"boom"}`, "error"},
		{`{"time":1,"msg":"x","level": "INFO"}`, "info"},
		{`{"severity":"WARNING","message":"x"}`, "warn"},
		{`{"level":50,"msg":"pino error"}`, "error"},
		{`{"level":30,"msg":"pino info"}`, "info"},
		{"GET /products 200 12ms", ""},
		{"listening on :3000", ""},
		{"", ""},
	} {
		if got := Level(c.line); got != c.want {
			t.Errorf("Level(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}
