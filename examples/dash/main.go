// Command dash is a minimal live status dashboard built entirely on the public
// doze library — the dogfood proof that you can build your own UI on top of it.
// It renders the declared topology + live state, then streams state transitions
// and re-renders, the way `doze` (the TUI) does, using only the doze package.
//
//	go run ./examples/dash -config ./doze.hcl
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	doze "github.com/doze-dev/doze"
)

func main() {
	cfgPath := flag.String("config", "", "path to doze.hcl (empty: search upward)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sess, err := doze.Attach(ctx, doze.Options{ConfigPath: *cfgPath})
	if err != nil {
		log.Fatal(err)
	}
	defer sess.Close()

	// Bring the stack up in the background; watch it come alive via Events.
	go func() {
		if err := sess.Up(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "up:", err)
		}
	}()

	render(ctx, sess)
	if err := sess.Events(ctx, func(doze.Instance) { render(ctx, sess) }); err != nil {
		fmt.Fprintln(os.Stderr, "events:", err)
	}
}

// render draws a status table from Topology (the declared graph) joined with
// Status (live state) — exactly the data `doze status` shows, from the library.
func render(ctx context.Context, sess *doze.Session) {
	live := map[string]doze.Instance{}
	if insts, err := sess.Status(ctx); err == nil {
		for _, in := range insts {
			live[in.Name] = in
		}
	}
	nodes := sess.Topology()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	fmt.Print("\033[H\033[2J") // clear
	fmt.Printf("stack %q — %s\n\n", sess.StackName(), time.Now().Format("15:04:05"))
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tENGINE\tSTATE\tPID\tRAM\tENDPOINT\tDEPENDS ON")
	for _, n := range nodes {
		in := live[n.Name]
		state := in.State
		if state == "" {
			state = "reaped"
		}
		pid := "-"
		if in.PID != 0 {
			pid = fmt.Sprint(in.PID)
		}
		ram := "-"
		if in.RAM != 0 {
			ram = fmt.Sprintf("%dMB", in.RAM/(1<<20))
		}
		ep := in.Endpoint
		if ep == "" {
			ep = "-"
		}
		deps := strings.Join(n.DependsOn, ",")
		if deps == "" {
			deps = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", n.Name, n.Engine, state, pid, ram, ep, deps)
	}
	w.Flush()
	fmt.Println("\nCtrl-C to detach (services keep running)")
}
