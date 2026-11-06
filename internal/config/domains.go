// Local DNS naming: the .doze TLD, stack/service labels, and the
// domain-collision check.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	names "github.com/doze-dev/doze-names"
	"golang.org/x/net/publicsuffix"
)

// DomainSuffix is the private TLD every doze stack lives under (a service is
// <name>.<stack>.doze). It is deliberately NOT under .local: .local is reserved
// for multicast DNS, so macOS routes *.local through mDNSResponder — slow, and it
// pressures that daemon. A plain .doze TLD resolves purely by unicast: the
// one-time /etc/resolver/doze drop-in sends *.doze straight to the built-in
// resolver on 127.0.0.1:5323 (see doctor / `doze dns-setup`), no multicast in the
// path. (.doze isn't an RFC-reserved name like .test, but it isn't a delegated
// gTLD either, so there's nothing to collide with on the public internet.)
const DomainSuffix = "doze"

// DomainLabel sanitizes a name into a valid DNS label: lowercase, with
// underscores and every other invalid rune collapsed to hyphens.
func DomainLabel(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// Stack returns this stack's DNS label: the `name` root attribute when set,
// else the config directory's name — sanitized either way.
func (c *Config) Stack() string {
	n := c.StackName
	if n == "" {
		// Absolute first. A config found in the working directory has a
		// relative path, so the directory is "." and its base is "." — which
		// sanitizes to nothing and falls through to "default". The daemon
		// resolves the path before it loads, so it registered the real name
		// while every CLI command printed aws.default.doze, a host that
		// resolves to NXDOMAIN. `doze env` emitted it too.
		dir := configDirOf(c.path)
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		n = filepath.Base(dir)
	}
	if l := DomainLabel(n); l != "" {
		return l
	}
	return "default"
}

// DomainFor returns an instance's local DNS name when defaults{names=true}:
// <service>.<stack>.doze (e.g. orders-pg.demo.doze).
func (c *Config) DomainFor(name string) string {
	return DomainLabel(name) + "." + c.Stack() + "." + DomainSuffix
}

// validateDomains checks that domain publication can't produce two instances
// with the same sanitized hostname ("orders_pg" and "orders-pg" both become
// orders-pg.local) — a silent collision would route one service's clients at
// the other.
func (cfg *Config) validateDomains() error {
	if !cfg.Defaults.Domains {
		return nil
	}
	owner := map[string]string{}
	for _, decl := range cfg.Instances {
		if !decl.Enabled {
			continue
		}
		d := cfg.DomainFor(decl.Name)
		if other, dup := owner[d]; dup {
			return fmt.Errorf("domain conflict: %q and %q both publish %s — rename one so the sanitized hostnames differ", other, decl.Name, d)
		}
		owner[d] = decl.Name
	}
	return nil
}

// checkDomains validates an app's own domains and certificate files
// (decode.go common): the names as names.ValidDomain has them, both files
// or neither, resolved against the block's folder and present.
// shape says only the config's shape is being read (lint, the docs): the
// files are not looked for then.
func checkDomains(head blockHead, what string, domains []string, cert, key, baseDir string, shape bool) ([]string, string, string, error) {
	if len(domains) == 0 && cert == "" && key == "" {
		return nil, "", "", nil
	}
	if head.kind != KindApp {
		return nil, "", "", fmt.Errorf("%s: `domains` is for app blocks; a %s is reached by its doze name", what, head.kind)
	}
	if len(domains) == 0 {
		return nil, "", "", fmt.Errorf("%s: `cert` and `key` go with `domains`", what)
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range domains {
		if err := names.ValidDomain(d); err != nil {
			return nil, "", "", fmt.Errorf("%s: domains: %v", what, err)
		}
		d = strings.TrimSuffix(strings.ToLower(d), ".")
		// A bare suffix (com, co.uk, github.io) is everyone's: routing it here
		// would take the whole of it for this machine.
		if _, err := publicsuffix.EffectiveTLDPlusOne(names.RouteDomain(d)); err != nil {
			return nil, "", "", fmt.Errorf("%s: domains: %q is a public suffix, not a domain of yours; declare a name under it", what, d)
		}
		if seen[d] {
			return nil, "", "", fmt.Errorf("%s: domains: %q is listed twice", what, d)
		}
		seen[d] = true
		out = append(out, d)
	}
	if (cert == "") != (key == "") {
		return nil, "", "", fmt.Errorf("%s: set both `cert` and `key`, or neither (doze then mints a certificate)", what)
	}
	if cert != "" {
		cert, key = absPath(baseDir, cert), absPath(baseDir, key)
	}
	if cert != "" && !shape {
		for _, f := range []string{cert, key} {
			if st, err := os.Stat(f); err != nil || st.IsDir() {
				return nil, "", "", fmt.Errorf("%s: %s is not a file", what, f)
			}
		}
		for _, d := range out {
			if _, err := names.CheckCertificate(cert, key, d); err != nil {
				return nil, "", "", fmt.Errorf("%s: %v", what, err)
			}
		}
	}
	return out, cert, key, nil
}

func absPath(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// validateAppDomains checks that no two apps declare one domain, and that
// domains are declared only where names are on, since they live in the
// same registry and are fronted by the same door.
func (cfg *Config) validateAppDomains() error {
	owner := map[string]string{}
	for _, decl := range cfg.Instances {
		for _, d := range decl.Domains {
			if other, dup := owner[d]; dup {
				return fmt.Errorf("domain conflict: %q and %q both declare %s", other, decl.Name, d)
			}
			owner[d] = decl.Name
		}
		if len(decl.Domains) > 0 && !cfg.Defaults.Domains {
			if names.Disabled() {
				return fmt.Errorf("%q declares domains, but names are switched off here (%s); unset it to serve them", decl.Name, names.EnvZone)
			}
			return fmt.Errorf("%q declares domains, which need names on: add `names = true` to the defaults block", decl.Name)
		}
	}
	return nil
}

// AppDomains lists every domain the stack declares, for the trust record
// and the front door.
func (cfg *Config) AppDomains() []string {
	var out []string
	for _, decl := range cfg.Decls() {
		if decl.Enabled {
			out = append(out, decl.Domains...)
		}
	}
	return out
}
