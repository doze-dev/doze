package sns

import (
	"context"
	"fmt"
	"net/url"

	"github.com/nerdmenot/doze/internal/awslocal"
	"github.com/nerdmenot/doze/internal/engine"
)

// Objects implements engine.Inventory: each declared topic is a tracked object.
// Subscriptions are not tracked — they are recreated idempotently by Converge and
// deleting one needs a SubscriptionArn doze doesn't persist; deleting the topic
// removes them anyway.
func (Driver) Objects(inst engine.Instance) []engine.Object {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg == nil {
		return nil
	}
	objs := make([]engine.Object, 0, len(cfg.Topics))
	for _, name := range cfg.Topics {
		objs = append(objs, engine.Object{Kind: "topic", Name: name, Hash: engine.HashOf(name)})
	}
	return objs
}

// Prune implements engine.Pruner: delete topics no longer declared.
func (Driver) Prune(ctx context.Context, _ engine.Instance, _ engine.Toolchain, ep engine.Endpoint, removed []engine.Object) error {
	client := awslocal.UnixHTTPClient(ep.Backend)
	for _, o := range removed {
		if o.Kind != "topic" {
			continue
		}
		form := url.Values{"Action": {"DeleteTopic"}, "TopicArn": {awslocal.ARN("sns", o.Name)}}
		if err := snsPost(ctx, client, form); err != nil {
			return fmt.Errorf("deleting topic %q: %w", o.Name, err)
		}
	}
	return nil
}
