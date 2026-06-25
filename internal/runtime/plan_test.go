package runtime

import (
	"strings"
	"testing"

	"github.com/nerdmenot/doze/internal/engine"
)

func names(specs []engine.SpawnSpec) []string {
	out := make([]string, len(specs))
	for i, s := range specs {
		out[i] = s.Name
	}
	return out
}

func TestOrderSpecs(t *testing.T) {
	// A documentdb-shaped plan: ferretdb After postgres.
	specs := []engine.SpawnSpec{
		{Name: "ferretdb", After: []string{"postgres"}},
		{Name: "postgres"},
	}
	order, err := orderSpecs(specs)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(order); got[0] != "postgres" || got[len(got)-1] != "ferretdb" {
		t.Fatalf("order = %v, want postgres before ferretdb", got)
	}

	// A single spec is returned as-is.
	if order, _ := orderSpecs([]engine.SpawnSpec{{Name: "solo"}}); len(order) != 1 || order[0].Name != "solo" {
		t.Fatalf("single-spec order = %v", names(order))
	}
}

func TestOrderSpecsCycle(t *testing.T) {
	_, err := orderSpecs([]engine.SpawnSpec{
		{Name: "a", After: []string{"b"}},
		{Name: "b", After: []string{"a"}},
	})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected a cycle error, got %v", err)
	}
}

func TestOrderSpecsUnknownDep(t *testing.T) {
	_, err := orderSpecs([]engine.SpawnSpec{{Name: "a", After: []string{"ghost"}}})
	if err == nil || !strings.Contains(err.Error(), "unknown spec") {
		t.Fatalf("expected an unknown-dep error, got %v", err)
	}
}
