package doze_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	doze "github.com/doze-dev/doze"
)

// TestEngineSchema proves config-key discovery: resolve a module's plugin and
// read its schema locally (no registry), so a UI or validator knows what an
// AddModule accepts. Uses the kafka module built from the workspace.
func TestEngineSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module plugin")
	}
	bin := filepath.Join(t.TempDir(), "kafka-plugin")
	build := exec.Command("go", "build", "-o", bin, "github.com/doze-dev/doze-modules/modules/kafka/plugin")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build kafka plugin (needs the workspace): %v\n%s", err, out)
	}
	t.Setenv("DOZE_KAFKA_PLUGIN", bin)

	base, err := os.MkdirTemp("/tmp", "dz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)

	sc, err := doze.EngineSchema(doze.Options{Home: base}, "kafka")
	if err != nil {
		t.Fatalf("EngineSchema: %v", err)
	}
	if sc.Engine != "kafka" || sc.Category != "queue" || sc.Port != 9092 {
		t.Fatalf("schema header = %+v", sc)
	}
	// The .Set() keys are discoverable, with types.
	args := map[string]doze.SchemaArg{}
	for _, a := range sc.Args {
		args[a.Name] = a
	}
	if a, ok := args["auto_create_topics"]; !ok || a.Type != "bool" {
		t.Fatalf("auto_create_topics arg = %+v (present=%v)", a, ok)
	}
	if a, ok := args["version"]; !ok || !a.Required {
		t.Fatalf("version arg should be required: %+v", a)
	}
	// Nested block is discoverable too.
	found := false
	for _, b := range sc.Blocks {
		if b.Name == "topic" {
			found = true
		}
	}
	if !found {
		t.Fatalf("topic block missing from schema: %+v", sc.Blocks)
	}
}
