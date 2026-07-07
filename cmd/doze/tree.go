package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/ui"
)

func treeCmd() *cobra.Command {
	var graph, jsonOut bool
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"tree", "ls", "ps"},
		Short:   "Show every service: state, endpoint, and resource use",
		Long: "status lists the stack as a grouped table — services by category, each with\n" +
			"its live state (active / idle / asleep / disabled), endpoint, open connections,\n" +
			"memory and CPU, and what it depends on. With the daemon down it shows the\n" +
			"declared structure. --graph draws the dependency tree instead; --json emits\n" +
			"the same facts machine-readably for scripts and CI.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, cfgErr := loadConfig()
			if cfgErr != nil {
				// A broken block must not brick the whole status view: fall back
				// to the driver-free shallow load so the declared stack (and any
				// live daemon state) still renders, with the error alongside.
				return degradedStatus(cfgErr)
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
			switch {
			case jsonOut:
				return renderStatusJSON(cfg, views, daemonUp)
			case graph:
				renderGraph(cfg, views, daemonUp)
			default:
				renderTable(cfg, views, daemonUp)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&graph, "graph", false, "draw the dependency tree instead of the table")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON instead of the table")
	return cmd
}

// statusJSON is the machine-readable shape of `doze status --json`. Field names
// are a stable contract; add, don't rename.
type statusJSON struct {
	Daemon   statusDaemonJSON    `json:"daemon"`
	Services []statusServiceJSON `json:"services"`
}

type statusDaemonJSON struct {
	Running bool `json:"running"`
}

type statusServiceJSON struct {
	Name       string   `json:"name"`
	Engine     string   `json:"engine"`
	Version    string   `json:"version,omitempty"`
	Group      string   `json:"group"` // "service" | "process"
	State      string   `json:"state"`
	Enabled    bool     `json:"enabled"`
	Endpoint   string   `json:"endpoint,omitempty"`
	Domain     string   `json:"domain,omitempty"`
	URL        string   `json:"url,omitempty"`
	Resource   string   `json:"resource,omitempty"` // full resource path/ARN or ingress :80 URL
	Conns      int      `json:"conns"`
	MemBytes   int64    `json:"memBytes"`
	CPUPercent float64  `json:"cpuPercent"`
	DependsOn  []string `json:"dependsOn,omitempty"`
}

