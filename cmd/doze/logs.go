package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/logstore"
	"github.com/doze-dev/doze/internal/ui"
)

func logsCmd() *cobra.Command {
	var (
		follow bool
		run    int
		tail   int
		since  string
	)
	cmd := &cobra.Command{
		Use:   "logs [name…]",
		Short: "Show what a service printed — also when it is asleep, crashed, or the daemon is down",
		Long: "logs prints the last lines of each named service's newest run, oldest\n" +
			"first; with no name, of every service, merged by time. Logs are kept on\n" +
			"disk per run, so this works for a service that is asleep or has crashed,\n" +
			"and with the daemon stopped.\n\n" +
			"  doze logs api              the newest run\n" +
			"  doze logs api --run -1     the run before it (what it said before it died)\n" +
			"  doze logs api --since 10m  only the last ten minutes\n" +
			"  doze logs -f api worker    follow both as they print\n\n" +
			"Lines from stderr are marked. -f needs the daemon running.",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: instanceCompletion,
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			names := args
			for _, n := range names {
				if cfg.Lookup(n) == nil {
					return instanceNotFound(cfg, n)
				}
			}
			if len(names) == 0 {
				for _, d := range cfg.Instances {
					names = append(names, d.Name)
				}
			}
			var from time.Time
			if since != "" {
				d, err := time.ParseDuration(since)
				if err != nil || d <= 0 {
					return fmt.Errorf("--since %q: want how far back, e.g. 10m or 2h", since)
				}
				from = time.Now().Add(-d)
			}
			d := logSource(cfg)
			prefix := len(names) > 1
			if !follow {
				return printLogs(context.Background(), d, os.Stdout, names, control.LogQuery{Run: run, Tail: tail, Since: from}, prefix)
			}
			if run != 0 {
				return fmt.Errorf("-f follows the newest run; it cannot be combined with --run")
			}
			if !daemonRunning(cfg) {
				return fmt.Errorf("-f follows a running stack, and the daemon is not running (doze up starts it)")
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return followLogs(ctx, d, os.Stdout, names, prefix, tail, from)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing as lines are written (Ctrl-C to stop)")
	cmd.Flags().IntVar(&run, "run", 0, "which run: 0 the newest, -1 the one before, N a run by number")
	cmd.Flags().IntVarP(&tail, "tail", "n", 0, "how many lines from the end of the run (default 200)")
	cmd.Flags().StringVar(&since, "since", "", "only lines newer than this long ago, e.g. 10m")
	return cmd
}

// logSource is where logs are read from: the daemon when it is running, and
// otherwise the log files themselves, which need no daemon to be read.
func logSource(cfg *config.Config) control.Daemon {
	if daemonRunning(cfg) {
		return daemonFor(cfg)
	}
	return diskLogs{store: logstore.Open(filepath.Join(cfg.ProjectDir(), "logs"), logstore.Options{})}
}

// diskLogs answers Logs from the log files on disk. It is the daemon a
// stopped stack has.
type diskLogs struct {
	control.Daemon // the other calls need a live daemon
	store          *logstore.Store
}

func (d diskLogs) Logs(_ context.Context, q control.LogQuery) ([]control.LogFrame, error) {
	return daemon.ReadLog(d.store, q)
}

// printLogs prints the queried lines of each instance, merged by time when
// there is more than one.
func printLogs(ctx context.Context, d control.Daemon, out io.Writer, names []string, q control.LogQuery, prefix bool) error {
	var all []control.LogFrame
	for _, name := range names {
		q.Instance = name
		lines, err := d.Logs(ctx, q)
		if err != nil {
			if len(names) == 1 {
				return err
			}
			continue // one instance with no such run does not hide the others
		}
		all = append(all, lines...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Time.Before(all[j].Time) })
	if len(all) == 0 && len(names) == 1 {
		fmt.Fprintln(out, ui.Muted(names[0]+" has not printed anything yet"))
		return nil
	}
	width := nameWidth(names)
	for _, l := range all {
		fmt.Fprintln(out, formatLogLine(l, prefix, width))
	}
	return nil
}

// followLogs prints the last backlog lines of each instance and then every
// line as it is written, until ctx is done. The backlog and what follows come
// from one subscription, so no line falls between them.
func followLogs(ctx context.Context, d control.Daemon, out io.Writer, names []string, prefix bool, backlog int, since time.Time) error {
	width := nameWidth(names)
	return d.Subscribe(ctx, control.Subscription{Topics: []string{control.TopicLogs}, Names: names, Backlog: backlog}, func(f control.Frame) error {
		if f.Log != nil && (since.IsZero() || !f.Log.Time.Before(since)) {
			fmt.Fprintln(out, formatLogLine(*f.Log, prefix, width))
		}
		return nil
	})
}

func nameWidth(names []string) int {
	w := 0
	for _, n := range names {
		if len(n) > w {
			w = len(n)
		}
	}
	return w
}

// formatLogLine renders one line: the time, the instance when several are
// shown, a mark for stderr or a step, and the text as the service printed it.
func formatLogLine(l control.LogFrame, prefix bool, width int) string {
	var b strings.Builder
	b.WriteString(ui.Muted(l.Time.Local().Format("15:04:05")))
	b.WriteString(" ")
	if prefix {
		b.WriteString(ui.Muted(fmt.Sprintf("%-*s │ ", width, l.Instance)))
	}
	stream, part, _ := strings.Cut(l.Stream, ":")
	switch stream {
	case logstore.Stderr:
		b.WriteString(ui.Warn("err") + " ")
	case "s":
		b.WriteString(ui.Muted("["+part+"]") + " ")
		part = ""
	}
	if part != "" { // one process of an instance that runs several
		b.WriteString(ui.Muted(part+":") + " ")
	}
	b.WriteString(l.Line)
	return b.String()
}
