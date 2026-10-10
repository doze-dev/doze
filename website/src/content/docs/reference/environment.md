---
title: "Environment variables"
description: Every variable doze reads, and the ones it writes for your processes.
---

## Variables doze reads

| Variable | Effect |
|---|---|
| `DOZE_HOME` | The shared home (default `~/.doze`): engine cache, module cache, per-project state. |
| `DOZE_VAR_<name>` | Sets config `variable "<name>"` (lower precedence than `--var`). |
| `DOZE_MODULES` | `off` disables module fetching entirely (offline / apps only). |
| `DOZE_<TYPE>_PLUGIN` | Path to a local plugin binary for an engine type; skips registry fetch *and* its metadata gates (module development). |
| `DOZE_<ENGINE>_BINDIR` | Explicit engine bin directory (e.g. `DOZE_POSTGRES_BINDIR`); bypasses mirror + lock. |
| `DOZE_<ENGINE>_MIRROR` | Per-engine binaries-mirror base. |
| `DOZE_MIRROR` | Global binaries-mirror root (engine name appended). |
| `NO_COLOR` | Plain output (also automatic when stdout isn't a terminal). |

Precedence, where they overlap: explicit local override
(`_PLUGIN`/`_BINDIR`) → lockfile pin → mirror/registry resolution.

## Variables doze writes

`doze env` prints these connection variables, `doze run --env` runs a command
with them set, and the same set is written to `.doze/endpoints.yaml` for
external tooling. An `app` block does not receive them automatically — it gets
the environment it declares; see
[Workflows](/guides/workflows/#getting-connection-strings-into-your-code):

| Variable | For |
|---|---|
| `DATABASE_URL` | postgres |
| `REDIS_URL` | valkey / kvrocks |
| `MONGODB_URI` | ferret |
| `MYSQL_URL` | mariadb |
| `AWS_ENDPOINT_URL_S3` / `_SQS` / `_SNS` | the local AWS engines |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_REGION` | dummy credentials the local AWS engines accept (`test` / `test` / `us-east-1`) |

Per-block attributes (`module.app.url`, `app.api.url`, …) are also
available as [config references](/reference/configuration/#references--expressions)
for wiring values explicitly.
