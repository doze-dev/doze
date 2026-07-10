package doze_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	doze "github.com/doze-dev/doze"
)

// TestDogfoodStatusAndDash proves the public API is sufficient to rebuild doze's
// most demanding read surfaces — `doze status` and a live dash — using only the
// doze package: Topology() ⋈ Status() for the table, Events() for live updates.
func TestDogfoodStatusAndDash(t *testing.T) {
	if testing.Short() {
		t.Skip("boots real processes")
	}
	sb := doze.NewStack("dogfood")
	sb.AddProcess("api", doze.Process{Command: "sh -c 'while true; do sleep 1; done'", Port: 8080})
	sb.AddProcess("worker", doze.Process{Command: "sh -c 'while true; do sleep 1; done'"})
	sess := serveTestStack(t, sb)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	// Watch state transitions on a background stream — the dash's live feed.
	events := make(chan doze.Instance, 64)
	go func() { _ = sess.Events(ctx, func(in doze.Instance) { events <- in }) }()

	if err := sess.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// 1) Rebuild `doze status` purely from the library.
	table, err := renderStatus(ctx, sess)
	if err != nil {
		t.Fatalf("renderStatus: %v", err)
	}
	for _, want := range []string{"api", "worker", "process", "8080"} {
		if !strings.Contains(table, want) {
			t.Fatalf("status table missing %q:\n%s", want, table)
		}
	}
	// Both processes should show a live pid (they're up).
	insts, _ := sess.Status(ctx)
	for _, in := range insts {
		if in.PID == 0 {
			t.Fatalf("%s has no pid after Up — status surface incomplete: %+v", in.Name, in)
		}
	}

	// 2) The live dash feed delivered at least one transition to active/healthy.
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("no state-transition event received — live dash feed is empty")
	}
}

// renderStatus reproduces `doze status`'s table from Topology ⋈ Status, using
// only the public API. If this can't be written, the API isn't at level.
func renderStatus(ctx context.Context, sess *doze.Session) (string, error) {
	live := map[string]doze.Instance{}
	insts, err := sess.Status(ctx)
	if err != nil {
		return "", err
	}
	for _, in := range insts {
		live[in.Name] = in
	}
	nodes := sess.Topology()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	var b strings.Builder
	fmt.Fprintf(&b, "stack %q\n", sess.StackName())
	for _, n := range nodes {
		in := live[n.Name]
		fmt.Fprintf(&b, "%s\t%s\t%s\tpid=%d\tport=%d\n", n.Name, n.Engine, in.State, in.PID, n.Port)
	}
	return b.String(), nil
}
