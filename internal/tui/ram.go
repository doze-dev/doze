package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// rssBytes returns the resident set size of a pid in bytes, or 0 if it cannot
// be determined (e.g. on platforms without /proc).
func rssBytes(pid int) int64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64) // resident pages
	if err != nil {
		return 0
	}
	return pages * int64(os.Getpagesize())
}

func humanRAM(b int64) string {
	if b <= 0 {
		return ""
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.0f%cB", float64(b)/float64(div), "KMGT"[exp])
}
