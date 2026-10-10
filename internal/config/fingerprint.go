package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Fingerprint is a stable hash of one declared block: what it is, how it is
// configured and what it waits for. Two loads of the same text give the same
// value, and a change to any argument gives a different one, which is what
// lets `doze sync` say which blocks differ from the ones a daemon started with.
func (d *InstanceDecl) Fingerprint() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%s|%d|%v\n", d.Kind, d.Type, d.Name, d.Version, d.Listen, d.Port, d.Enabled)
	deps := make([]string, 0, len(d.Deps))
	for _, dep := range d.Deps {
		deps = append(deps, dep.Name+":"+string(dep.Condition))
	}
	sort.Strings(deps)
	fmt.Fprintf(h, "%v\n", deps)
	switch s := d.Spec.(type) {
	case nil:
	case interface{ Raw() []byte }: // a module's own serialized config
		h.Write(s.Raw())
	default:
		// encoding/json sorts map keys, so the same config hashes the same.
		if b, err := json.Marshal(s); err == nil {
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// BlockFingerprints is the fingerprint of every declared block, by name.
func (c *Config) BlockFingerprints() map[string]BlockPrint {
	out := make(map[string]BlockPrint, len(c.Instances))
	for _, d := range c.Instances {
		out[d.Name] = BlockPrint{Kind: d.Kind, Type: d.Type, Fingerprint: d.Fingerprint()}
	}
	return out
}

// BlockPrint is one block as a daemon recorded it.
type BlockPrint struct {
	Kind        string `json:"kind"`
	Type        string `json:"type"`
	Fingerprint string `json:"fp"`
}
