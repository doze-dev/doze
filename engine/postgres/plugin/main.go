// Command postgres-plugin runs the postgres engine as an out-of-process doze
// module over the engine plugin protocol. Point doze at it with
// DOZE_POSTGRES_PLUGIN=/path/to/postgres-plugin.
package main

import (
	"encoding/gob"

	"github.com/nerdmenot/doze/engine/postgres"
	dozeplugin "github.com/nerdmenot/doze/internal/plugin"
)

func main() {
	gob.Register(&postgres.Config{})
	dozeplugin.Serve(postgres.Driver{})
}
