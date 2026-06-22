package s3

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/nerdmenot/doze/internal/engine"
)

// Config is the decoded `s3 "<name>" { … }` block.
type Config struct {
	Buckets []Bucket
}

// Bucket is one declared bucket.
type Bucket struct {
	Name       string
	Versioning bool
}

// DecodeConfig implements engine.ConfigDecoder.
func (Driver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string) (engine.EngineConfig, error) {
	var raw struct {
		Buckets []struct {
			Name       string `hcl:"name,label"`
			Versioning bool   `hcl:"versioning,optional"`
		} `hcl:"bucket,block"`
	}
	if d := gohcl.DecodeBody(body, ctx, &raw); d.HasErrors() {
		return nil, fmt.Errorf("%s", d.Error())
	}
	c := &Config{}
	seen := map[string]bool{}
	for _, b := range raw.Buckets {
		if b.Name == "" {
			return nil, fmt.Errorf("s3 bucket needs a name")
		}
		if seen[b.Name] {
			return nil, fmt.Errorf("s3 bucket %q is declared more than once", b.Name)
		}
		seen[b.Name] = true
		c.Buckets = append(c.Buckets, Bucket{Name: b.Name, Versioning: b.Versioning})
	}
	return c, nil
}
