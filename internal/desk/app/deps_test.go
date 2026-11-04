package app

import (
	"os/exec"
	"strings"
	"testing"
)

// TestOnlyThisPackageImportsWails keeps the GUI at the edge: the daemon, the
// CLI and every view model must stay testable and light without Wails, so
// nothing but this package may import it.
func TestOnlyThisPackageImportsWails(t *testing.T) {
	out, err := exec.Command("go", "list", "-tags", "purego", "-f", "{{.ImportPath}} {{join .Imports \" \"}}", "github.com/doze-dev/doze/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg, imports, _ := strings.Cut(line, " ")
		if pkg == "github.com/doze-dev/doze/internal/desk/app" {
			continue
		}
		if strings.Contains(imports, "github.com/wailsapp/") {
			t.Errorf("%s imports Wails; only internal/desk/app may", pkg)
		}
	}
}
