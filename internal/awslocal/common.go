// Package awslocal hosts doze's "local AWS" services (S3, SQS, SNS) inside the
// doze binary itself. Each service is a plain net/http handler; the daemon runs
// it as a child process via the hidden `doze __serve <service>` subcommand (see
// Serve), fronts it with the per-instance proxy, and reaps it when idle — so the
// same lazy boot-on-connect lifecycle as every other engine, with no external
// binary, no Docker, and no JVM.
//
// This package is a leaf: the engine drivers (engine/{s3,sqs,sns}) embed
// BaseDriver from here, and the service implementations (internal/{s3srv,sqssrv,
// snssrv}) register their handler factories here. It imports neither, so there
// is no cycle.
package awslocal

import "fmt"

// Conventional local-AWS identity. These match the values LocalStack uses, so
// tools and copy-pasted snippets that assume them keep working.
const (
	Region          = "us-east-1"
	AccountID       = "000000000000"
	AccessKeyID     = "test"
	SecretAccessKey = "test"

	// HealthPath is the readiness endpoint Serve always mounts and WaitReady
	// polls; it is namespaced so it can never collide with a real AWS route.
	HealthPath = "/_doze/health"
)

// ARN builds an AWS ARN for a resource of the given service (e.g.
// ARN("sqs", "my-queue") -> arn:aws:sqs:us-east-1:000000000000:my-queue).
func ARN(service, resource string) string {
	return fmt.Sprintf("arn:aws:%s:%s:%s:%s", service, Region, AccountID, resource)
}
