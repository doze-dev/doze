# doze.hcl — declarative local databases, no Docker.
#
# doze fetches real engine binaries, boots each declared instance on first
# connect, and reaps it when idle. For Postgres it converges the declared
# databases, schemas, roles/users, privileges, and extensions. It does NOT
# seed data or run migrations — your application owns those.
#
# Every instance pins its own port (doze never auto-assigns), so connection
# strings are stable. `doze env` prints them as eval-able exports.
#
# See README.md for the full reference.

defaults {
  idle_timeout = "5m"            # reap an instance after this long with no connections
  domains      = true            # publish <name>.local via mDNS (app-dev.local:5432)
}

# --- PostgreSQL -------------------------------------------------------------
postgres "app_dev" {
  version  = 16                  # major (newest minor), or an exact "16.14"
  port     = 5432
  owner    = "app"               # the role that owns the database
  encoding = "UTF8"

  # The light/dev tuning profile (fast, not crash-safe).
  shared_buffers  = "16MB"
  max_connections = 50
  fsync           = false
  autovacuum      = false

  extensions = ["uuid-ossp"]

  role "app" {                   # a "user" is a role with LOGIN (the default)
    password         = "app"
    connection_limit = 20
  }
  role "readwrite" {             # a group role
    login = false
  }
  role "reporter" {
    password  = "report"
    member_of = ["readwrite"]    # inherits readwrite's privileges
  }

  schema "billing" {
    owner = "app"
  }

  grant {
    role       = "app"
    database   = "app_dev"
    privileges = ["ALL"]
  }
  grant {
    role       = "reporter"
    schema     = "public"
    objects    = "tables"        # all current + future tables in public
    privileges = ["SELECT"]
  }
}

# --- Valkey (Redis protocol, in-memory) -------------------------------------
valkey "cache" {
  version   = 9
  port      = 6379
  maxmemory = "256mb"
}

# --- Kvrocks (Redis protocol, RocksDB-backed) -------------------------------
kvrocks "store" {
  version = 2
  port    = 6380
}

# --- FerretDB (MongoDB wire, Postgres under the hood) -----------------------
ferret "events" {
  version = 2
  port    = 27017
}

# --- Local AWS (one engine, every service) ----------------------------------
# ONE block is the whole local AWS: S3, SQS, SNS, DynamoDB, Lambda, KMS and the
# rest behind a single endpoint, with the web console at /_console. Declare the
# resources you want to exist; your app and its deploy tooling own the rest.
#
# `doze env` exports AWS_ENDPOINT_URL plus dummy creds and a region, so an
# unmodified AWS SDK or the `aws` CLI talks to it with no other change.

aws "cloud" {
  port = 4566

  bucket "uploads" {
    versioning = true
  }

  queue "jobs" {
    dlq          = "auto"   # a companion jobs-dlq, with redrive
    max_receives = 5
    visibility   = 30
  }

  topic "signups" {
    subscribe {
      queue = "jobs"
      raw   = true
    }
  }
}

# --- Application processes (no Docker) --------------------------------------
# A `process` runs your app directly on the host, wired to the services above by
# typed references. `doze up` boots the databases first, runs pre_start hooks
# (migrations), starts the app, then waits for its health probe before streaming
# logs. Uncomment and point `cwd` at a real app to try it.
#
# process "api" {
#   cwd     = "../approvals-engine"
#   command = "go run main.go -program api"
#   port    = 8080
#
#   env = {
#     DATABASE_URL = postgres.app_dev.url   # typed ref → boots app_dev first
#   }
#
#   hooks {
#     pre_start = ["go run main.go -program migrate -command up"]
#   }
#
#   health {
#     http     = "http://localhost:8080/health/ready"
#     interval = "2s"
#     retries  = 30
#   }
#
#   restart {
#     policy      = "on_failure"
#     max_retries = 5
#   }
# }
