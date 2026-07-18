// Local DNS naming: the .doze TLD, stack/service labels, and the
// domain-collision check.
package config

import (
	"fmt"
	"path/filepath"
	"strings"
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
		n = filepath.Base(configDirOf(c.path))
	}
	if l := DomainLabel(n); l != "" {
		return l
	}
	return "default"
}

// DomainFor returns an instance's local DNS name when defaults{domains=true}:
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
