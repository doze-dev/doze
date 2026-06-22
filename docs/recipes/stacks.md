# Recipes — Full stacks

Putting it together: realistic, polyglot environments declared in one place and
wired into apps with zero hardcoded ports.

## A typical web app: Postgres + cache + object storage + queue

```hcl
defaults { idle_timeout = "10m" }

postgres "app" {
  version = 16
  owner   = "app"
  role "app" { password = "app" }
  grant {
    role       = "app"
    database   = "app"
    privileges = ["ALL"]
  }
  extensions = ["uuid-ossp", "pg_trgm"]
}

valkey "cache" {
  version   = 9
  maxmemory = "256mb"
}

s3 "uploads" {
  bucket "user-uploads" {}
  bucket "exports" {}
}

sqs "jobs" {
  queue "email"  {}
  queue "export" {}
}
```

```sh
doze run -- ./bin/server           # gets DATABASE_URL, REDIS_URL,
                                   # AWS_ENDPOINT_URL_S3, AWS_ENDPOINT_URL_SQS, AWS_* creds
doze run -- ./bin/worker           # same env; processes the queues
```

## Event-driven services: SNS fanout into SQS

```hcl
sqs "bus" {
  queue "email-svc"  {}
  queue "audit-svc"  {}
}

sns "events" {
  sqs = "bus"
  topic "user-events" {}
  subscribe "user-events" {
    protocol = "sqs"
    endpoint = "email-svc"
    raw      = true
    filter   = { type = ["signup", "password_reset"] }
  }
  subscribe "user-events" {
    protocol = "sqs"
    endpoint = "audit-svc"     # gets everything (no filter)
    raw      = true
  }
}
```

Publishing to `user-events` fans out to both queues, with `email-svc` receiving
only the filtered subset.

## Mongo-style service

```hcl
postgres "docs_pg" {
  version    = 16
  extensions = ["documentdb"]
}
ferretdb "docs" {
  version = 2
  backend = "docs_pg"
}
```

`doze run -- ./svc` injects `MONGODB_URI`; the Postgres backend boots and is held
automatically.

## The kitchen sink (everything at once)

Split across `doze.d/` for readability — see [config layout](config-layout.md).

```hcl
# doze.hcl
defaults { idle_timeout = "10m" }
postgres "app" {
  version = 16
  role "app" { password = "app" }
}
valkey  "cache" { version = 9 }
kvrocks "kv"    { version = 2 }
```
```hcl
# doze.d/mongo.hcl
postgres "mongo_pg" {
  version    = 16
  extensions = ["documentdb"]
}
ferretdb "mongo" {
  version = 2
  backend = "mongo_pg"
}
```
```hcl
# doze.d/aws.hcl
s3 "blob" {
  bucket "data" {}
}
sqs "queue" {
  queue "tasks" {}
}
sns "topic" {
  sqs = "queue"
  topic "events" {}
  subscribe "events" {
    protocol = "sqs"
    endpoint = "tasks"
    raw      = true
  }
}
```

```sh
doze status      # one view of the whole environment
doze dash        # live, interactive
doze run -- make integration-test
```

## Framework wiring

doze injects the **standard** env vars apps already read — usually no code
changes.

**Rails** — reads `DATABASE_URL` and `REDIS_URL`:
```sh
doze run -- bin/rails server
doze run -- bin/rails test
```

**Django** (with `dj-database-url`) — reads `DATABASE_URL`:
```sh
doze run -- python manage.py migrate
doze run -- python manage.py runserver
```

**Node / Prisma** — `DATABASE_URL`; AWS SDK reads `AWS_ENDPOINT_URL_S3` + creds:
```sh
doze run -- npx prisma migrate dev
doze run -- npm run dev
```

**Go** — read the env directly:
```go
db, _ := sql.Open("pgx", os.Getenv("DATABASE_URL"))
rdb := redis.NewClient(opt(os.Getenv("REDIS_URL")))
s3c := s3.NewFromConfig(cfg, func(o *s3.Options){
    o.BaseEndpoint = aws.String(os.Getenv("AWS_ENDPOINT_URL_S3"))
    o.UsePathStyle = true
})
```
```sh
doze run -- go run ./cmd/api
```

With multiple instances of the same engine, use the per-instance
`DOZE_<NAME>_URL` (the conventional `DATABASE_URL`/`REDIS_URL` is only set when a
single instance claims it).
