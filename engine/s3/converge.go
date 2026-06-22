package s3

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nerdmenot/doze/internal/awslocal"
	"github.com/nerdmenot/doze/internal/engine"
)

// Converge implements engine.Converger: create each declared bucket (idempotent)
// and, where requested, enable versioning. It talks plain S3 over the instance's
// backend unix socket — CreateBucket is `PUT /<bucket>` — so it needs no AWS SDK.
func (Driver) Converge(ctx context.Context, inst engine.Instance, _ engine.Toolchain, ep engine.Endpoint) error {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg == nil || len(cfg.Buckets) == 0 {
		return nil
	}
	client := awslocal.UnixHTTPClient(ep.Backend)
	for _, b := range cfg.Buckets {
		if err := createBucket(ctx, client, b.Name); err != nil {
			return fmt.Errorf("bucket %q: %w", b.Name, err)
		}
		if b.Versioning {
			if err := enableVersioning(ctx, client, b.Name); err != nil {
				// The bolt backend does not support versioning; warn, don't fail.
				Logf("warning: s3 %q: could not enable versioning on %q (backend limitation): %v", inst.Name, b.Name, err)
			}
		}
	}
	return nil
}

// createBucket issues an idempotent path-style CreateBucket. A 409 means the
// bucket already exists and is ours — also success.
func createBucket(ctx context.Context, c *http.Client, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://unix/"+name, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer drain(resp)
	switch {
	case resp.StatusCode/100 == 2, resp.StatusCode == http.StatusConflict:
		return nil
	default:
		return fmt.Errorf("CreateBucket returned %s", resp.Status)
	}
}

const versioningEnabledBody = `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`

func enableVersioning(ctx context.Context, c *http.Client, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://unix/"+name+"?versioning", strings.NewReader(versioningEnabledBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("PutBucketVersioning returned %s", resp.Status)
	}
	return nil
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
