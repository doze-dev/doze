package sqs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/nerdmenot/doze/internal/awslocal"
	"github.com/nerdmenot/doze/internal/engine"
)

// Converge implements engine.Converger: create each declared queue (idempotent).
// It speaks the SQS JSON protocol over the instance's backend unix socket, so it
// needs no AWS SDK. RedrivePolicy is part of each queue's attributes.
func (Driver) Converge(ctx context.Context, inst engine.Instance, _ engine.Toolchain, ep engine.Endpoint) error {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg == nil || len(cfg.Queues) == 0 {
		return nil
	}
	client := awslocal.UnixHTTPClient(ep.Backend)
	for _, q := range cfg.Queues {
		if err := createQueue(ctx, client, q); err != nil {
			return fmt.Errorf("queue %q: %w", q.Name, err)
		}
	}
	return nil
}

func createQueue(ctx context.Context, c *http.Client, q QueueDecl) error {
	payload, _ := json.Marshal(map[string]any{
		"QueueName":  q.Name,
		"Attributes": q.Attrs,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonSQS.CreateQueue")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CreateQueue returned %s: %s", resp.Status, body)
	}
	return nil
}
