package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/daemon"
)

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down [db]",
		Short: "Stop a running backend (or all of them)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			var target string
			if len(args) == 1 {
				target = args[0]
			}

			client := control.NewClient(daemon.ControlSocketPath(cfg))
			if client.Available() {
				if _, err := client.Do(control.Request{Op: "down", DB: target}); err != nil {
					return err
				}
				fmt.Println("stopped", orAll(target))
				return nil
			}

			// No daemon: signal backends directly via their postmaster.pid.
			names := []string{target}
			if target == "" {
				names = names[:0]
				for _, decl := range cfg.Instances {
					names = append(names, decl.Name)
				}
			}
			stopped := 0
			for _, name := range names {
				ok, err := stopByPidFile(cfg, name)
				if err != nil {
					return err
				}
				if ok {
					fmt.Println("stopped", name)
					stopped++
				}
			}
			if stopped == 0 {
				fmt.Println("nothing running")
			}
			return nil
		},
	}
}

// stopByPidFile sends SIGINT (fast shutdown) to a backend identified by its
// data dir's postmaster.pid, when no daemon is available to do it for us.
func stopByPidFile(cfg *config.Config, name string) (bool, error) {
	pidPath := filepath.Join(cfg.ClusterDir(name), "postmaster.pid")
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	line := strings.SplitN(string(raw), "\n", 2)[0]
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		return false, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}
	if err := proc.Signal(syscall.SIGINT); err != nil {
		return false, nil
	}
	return true, nil
}

func orAll(target string) string {
	if target == "" {
		return "all instances"
	}
	return target
}
