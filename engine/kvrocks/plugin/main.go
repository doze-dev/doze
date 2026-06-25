// Command kvrocks-plugin runs the kvrocks engine as an out-of-process doze module
// over the engine plugin protocol. Point doze at it with
// DOZE_KVROCKS_PLUGIN=/path/to/kvrocks-plugin.
package main

import (
	"encoding/gob"

	"github.com/nerdmenot/doze/engine/kvrocks"
	dozeplugin "github.com/nerdmenot/doze/internal/plugin"
)

func main() {
	gob.Register(&kvrocks.Config{})
	dozeplugin.Serve(kvrocks.Driver{})
}
