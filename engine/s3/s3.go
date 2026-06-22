// Package s3 implements the doze engine.Driver for a local, S3-compatible object
// store. The server is built into doze (internal/s3srv, embedding gofakes3) and
// run via the shared awslocal.BaseDriver self-exec path; this driver adds the
// config schema (buckets) and a Converger that creates declared buckets.
package s3

import (
	"github.com/nerdmenot/doze/internal/awslocal"
	"github.com/nerdmenot/doze/internal/engine"

	_ "github.com/nerdmenot/doze/internal/s3srv" // register the s3 service factory
)

func init() {
	engine.Register(Driver{awslocal.BaseDriver{Name: "s3", EndpointEnv: "AWS_ENDPOINT_URL_S3"}})
}

// Logf is the sink for convergence warnings; cmd/doze points it at stderr.
var Logf = func(string, ...any) {}

// Driver is the S3 engine driver. It embeds BaseDriver for the resolve/spawn/
// ready/connstring/env boilerplate and adds DecodeConfig + Converge.
type Driver struct {
	awslocal.BaseDriver
}
