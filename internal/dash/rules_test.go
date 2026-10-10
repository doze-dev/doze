package dash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// phases.md exit measure: no file in internal/dash reaches the daemon except
// through the control.Daemon interface. A page that built a socket client
// could not be driven by fakedaemon, and so could not be tested the way the
// rest are.
func TestNoFileOpensASocket(t *testing.T) {
	forbidden := []string{"control.NewClient", "control.Client", ".Dial(", "net.Dial", "net.Listen", "daemon.ControlSocketPath"}
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, f := range forbidden {
			if strings.Contains(string(src), f) {
				t.Errorf("%s uses %s: reach the daemon through control.Daemon", path, f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
