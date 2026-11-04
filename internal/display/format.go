package display

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// How figures and times are written on every surface: a size, a countdown, an
// age and a path. They are here, once, so a size on the dash and the same size
// in the app read the same.

// MB is a size as the board writes it: 84 MB, 1.2 GB. Nothing is written as
// a fraction of a megabyte; under half of one is 0.
func MB(b int64) string {
	switch {
	case b <= 0:
		return "0"
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	}
	return fmt.Sprintf("%d MB", (b+(1<<19))>>20)
}

// Clock is a duration as m:ss, the way the board counts down to sleep.
func Clock(d time.Duration) string {
	d = d.Round(time.Second)
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// Ago is how long a duration is in the coarsest unit that still says
// something: 12s, 4m, 3h, 2d.
func Ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// Tilde writes a path under the home directory with a ~, as a shell would.
func Tilde(dir string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && strings.HasPrefix(dir, h) {
		return "~" + strings.TrimPrefix(dir, h)
	}
	return dir
}

// Plural is the s that makes a count read: "1 connection", "2 connections".
func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
