package display

import (
	"fmt"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
)

// The words beside an instance: how its state reads, what it is, what it is
// doing about it, and where to reach it. The dash and the app both say these,
// so they are chosen here and painted there.

// Tone is the colour class a piece of text is drawn in: a surface maps each to
// its own palette. They are the board's classes, not colours, so a theme can
// change what they look like and not what they mean.
type Tone string

// The tones. Bright is the brightest text, for a name or a figure that is the
// point of its line; Text is ordinary; Dim is the rest.
const (
	ToneGood   Tone = "good"
	ToneWarn   Tone = "warn"
	ToneCool   Tone = "cool"
	ToneBad    Tone = "bad"
	ToneDim    Tone = "dim"
	ToneText   Tone = "text"
	ToneBright Tone = "bright"
)

// Look is how an instance's state reads: the word, the glyph and the tone they
// are drawn in.
type Look struct {
	Word, Glyph string
	Tone        Tone
}

// LookOf is the look of an instance. A task has its own words (running, done,
// failed, ready) because it does not sleep; the rest follow the daemon's state.
func LookOf(v control.InstanceView) Look {
	switch {
	case v.Disabled:
		return Look{"disabled", "⊘", ToneDim}
	case v.Kind == "task" && v.State == "booting":
		return Look{"running", "◌", ToneCool}
	case v.Task != nil && v.State != "booting":
		if v.Task.Result == "failed" {
			return Look{"failed", "✗", ToneBad}
		}
		return Look{"done", "✓", ToneGood}
	case v.Kind == "task" && v.State != "booting":
		return Look{"ready", "▸", ToneDim}
	}
	switch v.State {
	case "active":
		return Look{"awake", "●", ToneGood}
	case "idle":
		return Look{"dozing", "◐", ToneWarn}
	case "booting":
		return Look{"waking", "◌", ToneCool}
	case "crashing":
		return Look{"crashing", "✗", ToneBad}
	case "failed", "error":
		return Look{"failed", "✗", ToneBad}
	}
	return Look{"asleep", "·", ToneDim}
}

// AwakeNow says the instance is up and serving: awake or dozing. Unlike Awake,
// which counts a process that is still booting, this is the screens' sense of
// the word: a figure of memory, a column of connections.
func AwakeNow(v control.InstanceView) bool { return v.State == "active" || v.State == "idle" }

// Troubled says something is wrong with it: it is crashing, has failed, or is
// a task whose last run failed.
func Troubled(v control.InstanceView) bool {
	return v.State == "crashing" || v.State == "failed" || v.State == "error" || (v.Task != nil && v.Task.Result == "failed")
}

// What is the third column: what the thing is, then what it is doing about it.
func What(v control.InstanceView, snap control.Snapshot, now time.Time) string {
	var base string
	switch v.Kind {
	case "app", "service":
		base = ShortRun(v.Run)
	case "task":
		if v.Task != nil {
			base = fmt.Sprintf("ran %s ago in %s", Ago(now.Sub(v.Task.RanAt)), (time.Duration(v.Task.TookMS) * time.Millisecond).Round(time.Millisecond))
			if v.Task.Result == "failed" {
				base = fmt.Sprintf("exit %d · %s", v.Task.ExitCode, base)
			}
		} else {
			base = "not run yet"
		}
	default:
		base = strings.TrimSpace(v.Engine + " " + strings.TrimPrefix(v.Version, "builtin"))
	}
	var extra string
	switch {
	case Troubled(v) && v.Task == nil:
		// What went wrong is the long part, so it goes last and is the part that
		// gets cut.
		base = FirstLine(v.LastError)
		if v.RestartMax > 0 {
			base, extra = fmt.Sprintf("try %d of %d", v.RestartCount, v.RestartMax), FirstLine(v.LastError)
		}
	case v.State == "booting":
		if n := FirstUnmet(v, snap); n != "" {
			base, extra = "waiting on "+n, ""
		} else if by := HolderNamed(v); by != "" {
			extra = "woken by " + by
		}
	case v.State == "idle" && !v.IdleSince.IsZero() && !v.AlwaysOn:
		after := snap.IdleTimeout
		if v.IdleAfter > 0 {
			after = v.IdleAfter
		}
		if left := after - now.Sub(v.IdleSince); left > 0 {
			extra = "sleeps in " + Clock(left)
		}
	}
	switch {
	case base == "":
		return extra
	case extra == "":
		return base
	}
	return base + " · " + extra
}

