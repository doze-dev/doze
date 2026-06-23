package s3

import (
	"context"
	"fmt"
	"net/http"

	"github.com/nerdmenot/doze/internal/awslocal"
	"github.com/nerdmenot/doze/internal/engine"
)

// Objects implements engine.Inventory: each declared bucket is a tracked object.
func (Driver) Objects(inst engine.Instance) []engine.Object {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg == nil {
		return nil
	}
	objs := make([]engine.Object, 0, len(cfg.Buckets))
	for _, b := range cfg.Buckets {
		objs = append(objs, engine.Object{Kind: "bucket", Name: b.Name, Hash: engine.HashOf(b)})
	}
	return objs
}

// Prune implements engine.Pruner: delete buckets no longer declared.
func (Driver) Prune(ctx context.Context, _ engine.Instance, _ engine.Toolchain, ep engine.Endpoint, removed []engine.Object) error {
	client := awslocal.UnixHTTPClient(ep.Backend)
	for _, o := range removed {
		if o.Kind != "bucket" {
			continue
		}
		if err := deleteBucket(ctx, client, o.Name); err != nil {
			return fmt.Errorf("deleting bucket %q: %w", o.Name, err)
		}
	}
	return nil
}

func deleteBucket(ctx context.Context, c *http.Client, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "http://unix/"+name, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer drain(resp)
	switch {
	case resp.StatusCode/100 == 2, resp.StatusCode == http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("DeleteBucket returned %s", resp.Status)
	}
}
