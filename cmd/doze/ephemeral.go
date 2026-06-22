package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/endpoints"
	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/proxy"
	"github.com/nerdmenot/doze/internal/runtime"
)

func ephemeralCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ephemeral <instance> [-- command...]",
		Short: "Boot a throwaway clone of an instance, then destroy it",
		Long: "ephemeral spins up a disposable copy of a declared instance (instant on\n" +
			"copy-on-write filesystems), injects its connection string, runs the given\n" +
			"command (or waits until Ctrl-C), then reaps the backend and deletes its\n" +
			"data. Ideal for an isolated, real database per test run.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := args[1:]
			// With a positional before it, cobra leaves the "--" separator in args.
			if len(command) > 0 && command[0] == "--" {
				command = command[1:]
			}
			code, err := runEphemeral(args[0], command)
			if err != nil {
				return err
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func runEphemeral(instance string, command []string) (int, error) {
	cfg, err := loadConfig()
	if err != nil {
		return 0, err
	}
	base := cfg.Lookup(instance)
	if base == nil {
		return 0, instanceNotFound(cfg, instance)
	}
	drv, ok := engine.Lookup(base.Type)
	if !ok {
		return 0, fmt.Errorf("no driver for engine %q", base.Type)
	}

	ephName := instance + "-eph-" + randSuffix()
	cfg.Add(&config.InstanceDecl{Type: base.Type, Name: ephName, Version: base.Version, Spec: base.Spec})

	rt, err := runtime.New(cfg)
	if err != nil {
		return 0, err
	}
	rt.SetLogger(stderrLogger)
	if err := rt.EnsureDataRoot(); err != nil {
		return 0, err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Always reap and delete the ephemeral data on the way out.
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = rt.Stop(sctx, ephName)
		_ = os.RemoveAll(cfg.ClusterDir(ephName))
		_ = os.RemoveAll(cfg.SocketDir(ephName))
		fmt.Fprintf(os.Stderr, "doze: destroyed ephemeral %s\n", ephName)
	}()

	// A transient proxy listener bridges TCP clients to the backend unix socket.
	ln, err := proxy.Listen("127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	px := proxy.New(rt)
	px.SetLogger(stderrLogger)
	go func() { _ = px.ServeInstance(ctx, ln, ephName, drv) }()

	// Boot + converge now so the instance is ready before the command runs.
	if err := rt.Up(ctx, ephName); err != nil {
		return 0, err
	}

	addr := ln.Addr().String()
	envVar, url := drv.ConnString(engine.Instance{Name: ephName, Type: base.Type, Spec: base.Spec}, engine.Endpoint{TCPAddr: addr})
	vars := endpoints.EnvVars([]endpoints.Endpoint{
		{Name: ephName, Engine: base.Type, Address: addr, EnvVar: envVar, URL: url},
	})

	if len(command) == 0 {
		fmt.Printf("ephemeral %s/%s ready:\n", base.Type, ephName)
		for k, v := range vars {
			fmt.Printf("  %s=%s\n", k, v)
		}
		fmt.Println("(press Ctrl-C to destroy)")
		<-ctx.Done()
		return 0, nil
	}

	child := exec.CommandContext(ctx, command[0], command[1:]...)
	child.Env = os.Environ()
	for k, v := range vars {
		child.Env = append(child.Env, k+"="+v)
	}
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = child.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return 0, err
}

func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
