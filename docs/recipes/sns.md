# Recipes — SNS (pub/sub)

A ground-up, pure-Go SNS implementation. Create topics and subscriptions, apply
message-attribute filter policies, and fan out to SQS queues or HTTP(S) webhooks.

## SNS → SQS fanout

Name the backing `sqs` instance; doze holds it running while SNS is up (a
dependency, like FerretDB→Postgres) and delivers to its queues.

```hcl
sqs "jobs" {
  queue "emails" {}
}

sns "events" {
  sqs = "jobs"                  # backing SQS instance for delivery

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
# the message arrives in the jobs/emails queue
aws sqs receive-message --queue-url \
  "$(aws sqs get-queue-url --queue-name emails --query QueueUrl --output text)"
```

Without `raw`, the queue receives the standard SNS JSON notification envelope
(`{"Type":"Notification","Message":...,"MessageAttributes":...}`).

## Filter policies

Only matching messages are delivered. Operators: exact list, `prefix`,
`anything-but`, `exists`.

```hcl
sns "events" {
  sqs = "jobs"
  topic "signups" {}
  subscribe "signups" {
    protocol = "sqs"
    endpoint = "emails"
    raw      = true
    filter   = { eventType = ["created", "updated"] }
  }
}
```

```sh
# matches the filter -> delivered
aws sns publish --topic-arn arn:aws:sns:us-east-1:000000000000:signups \
  --message '{"id":1}' \
  --message-attributes '{"eventType":{"DataType":"String","StringValue":"created"}}'
# does not match -> dropped
aws sns publish --topic-arn arn:aws:sns:us-east-1:000000000000:signups \
  --message '{"id":2}' \
  --message-attributes '{"eventType":{"DataType":"String","StringValue":"deleted"}}'
```

## HTTP(S) webhook subscription

doze performs the SubscriptionConfirmation handshake; your endpoint confirms by
fetching the `SubscribeURL` (or calling `ConfirmSubscription`), then receives
`Notification` POSTs.

```hcl
sns "events" {
  topic "signups" {}
  subscribe "signups" {
    protocol = "https"
    endpoint = "https://localhost:9000/hooks/sns"
  }
}
```

## Notes

- Topics/subscriptions declared in config are created on boot (`doze up`
  re-converges). You can also Subscribe/Publish dynamically via the SDK.
- ARNs use the conventional local account `000000000000` and region `us-east-1`.
