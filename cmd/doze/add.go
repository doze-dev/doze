package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/detect"
	"github.com/doze-dev/doze/internal/modules"
	"github.com/doze-dev/doze/internal/ui"
)

func addCmd() *cobra.Command {
	var name, version, as string
	var port int
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "add <module|service> [name]",
		Short: "Add a module or a service to doze.hcl",
		Long: "add writes a block into doze.hcl — beside what is already there, which it\n" +
			"does not reformat — and shows the block and the line that wires it into an\n" +
			"app. `doze add postgres` adds a registry module. A name that is not a module,\n" +
			"such as `doze add mongo`, is added as a service, and the three ways to run\n" +
			"one — a binary on your PATH, a container, a download — are offered.\n\n" +
			"  doze add postgres                  module postgres \"db\"\n" +
			"  doze add postgres main --port 5433\n" +
			"  doze add mongo                     service \"mongo\"\n" +
			"  doze add mongo --as container\n" +
			"  doze add valkey --dry-run          show the block, write nothing",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			kind := strings.ToLower(args[0])
			if len(args) == 2 {
				name = args[1]
			}
			return runAdd(kind, name, version, as, port, dryRun)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "engine version (default: the newest the module offers)")
	cmd.Flags().IntVar(&port, "port", 0, "the port to declare (default: the module's conventional one)")
	cmd.Flags().StringVar(&as, "as", "", "for a service: installed, container or download")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the block and write nothing")
	return cmd
}

// seedVersions and seedNames are the offline answers when the registry cannot
// be reached: the versions the starters have always used.
var seedVersions = map[string]string{"postgres": "18", "valkey": "9", "kvrocks": "2", "kafka": "4", "mariadb": "11"}

// moduleFacts is what is known about a module type: whether it exists, its
// newest engine version and its port.
type moduleFacts struct {
	isModule bool
	version  string
	port     int
}

func lookupModule(kind string) moduleFacts {
	if !modules.Enabled() {
		// offline by choice: the starters, without a network attempt
		goto seed
	}
	if mm, err := modules.NewManager(dozeHome()); err == nil {
		if entries, err := mm.CatalogModules(); err == nil && len(entries) > 0 {
			for _, e := range entries {
				if e.Name != kind {
					continue
				}
				f := moduleFacts{isModule: true, port: e.Port}
				if n := len(e.EngineVersions); n > 0 {
					f.version = e.EngineVersions[n-1]
				}
				if f.port == 0 {
					f.port = detect.ConventionalPort(kind)
				}
				return f
			}
			return moduleFacts{} // the registry answered, and has no such module
		}
	}
seed:
	if v, ok := seedVersions[kind]; ok || kind == "aws" {
		return moduleFacts{isModule: true, version: v, port: detect.ConventionalPort(kind)}
	}
	return moduleFacts{}
}

func runAdd(kind, name, version, as string, port int, dryRun bool) error {
	src, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	fresh := os.IsNotExist(err)
	ed, err := config.OpenEdit(src)
	if err != nil {
		return err
	}

	facts := lookupModule(kind)
	var block, label string
	switch {
	case facts.isModule && as != "":
		return fmt.Errorf("--as is for a service, and %s is a registry module", kind)
	case facts.isModule:
		if name == "" {
			name = freeName(ed, detect.DefaultName(kind))
		}
		if version == "" {
			version = facts.version
		}
		if port == 0 {
			port = facts.port
		}
		block = moduleBlock(kind, name, version, port)
		label = "module " + kind + " " + fmt.Sprintf("%q", name)
	default:
		block, name, label, err = serviceBlockFor(kind, name, as, port, ed)
		if err != nil {
			return err
		}
	}
	if ed.Declared(name) {
		return fmt.Errorf("%q is already declared in %s — pick another name: doze add %s <name>", name, configPath, kind)
	}

	if fresh {
		ed.SetStack(slugFor(filepath.Base(absDir())))
	}
	if err := ed.AppendBlock(block); err != nil {
		return err
	}
	out := ed.Bytes()
	// What would be written has to be a config doze reads, or nothing is.
	if _, err := config.ParseShape(out, absConfig()); err != nil {
		return fmt.Errorf("adding this would leave %s unreadable, so nothing was written:\n  %w", configPath, err)
	}

	fmt.Println(ui.Muted("→") + " " + label)
	for _, l := range strings.Split(block, "\n") {
		fmt.Println("  " + l)
	}
	if env, ref := wiringLine(kind, name, facts.isModule); env != "" {
		fmt.Println()
		fmt.Println(ui.Muted("  use it from an app:") + "  " + env + " = " + ref)
	}
	if dryRun {
		fmt.Println(ui.Muted("\n(dry run: nothing written)"))
		return nil
	}
	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		return err
	}
	fmt.Println(ui.OK("\n✓") + " added to " + configPath + ui.Muted(" — doze lint · doze up"))
	return nil
}

