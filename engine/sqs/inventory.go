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

// Objects implements engine.Inventory: each declared queue is a tracked object,
// fingerprinted by its attributes so an attribute change shows as an update.
func (Driver) Objects(inst engine.Instance) []engine.Object {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg == nil {
		return nil
	}
	objs := make([]engine.Object, 0, len(cfg.Queues))
	for _, q := range cfg.Queues {
		objs = append(objs, engine.Object{Kind: "queue", Name: q.Name, Hash: engine.HashOf(q)})
	}
	return objs
}

// Prune implements engine.Pruner: delete queues no longer declared.
func (Driver) Prune(ctx context.Context, _ engine.Instance, _ engine.Toolchain, ep engine.Endpoint, removed []engine.Object) error {
	client := awslocal.UnixHTTPClient(ep.Backend)
	for _, o := range removed {
		if o.Kind != "queue" {
			continue
		}
		if err := deleteQueue(ctx, client, o.Name); err != nil {
			return fmt.Errorf("deleting queue %q: %w", o.Name, err)
		}
	}
	return nil
}

func deleteQueue(ctx context.Context, c *http.Client, name string) error {
	// The local server's DeleteQueue accepts a bare QueueName (no URL needed).
	payload, _ := json.Marshal(map[string]any{"QueueName": name})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonSQS.DeleteQueue")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DeleteQueue returned %s: %s", resp.Status, body)
	}
	return nil
}
