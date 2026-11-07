---
title: Kafka
description: Run a single-node Kafka-protocol broker, with its topics declared in doze.hcl and a web console, without a JVM.
redirect_from:
  - /guides/recipes/kafka/
---

Run a Kafka broker for your producers and consumers, with the topics declared
in doze.hcl and a console to watch them.

Kafka itself runs on the JVM, and the usual local answer is Docker and a
gigabyte of images. This module is a single-node broker written in Go, from
the [doze Kafka](https://github.com/doze-dev/doze-kafka) project. It speaks the
Kafka wire protocol, with consumer groups (classic and KIP-848), compaction and
retention, so any Kafka client connects to it. It is for building and testing,
not a production cluster.

`version` is the Kafka protocol profile your clients expect (1 to 4), not a
broker build.

## Topics

```hcl
module kafka "events" {
  version            = 4
  auto_create_topics = false

  topic "orders" { partitions = 3 }
  topic "payments" {
    config = { "cleanup.policy" = "compact" }
  }
}
```

Declared topics are created on the broker's first start. Larder, the made-up
shop in these docs, sets `auto_create_topics = false`: its topics are the
contracts between services, so they are declared rather than created by
whichever client writes first. Leave it on, and `default_partitions` and
`retention` apply to topics clients create.

## Connect a client

A broker's reference is `.address`, the host and port a client bootstraps
from:

```hcl
app "emailer" {
  run = "go run ./cmd/emailer"
  env = { KAFKA_BROKERS = module.events.address }
}
```

```go
cl, _ := kgo.NewClient(
	kgo.SeedBrokers(os.Getenv("KAFKA_BROKERS")),
	kgo.ConsumerGroup("emailer"),
	kgo.ConsumeTopics("orders"),
)
```

From a terminal, with [kcat](https://github.com/edenhill/kcat):

```sh
doze url events
# events.shop.doze:9092
kcat -b "$(doze url events)" -t orders -P <<< '{"order":1}'
kcat -b "$(doze url events)" -t orders -C -o beginning
```

The broker wakes on the first connection. A consumer keeps its connection
open, so a stack with a running consumer keeps Kafka awake, which is what you
want. An app that only consumes Kafka gets no requests of its own, so give it
`sleep = "never"`, as Larder does for `orders` ([Sleep and wake](/hcl/sleep/)).

## The console

The broker serves a web console one port above its own (`9093` for a broker
on `9092`): topics with a live tape of messages, a produce panel, offsets per
partition, and consumer groups with their lag. In the app it is the service's
Console tab.

![The Kafka console in the doze app, showing Larder's topics](../../../assets/app/service-console.webp)
