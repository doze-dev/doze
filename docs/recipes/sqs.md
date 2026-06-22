# Recipes — SQS (queues)

A ground-up, pure-Go SQS implementation built into doze. It speaks both wire
protocols (the modern AWS JSON 1.0 used by current SDKs and the legacy
Query/XML), persists to disk, and supports visibility timeouts, long polling,
message attributes, FIFO, and dead-letter redrive.

## Standard queue

```hcl
sqs "jobs" {
  queue "emails" {
    visibility_timeout = "30s"
    delay              = "0s"
    retention          = "96h"     # 4 days
    wait_time          = "10s"     # default long-poll wait
    max_message_size   = 262144
  }
}
```

```sh
eval "$(doze env)"                 # AWS_ENDPOINT_URL_SQS + dummy creds
url=$(aws sqs get-queue-url --queue-name emails --query QueueUrl --output text)
aws sqs send-message --queue-url "$url" --message-body "hello"
aws sqs receive-message --queue-url "$url" --wait-time-seconds 5
```

Durations use Go syntax (`30s`, `5m`, `12h`) or bare seconds. Days aren't a unit,
so use hours — e.g. `96h` for 4 days.

## FIFO queue (ordering + dedup)

FIFO queue names must end in `.fifo`.

```hcl
sqs "orders" {
  queue "orders.fifo" {
    fifo                = true
    content_based_dedup = true     # dedupe by body hash within the 5-min window
  }
}
```

```sh
url=$(aws sqs get-queue-url --queue-name orders.fifo --query QueueUrl --output text)
aws sqs send-message --queue-url "$url" --message-body "o1" --message-group-id g
aws sqs send-message --queue-url "$url" --message-body "o1" --message-group-id g  # deduped
```

Per-group ordering is preserved; a group is "locked" while one of its messages is
in flight, so the next message in that group isn't delivered until you delete (or
the visibility timeout lapses).

## Dead-letter queue + redrive

After `max_receive_count` receives without deletion, a message moves to the
dead-letter queue.

```hcl
sqs "work" {
  queue "tasks"     { visibility_timeout = "5s" }
  queue "tasks-dlq" {}
  redrive "tasks" {
    dead_letter       = "tasks-dlq"
    max_receive_count = 5
  }
}
```

```sh
# Receive a "poison" message past max_receive_count and it lands in tasks-dlq.
dlq=$(aws sqs get-queue-url --queue-name tasks-dlq --query QueueUrl --output text)
aws sqs receive-message --queue-url "$dlq"
```

## Message attributes

```sh
aws sqs send-message --queue-url "$url" --message-body "hi" \
  --message-attributes '{"kind":{"DataType":"String","StringValue":"signup"}}'
aws sqs receive-message --queue-url "$url" --message-attribute-names All
```

doze computes the AWS attribute MD5, so SDK checksum validation passes.

## Notes

- Both protocols work: modern SDKs (Go v2, JS v3, recent boto3) use JSON; older
  SDK v1 / boto3 use Query/XML.
- Long polling (`--wait-time-seconds` / `wait_time`) is event-driven — a send
  wakes a waiting receiver immediately.
- Messages persist across reaps/restarts.
