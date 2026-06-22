# doze.hcl — declarative local databases, no Docker.
#
# doze fetches real engine binaries, boots each declared instance on first
# connect, and reaps it when idle. For Postgres it converges the declared
# databases, schemas, roles/users, privileges, and extensions. It does NOT
# seed data or run migrations — your application owns those.
#
# See README.md for the full reference.

defaults {
  idle_timeout = "5m"            # reap an instance after this long with no connections
}

# --- PostgreSQL -------------------------------------------------------------
postgres "app_dev" {
  version  = 16                  # major (newest minor), or an exact "16.14"
  owner    = "app"              # the role that owns the database
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
  maxmemory = "256mb"
}

# --- Kvrocks (Redis protocol, RocksDB-backed) -------------------------------
kvrocks "store" {
  version = 2
}

# --- FerretDB (MongoDB wire) — stores data in a Postgres backend ------------
postgres "events_pg" {
  version    = 16
  extensions = ["documentdb"]    # FerretDB v2's storage extension
}

ferretdb "events" {
  version = 2
  backend = "events_pg"          # boots and holds this postgres instance
}

# --- Local AWS (built into doze: no Docker, no JVM, no LocalStack) -----------
# doze run / doze env inject AWS_ENDPOINT_URL_S3/SQS/SNS + dummy creds + region,
# so an unmodified AWS SDK or the `aws` CLI talks to these. (Enable path-style
# addressing for S3, as with MinIO/LocalStack.)

s3 "media" {
  bucket "uploads" {}
  bucket "thumbs" {
    versioning = true
  }
}

sqs "jobs" {
  queue "emails" {
    visibility_timeout = "30s"
  }
  queue "orders.fifo" {            # FIFO queues end in .fifo
    fifo                = true
    content_based_dedup = true
  }
  queue "emails-dlq" {}
  redrive "emails" {               # dead-letter after N receives
    dead_letter       = "emails-dlq"
    max_receive_count = 5
  }
}

sns "events_bus" {
  sqs = "jobs"                     # backing SQS instance for fanout delivery

  topic "signups" {}

  subscribe "signups" {            # fan out to an SQS queue
    protocol = "sqs"
    endpoint = "emails"            # the jobs/emails queue
    raw      = true
    filter   = { eventType = ["created"] }   # message-attribute filter policy
  }
}
