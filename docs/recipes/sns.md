# Recipes — SNS (pub/sub)

A ground-up, pure-Go SNS built into doze. Create topics and subscriptions, apply
message-attribute filter policies, and fan out to SQS queues or HTTP(S) webhooks.

## SNS → SQS fanout

Name the backing `sqs` instance; doze holds it running while SNS is up and
delivers to its queues.

```hcl
sqs "jobs" {
  queue "emails" {}
}

sns "events" {
  sqs = sqs.jobs.name           # typed reference to the backing SQS instance

  topic "signups" {}
  subscribe "signups" {
    protocol = "sqs"
    endpoint = "emails"         # the jobs/emails queue (name or queue ARN)
    raw      = true             # raw delivery: body is the message, no envelope
  }
}
```

```sh
eval "$(doze env)"             # AWS_ENDPOINT_URL_SNS / _SQS + creds
aws sns publish \
  --topic-arn arn:aws:sns:us-east-1:000000000000:signups \
  --message "welcome!"

# it lands in the jobs/emails queue:
url=$(aws sqs get-queue-url --queue-name emails --query QueueUrl --output text)
aws sqs receive-message --queue-url "$url"
```

Drop `raw = true` and the queue receives the standard SNS JSON envelope
(`{"Type":"Notification","Message":...,"MessageAttributes":...}`) instead of the
bare body.

## Multiple subscribers + filter policies

A topic can fan out to several queues, each with its own filter. Operators:
exact list, `prefix`, `anything-but`, `exists`.

```hcl
sqs "bus" {
  queue "email-svc" {}
  queue "audit-svc" {}
}

sns "events" {
  sqs = "bus"
  topic "user-events" {}

  subscribe "user-events" {          # only signup/reset events
    protocol = "sqs"
    endpoint = "email-svc"
    raw      = true
    filter   = { type = ["signup", "password_reset"] }
  }
  subscribe "user-events" {          # everything, for the audit log
    protocol = "sqs"
    endpoint = "audit-svc"
    raw      = true
  }
}
```

```sh
# delivered to email-svc AND audit-svc:
aws sns publish --topic-arn arn:aws:sns:us-east-1:000000000000:user-events \
  --message '{"id":1}' \
  --message-attributes '{"type":{"DataType":"String","StringValue":"signup"}}'

# delivered to audit-svc only (filtered out of email-svc):
aws sns publish --topic-arn arn:aws:sns:us-east-1:000000000000:user-events \
  --message '{"id":2}' \
  --message-attributes '{"type":{"DataType":"String","StringValue":"login"}}'
```

## HTTP(S) webhook subscription

doze runs the SubscriptionConfirmation handshake; your endpoint confirms by
fetching the `SubscribeURL` (or calling `ConfirmSubscription`), then receives
`Notification` POSTs — perfect for testing webhook handlers locally.

```hcl
sns "events" {
  topic "signups" {}
  subscribe "signups" {
    protocol = "https"
    endpoint = "https://localhost:9000/hooks/sns"
  }
}
```

## Wire it into an app

```sh
# Publisher and subscribers all read the injected endpoint + creds:
doze run -- ./publisher
doze run -- ./consumer
```

**Go:** `o.BaseEndpoint = aws.String(os.Getenv("AWS_ENDPOINT_URL_SNS"))`
**Node v3:** `new SNSClient({ endpoint: process.env.AWS_ENDPOINT_URL_SNS })`
**boto3:** `boto3.client("sns", endpoint_url=os.environ["AWS_ENDPOINT_URL_SNS"])`

## Notes

- Topics/subscriptions declared in config are created on boot (and re-converged by
  `doze apply`); you can also Subscribe/Publish dynamically via the SDK.
- ARNs use the conventional local account `000000000000` and region `us-east-1`.
