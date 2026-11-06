package runtime_test

import (
	"os"
	"testing"

	"github.com/doze-dev/doze-sdk/engine"
)

// TestMain registers every test driver once, before any test runs. The engine
// registry is a plain map; registering from inside tests raced with watch
// goroutines a previous test left behind, which look drivers up.
func TestMain(m *testing.M) {
	engine.Register(leafDriver{})
	engine.Register(depDriver{})
	engine.Register(loopDriver{})
	engine.Register(steadyDriver{})
	engine.Register(shDriver{})
	engine.Register(deafDriver{})
	os.Exit(m.Run())
}
