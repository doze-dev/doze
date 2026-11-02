// headless answers D0's first question: what does linking Wails (purego, no
// cgo) cost a process that never opens a window: the CLI, the daemon. It
// prints its RSS and how many GUI libraries it has mapped.
package main

import (
	"fmt"
	"os"
	"strings"

	_ "github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	rss, maps := "", 0
	if b, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "VmRSS:") {
				rss = strings.TrimSpace(strings.TrimPrefix(l, "VmRSS:"))
			}
		}
	}
	if b, err := os.ReadFile("/proc/self/maps"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.Contains(l, "webkit") || strings.Contains(l, "gtk-4") {
				maps++
			}
		}
	}
	fmt.Printf("rss=%s gui_lib_mappings=%d\n", rss, maps)
}
