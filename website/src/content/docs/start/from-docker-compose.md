---
title: "Coming from docker-compose"
description: A mental-model mapping — how compose concepts translate to the doze way.
---

If you think in compose, this page re-bases that thinking. It's deliberately
**not a converter**: doze's model differs in ways worth adopting, not
transliterating. Ten minutes here saves the "wait, where's my depends_on"
moments later.

## The concept map

| You know | doze's counterpart | The difference that matters |
|---|---|---|
| `services:` entries | Engine blocks: `postgres "app" { … }` | The block *type* picks real software (a signed module), not an image name from a registry of mutable tags. |
| `image: postgres:16` | `version = 16` | Resolves to a pinned exact version in `doze.lock` — `:16` moves, your lock doesn't. |
| `ports: ["5432:5432"]` | `port = 5432` | There's no NAT — the engine listens on localhost natively. What you declare is what `lsof` shows. |
| `depends_on:` | References: `sqs = sqs.jobs.name` | The dependency is derived from the config *value*, validated at lint, and boots in order automatically. |
| `healthcheck:` | Built-in readiness per engine | Modules know their engine's real readiness (a socket accept, a protocol ping); you don't write retry loops. |
| `volumes:` | Nothing to write | Each instance gets its own data dir under the project's doze home. `doze reset` when you want a clean slate. |
| `.env` / `environment:` | `variable` blocks + `--var` / `DOZE_VAR_*` | Typed, referenced expressions instead of string interpolation. |
| `docker compose up -d` | `doze up` | Converges structure, boots in dependency order, then **everything idles to zero when unused** — the part compose has no equivalent for. |
| `docker compose down -v` | `doze down` / `doze reset` | `down` sleeps everything; `reset` wipes data (re-clones fresh). |
| `docker compose logs -f app` | `doze logs -f app` | Engine logs are also just files on your disk. |
| `docker exec -it db psql` | `doze shell app` | A real `psql` to a native process — no exec, no TTY plumbing. |

## What has no equivalent — in either direction

**Compose things you stop doing:** waiting for a VM; `host.docker.internal`;
bind-mount permission archaeology; hand-rolled `wait-for-it.sh`; pulling images
on hotel wifi; picking restart policies for a laptop.

**doze things compose can't say:**

- **Convergence.** `role "app" { … }`, `extension "pgvector" {}`, buckets,
  queues, grants — declared in the same file and *converged* into the running
  engine. In compose-world this lives in init-script volumes and entrypoint
  hacks.
- **Structure, not data.** doze creates roles/databases/buckets; your
  migrations own schema and rows. `doze sync` reconciles when you change the
  declaration.
- **The lockfile.** `doze.lock` pins engines, modules, and publisher keys —
  compose has image digests if you remember to use them; nobody does.
- **Sleep.** Idle instances reap to zero and wake on the next connection in
  well under a second. Your laptop is quiet because nothing is running.

## A worked translation

```yaml
# docker-compose.yml (before)
services:
  db:
    image: postgres:16
    ports: ["5432:5432"]
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: app
    volumes: ["dbdata:/var/lib/postgresql/data"]
  cache:
    image: redis:7
    ports: ["6379:6379"]
  minio:
    image: minio/minio
    command: server /data
    ports: ["9000:9000"]
volumes:
  dbdata:
```

```hcl
# doze.hcl (after)
postgres "db" {
  version = 16
  port    = 5432
  owner   = "app"

  role "app" {
    password = "app"
    login    = true
  }
}

valkey "cache" {
  version = 9        # the open-source Redis lineage
  port    = 6379
}

aws "cloud" {
  port = 4566        # all of AWS, no MinIO or LocalStack container
  bucket "uploads" {}
}
```

Then `doze up`, commit `doze.hcl` + `doze.lock`, and delete the YAML. Your
app's connection strings don't change — same ports, same localhost.

Read next: [Core concepts](/start/concepts/) for the model underneath, and
[Why HCL](/why/hcl/) if the config language choice needs justifying to your
team.
