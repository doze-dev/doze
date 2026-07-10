package doze_test

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	doze "github.com/doze-dev/doze"
)

// TestDogfoodStaticCommands proves the static/reconciliation CLI commands —
// lint, tree, and sync --dry-run — can be rebuilt on the public library with no
// daemon. If these render, that half of the CLI is buildable.
func TestDogfoodStaticCommands(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	t.Setenv("DOZE_HOME", base)

	sb := doze.NewStack("shop")
	sb.AddProcess("api", doze.Process{Command: "sh -c 'sleep 1'", Port: 8080})
	sb.AddProcess("worker", doze.Process{Command: "sh -c 'sleep 1'", DependsOn: []string{"api"}})

	in, err := doze.Load(doze.Options{Stack: sb})
	if err != nil {
		t.Fatalf("Load (lint would report this): %v", err)
	}
	defer in.Close()

	// `doze lint`
	lint := fmt.Sprintf("%s is valid: %d service(s)", in.StackName(), len(in.Services()))
	if !strings.Contains(lint, "shop is valid: 2 service(s)") {
		t.Fatalf("lint = %q", lint)
	}

	// `doze tree`
	tree := renderTree(in)
	for _, want := range []string{"api", "worker", "→ api"} {
		if !strings.Contains(tree, want) {
			t.Fatalf("tree missing %q:\n%s", want, tree)
		}
	}

	// `doze sync --dry-run`
	plan, err := in.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	add, change, destroy := plan.Counts()
	summary := fmt.Sprintf("plan: +%d ~%d -%d", add, change, destroy)
	if summary != "plan: +0 ~0 -0" { // process-only stack has no structural objects
		t.Fatalf("plan summary = %q, want empty", summary)
	}
}

// renderTree reconstructs `doze tree` from Topology alone.
func renderTree(in *doze.Inspection) string {
	nodes := in.Topology()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", in.StackName())
	for _, n := range nodes {
		line := fmt.Sprintf("  %s (%s)", n.Name, n.Engine)
		if len(n.DependsOn) > 0 {
			line += " → " + strings.Join(n.DependsOn, ", ")
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}
