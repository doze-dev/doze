package process

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strings"

	"github.com/nerdmenot/doze/internal/engine"
)

// probe runs the instance's health check once, returning nil when the probe
// passes. logs is the process's recent log output (used by the log_line probe).
func (h *Health) probe(ctx context.Context, logs []string) error {
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	switch h.Kind {
	case "http":
		return probeHTTP(ctx, h.Target)
	case "tcp":
		return probeTCP(ctx, h.Target)
	case "exec":
		return probeExec(ctx, h.Target)
	case "log_line":
		return probeLogLine(h.Target, logs)
	default:
		return fmt.Errorf("unknown health probe kind %q", h.Kind)
	}
}

// probeHTTP passes when target returns a 2xx status.
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
		return fmt.Errorf("health GET %s returned %d", target, resp.StatusCode)
	}
	return nil
}

// probeTCP passes when a TCP connection to target ("host:port") is accepted.
func probeTCP(ctx context.Context, target string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// probeExec passes when the command (run via `sh -c`) exits zero.
func probeExec(ctx context.Context, command string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("health exec failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// probeLogLine passes when any recent log line matches the target regex.
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

// CheckHealth implements engine.HealthChecker: one probe pass for the periodic
// liveness poll. A process with no health block is considered healthy as long as
// it is up (the runtime only probes running instances).
func (Driver) CheckHealth(ctx context.Context, inst engine.Instance) error {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg.Health == nil || cfg.Health.Kind == "log_line" {
		// No probe, or a one-shot log_line readiness signal that can't be re-run as a
		// liveness check — healthy as long as the process is up (the caller ensures it).
		return nil
	}
	return cfg.Health.probe(ctx, nil)
}
