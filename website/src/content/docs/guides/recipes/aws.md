---
title: "Recipes — AWS (the whole cloud, one block)"
---

One `aws` block runs the whole local cloud as a single process: S3, SQS, SNS,
DynamoDB (with Streams), Lambda, EventBridge, KMS, SSM Parameter Store, and
Secrets Manager — pure Go, one endpoint, with a web console at `/_console`.
Buckets, queues, tables, and functions are declared *inside* the block and
converged on boot.

- [The block](#the-block)
- [Point your SDK at it](#point-your-sdk-at-it)
- [Queues, topics, fanout](#queues-topics-fanout)
- [DynamoDB](#dynamodb)
- [Lambda + EventBridge](#lambda--eventbridge)
- [Config & secrets](#config--secrets)
- [The console](#the-console)

## The block

```hcl
aws "local" {
  port = 4566

  bucket "uploads" { versioning = true }

  queue "emails"      { dlq = "auto"  max_receives = 5 }
  queue "orders.fifo" { fifo = true  content_dedup = true }   # FIFO names end in .fifo

  topic "signups" {
    subscribe { queue = "emails"  raw = true }
  }

  table "sessions" {
    key = "session_id:S"
    ttl = "expires_at"
  }
}
```

Everything inside is created (or updated) when the instance boots, and it all
persists to disk across reaps like any other engine. There's no `version =` —
the services track current AWS APIs.

## Point your SDK at it

Reference `aws.local.url` from a `process` block and every AWS SDK picks it up
through the standard endpoint variables:

```hcl
process "api" {
  command = "go run ."
  env = {
    AWS_ENDPOINT_URL_S3  = aws.local.url    # boots + holds the aws instance
    BUCKET               = "uploads"        # resources are plain names
  }
}
```

```sh
export AWS_ENDPOINT_URL=$(doze env | grep -o 'http://aws[^"]*' | head -1)
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_REGION=us-east-1
aws s3 ls
aws sqs get-queue-url --queue-name emails
```

One endpoint serves every service — the gateway routes by protocol, exactly
like the standalone `doze-aws` binary.

## Queues, topics, fanout

```hcl
queue "emails" {
  visibility_timeout = "30s"
  retention          = "96h"     # Go durations; days aren't a unit, use hours
  dlq                = "auto"    # creates emails-dlq + redrive policy
  max_receives       = 5
}

topic "signups" {
  subscribe { queue = "emails"  raw = true }   # SNS→SQS fanout
}
```

## DynamoDB

```hcl
table "sessions" {
  key = "session_id:S"       # "name:type" — add a range key with sort = "at:N"
  ttl = "expires_at"
}
```

Streams are on — a Lambda can subscribe to table changes.

## Lambda + EventBridge

```hcl
function "resize" {
  code    = "./functions/resize"    # a dir with a provided.al2 bootstrap binary
  timeout = 10
}

rule "order_placed" {
  pattern = "{\"source\":[\"shop.checkout\"]}"   # a literal JSON string
  targets = ["lambda:resize", "queue:emails"]    # kind:name shorthand
}
```

Functions scale to zero like everything else — cold on first invoke, warm for a
while, then asleep. `PutEvents` on the default bus fans out to every matching
rule target.

## Config & secrets

```hcl
key       "app_key"     {}                          # KMS
parameter "feature_flag" { value = "on" }           # SSM Parameter Store
secret    "db_password"  { value = "local-only" }   # Secrets Manager
```

## The console

Open the instance in the dash (`o`) or browse to `http://<endpoint>/_console`:
every service has a page — browse buckets and objects, peek and purge queues,
scan tables, invoke functions, decrypt nothing (KMS values stay masked) — plus
a live **Traffic** tape of every API call your app makes, each replayable as a
`curl`.

Dev-grade by design: fast and faithful enough to build and test against, not a
replacement for real AWS in production.
