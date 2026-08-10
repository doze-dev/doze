package loopback

import (
	"fmt"
	"os"
	"testing"
)

// requireLoopback skips on machines where addresses past 127.0.0.1 can't be
// bound (macOS without `doze dns-setup` — including CI runners); Linux always
// can, so CI's ubuntu leg keeps these covered.
func requireLoopback(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("loopback range not aliased on this machine (doze dns-setup); covered on Linux")
	}
}

func TestAllocatorDistinctAndStable(t *testing.T) {
	requireLoopback(t)
	home := t.TempDir()
	a := NewAllocator(home, os.Getpid())

	ip1, err := a.For("demo", "orders_pg")
	if err != nil {
		t.Fatal(err)
	}
	ip2, err := a.For("demo", "analytics_pg")
	if err != nil {
		t.Fatal(err)
	}
	if ip1.Equal(ip2) {
		t.Fatalf("two services got the same IP: %v", ip1)
	}
	if ip1.String() != "127.0.0.2" {
		t.Fatalf("first alloc = %v, want 127.0.0.2", ip1)
	}
	// Same key → same IP (stable across calls / restarts).
	again, _ := a.For("demo", "orders_pg")
	if !again.Equal(ip1) {
		t.Fatalf("re-alloc = %v, want stable %v", again, ip1)
	}
	// A fresh allocator over the same file reuses the recorded IPs.
	b := NewAllocator(home, os.Getpid())
	if r, _ := b.For("demo", "orders_pg"); !r.Equal(ip1) {
		t.Fatalf("cross-allocator = %v, want %v", r, ip1)
	}
}

func TestAllocatorReapsDeadStacks(t *testing.T) {
	requireLoopback(t)
	home := t.TempDir()
	// Record an allocation owned by a definitely-dead pid.
	dead := NewAllocator(home, 999999)
	ipDead, _ := dead.For("old", "svc")

	// A live allocator should reclaim the dead stack's IP for a new service.
	live := NewAllocator(home, os.Getpid())
	ipNew, err := live.For("new", "svc")
	if err != nil {
		t.Fatal(err)
	}
	if !ipNew.Equal(ipDead) {
		t.Fatalf("new alloc = %v, want the reclaimed %v", ipNew, ipDead)
	}
}

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

func TestAllocatorNeverHandsOutAReservedAddress(t *testing.T) {
	// .2–.9 belong to the shared zone's apex names and .54 to the resolver on
	// Linux. Handing any of them to a stack service would put two things on one
	// address — and for .54, take the machine's DNS down to serve one instance.
	requireLoopback(t)
	a := NewAllocator(t.TempDir(), os.Getpid())
	for i := 0; i < 12; i++ {
		ip, err := a.For("demo", fmt.Sprintf("svc%d", i))
		if err != nil {
			break // pool exhaustion is fine; wrong addresses are not
		}
		last := ip.To4()[3]
		if last < hostStart {
			t.Errorf("%v is reserved for an apex name", ip)
		}
		if int(last) == resolverHost {
			t.Errorf("%v is the resolver's own address", ip)
		}
	}
}
