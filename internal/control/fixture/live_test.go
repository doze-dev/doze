package fixture

import (
	"slices"
	"testing"
	"time"
)

// Waking the storefront of a sleeping Larder starts what it needs first:
// everything before the gateway, and each service after what it needs.
func TestWakeOrderStartsWhatIsNeededFirst(t *testing.T) {
	f := LarderAsleep(time.Now())
	order := wakeOrder(f.Snap.Instances, "storefront")
	at := func(n string) int { return slices.Index(order, n) }
	for _, n := range []string{"gateway", "catalog", "orders", "postgres", "redis", "kafka", "aws"} {
		if at(n) < 0 {
			t.Fatalf("%s missing from %v", n, order)
		}
	}
	for _, c := range [][2]string{{"postgres", "catalog"}, {"catalog", "orders"}, {"orders", "gateway"}, {"redis", "gateway"}} {
		if at(c[0]) > at(c[1]) {
			t.Errorf("%s wakes after %s: %v", c[0], c[1], order)
		}
	}
	if slices.Contains(order, "storefront") || slices.Contains(order, "migrate-catalog") {
		t.Errorf("the target and tasks are not in the order: %v", order)
	}
}