// moduleBlock renders a module block.
func moduleBlock(kind, name, version string, port int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "module %s %q {\n", kind, name)
	if version != "" {
		fmt.Fprintf(&b, "  version = %s\n", version)
	}
	if port > 0 {
		fmt.Fprintf(&b, "  port    = %d\n", port)
	}
	b.WriteString("}")
	return b.String()
}

// serviceBlockFor is the path for something that is not a module: the three
// ways to bring it, and which one was taken.
func serviceBlockFor(kind, name, as string, port int, ed *config.Edit) (block, blockName, label string, err error) {
	if name == "" {
		name = freeName(ed, strings.NewReplacer(" ", "-", "_", "-").Replace(kind))
	}
	known, isKnown := detect.KnownService(kind)
	if port == 0 {
		port = known
	}
	found := ""
	if p, err := exec.LookPath(binaryOf(kind)); err == nil {
		found = p
	}

	switch as {
	case "":
		// Take the first way that works here, and say the others.
		fmt.Println(ui.Muted("→") + " " + kind + " is not a doze module — adding it as a service")
		switch {
		case found != "" && isKnown:
			fmt.Printf("  found %s at %s — the block runs it\n", binaryOf(kind), found)
		case found != "":
			fmt.Printf("  found %s at %s, but doze does not know its flags — edit `run`\n", binaryOf(kind), found)
		case isKnown:
			fmt.Printf("  %s is not installed — the block runs it as a container\n", binaryOf(kind))
		default:
			fmt.Println("  nothing found on this machine — the block is a download to fill in")
		}
		fmt.Println(ui.Muted("  other ways:") + " --as installed · --as container · --as download")
		fmt.Println()
		return detect.ServiceBlock(kind, port, found, ""), name, "service " + fmt.Sprintf("%q", name), nil
	case "installed":
		if found == "" {
			return "", "", "", fmt.Errorf("%s is not on your PATH: install it, or use --as container or --as download", binaryOf(kind))
		}
		return detect.ServiceBlock(kind, port, found, ""), name, "service " + fmt.Sprintf("%q", name), nil
	case "container":
		return detect.ServiceBlock(kind, port, "", containerImage(kind)), name, "service " + fmt.Sprintf("%q", name), nil
	case "download":
		return detect.DownloadBlock(kind, port), name, "service " + fmt.Sprintf("%q", name), nil
	}
	return "", "", "", fmt.Errorf("--as takes installed, container or download, not %q", as)
}

// binaryOf is the executable a service name is conventionally run from.
func binaryOf(kind string) string {
	switch kind {
	case "mongo":
		return "mongod"
	case "nats":
		return "nats-server"
	}
	return kind
}

func containerImage(kind string) string {
	if kind == "mongo" {
		return "mongo:7"
	}
	return kind
}

// freeName is base, or base2, base3… when taken.
func freeName(ed *config.Edit, base string) string {
	if !ed.Declared(base) {
		return base
	}
	for i := 2; ; i++ {
		if n := fmt.Sprintf("%s%d", base, i); !ed.Declared(n) {
			return n
		}
	}
}

// wiringLine is the line that connects what was added to an app.
func wiringLine(kind, name string, isModule bool) (env, ref string) {
	if isModule {
		e, attr := detect.Wiring(kind)
		if e == "" {
			return "", ""
		}
		return e, "module." + name + "." + attr
	}
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_ADDR", "service." + name + ".address"
}

func absDir() string {
	a, err := filepath.Abs(configDir(configPath))
	if err != nil {
		return "."
	}
	return a
}

func absConfig() string {
	a, err := filepath.Abs(configPath)
	if err != nil {
		return configPath
	}
	return a
}

func slugFor(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if out := strings.Trim(b.String(), "-"); out != "" {
		return out
	}
	return "stack"
}
