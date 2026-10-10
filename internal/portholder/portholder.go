// Package portholder answers "what is listening on this port": the process
// that holds a port doze wants, so a person can be told who to stop and not
// just that the bind failed.
package portholder

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Holder is a process listening on a TCP port.
type Holder struct {
	PID     int
	Command string
	Addr    string // as the system reports it, e.g. "127.0.0.1:5432" or "*:5432"
}

// Who looks up the process listening on a TCP port. ok is false when nothing
// is, or when the system cannot say — lsof missing, or a process owned by
// someone else that it may not look at. An unknown holder is reported as
// "something", never invented.
func Who(port int) (Holder, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpcn").Output()
	if err != nil && len(out) == 0 {
		return Holder{}, false
	}
	return parse(out)
}

// parse reads lsof's -F output: p<pid>, c<command>, n<address> lines, a field
// set per process.
func parse(out []byte) (Holder, bool) {
	var h Holder
	for _, line := range bytes.Split(out, []byte("\n")) {
		s := string(line)
		if len(s) < 2 {
			continue
		}
		switch s[0] {
		case 'p':
			if h.PID != 0 {
				return h, true // the first process is enough
			}
			h.PID, _ = strconv.Atoi(s[1:])
		case 'c':
			h.Command = s[1:]
		case 'n':
			if h.Addr == "" {
				h.Addr = strings.TrimSpace(s[1:])
			}
		}
	}
	return h, h.PID != 0
}
