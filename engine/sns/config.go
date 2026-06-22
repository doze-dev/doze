package sns

import (
	"encoding/json"
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/nerdmenot/doze/internal/engine"
)

// Config is the decoded `sns "<name>" { … }` block.
type Config struct {
	SQS    string // backing sqs instance name for fanout ("" if none)
	Topics []string
	Subs   []SubDecl
}

// SubDecl is a declared subscription.
type SubDecl struct {
	Topic        string
	Protocol     string
	Endpoint     string
	Raw          bool
	FilterPolicy string // JSON, "" if none
}

// DecodeConfig implements engine.ConfigDecoder.
func (Driver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string) (engine.EngineConfig, error) {
	var raw struct {
		SQS    string `hcl:"sqs,optional"`
		Topics []struct {
			Name string `hcl:"name,label"`
		} `hcl:"topic,block"`
		Subs []struct {
			Topic    string              `hcl:"topic,label"`
			Protocol string              `hcl:"protocol"`
			Endpoint string              `hcl:"endpoint"`
			Raw      bool                `hcl:"raw,optional"`
			Filter   map[string][]string `hcl:"filter,optional"`
		} `hcl:"subscribe,block"`
	}
	if d := gohcl.DecodeBody(body, ctx, &raw); d.HasErrors() {
		return nil, fmt.Errorf("%s", d.Error())
	}

	c := &Config{SQS: raw.SQS}
	seen := map[string]bool{}
	for _, t := range raw.Topics {
		if t.Name == "" {
			return nil, fmt.Errorf("sns topic needs a name")
		}
		if seen[t.Name] {
			return nil, fmt.Errorf("sns topic %q is declared more than once", t.Name)
		}
		seen[t.Name] = true
		c.Topics = append(c.Topics, t.Name)
	}
	for _, s := range raw.Subs {
		if s.Protocol == "" || s.Endpoint == "" {
			return nil, fmt.Errorf("sns subscribe %q needs protocol and endpoint", s.Topic)
		}
		sd := SubDecl{Topic: s.Topic, Protocol: s.Protocol, Endpoint: s.Endpoint, Raw: s.Raw}
		if len(s.Filter) > 0 {
			fp, _ := json.Marshal(s.Filter)
			sd.FilterPolicy = string(fp)
		}
		c.Subs = append(c.Subs, sd)
	}
	return c, nil
}
