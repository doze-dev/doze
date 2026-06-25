// Command sns-plugin runs the local-SNS engine as an out-of-process doze module.
// The binary is dual-purpose: invoked plainly it speaks the engine plugin
// protocol; invoked as `sns-plugin __serve sns …` (what BaseDriver.Plan spawns via
// os.Executable) it runs the SNS service itself, which fans out to a backing SQS
// instance via the DOZE_SQS_SOCKET its Plan injects. Point doze at it with
// DOZE_SNS_PLUGIN=/path/to/sns-plugin.
package main

import (
	"encoding/gob"
	"fmt"
	"os"

	"github.com/nerdmenot/doze/engine/sns" // its init registers the configured Driver (incl. childEnv) + snssrv factory
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
	// Use the driver sns's init() registered (BaseDriver populated, including the
	// unexported childEnv that fans out to SQS), not a blank sns.Driver{}.
	drv, ok := engine.Lookup("sns")
	if !ok {
		fmt.Fprintln(os.Stderr, "sns driver not registered")
		os.Exit(1)
	}
	gob.Register(&sns.Config{})
	dozeplugin.Serve(drv)
}
