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
	"github.com/nerdmenot/doze/internal/endpoints"
	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/ui"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"ls"},
		Short:   "List instances and their state",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			client := control.NewClient(daemon.ControlSocketPath(cfg))
			if client.Available() {
				resp, err := client.Do(control.Request{Op: "status"})
				if err != nil {
					return err
				}
				fmt.Printf("daemon: listening on %s\n\n", resp.Listen)
				renderTable(resp.Instances)
				return nil
			}

			fmt.Println("daemon: not running (showing on-disk state)")
			fmt.Println()
			renderTable(diskStatus(cfg))
			return nil
		},
	}
}

// diskStatus reconstructs instance state from disk when no daemon is running.
func diskStatus(cfg *config.Config) []control.InstanceView {
	addrs := map[string]string{}
	if eps, err := endpoints.For(cfg); err == nil {
		for _, ep := range eps {
			addrs[ep.Name] = ep.Address
		}
	}
	var out []control.InstanceView
	for _, decl := range cfg.Instances {
		view := control.InstanceView{
			Name: decl.Name, Engine: decl.Type, Version: decl.Version.String(),
			Declared: true, State: "reaped", Endpoint: addrs[decl.Name],
		}
		dataDir := cfg.ClusterDir(decl.Name)
		// PG_VERSION / postmaster.pid are Postgres-specific; only meaningful there.
		if decl.Type == "postgres" {
			if _, err := os.Stat(filepath.Join(dataDir, "PG_VERSION")); err != nil {
				view.State = "not provisioned"
			} else if pid := livePid(dataDir); pid != 0 {
				view.State = "running"
				view.PID = pid
			}
		}
		out = append(out, view)
	}
	return out
}

// livePid returns the pid from a data dir's postmaster.pid if alive, else 0.
func livePid(dataDir string) int {
	raw, err := os.ReadFile(filepath.Join(dataDir, "postmaster.pid"))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0]))
	if err != nil || pid <= 0 {
		return 0
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return 0
	}
	return pid
}

func renderTable(views []control.InstanceView) {
	if len(views) == 0 {
		fmt.Println("no instances declared")
		return
	}
	header := []string{"NAME", "ENGINE", "VERSION", "STATE", "CONNS", "CPU", "RAM", "UPTIME", "ENDPOINT", "PID"}
	pids := make([]int, 0, len(views))
	for _, v := range views {
		if v.PID != 0 {
			pids = append(pids, v.PID)
		}
	}
	stats := ui.ProcStats(pids)

	var failed []control.InstanceView
	rowFor := func(v control.InstanceView) []string {
		state := v.State
		flagged := false
		if v.LastError != "" && (v.State == "reaped" || v.State == "") {
			state, flagged = "error", true
		}
		// A running-but-tainted instance is serving with known-incomplete structure
		// (its last converge failed) — flag it so it never looks healthy.
		if v.Tainted {
			state, flagged = "tainted", true
		}
		if flagged {
			failed = append(failed, v)
		}
		pid, ram, cpu := "", "", ""
		if v.PID != 0 {
			pid = strconv.Itoa(v.PID)
			st := stats[v.PID]
			ram, cpu = ui.HumanBytes(st.RSS), ui.CPUStr(st.CPU)
		}
		return []string{
			v.Name, v.Engine, v.Version, ui.State(state),
			strconv.Itoa(v.Conns), cpu, ram, ui.Uptime(v.StartedAt), v.Endpoint, pid,
		}
	}

	// Group by engine category so the list reads by concern (databases, caches,
	// queues, storage, your services) instead of as a flat dump.
	byCat := map[string][]control.InstanceView{}
	for _, v := range views {
		byCat[instanceGroup(v)] = append(byCat[instanceGroup(v)], v)
	}
	var rows [][]string
	first := true
	for _, cat := range engine.CategoryOrder {
		group := byCat[cat]
		if len(group) == 0 {
			continue
		}
		if !first {
			rows = append(rows, make([]string, len(header))) // blank separator row
		}
		first = false
		rows = append(rows, sectionRow(len(header), cat))
		for _, v := range group {
			rows = append(rows, rowFor(v))
		}
	}

	fmt.Println(ui.Table(header, rows))
	for _, v := range failed {
		msg := v.LastError
		if msg == "" && v.Tainted {
			msg = "structure incomplete — run `doze apply` to re-converge"
		}
		fmt.Printf("  %s %s: %s\n", ui.Fail("✗"), v.Name, msg)
	}
}

// instanceGroup is the heading an instance falls under (an explicit group wins;
// otherwise its engine category).
func instanceGroup(v control.InstanceView) string {
	if v.Group != "" {
		return v.Group
	}
	return engine.Category(v.Engine)
}

// sectionRow is a styled category heading row spanning the table's NAME column.
func sectionRow(cols int, label string) []string {
	r := make([]string, cols)
	r[0] = ui.Muted("▸ " + label)
	return r
}
