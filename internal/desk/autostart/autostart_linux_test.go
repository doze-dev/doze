//go:build linux

package autostart

import "testing"

func TestQuotePercent(t *testing.T) {
	if got := quote("/opt/100%/doze"); got != "/opt/100%%/doze" {
		t.Fatalf("quote = %q", got)
	}
}
