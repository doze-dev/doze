//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
)

// psql runs a query against the doze proxy as the postgres superuser (the proxy
// forwards to the backend over its local-trust unix socket, so no password is
// needed). Returns the trimmed scalar output.
func psql(t *testing.T, addr, db, sql string) (string, error) {
	dsn := fmt.Sprintf("postgres://postgres@%s/%s?sslmode=disable", addr, db)
	return run(t, tool("DOZE_POSTGRES_BINDIR", "psql"), dsn, "-tAc", sql)
}

func pgConfig(name string, port int, roles ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "module postgres %q {\n  version = 16\n  port    = %d\n", name, port)
	for _, r := range roles {
		fmt.Fprintf(&b, "  role %q {}\n", r)
	}
	b.WriteString("}\n")
	return b.String()
}

// TestPostgresConverge is the end-to-end proof that core -> plugin -> real Postgres
// -> client works: boot a Postgres instance with a declared role and confirm the
// role actually exists in the running backend.
func TestPostgresConverge(t *testing.T) {
	port := FreePort(t)
	p := NewProject(t, "postgres", "shop", port, pgConfig("shop", port, "app"))
	p.Up()

	got, err := psql(t, p.ProxyAddr(), "shop", "SELECT rolname FROM pg_roles WHERE rolname='app'")
	if err != nil {
		t.Fatalf("querying pg_roles: %v (%s)", err, got)
	}
	if got != "app" {
		t.Fatalf("role 'app' not found after converge; pg_roles query returned %q", got)
	}
}

// TestPostgresReconvergeAddsRole proves the user's original bug is fixed: adding a
// role to an ALREADY-PROVISIONED instance and booting again creates the new role
// (convergence re-applies to an existing data dir, not just on first provision).
// It uses a full down+up cycle so a fresh daemon re-reads the edited config; the
// pure lazy-reconnect variant of this path is covered by the specFingerprint unit
// test and the drift boot guard.
func TestPostgresReconvergeAddsRole(t *testing.T) {
	port := FreePort(t)
	p := NewProject(t, "postgres", "shop", port, pgConfig("shop", port, "app"))
	p.Up()

	// Sanity: only "app" exists so far.
	if got, _ := psql(t, p.ProxyAddr(), "shop", "SELECT count(*) FROM pg_roles WHERE rolname IN ('app','api')"); got != "1" {
		t.Fatalf("expected exactly 1 of app/api before edit, got %q", got)
	}

	// Edit config to add a second role, then down+up so the change is picked up.
	p.Cleanup() // doze down
	p.WriteConfig(pgConfig("shop", port, "app", "api"))
	p.Up()

	got, err := psql(t, p.ProxyAddr(), "shop", "SELECT count(*) FROM pg_roles WHERE rolname IN ('app','api')")
	if err != nil {
		t.Fatalf("querying pg_roles: %v (%s)", err, got)
	}
	if got != "2" {
		t.Fatalf("added role 'api' not converged onto the existing instance; app/api count = %q", got)
	}
}
