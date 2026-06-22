package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "dev"

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the doze version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Printf("doze %s (%s/%s, %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		},
	}
}
