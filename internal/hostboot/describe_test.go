package hostboot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A module's own port comes from its __describe, asked once per binary.
func TestDefaultPortAsksTheModulesDescribeOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for the plugin")
	}
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	bin := filepath.Join(dir, "plugin")
	script := "#!/bin/sh\n[ \"$1\" = __describe ] || exit 1\necho x >> " + count + "\necho '{\"Port\": 6400}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	resolve := func(engineType string) (string, []string, bool) {
		if engineType != "valkey" {
			return "", nil, false
		}
		return bin, nil, true
	}
	port := defaultPort(resolve)
	for range 3 {
		if got := port("valkey"); got != 6400 {
			t.Fatalf("port = %d, want 6400", got)
		}
	}
	if got := port("postgres"); got != 0 {
		t.Errorf("an engine no module provides gives %d, want 0", got)
	}
	runs, _ := os.ReadFile(count)
	if n := len(runs) / 2; n != 1 {
		t.Errorf("__describe ran %d times, want once", n)
	}
}