// ShortRun reads a command line the way the board does: what it runs, not the
// whole line. A container command says which image.
func ShortRun(run string) string {
	f := strings.Fields(run)
	if len(f) >= 2 && (f[0] == "docker" || f[0] == "podman") && f[1] == "run" {
		image := ""
		// the image is the first word that is neither a flag nor a flag's value
		takes := map[string]bool{"-p": true, "--publish": true, "-e": true, "--env": true, "--env-file": true, "-v": true,
			"--volume": true, "--name": true, "--network": true, "--net": true, "-w": true, "--workdir": true, "-u": true,
			"--user": true, "--entrypoint": true, "--platform": true, "-l": true, "--label": true, "--add-host": true,
			"-m": true, "--memory": true, "--cpus": true, "-h": true, "--hostname": true, "--mount": true}
		for i := 2; i < len(f); i++ {
			if a := f[i]; strings.HasPrefix(a, "-") {
				if takes[a] && !strings.Contains(a, "=") {
					i++
				}
			} else {
				image = a
				break
			}
		}
		if image == "" {
			return f[0] + " run"
		}
		return f[0] + " run " + image
	}
	return run
}

// FirstUnmet is the first thing v needs that is not up yet.
func FirstUnmet(v control.InstanceView, snap control.Snapshot) string {
	for _, n := range v.Needs {
		if o, ok := Find(snap, n); ok && !AwakeNow(o) && (o.Task == nil || o.Task.Result != "done") {
			return n
		}
	}
	return ""
}

// HolderNamed is the first dependant holding v awake, or "".
func HolderNamed(v control.InstanceView) string {
	for _, h := range v.Holders {
		if h.Kind == "dependant" {
			return h.Name
		}
	}
	return ""
}

// Mem is the memory column: a figure, a dash while it wakes, 0 asleep, and
// nothing for a task.
func Mem(v control.InstanceView) string {
	switch {
	case v.Kind == "task":
		return ""
	case v.State == "booting":
		return "—"
	case AwakeNow(v):
		return MB(v.RAM)
	}
	return "0"
}

// URL is where to reach it: the connection string of a module, the name of an
// app with the port it listens on behind it. port is "" unless the name stands
// in front of a port, when it is ":8080", drawn dim after the address.
func URL(v control.InstanceView) (addr, port string) {
	switch {
	case v.URL != "":
		return v.URL, ""
	case v.Resource != "":
		return v.Resource, ""
	case v.Domain != "":
		if _, p, ok := strings.Cut(v.Bind, ":"); ok && !strings.HasPrefix(v.Bind, "unix:") {
			return "http://" + v.Domain, ":" + p
		}
		return "http://" + v.Domain, ""
	case v.Endpoint != "":
		return v.Endpoint, ""
	}
	return "", ""
}

// Need is one thing an instance waits on, with how that thing is.
type Need struct {
	Name  string
	Known bool // it is in the stack; an unknown name is drawn as it is
	Look  Look
	Glyph string // the look's glyph, or ✓ when it is up or done
	Task  bool   // it is a task that has run
}

// Needs is each thing v waits on, with how it is.
func Needs(v control.InstanceView, snap control.Snapshot) []Need {
	var out []Need
	for _, n := range v.Needs {
		o, ok := Find(snap, n)
		if !ok {
			out = append(out, Need{Name: n})
			continue
		}
		lk := LookOf(o)
		glyph := lk.Glyph
		if lk.Word == "awake" || lk.Word == "done" {
			glyph = "✓"
		}
		out = append(out, Need{Name: n, Known: true, Look: lk, Glyph: glyph, Task: o.Task != nil})
	}
	return out
}

// Address is where a client connects: the name and port where there is a name,
// else the address itself. "" when there is nothing to connect to.
func Address(v control.InstanceView) string {
	if v.Endpoint == "" {
		return ""
	}
	if v.Domain != "" {
		if _, port, ok := strings.Cut(v.Endpoint, ":"); ok && !strings.HasPrefix(v.Endpoint, "unix:") {
			return v.Domain + ":" + port
		}
		return v.Domain
	}
	return strings.TrimPrefix(v.Endpoint, "http://")
}

// AddressOf is what to paste for an instance: the address alone. A page draws
// "http://api.shop.doze → :8080", which is a description, not something that
// can be pasted into a browser.
func AddressOf(v control.InstanceView) string {
	switch {
	case v.URL != "":
		return v.URL
	case v.Resource != "":
		return v.Resource
	case v.Domain != "":
		return "http://" + v.Domain
	}
	return v.Endpoint
}

// Hint is the part of what a row says that is about its moment: that it waits
// on something, that it sleeps soon, who woke it.
func Hint(v control.InstanceView, snap control.Snapshot, now time.Time) string {
	for _, h := range v.Holders {
		if h.Kind == "pin" {
			return "pinned awake"
		}
	}
	switch {
	case v.State == "booting":
		if n := FirstUnmet(v, snap); n != "" {
			return "waiting on " + n
		}
		if by := HolderNamed(v); by != "" {
			return "woken by " + by
		}
	case v.State == "idle" && !v.IdleSince.IsZero():
		after := snap.IdleTimeout
		if v.IdleAfter > 0 {
			after = v.IdleAfter
		}
		if left := after - now.Sub(v.IdleSince); left > 0 {
			return "sleeps in " + Clock(left)
		}
	}
	return ""
}
