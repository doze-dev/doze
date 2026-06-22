package sqs

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/nerdmenot/doze/internal/awslocal"
	"github.com/nerdmenot/doze/internal/engine"
)

// Config is the decoded `sqs "<name>" { … }` block.
type Config struct {
	Queues []QueueDecl
}

// QueueDecl is a declared queue plus the SQS attribute map to create it with.
type QueueDecl struct {
	Name  string
	Attrs map[string]string
}

// DecodeConfig implements engine.ConfigDecoder.
func (Driver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string) (engine.EngineConfig, error) {
	var raw struct {
		Queues []struct {
			Name              string `hcl:"name,label"`
			FIFO              bool   `hcl:"fifo,optional"`
			ContentBasedDedup bool   `hcl:"content_based_dedup,optional"`
			VisibilityTimeout string `hcl:"visibility_timeout,optional"`
			Delay             string `hcl:"delay,optional"`
			Retention         string `hcl:"retention,optional"`
			WaitTime          string `hcl:"wait_time,optional"`
			MaxMessageSize    int    `hcl:"max_message_size,optional"`
		} `hcl:"queue,block"`
		Redrives []struct {
			Queue           string `hcl:"queue,label"`
			DeadLetter      string `hcl:"dead_letter"`
			MaxReceiveCount int    `hcl:"max_receive_count"`
		} `hcl:"redrive,block"`
	}
	if d := gohcl.DecodeBody(body, ctx, &raw); d.HasErrors() {
		return nil, fmt.Errorf("%s", d.Error())
	}

	c := &Config{}
	index := map[string]int{}
	for _, q := range raw.Queues {
		if q.Name == "" {
			return nil, fmt.Errorf("sqs queue needs a name")
		}
		if _, dup := index[q.Name]; dup {
			return nil, fmt.Errorf("sqs queue %q is declared more than once", q.Name)
		}
		attrs := map[string]string{}
		if q.FIFO {
			attrs["FifoQueue"] = "true"
		}
		if q.ContentBasedDedup {
			attrs["ContentBasedDeduplication"] = "true"
		}
		if err := setSeconds(attrs, "VisibilityTimeout", q.VisibilityTimeout); err != nil {
			return nil, fmt.Errorf("sqs queue %q: %w", q.Name, err)
		}
		if err := setSeconds(attrs, "DelaySeconds", q.Delay); err != nil {
			return nil, fmt.Errorf("sqs queue %q: %w", q.Name, err)
		}
		if err := setSeconds(attrs, "MessageRetentionPeriod", q.Retention); err != nil {
			return nil, fmt.Errorf("sqs queue %q: %w", q.Name, err)
		}
		if err := setSeconds(attrs, "ReceiveMessageWaitTimeSeconds", q.WaitTime); err != nil {
			return nil, fmt.Errorf("sqs queue %q: %w", q.Name, err)
		}
		if q.MaxMessageSize > 0 {
			attrs["MaximumMessageSize"] = strconv.Itoa(q.MaxMessageSize)
		}
		index[q.Name] = len(c.Queues)
		c.Queues = append(c.Queues, QueueDecl{Name: q.Name, Attrs: attrs})
	}

	for _, rd := range raw.Redrives {
		i, ok := index[rd.Queue]
		if !ok {
			return nil, fmt.Errorf("sqs redrive references undeclared queue %q", rd.Queue)
		}
		if rd.DeadLetter == "" {
			return nil, fmt.Errorf("sqs redrive for %q needs a dead_letter queue", rd.Queue)
		}
		policy, _ := json.Marshal(map[string]string{
			"deadLetterTargetArn": awslocal.ARN("sqs", rd.DeadLetter),
			"maxReceiveCount":     strconv.Itoa(rd.MaxReceiveCount),
		})
		c.Queues[i].Attrs["RedrivePolicy"] = string(policy)
	}
	return c, nil
}

// setSeconds parses a duration ("30s", "5m") or bare seconds and stores it under
// key; empty leaves the attribute unset (the server applies its default).
func setSeconds(attrs map[string]string, key, val string) error {
	if val == "" {
		return nil
	}
	if d, err := time.ParseDuration(val); err == nil {
		attrs[key] = strconv.Itoa(int(d.Seconds()))
		return nil
	}
	if n, err := strconv.Atoi(val); err == nil {
		attrs[key] = strconv.Itoa(n)
		return nil
	}
	return fmt.Errorf("invalid duration %q for %s", val, key)
}
