//go:build darwin

package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doze-dev/doze/internal/control/fixture"
)

// Removing a stale route runs the prune behind the password prompt, the
// way doze trust does, and only then forgets it; with no prompt to ask,
// nothing is forgotten.
func TestClearRoutesAsksAndForgets(t *testing.T) {
	h, _ := newHub(t, fixture.Shop(epoch))
	home := h.Home()
	gone := filepath.Join(t.TempDir(), "deleted-project") // no doze.hcl: nobody declares it
	record := `{"stale.doze-test.example": [` + `"` + gone + `"]}`
	if err := os.WriteFile(filepath.Join(home, "routes.json"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(h, nil)
	if err := s.clearRoutes([]string{"stale.doze-test.example"}); err == nil {
		t.Fatal("with no prompt it should refuse")
	}
	if b, _ := os.ReadFile(filepath.Join(home, "routes.json")); !strings.Contains(string(b), "stale.doze-test.example") {
		t.Fatal("refused, yet the record was forgotten")
	}
	var asked []string
	s.SetRepair(func(what, script string) error {
		asked = append(asked, what+"\n"+script)
		return nil // the file never existed, so it is gone either way
	})
	if err := s.clearRoutes([]string{"stale.doze-test.example"}); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || !strings.Contains(asked[0], "rm -f '/etc/resolver/stale.doze-test.example'") || !strings.Contains(asked[0], "remove the stale route") {
		t.Fatalf("asked = %q", asked)
	}
	if b, _ := os.ReadFile(filepath.Join(home, "routes.json")); strings.Contains(string(b), "stale.doze-test.example") {
		t.Errorf("the route is gone, yet still recorded: %s", b)
	}
}
