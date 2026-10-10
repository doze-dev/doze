// The AWS front door: ONE host per stack — aws.<stack>.doze on the shared :80
// listener — forwarded whole to the stack's aws instance. The instance is the
// entire local AWS (every service behind one gateway, routed by protocol, with
// the web console at /_console), so doze's job here is exactly one hop: host →
// backend. No path dispatch, no resource extraction, no synthesized calls.
package daemon

import (
	"net"

	"github.com/doze-dev/doze/internal/config"
)

// buildAWSRoutes derives the stack's AWS forward (aws.<stack>.doze → the aws
// instance) and makes the host resolve to the front door. It returns the route
// to publish, host → target.
func (d *Daemon) buildAWSRoutes(plan *bindPlan) map[string]string {
	host := "aws." + d.cfg.Stack() + "." + config.DomainSuffix
	for _, decl := range d.cfg.Instances {
		if !decl.Enabled || decl.Type != config.AWSUnifiedType {
			continue
		}
		target := plan.bind[decl.Name]
		if target == "" {
			continue
		}
		// The instance's directly-addressable thing is its console.
		if d.resources != nil {
			d.resources[decl.Name] = "http://" + host + "/" + config.AWSConsolePrefix
		}
		// The host resolves to the front door (127.0.0.1:80, wildcard bind).
		plan.resolve[host] = net.IPv4(127, 0, 0, 1)
		return map[string]string{host: target}
	}
	return map[string]string{}
}
