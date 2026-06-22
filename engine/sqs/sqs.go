// Package sqs implements the doze engine.Driver for a local, SQS-compatible
// queue service. The server is built into doze (internal/sqssrv, pure Go) and
// run via the shared awslocal.BaseDriver self-exec path; this driver adds the
// config schema (queues + redrive) and a Converger that creates declared queues.
package sqs

import (
	"github.com/nerdmenot/doze/internal/awslocal"
	"github.com/nerdmenot/doze/internal/engine"

	_ "github.com/nerdmenot/doze/internal/sqssrv" // register the sqs service factory
)

func init() {
	engine.Register(Driver{awslocal.BaseDriver{Name: "sqs", EndpointEnv: "AWS_ENDPOINT_URL_SQS"}})
}

// Logf is the sink for convergence warnings; cmd/doze points it at stderr.
var Logf = func(string, ...any) {}

// Driver is the SQS engine driver.
type Driver struct {
	awslocal.BaseDriver
}
