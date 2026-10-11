package main

import (
	"errors"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"os"
)

func dashCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dash",
		Short: "The live dashboard (being rebuilt)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("the dashboard is being rebuilt; `doze status`, `doze logs` and `doze task` cover it meanwhile")
		},
	}
}

// isTerminal reports whether f is attached to an interactive terminal. Not a
// ModeCharDevice check: /dev/null is a char device and must count as non-TTY.
func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd())
}
