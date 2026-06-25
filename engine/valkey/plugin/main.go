// Command valkey-plugin runs the valkey engine as an out-of-process doze module:
// it serves the in-tree valkey.Driver over the engine plugin protocol. Build it and
// point doze at it with DOZE_VALKEY_PLUGIN=/path/to/valkey-plugin.
package main

import (
	"encoding/gob"

	"github.com/nerdmenot/doze/engine/valkey"
	dozeplugin "github.com/nerdmenot/doze/internal/plugin"
)

func main() {
	// The engine config crosses the wire as gob, so its concrete type is registered.
	gob.Register(&valkey.Config{})
	dozeplugin.Serve(valkey.Driver{})
}
