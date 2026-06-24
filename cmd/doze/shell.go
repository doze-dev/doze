package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/endpoints"
)

func shellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "shell <instance> [-- client args...]",
		Aliases: []string{"psql"},
		Short:   "Open an interactive client shell to an instance (booting it if cold)",
		Long: "shell opens the right CLI for an instance's engine — psql for postgres,\n" +
			"redis-cli for valkey/kvrocks, mongosh for documentdb — connected through\n" +
			"doze's endpoint, booting the backend on connect. Arguments after the name\n" +
			"pass through to the client, e.g. `doze shell app -- -c 'SELECT 1'`.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			extra := args[1:]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			decl := cfg.Lookup(name)
			if decl == nil {
				return instanceNotFound(cfg, name)
			}
			// Ensure the daemon is up so the connection boots and holds the backend,
			// then connect the client to the instance's endpoint.
			if !daemonRunning(cfg) {
				if err := startDaemon(cfg); err != nil {
					return err
				}
			}
			addr, err := endpoints.ClientAddr(cfg, name)
			if err != nil {
				return err
			}
			c, err := shellClient(decl.Type, addr, name, extra)
			if err != nil {
				return err
			}
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			return c.Run()
		},
	}
	cmd.Flags().SetInterspersed(false)
	return cmd
}

// shellClient builds the interactive client command for an engine, connecting to
// addr (a "host:port" or "unix:/path" doze endpoint). Engines without a shell
// (s3/sqs/sns) return an error.
func shellClient(engineType, addr, name string, extra []string) (*exec.Cmd, error) {
	host, port := splitClientAddr(addr)
	unixSock := strings.TrimPrefix(addr, "unix:")
	isUnix := strings.HasPrefix(addr, "unix:")
	switch engineType {
	case "postgres":
		a := []string{"-h", host, "-U", "postgres", "-d", name}
		if port != "" {
			a = append(a, "-p", port)
		}
		return exec.Command("psql", append(a, extra...)...), nil
	case "valkey", "kvrocks":
		a := []string{"-h", host, "-p", port}
		if isUnix {
			a = []string{"-s", unixSock}
		}
		return exec.Command("redis-cli", append(a, extra...)...), nil
	case "documentdb":
		uri := "mongodb://" + host
		if port != "" {
			uri += ":" + port
		}
		uri += "/"
		return exec.Command("mongosh", append([]string{uri}, extra...)...), nil
	default:
		return nil, fmt.Errorf("no interactive shell for engine %q (try `doze run` or the service's own CLI)", engineType)
	}
}

// splitClientAddr converts a client address into a host and port. A unix address
// yields the socket's directory as host and an empty port.
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
