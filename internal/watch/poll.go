// Package watch notices when files change, for `watch` on an app and for the
// inputs of the tasks it waits for.
//
// It polls: once an interval it lists the files the globs match and compares
// their size and modification time with the last look. That is not what a
// kernel notification would do, and it is chosen on purpose. On macOS the
// notification API holds a file descriptor for every watched file, which a
// large repository runs out of; and a poll costs a stat per matched file, once
// a second, only while the app is awake, with no dependency to carry. A change
// is reported after a quiet interval, so a save that writes twice, or a
// checkout that touches a hundred files, is one event.
package watch

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/doze-dev/doze/internal/inputs"
)

// Set is one directory and the globs, relative to it, that matter in it.
type Set struct {
	Dir   string
	Globs []string
}

// DefaultInterval is how often the files are looked at.
const DefaultInterval = time.Second

// snapshot maps an absolute path to its size and modification time.
func snapshot(sets []Set) map[string]string {
	out := map[string]string{}
	for _, s := range sets {
		files, err := inputs.Files(s.Dir, s.Globs)
		if err != nil {
			continue
		}
		for _, rel := range files {
			p := filepath.Join(s.Dir, filepath.FromSlash(rel))
			if fi, err := os.Stat(p); err == nil {
				out[p] = strconv.FormatInt(fi.Size(), 10) + "/" + strconv.FormatInt(fi.ModTime().UnixNano(), 10)
			}
		}
	}
	return out
}

// changed returns the paths that differ between two snapshots, sorted.
func changed(prev, cur map[string]string) []string {
	var out []string
	for p, sig := range cur {
		if prev[p] != sig {
			out = append(out, p)
		}
	}
	for p := range prev {
		if _, ok := cur[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Poll calls onChange with the first changed path once the files have been
// quiet for an interval, until ctx ends. onChange runs on the polling
// goroutine, so nothing is looked at while it runs.
func Poll(ctx context.Context, sets []Set, interval time.Duration, onChange func(path string)) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	prev := snapshot(sets)
	var pending []string
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cur := snapshot(sets)
		diff := changed(prev, cur)
		prev = cur
		switch {
		case len(diff) > 0:
			pending = append(pending, diff...) // still changing: wait for quiet
		case len(pending) > 0:
			path := pending[0]
			pending = nil
			onChange(path)
			prev = snapshot(sets) // what the reaction wrote is not a change
		}
	}
}
