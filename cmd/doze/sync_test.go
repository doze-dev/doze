package main

import (
	"strings"
	"testing"
)

// Without a terminal on stdin, sync must refuse and name the flag, not read
// the empty input as "no" and exit 0.
func TestSyncCannotAskWithoutATerminal(t *testing.T) {
	err := canAsk(false)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("canAsk(false) = %v, want an error naming --yes", err)
	}
	if err := canAsk(true); err != nil {
		t.Fatalf("canAsk(true) = %v, want nil", err)
	}
}
