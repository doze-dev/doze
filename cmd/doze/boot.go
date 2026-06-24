package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/daemon"
	"github.com/nerdmenot/doze/internal/ui"
)

func bootCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "boot [instance]",
		Short: "Boot an instance's backend now, without waiting for a connection",
		Long: "boot starts an instance's backend immediately — warming it up instead of\n" +
			"waiting for the first client connection. It ensures the daemon is running so\n" +
			"the backend is held alive and idle-reaped like any other. With no argument it\n" +
			"boots every declared instance. (Unlike `apply`, boot touches no structure.)",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			var target string
			if len(args) == 1 {
				target = args[0]
				if cfg.Lookup(target) == nil {
					return instanceNotFound(cfg, target)
				}
			}
			if !daemonRunning(cfg) {
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			client := control.NewClient(daemon.ControlSocketPath(cfg))
			for _, name := range bootTargets(cfg, target) {
				fmt.Print(ui.Muted("› booting "+name+"…") + "\r")
				if _, err := client.Do(control.Request{Op: "boot", DB: name}); err != nil {
					fmt.Println(ui.Fail("✗") + " " + name + ": " + err.Error())
					return err
				}
				fmt.Println(ui.OK("✓") + " booted " + name + "    ")
			}
			return nil
		},
	}
}

func bootTargets(cfg *config.Config, target string) []string {
	if target != "" {
		return []string{target}
	}
	names := make([]string, 0, len(cfg.Instances))
	for _, d := range cfg.Instances {
		names = append(names, d.Name)
	}
	return names
}
