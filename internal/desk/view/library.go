package view

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Library is the first tab, as the Library board lays it out: every stack
// doze knows, running first with live figures, stopped below with when
// they last ran; the help strip while it is still useful; the machine
// line. Nothing here comes from the page: the hub knows the stacks, the
// project records know the stopped ones, prefs knows the strip.
type Library struct {
	Stacks []LibStack `json:"stacks"`
	// Counts is the head's line: "4 stacks · 2 up".
	Counts string `json:"counts"`
	// Steps is the help strip, nil once dismissed or all three are done.
	Steps []Step `json:"steps,omitempty"`
	// Machine is the line at the bottom: each part of the setup and
	// whether it is in place. Filled by the service, which knows the
	// machine.
	Machine []Check `json:"machine"`
	// Doctor is the same setup part by part, as Settings lists it.
	Doctor []Check `json:"doctor"`
	// Repairable says Repair can put the failing parts in place from
	// here: one is missing, and this face can ask for an administrator's
	// password. Repairing says it is doing so now.
	Repairable bool `json:"repairable,omitempty"`
	Repairing  bool `json:"repairing,omitempty"`
}

// LibStack is one row of the library.
type LibStack struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Running bool   `json:"running"`
	Live    bool   `json:"live"`
	// Dot is the row's dot: g (up and well), r (something needs you), y
	// (up but the daemon is not answering), "" (stopped).
	Dot string `json:"dot"`
	// Declared counts what doze.hcl declares, tasks aside.
	Declared int `json:"declared"`
	// Fig is the first figure line, in toned spans: "4 awake · 2 waking ·
	// 4 asleep", or "stopped".
	Fig []Bit `json:"fig"`
	// Sub is the dim line under it: "551 MB · 4 connections", or "last
	// time: api, web, db".
	Sub string `json:"sub"`
	// Need is the red pill: "search is crashing · try 2 of 5"; empty when
	// nothing needs you.
	Need string `json:"need,omitempty"`
	// Addresses are the pills: a domain's https address first, then the
	// apps' names. More counts the ones not shown.
	Addresses []Addr `json:"addresses"`
	More      int    `json:"more,omitempty"`
	// When is the right-hand figure: "up 14m", or "up 3 days ago".
	When string `json:"when"`
	// LastSet is what Up as last time starts; empty means everything.
	LastSet []string `json:"lastSet,omitempty"`
	// Choices are the instances the switch's menu lists for Choose, tasks
	// aside, each with what it brings and whether it was in the last set.
	Choices []Pick `json:"choices,omitempty"`
}

// Bit is a run of text in a tone.
type Bit struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

// Addr is one address pill.
type Addr struct {
	Text string `json:"text"`
	// URL is what Open uses; empty for a name that is not http.
	URL string `json:"url,omitempty"`
	Up  bool   `json:"up"`
}

// Pick is one line of the switch's Choose list.
type Pick struct {
	Name   string `json:"name"`
	Brings string `json:"brings,omitempty"`
	On     bool   `json:"on"`
}

// Step is one of the help strip's three.
type Step struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Done   bool   `json:"done"`
	Now    bool   `json:"now"`
}

// Check is one part of the machine's setup.
type Check struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Text string `json:"text,omitempty"`
	// Section groups the Doctor table: "names", "domains", "app".
	Section string `json:"section,omitempty"`
	// Fix is the command that puts a part in place in a terminal; Repair
	// does the same from the app.
	Fix string `json:"fix,omitempty"`
}

// Hints is what the help strip needs from prefs: whether it was dismissed,
// and whether a connection was ever seen.
type Hints struct {
	Dismissed bool
	Connected bool
}

// LibraryOf computes the library at now. The machine line is the
// service's to fill.
func LibraryOf(stacks []hub.StackView, hints Hints, now time.Time) Library {
	lib := Library{Stacks: []LibStack{}}
	up := 0
	var first *hub.StackView
	anyUp, anyConn := false, hints.Connected
	for i := range stacks {
		st := stacks[i]
		row := libStackOf(st, now)
		lib.Stacks = append(lib.Stacks, row)
		if st.Running {
			up++
			anyUp = true
		}
		if !st.LastUp.IsZero() {
			anyUp = true
		}
		for _, v := range st.Instances {
			if v.Conns > 0 {
				anyConn = true
			}
		}
		if first == nil {
			first = &stacks[i]
		}
	}
	n := len(stacks)
	switch {
	case n == 0:
		lib.Counts = "no stacks yet"
	case up == 0:
		lib.Counts = fmt.Sprintf("%d stack%s · none up", n, display.Plural(n))
	default:
		lib.Counts = fmt.Sprintf("%d stack%s · %d up", n, display.Plural(n), up)
	}
	if !hints.Dismissed && (n == 0 || !anyUp || !anyConn) {
		lib.Steps = stepsOf(first, anyUp, anyConn)
	}
	return lib
}

func stepsOf(first *hub.StackView, anyUp, anyConn bool) []Step {
	s := []Step{
		{Title: "Open a project", Detail: "a folder with your code, or an empty one"},
		{Title: "Bring it up", Detail: "one switch; doze starts what the stack needs, in order"},
		{Title: "Connect to it", Detail: "every service gets a name: db.shop.doze, web.shop.doze"},
	}
	if first != nil {
		s[0].Done = true
		s[0].Detail = first.Name + ", from " + display.Tilde(first.Dir)
	}
	if anyUp {
		s[1].Done = true
		s[1].Detail = "the rest wake when something connects"
	}
	if anyConn {
		s[2].Done = true
	}
	if first != nil && first.Running {
		if ex := connectExample(*first); ex != "" {
			s[2].Detail = ex
		}
	}
	for i := range s {
		if !s[i].Done {
			s[i].Now = true
			break
		}
	}
	return s
}

