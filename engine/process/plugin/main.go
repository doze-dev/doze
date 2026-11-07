// Command process-plugin runs the generic process engine as an out-of-process
// doze module over the engine plugin protocol, for exercising the protocol with
// a known driver (doze itself runs the process engine in-tree).
package main

import (
	"encoding/gob"

	dozeplugin "github.com/doze-dev/doze-sdk/plugin"
	"github.com/doze-dev/doze/engine/process"
)

func main() {
	gob.Register(&process.Config{})
	dozeplugin.Serve(process.Driver{})
}
