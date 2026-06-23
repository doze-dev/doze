package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/ui"
)

func outputCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "output [name]",
		Short: "Print declared output values",
		Long: "output prints the values declared in `output` blocks — the connection\n" +
			"strings and facts a stack exposes. With a name, prints just that value\n" +
			"(raw, for scripting); with no argument, lists them all.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			// A single named output prints its raw value, so `$(doze output url)`
			// works in scripts (sensitive values included — the user asked for it).
			if len(args) == 1 {
				out, ok := cfg.Outputs[args[0]]
				if !ok {
					return fmt.Errorf("no output named %q", args[0])
				}
				fmt.Println(out.Value)
				return nil
			}
			if len(cfg.OutputOrder) == 0 {
				fmt.Println("no outputs declared")
				return nil
			}
			for _, name := range cfg.OutputOrder {
				out := cfg.Outputs[name]
				val := out.Value
				if out.Sensitive {
					val = ui.Muted("(sensitive)")
				}
				fmt.Printf("%s = %s\n", ui.Title(name), val)
			}
			return nil
		},
	}
}
