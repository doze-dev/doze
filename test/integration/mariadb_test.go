//go:build integration

package integration

import (
	"fmt"
	"testing"
)

// mariadbQ runs a query against the doze proxy as root over TCP (the proxy forwards
// to the backend's local-trust socket, so root needs no password).
func mariadbQ(t *testing.T, addr, sql string) (string, error) {
	host, port := splitHostPort(t, addr)
	return run(t, mustTool(t, "DOZE_MARIADB_BINDIR", "mariadb"),
		"--host="+host, "--port="+port, "--user=root",
		"--batch", "--skip-column-names", "-e", sql)
}

// TestMariaDBConverge proves core -> plugin -> real MariaDB -> client works: boot
// with a declared user + grant and confirm the instance database and the user
// exist in the running backend.
func TestMariaDBConverge(t *testing.T) {
	port := FreePort(t)
	cfg := fmt.Sprintf(`module mariadb "shop" {
  version = "11.4"
  port    = %d

  user "app" {
    password = "secret"
    host     = "%%"
  }
  grant {
    user       = "app"
    privileges = ["ALL PRIVILEGES"]
    database   = "shop"
  }
}
`, port)
	p := NewProject(t, "mariadb", "shop", port, cfg)
	p.Up()

	if got, err := mariadbQ(t, p.ProxyAddr(), "SHOW DATABASES LIKE 'shop'"); err != nil || got != "shop" {
		t.Fatalf("instance database 'shop' not found: got %q err %v", got, err)
	}
	if got, err := mariadbQ(t, p.ProxyAddr(), "SELECT COUNT(*) FROM mysql.user WHERE User='app'"); err != nil || got != "1" {
		t.Fatalf("user 'app' not converged: got %q err %v", got, err)
	}
}
