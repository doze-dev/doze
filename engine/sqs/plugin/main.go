// Command sqs-plugin runs the local-SQS engine as an out-of-process doze module.
// The binary is dual-purpose: invoked plainly it speaks the engine plugin
// protocol; invoked as `sqs-plugin __serve sqs …` (what BaseDriver.Plan spawns via
// os.Executable) it runs the SQS service itself. Point doze at it with
// DOZE_SQS_PLUGIN=/path/to/sqs-plugin.
package main

import (
	"encoding/gob"
	"fmt"
	"os"

	"github.com/nerdmenot/doze/engine/sqs" // its init registers the configured Driver + sqssrv factory
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
	// Use the driver sqs's init() registered (BaseDriver populated), not a blank
	// zero-value sqs.Driver{}.
	drv, ok := engine.Lookup("sqs")
	if !ok {
		fmt.Fprintln(os.Stderr, "sqs driver not registered")
		os.Exit(1)
	}
	gob.Register(&sqs.Config{})
	dozeplugin.Serve(drv)
}
