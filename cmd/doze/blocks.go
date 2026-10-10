package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/ui"
)

// blockChange is one block that differs from what the running daemon started with.
type blockChange struct {
	name, kind, what string // what: "changed", "new", "removed"
}

// diffBlocks compares the file as it is now with the blocks a daemon recorded
// when it started. It returns nil when there is no daemon to compare with.
func diffBlocks(cfg *config.Config) (changes []blockChange, unchanged int, ok bool) {
	if !daemonRunning(cfg) {
		return nil, 0, false
	}
	raw, err := os.ReadFile(filepath.Join(cfg.RunDir(), daemon.BlocksFile))
	if err != nil {
		return nil, 0, false
	}
	var then map[string]config.BlockPrint
	if json.Unmarshal(raw, &then) != nil {
		return nil, 0, false
	}
	now := cfg.BlockFingerprints()
	for name, n := range now {
		t, had := then[name]
		switch {
		case !had:
			changes = append(changes, blockChange{name, n.Kind, "new"})
		case t.Fingerprint != n.Fingerprint:
			changes = append(changes, blockChange{name, n.Kind, "changed"})
		default:
			unchanged++
		}
	}
	for name, t := range then {
		if _, still := now[name]; !still {
			changes = append(changes, blockChange{name, t.Kind, "removed"})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].name < changes[j].name })
	return changes, unchanged, true
}

// renderBlocks says which blocks differ from the running daemon and what
// applying them does. The daemon reads doze.hcl once, when it starts, so
// applying a change to a block means starting it again.
func renderBlocks(cfg *config.Config) {
	changes, unchanged, ok := diffBlocks(cfg)
	if !ok || len(changes) == 0 {
		return
	}
	fmt.Println("doze.hcl differs from what the running daemon started with:")
	fmt.Println()
	for _, c := range changes {
		sym, effect := ui.Warn("~"), ""
		switch c.what {
		case "new":
			sym, effect = ui.OK("+"), "starts when something uses it"
		case "removed":
			sym, effect = ui.Fail("-"), "stops; its data is kept"
		default:
			effect = "restarts"
			if c.kind == config.KindModule {
				effect = "restarts, and its structure is re-applied"
			}
			if c.kind == config.KindTask {
				effect = "runs again if its inputs changed"
			}
		}
		fmt.Printf("  %s %-14s %-9s %-8s %s\n", sym, c.name, c.kind, c.what, ui.Muted(effect))
	}
	if unchanged > 0 {
		fmt.Printf("\n  %d block%s unchanged\n", unchanged, map[bool]string{true: "", false: "s"}[unchanged == 1])
	}
	fmt.Println()
	fmt.Println(ui.Muted("  the daemon reads doze.hcl when it starts: ") + "doze down && doze up" + ui.Muted(" applies this (data is kept)"))
	fmt.Println()
}
