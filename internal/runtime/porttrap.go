package runtime

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// envPort is the $PORT doze gave a spec, or 0 when it gave none (an app that
// binds its own port, or a backend).
func envPort(env []string) int {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PORT="); ok {
			p, _ := strconv.Atoi(v)
			return p
		}
	}
	return 0
}

// portTrapHint says why an app doze fronts was never ready, when the reason is
// the common one: it ignored $PORT and listens somewhere else, or listens
// nowhere. listening is every TCP port its process tree listens on. It is ""
// when the app does listen on $PORT, so the probe failed for some other
// reason, and when doze gave no $PORT.
func portTrapHint(name string, port int, listening []int) string {
	if port <= 0 || slices.Contains(listening, port) {
		return ""
	}
	if len(listening) == 0 {
		return fmt.Sprintf("%s started but listens on no port; doze waited for $PORT (%d)", name, port)
	}
	where := make([]string, len(listening))
	for i, p := range listening {
		where[i] = ":" + strconv.Itoa(p)
	}
	return fmt.Sprintf("%s listens on %s, not on $PORT (%d) that doze gave it; "+
		"start it with --port $PORT (vite: --port $PORT --strictPort), "+
		"or set direct = true to have doze leave its port alone",
		name, strings.Join(where, ", "), port)
}
