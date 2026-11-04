package display

import (
	"fmt"
	"strings"
	"time"
)

// What a surface says about its daemon. A screen that goes on showing the
// last picture of a daemon that has gone is the worst thing a dashboard can
// do: it looks healthy and is not. So every page says when what it shows
// cannot be believed, in these words.

// StaleAfter is how long without a frame before the page says its picture is
// old. The daemon sends the meters every second, so three is already late.
const StaleAfter = 3 * time.Second

// Banner says whether what the page shows can be believed, and if not, what
// to put at the top right of it in place of the resident total. connecting
// and down are the connection's state; retryIn is how long until the next
// attempt while down; sinceFrame is how long since the last frame arrived,
// or zero when none has.
func Banner(connecting, down bool, retryIn, sinceFrame time.Duration) (ok bool, text string) {
	switch {
	case connecting:
		return false, "connecting to the daemon…"
	case down:
		out := "daemon not responding"
		if left := retryIn.Round(time.Second); left > 0 {
			out += fmt.Sprintf(" · retrying in %ds", int(left.Seconds()))
		} else {
			out += " · retrying"
		}
		return false, out
	case sinceFrame > StaleAfter:
		return false, fmt.Sprintf("last update %ds ago", int(sinceFrame.Seconds()))
	}
	return true, ""
}

// Humanise makes a connection error one short sentence: what happened first,
// the path it happened on last.
func Humanise(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "connection refused"), strings.Contains(s, "no such file"), strings.Contains(s, "not reachable"):
		return "the daemon is not reachable"
	case strings.Contains(s, "EOF"), strings.Contains(s, "broken pipe"), strings.Contains(s, "connection reset"):
		return "the daemon closed the connection"
	}
	return s
}
