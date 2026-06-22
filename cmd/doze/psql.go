package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/control"
	"github.com/nerdmenot/doze/internal/daemon"
	"github.com/nerdmenot/doze/internal/endpoints"
	"github.com/nerdmenot/doze/internal/runtime"
)

func psqlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "psql <instance> [-- psql args...]",
		Short: "Open a psql shell to a postgres instance (booting it if cold)",
		Long: "psql opens an interactive shell to a postgres instance, booting it if cold.\n" +
			"Any arguments after the name are passed through to psql, e.g.\n" +
			"`doze psql shop -- -c 'SELECT 1'`.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			extra := args[1:]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			// Daemon up: connect through the instance's proxy endpoint, which
			// exercises the real lazy-boot path and keeps the backend warm.
			client := control.NewClient(daemon.ControlSocketPath(cfg))
			if client.Available() {
				addr, err := endpoints.ClientAddr(cfg, name)
				if err != nil {
					return err
				}
				host, port := splitClientAddr(addr)
				return runPsql("psql", host, port, name, extra)
			}

			// Standalone: boot the backend directly, attach psql, then reap.
			if cfg.Lookup(name) == nil {
				return instanceNotFound(cfg, name)
			}
			rt, err := runtime.New(cfg)
			if err != nil {
				return err
			}
			rt.SetLogger(stderrLogger)
			if err := rt.EnsureDataRoot(); err != nil {
				return err
			}
			ctx := context.Background()
			ep, err := rt.Boot(ctx, name)
			if err != nil {
				return err
			}
			defer rt.Stop(context.Background(), name)

			tc, err := rt.ResolveToolchain(ctx, name)
			if err != nil {
				return err
			}
			// psql -h <dir> finds .s.PGSQL.<port> in the backend's socket dir.
			return runPsql(tc.Path("psql"), filepath.Dir(ep.Backend), "", name, extra)
		},
	}
	cmd.Flags().SetInterspersed(false)
	return cmd
}

// splitClientAddr converts a client address into psql -h/-p arguments.
func splitClientAddr(addr string) (host, port string) {
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		return filepath.Dir(path), ""
	}
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, ""
	}
	return h, p
}

func runPsql(bin, host, port, db string, extra []string) error {
	args := []string{"-h", host, "-U", "postgres", "-d", db}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, extra...)
	c := exec.Command(bin, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
