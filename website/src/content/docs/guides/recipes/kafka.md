---
title: "Recipes — Kafka (events, no JVM)"
---

A single-node, Kafka-protocol broker in pure Go — real wire protocol, real
consumer groups, compaction and retention, verified against franz-go. Any Kafka
client connects unchanged. The `version` you declare is the **protocol
profile** (1–4) your clients expect.

- [Topics](#topics)
- [Wire it into an app](#wire-it-into-an-app)
- [Produce & consume from the shell](#produce--consume-from-the-shell)
- [Consumer groups & lag](#consumer-groups--lag)
- [The console](#the-console)

## Topics

```hcl
kafka "events" {
  version = 4
  port    = 9092

  auto_create_topics = true
  default_partitions = 1
  retention          = "168h"    # 7 days

  topic "orders" {
    partitions = 3
  }
  topic "payments" {
    partitions = 1
    config = { "cleanup.policy" = "compact" }
  }
}
```

Declared topics are converged on boot; `auto_create_topics` covers the rest.

## Wire it into an app

The reference for a broker is `.address` (host:port bootstrap, not a URL):

```hcl
process "worker" {
  command = "go run ./worker"
  env = {
    KAFKA_BROKERS = kafka.events.address   # boots + holds the broker
  }
}
```

```go
cl, _ := kgo.NewClient(
    kgo.SeedBrokers(os.Getenv("KAFKA_BROKERS")),
    kgo.ConsumerGroup("emailer"),
    kgo.ConsumeTopics("orders"),
)
```

## Produce & consume from the shell

```sh
addr=events.demo.doze:9092        # or: doze env | grep KAFKA

kcat -b "$addr" -t orders -P <<< '{"order":1}'   # produce
kcat -b "$addr" -t orders -C -o beginning        # consume
```

The broker wakes on the first connect and reaps when idle, like every engine —
a long-polling consumer holds it awake, which is what you want.

## Consumer groups & lag

Both classic and KIP-848 group protocols work. The dash's kafka page shows each
group's **lag** with a trend — the one number that answers "is my consumer
keeping up?" — and flags a group whose lag keeps growing.

## The console

A web console ships with the broker, one port above it
(`http://<host>:9093` for a `:9092` broker): topics with a live message tape,
a produce panel, per-partition offsets, and consumer groups with lag. In the
dash, `o` on the kafka row opens it.

A dev-grade single node for building and testing — not a replacement for a
production cluster.
