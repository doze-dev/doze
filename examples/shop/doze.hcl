# shop — the fixture stack.
#
# A small but real project that doze's own tests and measurements run against:
# three Go programs and four modules, wired together by references. It is
# written in today's syntax and is ported as the redesign lands (see
# docs/design/next/phases.md).
#
#   doze up            from this directory
#   doze up --var domains=false    on a machine without `doze dns-setup`,
#                                  or beside other stacks you must not disturb

name = "shop"

variable "domains" {
  type        = bool
  default     = true
  description = "Publish <service>.shop.doze names. Off binds everything on 127.0.0.1."
}

defaults {
  idle_timeout = "5m"
  domains      = var.domains
}

# ── modules ───────────────────────────────────────────────────────────────

postgres "db" {
  version = 16
  port    = 5432
}

valkey "cache" {
  version = 9
  port    = 6379
}

kafka "events" {
  version = 4
  port    = 9092

  topic "orders" {
    partitions = 3
  }
}

aws "cloud" {
  port = 4566

  bucket "uploads" {}
  queue "orders" {}
}

# ── apps ──────────────────────────────────────────────────────────────────

process "api" {
  command = "go run ./cmd/api"
  port    = 8080

  env = {
    PORT          = "8080"
    DATABASE_URL  = postgres.db.url
    CACHE_ADDR    = valkey.cache.address
    KAFKA_BROKERS = kafka.events.address
  }

  health {
    http     = "http://127.0.0.1:8080/healthz"
    interval = "1s"
    retries  = 90 # the first `go run` compiles the program
  }
}

process "worker" {
  command = "go run ./cmd/worker"

  env = {
    KAFKA_BROKERS = kafka.events.address
    AWS_ENDPOINT  = aws.cloud.url
    ORDERS_QUEUE  = "orders"
    UPLOADS       = "uploads"
  }
}

process "web" {
  command = "go run ./cmd/web"
  port    = 8081

  env = {
    PORT    = "8081"
    API_URL = "http://${process.api.address}"
  }

  health {
    http     = "http://127.0.0.1:8081/healthz"
    interval = "1s"
    retries  = 90
  }
}
