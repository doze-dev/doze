// Command s3-plugin runs the local-S3 engine as an out-of-process doze module.
// The binary is dual-purpose: invoked plainly it speaks the engine plugin
// protocol; invoked as `s3-plugin __serve s3 …` (what BaseDriver.Plan spawns via
// os.Executable) it runs the S3 service itself. Point doze at it with
// DOZE_S3_PLUGIN=/path/to/s3-plugin.
package main

import (
	"encoding/gob"
	"fmt"
	"os"

	"github.com/nerdmenot/doze/engine/s3" // its init registers the configured Driver + s3srv factory
	"github.com/nerdmenot/doze/internal/awslocal"
	"github.com/nerdmenot/doze/internal/engine"
	dozeplugin "github.com/nerdmenot/doze/internal/plugin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__serve" {
		if err := awslocal.ServeFromArgs(os.Args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	// Use the driver s3's init() registered (BaseDriver populated with Name/endpoint),
	// not a zero-value s3.Driver{} whose embedded BaseDriver would be blank.
	drv, ok := engine.Lookup("s3")
	if !ok {
		fmt.Fprintln(os.Stderr, "s3 driver not registered")
		os.Exit(1)
	}
	gob.Register(&s3.Config{})
	dozeplugin.Serve(drv)
}