func renderStatusJSON(cfg *config.Config, views map[string]control.InstanceView, daemonUp bool) error {
	out := statusJSON{Daemon: statusDaemonJSON{Running: daemonUp}, Services: []statusServiceJSON{}}
	for _, d := range cfg.Instances {
		v, running := views[d.Name]
		group := "service"
		if categoryOf(d.Type) == "process" {
			group = "process"
		}
		var deps []string
		for _, dep := range d.Deps {
			deps = append(deps, dep.Name)
		}
		sort.Strings(deps)
		endpoint := v.Endpoint
		if endpoint == "" {
			if addr, err := cfg.InstanceAddr(d); err == nil {
				endpoint = addr
			}
		}
		domain := v.Domain
		if domain == "" && cfg.Defaults.Domains && endpoint != "" && !strings.HasPrefix(endpoint, "unix:") {
			domain = cfg.DomainFor(d.Name)
		}
		// AWS built-ins share one port-less endpoint per type; the internal backend
		// address never surfaces (no per-instance domain either).
		if host, ok := cfg.AWSHost(d.Type); ok {
			endpoint, domain = host, ""
		}
		// A forwarded process surfaces its public port, not the app's self-bound one.
		if fe := forwardHostPort(cfg, d); fe != "" {
			endpoint = fe
		}
		s := statusServiceJSON{
			Name:      d.Name,
			Engine:    d.Type,
			Version:   versionOf(d, v, running),
			Group:     group,
			State:     plainState(v, running, d),
			Enabled:   d.Enabled,
			Endpoint:  endpoint,
			Domain:    domain,
			URL:       v.URL,
			Resource:  v.Resource,
			DependsOn: deps,
		}
		if running {
			s.Conns, s.MemBytes, s.CPUPercent = v.Conns, v.RAM, v.CPU
		}
		out.Services = append(out.Services, s)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// plainState is stateText without terminal styling — for JSON.
func plainState(v control.InstanceView, running bool, decl *config.InstanceDecl) string {
	switch {
	case decl != nil && !decl.Enabled:
		return "disabled"
	case running && (v.Tainted || v.State == "tainted" || v.State == "error"):
		return stateWord(v.State)
	case running && awakeState(v.State):
		return v.State
	default:
		return "asleep"
	}
}

// degradedStatus renders what a failing config still allows: the engine block
// headers from a shallow load, joined with live state from a running daemon
// (found via the default project dir). The load error prints after the table
// so the user sees both their stack and what's wrong with it.
func degradedStatus(cfgErr error) error {
	sc, serr := config.LoadShallow(configPath)
	if serr != nil {
		return cfgErr // not even parseable — the full error is the answer
	}
	views := map[string]control.InstanceView{}
	client := control.NewClient(daemon.ControlSocketPathIn(config.DefaultProjectDir(sc.Path())))
	daemonUp := client.Available()
	if daemonUp {
		if resp, err := client.Do(control.Request{Op: "status"}); err == nil {
			for _, v := range resp.Instances {
				views[v.Name] = v
			}
		}
	}

	fmt.Println(ui.Fail("config has errors") + ui.Muted(" — showing declared blocks; details below") + "\n")
	header := []string{"NAME", "ENGINE", "STATE", "ENDPOINT"}
	widths := make([]int, len(header))
	rows := make([][]string, 0, len(sc.Decls))
	for i, h := range header {
		widths[i] = ui.Width(h)
	}
	for _, d := range sc.Decls {
		v, running := views[d.Name]
		eng := d.Type
		if d.Version != "" {
			eng += " " + d.Version
		}
		state := ui.Muted("asleep")
		if running {
			state = stateText(v, true, nil)
		}
		cells := []string{ui.Title(d.Name), ui.Muted(eng), state, endpointCell(nil, nil, v, running)}
		for i, c := range cells {
			if w := ui.Width(c); w > widths[i] {
				widths[i] = w
			}
		}
		rows = append(rows, cells)
	}
	fmt.Println(tableRow(header, widths, true))
	for _, r := range rows {
		fmt.Println(tableRow(r, widths, false))
	}
	fmt.Println()
	fmt.Println(cfgErr.Error())
	return exitCodeError(1)
}

// category groups: just two divisions — the backing services doze runs for you,
// and your own supervised app processes. (Engines arrive as plugin modules, but
// that's plumbing — users think in services.)
var categoryOrder = []string{"module", "process"}

var categoryLabel = map[string]string{
	"module":  "Services",
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
			endpointCell(cfg, d, v, running),
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
// asleep — connecting wakes it). With the daemon down the declared address still
// prints (muted) — the endpoint is the whole point of the table, and connecting
// to it is exactly how you wake the stack. With defaults{domains=true} the local
// DNS name renders instead of the loopback address (both resolve to the same
// listener). Portless services render as "-".
func endpointCell(cfg *config.Config, decl *config.InstanceDecl, v control.InstanceView, running bool) string {
	// AWS built-ins are reached at their shared per-type endpoint (same host for
	// every bucket/queue/topic); the backend port is internal, so never show it.
	if cfg != nil && decl != nil {
		if host, ok := cfg.AWSHost(decl.Type); ok {
			if running {
				return host
			}
			return ui.Muted(host)
		}
		// A forwarded process is reached at its public port (http://<name>…:<port>);
		// the app's self-bound high port is internal, so show the forward instead.
		if fe := forwardHostPort(cfg, decl); fe != "" {
			if running {
				return fe
			}
			return ui.Muted(fe)
		}
	}
	if running && v.Endpoint != "" {
		if v.Domain != "" {
			if _, port, ok := strings.Cut(v.Endpoint, ":"); ok {
				return v.Domain + ":" + port
			}
		}
		return v.Endpoint
	}
	if cfg != nil && decl != nil {
		if addr, err := cfg.InstanceAddr(decl); err == nil && addr != "" {
			if cfg.Defaults.Domains && !strings.HasPrefix(addr, "unix:") {
				if i := strings.LastIndex(addr, ":"); i > 0 {
					return ui.Muted(cfg.DomainFor(decl.Name) + addr[i:])
				}
			}
			return ui.Muted(addr)
		}
	}
	return ui.Muted("-")
}

// forwardHostPort returns a forwarded process's public host[:port]
// (checkout-api.demo.doze, or …:8090 when the forward port isn't 80), or
// "" when the instance isn't a forwarded process. Derived from config so it shows
// whether or not the daemon is up.
func forwardHostPort(cfg *config.Config, decl *config.InstanceDecl) string {
	if cfg == nil || decl == nil || !cfg.Defaults.Domains {
		return ""
	}
	fwd, ok := decl.Spec.(interface{ ForwardPort() int })
	if !ok || fwd.ForwardPort() <= 0 {
		return ""
	}
	host := cfg.DomainFor(decl.Name)
	if p := fwd.ForwardPort(); p != 80 {
		host += fmt.Sprintf(":%d", p)
	}
	return host
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
	if ep := endpointCell(cfg, decl, v, running); ep != ui.Muted("-") {
		line += ui.Muted("  ") + ep
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