// connectExample is the third step's detail for a running stack: the
// first two things one could connect to.
func connectExample(st hub.StackView) string {
	var parts []string
	for _, v := range st.Instances {
		if v.Kind == "task" || len(parts) == 2 {
			continue
		}
		addr, _ := display.URL(v)
		switch {
		case v.Kind == "module" && v.Domain != "":
			parts = append(parts, "connect to "+v.Domain)
		case strings.HasPrefix(addr, "http"):
			parts = append(parts, "open "+addr)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ") + " · or copy an address from any row"
}

func libStackOf(st hub.StackView, now time.Time) LibStack {
	row := LibStack{Key: st.Key, Name: st.Name, Dir: display.Tilde(st.Dir), Running: st.Running, Live: st.Conn == hub.ConnLive, Addresses: []Addr{}}
	var awake, dozing, waking, asleep, off, trouble int
	var mem int64
	var conns int
	var need string
	for _, v := range st.Instances {
		if v.Kind == "task" {
			continue
		}
		row.Declared++
		look := display.LookOf(v)
		switch {
		case display.Troubled(v):
			trouble++
			if need == "" {
				need = v.Name + " is " + look.Word
				if b := display.Because(v, snapshot(st), now); b != "" {
					need += " · " + b
				}
			}
		case look.Word == "awake":
			awake++
		case look.Word == "dozing":
			dozing++
		case look.Word == "waking":
			waking++
		case look.Word == "disabled" || look.Word == "off":
			off++
		default:
			asleep++
		}
		if display.AwakeNow(v) {
			mem += v.RAM
			conns += display.Connections(v)
		}
	}
	row.Need = need
	switch {
	case !st.Running:
		row.Fig = []Bit{{Text: "stopped", Tone: "dim"}}
		if row.Declared == 0 {
			row.Declared = declaredIn(st.Dir)
		}
		if len(st.LastSet) > 0 {
			row.Sub = "last time: " + strings.Join(st.LastSet, ", ")
		} else if !st.LastUp.IsZero() {
			row.Sub = "last time: everything"
		} else {
			row.Sub = "never started here"
		}
		if !st.LastUp.IsZero() {
			row.When = "up " + display.Ago(now.Sub(st.LastUp)) + " ago"
		} else {
			row.When = "never up"
		}
	default:
		row.Fig = figOf(awake, dozing, waking, asleep, off, trouble)
		row.Sub = fmt.Sprintf("%s · %s", display.MB(mem), connWord(conns))
		row.When = "up " + display.Ago(now.Sub(st.Since))
		switch {
		case trouble > 0:
			row.Dot = "r"
		case st.Conn != hub.ConnLive:
			row.Dot = "y"
		default:
			row.Dot = "g"
		}
	}
	row.LastSet = st.LastSet
	row.Addresses, row.More = addressesOf(st)
	row.Choices = choicesOf(st)
	return row
}

func figOf(awake, dozing, waking, asleep, off, trouble int) []Bit {
	var out []Bit
	add := func(n int, word, tone string) {
		if n == 0 {
			return
		}
		if len(out) > 0 {
			out = append(out, Bit{Text: " · "})
		}
		out = append(out, Bit{Text: fmt.Sprintf("%d %s", n, word), Tone: tone})
	}
	add(awake, "awake", "ok")
	add(waking, "waking", "cool")
	add(dozing, "dozing", "warn")
	add(asleep, "asleep", "")
	add(off, "off", "dim")
	add(trouble, "crashing", "bad")
	if len(out) == 0 {
		out = []Bit{{Text: "nothing awake", Tone: "dim"}}
	}
	return out
}

func connWord(n int) string {
	switch n {
	case 0:
		return "no connections"
	case 1:
		return "1 connection"
	}
	return fmt.Sprintf("%d connections", n)
}

// addressesOf picks the pills: every https domain, then the apps by name,
// three at most, with how many more there are.
func addressesOf(st hub.StackView) ([]Addr, int) {
	var out []Addr
	seen := map[string]bool{}
	for _, v := range st.Instances {
		for _, d := range v.Domains {
			if strings.HasPrefix(d, "*.") || seen[d] {
				continue
			}
			seen[d] = true
			out = append(out, Addr{Text: "https://" + d, URL: "https://" + d, Up: display.AwakeNow(v)})
		}
	}
	names := make([]control.InstanceView, 0, len(st.Instances))
	for _, v := range st.Instances {
		if v.Kind == "task" || v.Domain == "" {
			continue
		}
		names = append(names, v)
	}
	sort.SliceStable(names, func(i, j int) bool {
		ai, aj := names[i].Kind == "app", names[j].Kind == "app"
		if ai != aj {
			return ai
		}
		return false
	})
	for _, v := range names {
		addr, _ := display.URL(v)
		a := Addr{Text: v.Domain, Up: display.AwakeNow(v)}
		if strings.HasPrefix(addr, "http") {
			a.URL = addr
		}
		out = append(out, a)
	}
	if out == nil {
		out = []Addr{}
	}
	if len(out) > 3 {
		return out[:3], len(out) - 3
	}
	return out, 0
}

func choicesOf(st hub.StackView) []Pick {
	var out []Pick
	last := map[string]bool{}
	for _, n := range st.LastSet {
		last[n] = true
	}
	for _, v := range st.Instances {
		if v.Kind == "task" {
			continue
		}
		c := Pick{Name: v.Name, On: len(st.LastSet) == 0 || last[v.Name]}
		if len(v.Needs) > 0 {
			c.Brings = "brings " + strings.Join(v.Needs, ", ")
		}
		out = append(out, c)
	}
	return out
}
