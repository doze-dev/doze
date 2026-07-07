package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/daemon"
	"github.com/doze-dev/doze/internal/loopback"
	"github.com/doze-dev/doze/internal/ui"
)

func dnsSetupCmd() *cobra.Command {
	var check, uninstall bool
	cmd := &cobra.Command{
		Use:   "dns-setup",
		Short: "One-time setup so services share canonical ports (5432, 6379, …) by DNS name",
		Long: "dns-setup aliases a small pool of loopback addresses (127.0.0.2–127.0.0.65)\n" +
			"onto lo0, so doze can give every service its own address and expose them\n" +
			"all on their canonical port — every Postgres on 5432, reached by name\n" +
			"(orders-pg.<stack>.doze) instead of a hand-picked high port.\n\n" +
			"macOS restricts loopback aliases to root, so this is the one command that\n" +
			"needs sudo — run it once. It installs a launchd job so the range persists\n" +
			"across reboots. Linux binds all of 127.0.0.0/8 already; nothing to do.\n\n" +
			"--check reports the current state without sudo; --uninstall removes it.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if runtime.GOOS == "linux" {
				fmt.Println(ui.OK("✓") + " Linux binds all of 127.0.0.0/8 — no setup needed.")
				return nil
			}
			if runtime.GOOS != "darwin" {
				return fmt.Errorf("dns-setup supports macOS and Linux; on %s, ensure 127.0.0.2+ are bindable", runtime.GOOS)
			}

			switch {
			case check:
				return dnsSetupCheck()
			case uninstall:
				return dnsSetupUninstall()
			default:
				return dnsSetupInstall()
			}
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report whether the loopback range is set up (no sudo)")
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "remove the loopback aliasing (needs sudo)")
	return cmd
}

func dnsSetupCheck() error {
	aliased := loopback.Available()
	_, plistErr := os.Stat(loopback.LaunchdPath)
	persistent := plistErr == nil
	resolver := daemon.ResolverConfigured()

	mark := func(ok bool) string {
		if ok {
			return ui.OK("✓")
		}
		return ui.Fail("✗")
	}
	fmt.Printf("  %s  loopback range active   %s\n", mark(aliased), aliasStateText(aliased))
	fmt.Printf("  %s  persists across reboots %s\n", mark(persistent), plistStateText(persistent))
	fmt.Printf("  %s  resolver route          %s\n", mark(resolver), resolverStateText(resolver))
	if aliased && persistent && resolver {
		fmt.Println("\n" + ui.OK("ready") + " — services can share canonical ports by DNS name.")
		return nil
	}
	fmt.Println("\nrun " + ui.Title("doze dns-setup") + " to enable it (one sudo).")
	return exitCodeError(1)
}

func resolverStateText(ok bool) string {
	if ok {
		return ui.Muted("*." + config.DomainSuffix + " → 127.0.0.1:" + fmt.Sprint(daemon.DNSPort))
	}
	return ui.Muted("/etc/resolver/" + config.DomainSuffix + " not installed")
}

func aliasStateText(ok bool) string {
	if ok {
		return ui.Muted("127.0.0.2–127.0.0.65 bindable")
	}
	return ui.Muted("not aliased — services fall back to unique ports")
}

func plistStateText(ok bool) string {
	if ok {
		return ui.Muted(loopback.LaunchdPath)
	}
	return ui.Muted("no launchd job installed")
}

// dnsSetupInstall writes the launchd plist and loads it under one sudo prompt,
// which both aliases the range now (RunAtLoad) and re-aliases it at every boot.
func dnsSetupInstall() error {
	// Skip only when everything is in place AND the installed launchd plist still
	// matches what we'd write — otherwise (e.g. the alias pool changed) re-apply so
	// the update actually lands.
	if loopback.Available() && daemon.ResolverConfigured() {
		if cur, err := os.ReadFile(loopback.LaunchdPath); err == nil && string(cur) == loopback.LaunchdPlist() {
			fmt.Println(ui.OK("✓") + " already set up — nothing to do.")
			return nil
		}
	}

	// The whole privileged step is one heredoc so the user sees exactly one
	// sudo prompt: alias the loopback range (launchd), AND route *.doze to
	// the unicast resolver. The resolver route is required in per-service mode:
	// macOS getaddrinfo drops non-127.0.0.1 loopback addresses learned via mDNS
	// (a security guard), so per-service IPs must come over unicast DNS instead.
	// Reload (bootout + load) rather than a bare load so re-running after the pool
	// grows re-fires RunAtLoad and aliases the new blocks in this session, not just
	// at the next boot.
	script := fmt.Sprintf(`set -e
cat > %s <<'PLIST'
%sPLIST
launchctl bootout system %s 2>/dev/null || launchctl unload %s 2>/dev/null || true
launchctl load -w %s 2>/dev/null || launchctl bootstrap system %s 2>/dev/null || true
mkdir -p /etc/resolver
rm -f /etc/resolver/doze.local
printf 'nameserver 127.0.0.1\nport %d\n' > /etc/resolver/%s`,
		loopback.LaunchdPath, loopback.LaunchdPlist(), loopback.LaunchdPath, loopback.LaunchdPath,
		loopback.LaunchdPath, loopback.LaunchdPath,
		daemon.DNSPort, config.DomainSuffix)

	fmt.Println(ui.Muted("doze needs sudo once to alias a small loopback pool (127.0.0.2–127.0.0.65) onto lo0"))
	fmt.Println(ui.Muted("and install a launchd job so they persist. You'll be prompted for your password."))
	fmt.Println()

	c := exec.Command("sudo", "sh", "-c", script)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, ui.Fail("✗")+" sudo step failed. To do it by hand:")
		fmt.Fprintln(os.Stderr, "  sudo tee "+loopback.LaunchdPath+" <<'PLIST'")
		fmt.Fprint(os.Stderr, loopback.LaunchdPlist())
		fmt.Fprintln(os.Stderr, "PLIST")
		fmt.Fprintln(os.Stderr, "  sudo launchctl load -w "+loopback.LaunchdPath)
		return fmt.Errorf("dns-setup: %w", err)
	}

	// launchd applies RunAtLoad asynchronously, so the aliases may land a beat
	// after launchctl returns — poll briefly before declaring failure.
	ready := false
	for i := 0; i < 20; i++ {
		if loopback.Available() {
			ready = true
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !ready {
		return fmt.Errorf("dns-setup ran but 127.0.0.2 is still not bindable — check `sudo ifconfig lo0` (the launchd job may take a moment; re-run `doze dns-setup --check`)")
	}
	fmt.Println()
	fmt.Println(ui.OK("✓") + " loopback range aliased + *." + config.DomainSuffix + " routed to the doze resolver.")
	fmt.Println(ui.Muted("  services now share canonical ports by DNS name — e.g. two Postgres both on 5432."))
	return nil
}

func dnsSetupUninstall() error {
	script := strings.Join([]string{
		"launchctl unload " + loopback.LaunchdPath + " 2>/dev/null || true",
		"rm -f " + loopback.LaunchdPath,
		"rm -f /etc/resolver/" + config.DomainSuffix,
	}, "\n")
	c := exec.Command("sudo", "sh", "-c", script)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("dns-setup --uninstall: %w", err)
	}
	fmt.Println(ui.OK("✓") + " removed. Aliases clear on the next reboot (or `sudo ifconfig lo0 -alias 127.0.0.2` …).")
	return nil
}
