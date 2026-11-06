package runtime

import "testing"

func TestPortTrapHint(t *testing.T) {
	cases := []struct {
		name      string
		port      int
		listening []int
		want      string
	}{
		{"elsewhere", 41234, []int{5173},
			"web listens on :5173, not on $PORT (41234) that doze gave it; start it with --port $PORT (vite: --port $PORT --strictPort), or set direct = true to have doze leave its port alone"},
		{"several", 41234, []int{3000, 24678},
			"web listens on :3000, :24678, not on $PORT (41234) that doze gave it; start it with --port $PORT (vite: --port $PORT --strictPort), or set direct = true to have doze leave its port alone"},
		{"nowhere", 41234, nil,
			"web started but listens on no port; doze waited for $PORT (41234)"},
		{"on $PORT: the probe failed for another reason", 41234, []int{41234, 9229}, ""},
		{"no $PORT given", 0, []int{5173}, ""},
	}
	for _, c := range cases {
		if got := portTrapHint("web", c.port, c.listening); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestEnvPort(t *testing.T) {
	if got := envPort([]string{"HOME=/x", "PORT=41234", "HOST=127.0.0.1"}); got != 41234 {
		t.Fatalf("envPort = %d", got)
	}
	if got := envPort([]string{"APORT=1", "PORTS=2"}); got != 0 {
		t.Fatalf("envPort without PORT = %d", got)
	}
}
