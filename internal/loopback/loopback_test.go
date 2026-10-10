package loopback

import (
	"fmt"
	"testing"
)

func TestSetupCommandsCoverRange(t *testing.T) {
	// Aliasing covers a WIDER range than allocation: the shared zone's apex
	// names sit at .2–.9, so those have to be aliased even though the allocator
	// will never hand one out. Asserting poolSize here — as this test used to —
	// silently stopped aliasing them, which leaves aws.doze pointing at an
	// address nothing can bind.
	cmds := SetupCommands()
	if len(cmds) != aliasCount {
		t.Fatalf("SetupCommands = %d, want %d (the ALIAS range, not the pool)", len(cmds), aliasCount)
	}
	if cmds[0] != fmt.Sprintf("ifconfig lo0 alias 127.0.0.%d up", aliasStart) {
		t.Fatalf("first cmd = %q, want the range to start at the apex names", cmds[0])
	}
	if aliasStart >= hostStart {
		t.Fatalf("aliasing must start below the pool: alias %d, pool %d", aliasStart, hostStart)
	}
	// A small, contiguous pool in the first /24 — kept modest so macOS's
	// mDNSResponder isn't handed hundreds of aliases to register.
	if last := cmds[len(cmds)-1]; last != fmt.Sprintf("ifconfig lo0 alias 127.0.0.%d up", aliasStart+aliasCount-1) {
		t.Fatalf("last cmd = %q", last)
	}
	if poolSize > 128 {
		t.Fatalf("pool of %d is too large for macOS aliasing comfort", poolSize)
	}
}
