// The aws engine's shared, port-less endpoint.
package config

// AWSUnifiedType is the engine that runs the WHOLE local AWS as one instance:
// every service behind one gateway (routed by protocol, like the standalone
// doze-aws binary's :4566), with the web console at /_console and the traffic
// recorder in the request path. It is the only engine the :80 ingress fronts
// with an AWS host.
const AWSUnifiedType = "aws"

// AWSConsolePrefix is the web console's path on the aws endpoint (the
// console's own mount prefix inside the instance).
const AWSConsolePrefix = "_console"

// IsAWSBuiltin reports whether an engine type is served behind the shared AWS
// endpoint (its per-instance address is internal-only).
func IsAWSBuiltin(engineType string) bool { return engineType == AWSUnifiedType }

// AWSBaseHost returns the stack's AWS host in domains mode (aws.demo.doze) —
// one DNS name for the whole surface.
func (c *Config) AWSBaseHost() (string, bool) {
	if !c.Defaults.Domains {
		return "", false
	}
	return "aws." + c.Stack() + "." + DomainSuffix, true
}

// AWSHost returns the resolvable host serving the aws engine in domains mode,
// and false for any other engine or when domains are off.
func (c *Config) AWSHost(engineType string) (string, bool) {
	if engineType != AWSUnifiedType {
		return "", false
	}
	return c.AWSBaseHost()
}

// AWSEndpoint returns the port-less SDK endpoint URL for the aws engine in
// domains mode (http://aws.demo.doze) — what AWS_ENDPOINT_URL and an
// `aws.<name>.url` reference resolve to. One endpoint configures every SDK.
func (c *Config) AWSEndpoint(engineType string) (string, bool) {
	if host, ok := c.AWSHost(engineType); ok {
		return "http://" + host, true
	}
	return "", false
}
