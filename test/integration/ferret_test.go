//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

// TestFerretConvergeAndSeed proves core -> plugin -> real FerretDB -> mongo client
// works: boot with a declared database + a seeded collection and confirm the seed
// documents are present (and that a second boot does not duplicate them).
//
// FerretDB ships no mongosh, so the assertion uses mongosh from PATH and skips if
// it is absent — the convergence itself (mongo Go driver) runs inside the plugin.
func TestFerretConvergeAndSeed(t *testing.T) {
	mongosh := mustTool(t, "DOZE_FERRET_BINDIR", "mongosh")

	port := FreePort(t)
	cfg := fmt.Sprintf(`module ferret "shop" {
  version = "2.7"
  port    = %d

  database "catalog" {
    collection "products" { seed = "./products.json" }
  }
}
`, port)
	p := NewProject(t, "ferret", "shop", port, cfg)
	// Seed file, resolved relative to the config dir by the ferret decoder.
	p.WriteFile("products.json", `[{"_id":1,"name":"widget"},{"_id":2,"name":"gadget"}]`)
	p.Up()

	uri := fmt.Sprintf("mongodb://127.0.0.1:%d/catalog", port)
	count := func() string {
		out, err := run(t, mongosh, uri, "--quiet", "--eval", "db.products.countDocuments()")
		if err != nil {
			t.Fatalf("mongosh count: %v (%s)", err, out)
		}
		return strings.TrimSpace(out)
	}
	if got := count(); got != "2" {
		t.Fatalf("seeded products count = %q, want 2", got)
	}

	// Re-boot: seeding must be idempotent (only seeds an empty collection).
	p.Cleanup()
	p.Up()
	if got := count(); got != "2" {
		t.Fatalf("after re-boot products count = %q, want 2 (seed must not duplicate)", got)
	}
}
