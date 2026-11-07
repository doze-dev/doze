---
title: AWS
description: Run local AWS (S3, SQS, SNS, DynamoDB, Lambda and more) as one service, with buckets, queues and tables declared in doze.hcl.
redirect_from:
  - /guides/recipes/aws/
---

Run the AWS services your app calls on your machine, with the buckets, queues,
topics and tables declared in doze.hcl, behind one endpoint every AWS SDK
understands.

One `aws` block is one process from the
[doze AWS](https://github.com/doze-dev/doze-aws) project, written in Go: S3,
SQS, SNS, DynamoDB (with Streams), Lambda, EventBridge, KMS, SSM Parameter
Store, Secrets Manager, STS and Step Functions behind one gateway, with a web
console at `/_console`. No Docker, no JVM. It is for building and testing,
not a stand-in for AWS in production.

There is no `version`: the services follow the current AWS APIs.

## The block

Larder, the made-up shop in these docs, keeps product images and receipts in
S3 and hands pick lists to the warehouse over SQS:

```hcl
module aws "aws" {
  port = 4566

  bucket "product-images" {}
  bucket "receipts" { versioning = true }

  queue "pick-lists" {
    dlq          = "auto"
    max_receives = 5
  }
}
```

Everything inside is created when the service first starts, updated when you
change it, and kept on disk across sleeps. `dlq = "auto"` makes a
`pick-lists-dlq` queue and sends a message there after five failed receives.

## Point your SDK at it

```hcl
app "catalog" {
  run  = "go run ./services/catalog"
  port = 8002
  env = {
    AWS_ENDPOINT_URL = module.aws.url
    IMAGES_BUCKET    = "product-images"
  }
}
```

Every current AWS SDK reads `AWS_ENDPOINT_URL`. Resources are plain names.
The SDK still wants credentials and a region; the local services accept any.
From a terminal, `doze env` prints all of it:

```sh
eval "$(doze env aws)"
aws s3 ls
aws sqs get-queue-url --queue-name pick-lists
```

`doze env` exports `AWS_ENDPOINT_URL`, `AWS_ACCESS_KEY_ID`,
`AWS_SECRET_ACCESS_KEY` and `AWS_REGION`.

## Its address

With names on, the service is reached at its name: a block called `aws` in a
stack called `shop` is `http://aws.shop.doze` (with `port` declared, that port
is added). Every name under it reaches it too, so SDKs that put the service or
the bucket in the host work unchanged: `sqs.us-east-1.aws.shop.doze`, and S3 by
virtual host (`uploads.s3.us-east-1.aws.shop.doze`) without
`force_path_style`. `routes = false` keeps it on its port only. See
[Names and addresses](/hcl/names/).

## Queues, topics and fan-out

```hcl
module aws "aws" {
  queue "emails" {
    dlq          = "auto"
    max_receives = 5
  }

  queue "orders.fifo" { fifo = true }

  topic "signups" {
    subscribe {
      queue = "emails"
      raw   = true
    }
  }
}
```

A `subscribe` block in a topic delivers each message to a queue (or a Lambda
or an HTTP endpoint).

## DynamoDB

```hcl
module aws "aws" {
  table "sessions" {
    key = "session_id:S"
    ttl = "expires_at"
  }
}
```

`key` is `"name:type"`, with a sort key after a space: `"pk:S sk:N"`. Streams
are on, so a Lambda can follow a table's changes.

## Lambda and EventBridge

```hcl
module aws "aws" {
  function "resize" {
    code    = "./functions/resize"
    timeout = 10
  }

  rule "order-placed" {
    pattern = "{\"source\":[\"shop.checkout\"]}"
    targets = ["lambda:resize", "queue:emails"]
  }
}
```

`code` is a folder with a `bootstrap` executable (the `provided.al2`
runtime); the function runs as a local process. `PutEvents` on the default
bus delivers to every rule whose pattern matches.

## Keys, parameters and secrets

```hcl
module aws "aws" {
  key "app" {}
  parameter "/app/feature/banner" { value = "on" }
  secret "db-password" { value = "local-only" }
}
```

## The console

`/_console` on the service's address has a page per service: browse buckets
and objects, peek at and purge queues, scan tables, invoke functions. Its
Traffic tab shows every API call your app makes. In the app it is the
service's Console tab.

![The AWS console in the doze app, showing Larder's buckets and queues](../../../assets/app/service-console-aws.webp)
