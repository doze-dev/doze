package sns

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nerdmenot/doze/internal/awslocal"
	"github.com/nerdmenot/doze/internal/engine"
)

// Converge implements engine.Converger: create declared topics and subscriptions
// by speaking the SNS Query protocol over the instance's backend unix socket.
func (Driver) Converge(ctx context.Context, inst engine.Instance, _ engine.Toolchain, ep engine.Endpoint) error {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg == nil {
		return nil
	}
	client := awslocal.UnixHTTPClient(ep.Backend)
	for _, name := range cfg.Topics {
		if err := snsPost(ctx, client, url.Values{"Action": {"CreateTopic"}, "Name": {name}}); err != nil {
			return fmt.Errorf("topic %q: %w", name, err)
		}
	}
	for _, sub := range cfg.Subs {
		form := url.Values{
			"Action":                {"Subscribe"},
			"TopicArn":              {awslocal.ARN("sns", sub.Topic)},
			"Protocol":              {sub.Protocol},
			"Endpoint":              {subscriptionEndpoint(sub)},
			"ReturnSubscriptionArn": {"true"},
		}
		i := 1
		add := func(k, v string) {
			form.Set(fmt.Sprintf("Attributes.entry.%d.key", i), k)
			form.Set(fmt.Sprintf("Attributes.entry.%d.value", i), v)
			i++
		}
		if sub.Raw {
			add("RawMessageDelivery", "true")
		}
		if sub.FilterPolicy != "" {
			add("FilterPolicy", sub.FilterPolicy)
		}
		if err := snsPost(ctx, client, form); err != nil {
			return fmt.Errorf("subscribe %q -> %s: %w", sub.Topic, sub.Endpoint, err)
		}
	}
	return nil
}

// subscriptionEndpoint normalizes an sqs subscription endpoint to a queue ARN
// (AWS-faithful); http(s) endpoints pass through unchanged.
func subscriptionEndpoint(sub SubDecl) string {
	if sub.Protocol == "sqs" && !strings.HasPrefix(sub.Endpoint, "arn:") {
		name := sub.Endpoint
		if i := strings.LastIndexAny(name, "/:"); i >= 0 {
			name = name[i+1:]
		}
		return awslocal.ARN("sqs", name)
	}
	return sub.Endpoint
}

func snsPost(ctx context.Context, c *http.Client, form url.Values) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s returned %s: %s", form.Get("Action"), resp.Status, body)
	}
	return nil
}
