# doze documentation

Real databases and AWS services on your laptop — asleep until you need them.
New to doze? Start with the [project overview](../README.md).

## Start here

- **[Why doze](guide/why-doze.md)** — the case for it over docker-compose, a
  native install, and LocalStack; and whether it's for you.
- **[Resource footprint](guide/resource-footprint.md)** — what doze actually
  uses, measured, vs Docker and LocalStack.
- **[The engines](guide/engines.md)** — what Postgres, Valkey, Kvrocks, FerretDB,
  and the built-in S3/SQS/SNS each are, and when to reach for them.

## Learn

Read these in order to build a working mental model:

1. **[Getting started](guide/getting-started.md)** — a hands-on tour from install
   to a running app, explaining what you see at each step.
2. **[Core concepts](guide/concepts.md)** — the daemon, lazy boot, idle reaping,
   convergence, endpoints, versions, and instance dependencies.
3. **[Files & storage](guide/files-and-storage.md)** — where doze keeps engines,
   data, sockets, and logs; what to commit vs ignore; and splitting config across
   files.

## Do

- **[Recipes](recipes/README.md)** — copy-pasteable examples, by topic:
  [Postgres](recipes/postgres.md) · [Valkey & Kvrocks](recipes/valkey-kvrocks.md)
  · [FerretDB](recipes/ferretdb.md) · [S3](recipes/s3.md) · [SQS](recipes/sqs.md)
  · [SNS](recipes/sns.md) · [Workflows](recipes/workflows.md) ·
  [Config layout](recipes/config-layout.md) · [Full stacks](recipes/stacks.md)

## When things go wrong

- **[Troubleshooting](guide/troubleshooting.md)** — daemon won't start, downloads
  fail, an instance errors, can't connect, resetting state.
- **[FAQ](guide/faq.md)** — production readiness, CI, Windows/WSL/Docker, the
  lockfile, sharing a setup, uninstalling.

## Reference

- **[Configuration](reference/configuration.md)** — every block and field in `doze.hcl`.
- **[CLI](reference/cli.md)** — every command and flag.
- **[Managing binaries](BINARIES.md)** — the mirror, the lockfile, self-hosting.
- **[Extensions](EXTENSIONS.md)** — Postgres extensions, including from source.

## Under the hood

- **[Architecture](ARCHITECTURE.md)** — the engine-driver contract and how the
  generic core, proxy, runtime, and daemon fit together (for contributors).

The companion [`NerdMeNot/doze-binaries`](https://github.com/NerdMeNot/doze-binaries)
repo builds and publishes the engine binaries doze downloads.
