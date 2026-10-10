# shop — the fixture stack.
#
# A small but real project that doze's own tests and measurements run against:
# three Go programs and four modules, wired together by references (see
# docs/design/next/phases.md).
#
#   doze up            from this directory
#   doze up --var domains=false    on a machine without `doze dns-setup`,
#                                  or beside other stacks you must not disturb

stack = "shop"

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

module postgres "db" {
  version = 16
  port    = 5432
}

module valkey "cache" {
  version = 9
  port    = 6379
}

module kafka "events" {
  version = 4
  port    = 9092

  topic "orders" {
    partitions = 3
  }
}

module aws "cloud" {
  port = 4566

  bucket "uploads" {}
  queue "orders" {}
}

# ── apps ──────────────────────────────────────────────────────────────────

app "api" {
  run  = "go run ./cmd/api"
  port = 8080

  env = {
    PORT          = "8080"
    DATABASE_URL  = module.db.url
    CACHE_ADDR    = module.cache.address
    KAFKA_BROKERS = module.events.address
  }

  ready         = http("/healthz")
  ready_timeout = "90s" # the first `go run` compiles the program
}

app "worker" {
  run = "go run ./cmd/worker"

  env = {
    KAFKA_BROKERS = module.events.address
    AWS_ENDPOINT  = module.cloud.url
    ORDERS_QUEUE  = "orders"
    UPLOADS       = "uploads"
  }
}

app "web" {
  run  = "go run ./cmd/web"
  port = 8090

  env = {
    PORT    = "8090"
    API_URL = "http://${app.api.address}"
  }

  ready         = http("/healthz")
  ready_timeout = "90s"
}
