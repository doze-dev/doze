package view

import (
	"strings"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/display"
)

// addressOf is a row's address as the overview shows it, and the URL Open
// uses: an app's https domain when it has one, else the port-less http
// name the front door serves, else a module's name and port. A task has
// none.
func addressOf(v control.InstanceView) (address, url string) {
	if v.Kind == "task" {
		return "", ""
	}
	if v.HTTPS != "" {
		return v.HTTPS, v.HTTPS
	}
	if strings.HasPrefix(v.Resource, "http://") || strings.HasPrefix(v.Resource, "https://") {
		return v.Resource, v.Resource
	}
	addr, port := display.URL(v)
	switch {
	case addr == "":
		return "", ""
	case strings.HasPrefix(addr, "http") && v.Kind == "module" && v.URL == "" && v.Resource == "":
		// A module is web only when it says so; its name alone is not.
		return display.Address(v), ""
	case strings.HasPrefix(addr, "http"):
		// Whole, http:// and all: a browser takes a bare .doze name for a
		// search.
		return addr + port, addr + port
	}
	return addr + port, ""
}

// Map geometry, in CSS pixels, as the Map board draws it: three columns,
// nodes 224 wide and 64 tall, 124 apart.
const (
	mapColW   = 400
	mapNodeW  = 224
	mapNodeH  = 64
	mapTaskW  = 200
	mapStep   = 124
	mapTop    = 76
	mapLeft   = 30
	mapColGap = 6
)

// mapOf places every instance by its group, apps left, modules middle,
// the rest right with tasks under them, and draws an edge for each need.
func mapOf(st hub.StackView, rows []display.Row) MapView {
	m := MapView{Columns: []MapColumn{}, Nodes: []MapNode{}, Edges: []MapEdge{}}
	// Column per group: 0 apps, 1 modules, 2 outside, 3 tasks (under outside).
	col := func(group int) int {
		if group >= 2 {
			return 2
		}
		return group
	}
	next := [3]int{mapTop, mapTop, mapTop}
	at := map[string]MapNode{}
	awake := map[string]bool{}
	off := map[string]bool{}
	var lastGroup [3]int
	for i := range lastGroup {
		lastGroup[i] = -1
	}
	for _, r := range rows {
		c := col(r.Group)
		if lastGroup[c] != r.Group {
			g := display.Groups[r.Group]
			y := next[c]
			if lastGroup[c] >= 0 {
				y += 12 // a little air before the next heading in the column
			}
			m.Columns = append(m.Columns, MapColumn{Title: g.Title, What: g.What, X: mapLeft + c*mapColW, Y: y - 44})
			if lastGroup[c] >= 0 {
				next[c] = y
			}
			lastGroup[c] = r.Group
		}
		n := MapNode{Name: r.V.Name, X: mapLeft + c*mapColW, Y: next[c], W: mapNodeW, H: mapNodeH, Task: r.V.Kind == "task"}
		if n.Task {
			n.W = mapTaskW
		}
		next[c] += mapStep
		m.Nodes = append(m.Nodes, n)
		at[n.Name] = n
		awake[n.Name] = display.AwakeNow(r.V)
		off[n.Name] = r.V.Disabled
	}
	for _, r := range rows {
		from, ok := at[r.V.Name]
		if !ok {
			continue
		}
		for _, need := range r.V.Needs {
			to, ok := at[need]
			if !ok {
				continue
			}
			e := MapEdge{From: from.Name, To: to.Name, Tone: "dim"}
			switch {
			case off[from.Name]:
				e.Tone = "off"
			case awake[from.Name] && awake[to.Name]:
				e.Tone = "on"
			}
			if from.X == to.X {
				e.Same = true
				e.X1, e.Y1 = from.X+from.W/2, from.Y
				e.X2, e.Y2 = to.X+to.W/2, to.Y+to.H
				if to.Y > from.Y {
					e.Y1, e.Y2 = from.Y+from.H, to.Y
				}
			} else if to.X > from.X {
				e.X1, e.Y1 = from.X+from.W, from.Y+from.H/2
				e.X2, e.Y2 = to.X, to.Y+to.H/2
			} else {
				e.X1, e.Y1 = from.X, from.Y+from.H/2
				e.X2, e.Y2 = to.X+to.W, to.Y+to.H/2
			}
			m.Edges = append(m.Edges, e)
		}
	}
	m.W = mapLeft + 3*mapColW
	for _, n := range next {
		if n+mapTop > m.H {
			m.H = n + mapTop
		}
	}
	if m.H < 580 {
		m.H = 580
	}
	return m
}
