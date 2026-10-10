package main

import (
	"fmt"
	"os"

	names "github.com/doze-dev/doze-names"
	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/ui"
)

// The one-time machine setup for .doze names belongs to doze-names, because
// the zone is shared: doze, doze-aws and doze-kafka install the same thing,
// and whichever of them a person runs first sets it up for all three. doze
// used to carry a copy of its own, which had drifted — it installed nothing on
// Linux. `doze doctor --fix` is the way in; `dns-setup` is kept as another
// name for it.

func dnsSetupCmd() *cobra.Command {
	var check, uninstall, print bool
	cmd := &cobra.Command{
		Use:   "dns-setup",
		Short: "One-time machine setup for .doze names (the same as `doze doctor --fix`)",
		Long: "dns-setup makes <service>.<stack>.doze names work on this machine: it routes\n" +
			"the .doze zone to doze's resolver and, on macOS, gives services loopback\n" +
			"addresses of their own so several can share a port such as 5432. It needs\n" +
			"sudo once. The setup is shared with doze-aws and doze-kafka: any of them\n" +
			"installs it for all.\n\n" +
			"--check reports the current state without sudo; --print shows the script\n" +
			"instead of running it; --uninstall removes it.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			switch {
			case check:
				return namesCheck()
			case uninstall:
				return names.Uninstall(names.Options{Print: print})
			default:
				return namesFix(print)
			}
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report what is set up (no sudo)")
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "remove the setup (needs sudo)")
	cmd.Flags().BoolVar(&print, "print", false, "print the privileged script instead of running it")
	return cmd
}

// namesCheck prints each part of the setup and whether it is in place.
func namesCheck() error {
	st := names.Check()
	for _, step := range st.Steps {
		mark := ui.OK("✓")
		if !step.Done {
			mark = ui.Fail("✗")
		}
		fmt.Printf("  %s  %-22s %s\n", mark, step.Name, ui.Muted(step.Detail))
	}
	if st.OK() {
		fmt.Println("\n" + ui.OK("ready") + " — services are reachable by name and can share ports")
		return nil
	}
	fmt.Println("\nrun " + ui.Title("doze doctor --fix") + " to set it up (one sudo)")
	return exitCodeError(1)
}

// namesFix runs the setup, or prints its script.
func namesFix(print bool) error {
	if names.Disabled() {
		return fmt.Errorf("names are switched off (DOZE_ZONE=off), so there is nothing to set up")
	}
	return names.Install(names.Options{Out: os.Stdout, Print: print})
}
