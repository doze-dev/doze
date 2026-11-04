package view

import (
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// Home is a stack's home page: the meters, then every instance under its
// group, with the dash's columns.
type Home struct {
	Stack   string  `json:"stack"`
	Dir     string  `json:"dir"`
	Running bool    `json:"running"`
	Banner  Banner  `json:"banner"`
	Meters  Meters  `json:"meters"`
	Groups  []Group `json:"groups"`
	// Awake and Total count the instances up and declared, tasks aside.
	Awake int `json:"awake"`
	Total int `json:"total"`
}

// Meters are the two bars at the top: who holds how much memory and CPU.
type Meters struct {
	Mem Meter `json:"mem"`
	CPU Meter `json:"cpu"`
}

// Meter is one bar: shares largest first, the total as a label, and how
// many instances were doing too little to draw.
type Meter struct {
	Total  string  `json:"total"`
	Shares []Share `json:"shares"`
	Idle   int     `json:"idle"`
}

// Share is one instance's part of a meter.
type Share struct {
	Name string `json:"name"`
	// Part is the share of the sum, 0 to 1, for the bar's width.
	Part  float64 `json:"part"`
	Label string  `json:"label"`
}

// Group is a heading over part of the stack.
type Group struct {
	Title string `json:"title"`
	What  string `json:"what"`
	Rows  []Row  `json:"rows"`
}

// Row is one instance in the table: the dash's columns, computed.
type Row struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Look Look   `json:"look"`
	// Runs is what it runs: the command, or a module's address.
	Runs    string `json:"runs"`
	Because string `json:"because"`
	Mem     string `json:"mem,omitempty"`
	Addr    string `json:"addr,omitempty"`
	Port    string `json:"port,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Awake   bool   `json:"awake"`
	Trouble bool   `json:"trouble"`
	// Depth is the row's depth in the dependency tree, for the tree view.
	Depth int `json:"depth"`
}

// HomeOf computes the home page of a stack at now.
func HomeOf(st hub.StackView, now time.Time) Home {
	snap := snapshot(st)
	h := Home{Stack: st.Name, Dir: display.Tilde(st.Dir), Running: st.Running, Banner: BannerOf(st, now)}
	rows := display.Rows(snap)
	_, depth := display.TreeOrder(rows)
	for gi, g := range display.Groups {
		grp := Group{Title: g.Title, What: g.What}
		for _, r := range rows {
			if r.Group != gi {
				continue
			}
			v := r.V
			addr, port := display.URL(v)
			row := Row{
				Name: v.Name, Kind: v.Kind, Look: lookOf(v),
				Runs: display.Runs(v), Because: display.Because(v, snap, now),
				Mem: display.Mem(v), Addr: addr, Port: port, Hint: display.Hint(v, snap, now),
				Awake: display.AwakeNow(v), Trouble: display.Troubled(v), Depth: depth[v.Name],
			}
			grp.Rows = append(grp.Rows, row)
			if v.Kind != "task" {
				h.Total++
				if row.Awake {
					h.Awake++
				}
			}
		}
		if len(grp.Rows) > 0 {
			h.Groups = append(h.Groups, grp)
		}
	}
	h.Meters = metersOf(st.Instances)
	return h
}

// metersOf composes the two bars from the instances' latest readings.
func metersOf(vs []control.InstanceView) Meters {
	var mem, cpu []display.Share
	for _, v := range vs {
		if !display.AwakeNow(v) && v.State != "booting" {
			continue
		}
		mem = append(mem, display.Share{Name: v.Name, V: float64(v.RAM)})
		cpu = append(cpu, display.Share{Name: v.Name, V: v.CPU})
	}
	memShares, memIdle, memSum := display.Shares(mem, 0)
	cpuShares, cpuIdle, cpuSum := display.Shares(cpu, 0.05)
	unit := display.MemUnit(memSum)
	m := Meters{
		Mem: Meter{Total: unit(memSum), Idle: memIdle},
		CPU: Meter{Total: display.Cores(cpuSum), Idle: cpuIdle},
	}
	for _, s := range memShares {
		m.Mem.Shares = append(m.Mem.Shares, Share{Name: s.Name, Part: part(s.V, memSum), Label: unit(s.V)})
	}
	for _, s := range cpuShares {
		m.CPU.Shares = append(m.CPU.Shares, Share{Name: s.Name, Part: part(s.V, cpuSum), Label: display.Cores(s.V)})
	}
	return m
}

func part(v, sum float64) float64 {
	if sum <= 0 {
		return 0
	}
	return float64(int(v/sum*1000+0.5)) / 1000
}
