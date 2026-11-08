// runner times the probes and writes a Markdown report: start-and-exit time
// over many interleaved runs, peak memory, DLLs loaded before main, the COM
// state of the main thread, and Go's own per-package init trace for the
// Wails packages. go run ./runner -n 300 plain.exe wails.exe
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	n := flag.Int("n", 300, "runs of each probe")
	flag.Parse()
	bins := flag.Args()
	if len(bins) < 2 {
		fmt.Fprintln(os.Stderr, "usage: runner -n N plain.exe wails.exe")
		os.Exit(2)
	}
	for i := range bins {
		bins[i], _ = filepath.Abs(bins[i])
	}
	// Warm up: the first runs of a new exe pay for the virus scanner.
	for i := 0; i < 20; i++ {
		for _, b := range bins {
			run(b, nil)
		}
	}
	times := make([][]float64, len(bins))
	for i := 0; i < *n; i++ {
		for j, b := range bins {
			t := time.Now()
			run(b, nil)
			times[j] = append(times[j], float64(time.Since(t).Microseconds())/1000)
		}
	}
	reps := make([]map[string]string, len(bins))
	for j, b := range bins {
		reps[j] = report(b)
	}

	host, _ := os.Hostname()
	fmt.Printf("### %s (%s)\n\n", os.Getenv("RUNNER_OS")+" "+os.Getenv("RUNNER_ARCH"), host)
	fmt.Printf("%d interleaved runs each, after 20 warm-up runs.\n\n", *n)
	fmt.Println("| probe | median start+exit | p10 | p90 | peak working set | DLLs at main | main thread COM |")
	fmt.Println("|---|---|---|---|---|---|---|")
	for j, b := range bins {
		s := times[j]
		sort.Float64s(s)
		ws, _ := strconv.ParseFloat(reps[j]["peak-working-set"], 64)
		fmt.Printf("| %s | %.2f ms | %.2f ms | %.2f ms | %.1f MB | %s | %s |\n",
			filepath.Base(b), s[len(s)/2], s[len(s)/10], s[len(s)*9/10], ws/1e6, reps[j]["modules"], reps[j]["com"])
	}
	base := set(reps[0]["module-list"])
	for j := 1; j < len(bins); j++ {
		var extra []string
		for m := range set(reps[j]["module-list"]) {
			if !base[m] {
				extra = append(extra, m)
			}
		}
		sort.Strings(extra)
		fmt.Printf("\nDLLs %s loads before main that %s does not (%d): %s\n", filepath.Base(bins[j]), filepath.Base(bins[0]), len(extra), strings.Join(extra, ", "))
		fmt.Printf("\nGo package inits in %s that take time (GODEBUG=inittrace=1, one run):\n\n```\n%s```\n", filepath.Base(bins[j]), inittrace(bins[j]))
	}
}

func run(bin string, env []string) []byte {
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n%s", bin, err, out.Bytes())
		os.Exit(1)
	}
	return out.Bytes()
}

func report(bin string) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(run(bin, []string{"WAILSWIN_REPORT=1"})))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), " ")
		m[k] = v
	}
	return m
}

func set(list string) map[string]bool {
	s := map[string]bool{}
	for _, m := range strings.Split(list, ",") {
		if m != "" {
			s[m] = true
		}
	}
	return s
}

// inittrace keeps the init lines that took 0.05 ms or more, slowest first.
func inittrace(bin string) string {
	line := regexp.MustCompile(`^init (\S+) @\S+ ms, ([0-9.]+) ms clock, (\d+) bytes, (\d+) allocs`)
	type row struct {
		pkg   string
		ms    float64
		bytes string
	}
	var rows []row
	for _, l := range strings.Split(string(run(bin, []string{"GODEBUG=inittrace=1"})), "\n") {
		if m := line.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			ms, _ := strconv.ParseFloat(m[2], 64)
			if ms >= 0.05 {
				rows = append(rows, row{m[1], ms, m[3]})
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ms > rows[j].ms })
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%7.2f ms  %9s bytes  %s\n", r.ms, r.bytes, r.pkg)
	}
	if b.Len() == 0 {
		return "(none over 0.05 ms)\n"
	}
	return b.String()
}
