package main

import (
	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/awslocal"
)

// serveInternalCmd is the hidden worker the local-AWS engines (s3/sqs/sns) spawn
// via BaseDriver.Spawn: `doze __serve <service> --socket <path> --datadir <dir>`.
// It runs the named service (built into doze) on a unix socket until the daemon
// stops it. Not for direct use.
func serveInternalCmd() *cobra.Command {
	var socket, datadir string
	c := &cobra.Command{
		Use:    "__serve <service>",
		Short:  "Internal: run a built-in AWS service (s3|sqs|sns) on a socket",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return awslocal.Serve(args[0], socket, datadir)
		},
	}
	c.Flags().StringVar(&socket, "socket", "", "unix socket to listen on")
	c.Flags().StringVar(&datadir, "datadir", "", "service data directory")
	return c
}
