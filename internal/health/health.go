// Package health runs the generic readiness/liveness probes core uses to gate a
// SpawnPlan spec (and to periodically check a supervised process). The probe kinds
// — http, tcp, exec, log_line, socket — are protocol-agnostic and belong in core,
// declared by an engine.Ready spec rather than implemented per engine.
package health

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/nerdmenot/doze/internal/engine"
)

const (
	defaultInterval = time.Second
	defaultTimeout  = 3 * time.Second
	defaultRetries  = 30
	livenessGrace   = 750 * time.Millisecond // "stayed alive briefly" window for a probe-less spec
)

// WaitReady blocks until the spec's probe passes, the process dies, or the budget
// (Interval × Retries) is exhausted. A nil spec means liveness — the process must
// simply still be running after a short grace period. alive/logs observe the
// process being gated (logs feeds the log_line probe).
func WaitReady(ctx context.Context, spec *engine.Ready, alive func() bool, logs func() []string) error {
	if spec == nil {
		select {
		case <-time.After(livenessGrace):
		case <-ctx.Done():
			return ctx.Err()
		}
		if !alive() {
			return fmt.Errorf("exited immediately")
		}
		return nil
	}
	interval := spec.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	retries := spec.Retries
	if retries <= 0 {
		retries = defaultRetries
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for attempt := 0; attempt < retries; attempt++ {
		if !alive() {
			return fmt.Errorf("exited during startup")
		}
		if err := Probe(ctx, spec, logs); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return fmt.Errorf("did not become ready within %s", time.Duration(retries)*interval)
}

// Probe runs the spec's check once, returning nil when it passes. logs is consulted
// only by the log_line probe (may be nil for the others).
func Probe(ctx context.Context, spec *engine.Ready, logs func() []string) error {
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	switch spec.Kind {
	case "http":
		return probeHTTP(ctx, spec.Target)
	case "tcp":
		return probeDial(ctx, "tcp", spec.Target)
	case "socket":
		return probeDial(ctx, "unix", spec.Target)
	case "exec":
		return probeExec(ctx, spec.Target)
	case "log_line":
		return probeLogLine(spec.Target, logsOrNil(logs))
	default:
		return fmt.Errorf("unknown probe kind %q", spec.Kind)
	}
}

func logsOrNil(logs func() []string) []string {
	if logs == nil {
		return nil
	}
	return logs()
}

func probeHTTP(ctx context.Context, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s returned %d", target, resp.StatusCode)
	}
	return nil
}

func probeDial(ctx context.Context, network, target string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, network, target)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func probeExec(ctx context.Context, command string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func probeLogLine(pattern string, logs []string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid log_line regex %q: %w", pattern, err)
	}
	for _, line := range logs {
		if re.MatchString(line) {
			return nil
		}
	}
	return fmt.Errorf("log_line %q not seen yet", pattern)
}
