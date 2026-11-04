package display

import (
	"strings"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
)

var epoch = time.Date(2026, 10, 11, 14, 4, 40, 0, time.UTC)

// An instance that never sleeps says so, and is never counted down to sleep;
// one that sleeps and has nothing holding it says that instead.
func TestAlwaysOnRowsSayTheyRunUntilStopped(t *testing.T) {
	snap := control.Snapshot{IdleTimeout: 5 * time.Minute}
	idle := control.InstanceView{Name: "worker", Kind: "app", State: "idle", IdleSince: epoch.Add(-time.Second), AlwaysOn: true}
	if got := Because(idle, snap, epoch); got != "runs until stopped" {
		t.Errorf("always-on idle row says %q", got)
	}
	idle.AlwaysOn = false
	idle.Endpoint = "127.0.0.1:3000"
	if got := Because(idle, snap, epoch); !strings.HasPrefix(got, "sleeps in") {
		t.Errorf("sleeping app row says %q", got)
	}
	active := control.InstanceView{Name: "api", Kind: "app", State: "active"}
	if got := Because(active, snap, epoch); got != "nothing holds it" {
		t.Errorf("fronted app with no holders says %q", got)
	}
}
