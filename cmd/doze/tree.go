package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/ui"
)

func treeCmd() *cobra.Command {
	var graph bool
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"tree", "ls", "ps"},
		Short:   "Show every service: state, endpoint, and resource use",
		Long: "status lists the stack as a grouped table — services by category, each with\n" +
			"its live state (active / idle / asleep / disabled), endpoint, open connections,\n" +
			"memory and CPU, and what it depends on. With the daemon down it shows the\n" +
			"declared structure. --graph draws the dependency tree instead.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			views := map[string]control.InstanceView{}
			client := control.NewClient(daemon.ControlSocketPath(cfg))
			daemonUp := client.Available()
			if daemonUp {
				if resp, err := client.Do(control.Request{Op: "status"}); err == nil {
					for _, v := range resp.Instances {
						views[v.Name] = v
					}
				}
			}
			if graph {
				renderGraph(cfg, views, daemonUp)
			} else {
				renderTable(cfg, views, daemonUp)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&graph, "graph", false, "draw the dependency tree instead of the table")
	return cmd
}

// category groups: just two divisions — modules (the engines) and processes (your
// own supervised apps).
var categoryOrder = []string{"module", "process"}

var categoryLabel = map[string]string{
	"module":  "Modules",
	"process": "Processes",
}

// categoryOf maps an engine type to its display group: your `process` apps are
// "process", every engine module is "module".
func categoryOf(engineType string) string {
	if engineType == "process" {
		return "process"
	}
	return "module"
}

// renderTable prints the stack as a grouped, column-aligned table: one section per
// category, sharing column widths across the whole output so everything lines up.
func renderTable(cfg *config.Config, views map[string]control.InstanceView, daemonUp bool) {
	depsOf := map[string][]string{}
	for _, d := range cfg.Instances {
		for _, dep := range d.Deps {
			depsOf[d.Name] = append(depsOf[d.Name], dep.Name)
		}
	}

	type rowData struct {
		name  string
		cells []string
	}
	header := []string{"NAME", "ENGINE", "STATE", "ENDPOINT", "CONNS", "MEM", "CPU", "DEPENDS ON"}
	rowsByCat := map[string][]rowData{}
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = ui.Width(h)
	}
	note := func(cells []string) {
		for i, c := range cells {
			if w := ui.Width(c); w > widths[i] {
				widths[i] = w
			}
		}
	}

	for _, d := range cfg.Instances {
		v, running := views[d.Name]
		deps := append([]string(nil), depsOf[d.Name]...)
		sort.Strings(deps)
		cells := []string{
			stateDot(d.Type, v, running, d) + " " + ui.Title(d.Name),
			engineCell(d, v, running),
			stateText(v, running, d),
			endpointCell(v, running),
			metricConns(v, running),
			metricMem(v, running),
			metricCPU(v, running),
			strings.Join(deps, ", "),
		}
		note(cells)
		cat := categoryOf(d.Type)
		rowsByCat[cat] = append(rowsByCat[cat], rowData{name: d.Name, cells: cells})
	}

	if !daemonUp {
		fmt.Println(ui.Muted("doze is not running — showing declared services") + "\n")
	}
	fmt.Println(tableRow(header, widths, true))
	for _, cat := range categoryOrder {
		rows := rowsByCat[cat]
		if len(rows) == 0 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
		fmt.Println(ui.Muted(categoryLabel[cat]))
		for _, r := range rows {
			fmt.Println(tableRow(r.cells, widths, false))
		}
	}

	if daemonUp {
		awake, ram := 0, int64(0)
		for _, v := range views {
			if awakeState(v.State) {
				awake++
				ram += v.RAM
			}
		}
		fmt.Println()
		fmt.Println(ui.Muted(fmt.Sprintf("%d awake · %s resident · connect to any endpoint to wake it", awake, ui.HumanBytes(ram))))
	}
}

// tableRow lays out one line with shared column widths. The final column is left
// unpadded (no trailing whitespace) so simple line parsers stay reliable.
func tableRow(cells []string, widths []int, header bool) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		val := c
		if header {
			val = ui.Header(c)
		}
		if i == len(cells)-1 {
			parts[i] = val // last column: no trailing pad
			continue
		}
		if pad := widths[i] - ui.Width(c); pad > 0 {
			val += strings.Repeat(" ", pad)
		}
		parts[i] = val
	}
	return strings.TrimRight("  "+strings.Join(parts, "   "), " ")
}

// --- cell renderers ---

func stateDot(_ string, v control.InstanceView, running bool, decl *config.InstanceDecl) string {
	switch {
	case decl != nil && !decl.Enabled:
		return ui.Muted("⊘")
	case running && (v.Tainted || v.State == "tainted" || v.State == "error"):
		return ui.Fail("●")
	case running && awakeState(v.State):
		return ui.OK("●")
	default:
		return ui.Muted("○")
	}
}

func stateText(v control.InstanceView, running bool, decl *config.InstanceDecl) string {
	switch {
	case decl != nil && !decl.Enabled:
		return ui.Muted("disabled")
	case running && (v.Tainted || v.State == "tainted" || v.State == "error"):
		return ui.Fail(stateWord(v.State))
	case running && awakeState(v.State):
		return ui.OK(v.State)
	default:
		return ui.Muted("asleep")
	}
}

// engineCell is the engine type plus its version, hiding the "builtin" pseudo-version.
func engineCell(decl *config.InstanceDecl, v control.InstanceView, running bool) string {
	if decl == nil {
		return ""
	}
	ver := versionOf(decl, v, running)
	if ver == "" || ver == "builtin" || ver == "0" {
		return ui.Muted(decl.Type)
	}
	return ui.Muted(decl.Type + " " + ver)
}

// endpointCell shows the client-facing address whenever the daemon knows it (even
// asleep — connecting wakes it). Portless services and unknowns render as "-".
func endpointCell(v control.InstanceView, running bool) string {
	if running && v.Endpoint != "" {
		return v.Endpoint
	}
	return ui.Muted("-")
}

func metricConns(v control.InstanceView, running bool) string {
	if running && awakeState(v.State) && v.Conns > 0 {
		return fmt.Sprintf("%dc", v.Conns)
	}
	return ui.Muted("-")
}

func metricMem(v control.InstanceView, running bool) string {
	if running && v.RAM > 0 {
		return ui.HumanBytes(v.RAM)
	}
	return ui.Muted("-")
}

func metricCPU(v control.InstanceView, running bool) string {
	if running && awakeState(v.State) && v.CPU >= 0.5 {
		return fmt.Sprintf("%.0f%%", v.CPU)
	}
	return ui.Muted("-")
}

// --- dependency graph (opt-in via --graph) ---

func renderGraph(cfg *config.Config, views map[string]control.InstanceView, daemonUp bool) {
	depsOf := map[string][]string{}
	hasDependents := map[string]bool{}
	for _, d := range cfg.Instances {
		for _, dep := range d.Deps {
			depsOf[d.Name] = append(depsOf[d.Name], dep.Name)
			hasDependents[dep.Name] = true
		}
	}
	var roots []string
	for _, d := range cfg.Instances {
		if !hasDependents[d.Name] {
			roots = append(roots, d.Name)
		}
	}
	sort.Strings(roots)

	if !daemonUp {
		fmt.Println(ui.Muted("doze is not running — showing declared services") + "\n")
	}
	for i, r := range roots {
		printTreeNode(cfg, views, depsOf, r, "", i == len(roots)-1, true)
	}
	if daemonUp {
		awake, ram := 0, int64(0)
		for _, v := range views {
			if awakeState(v.State) {
				awake++
				ram += v.RAM
			}
		}
		fmt.Println()
		fmt.Println(ui.Muted(fmt.Sprintf("%d awake · %s resident", awake, ui.HumanBytes(ram))))
	}
}

func printTreeNode(cfg *config.Config, views map[string]control.InstanceView, depsOf map[string][]string, name, prefix string, last, root bool) {
	connector := "├─ "
	if last {
		connector = "└─ "
	}
	if root {
		connector = ""
	}
	fmt.Println(prefix + connector + nodeLabel(cfg, views, name))

	childPrefix := prefix
	if !root {
		if last {
			childPrefix += "   "
		} else {
			childPrefix += "│  "
		}
	}
	deps := append([]string(nil), depsOf[name]...)
	sort.Strings(deps)
	for i, dep := range deps {
		printTreeNode(cfg, views, depsOf, dep, childPrefix, i == len(deps)-1, false)
	}
}

// nodeLabel renders one graph node: a state dot, name + engine, the state word, and
// (when awake) its endpoint and a compact resource tail.
func nodeLabel(cfg *config.Config, views map[string]control.InstanceView, name string) string {
	decl := cfg.Lookup(name)
	v, running := views[name]

	line := stateDot("", v, running, decl) + " " + ui.Title(name)
	if eng := engineCell(decl, v, running); eng != "" {
		line += " " + eng
	}
	line += "  " + stateText(v, running, decl)
	if running && v.Endpoint != "" {
		line += ui.Muted("  " + v.Endpoint)
	}
	if running && awakeState(v.State) {
		var m []string
		if v.Conns > 0 {
			m = append(m, fmt.Sprintf("%dc", v.Conns))
		}
		if v.RAM > 0 {
			m = append(m, ui.HumanBytes(v.RAM))
		}
		if v.CPU >= 0.5 {
			m = append(m, fmt.Sprintf("%.0f%%", v.CPU))
		}
		if len(m) > 0 {
			line += ui.Muted("  " + strings.Join(m, " · "))
		}
	}
	return line
}

// versionOf returns the engine version for a node: the live value when running,
// else the declared spec.
func versionOf(decl *config.InstanceDecl, v control.InstanceView, running bool) string {
	if running && v.Version != "" {
		return v.Version
	}
	if decl != nil {
		return decl.Version.String()
	}
	return ""
}

// awakeState reports whether a backend is alive (booting, serving, or idling
// before the reaper takes it) — as opposed to reaped/absent.
func awakeState(s string) bool {
	return s == "active" || s == "idle" || s == "booting"
}

func stateWord(s string) string {
	if s == "" {
		return "asleep"
	}
	return s
}
