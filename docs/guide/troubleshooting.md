# Troubleshooting

Something not working? Start here. Most issues fall into a few buckets, and three
commands tell you almost everything:

```sh
doze status     # what's up, what's reaped, and any instance in an "error" state
doze doctor     # config parses? platform? toolchains? daemon?
doze logs       # the daemon log — or `doze logs <instance>` for a backend's log
```

## `doze: command not found`

The binary isn't on your `PATH`. `go install` puts it in `$(go env GOBIN)` (or
`$(go env GOPATH)/bin`). Add that to your `PATH`, or build to a known location:

```sh
go build -o ~/bin/doze ./cmd/doze     # then ensure ~/bin is on PATH
```

## The daemon won't start / "address already in use"

Another process (or a stale doze daemon) holds the listen port.

```sh
doze status                      # is a daemon already running?
doze stop --all                  # stop a stale daemon
lsof -iTCP:6432 -sTCP:LISTEN     # who's on the port? (adjust to your `listen`)
```

If you genuinely need a different port, change the base address in `doze.hcl`:

```hcl
listen = "127.0.0.1:7000"
```

When the daemon can't come up it now prints the real reason from the log — read
that message; it usually names the port or path it failed to bind.

## An instance shows `error` in `doze status`

The backend failed to boot or converge. `doze status` shows the reason inline, and
the full detail is in the backend's log:

```sh
doze status            # e.g.  app  postgres  error  …   ✗ app: <reason>
doze logs app          # the backend's own output
```

Common causes:
- **Postgres extension not found** — a name in `extensions` (or an `extension`
  block) that the binary doesn't ship. Remove it or provide a `source` bundle
  ([Extensions](../EXTENSIONS.md)).
- **DocumentDB first boot is slow / times out** — its first boot builds a private
  Postgres cluster and runs `CREATE EXTENSION` (a few minutes). Warm it ahead of
  time with `doze start docs`; later boots are sub-second.
- **Bad config value** — re-run `doze doctor`; config errors point at the file and
  line.

After fixing config, `doze apply <instance>` (or just reconnect) re-converges.

## Engine binaries won't download

doze fetches engine binaries on first use. If you're offline, behind a corporate
proxy, or a download fails:

- **Use a local build** instead of downloading — point doze at an existing bin
  directory:
  ```sh
  DOZE_POSTGRES_BINDIR=/opt/homebrew/opt/postgresql@16/bin doze shell app
  ```
- **Point at a mirror you can reach** with `DOZE_<ENGINE>_MIRROR` /
  `DOZE_MIRROR`, including a `file://` path for a fully offline mirror — see
  [Managing binaries](../BINARIES.md).
- The built-in **S3/SQS/SNS need no download** — they ship inside the doze binary.

## It says `idle`/`active` but I can't connect

- **Wrong address.** Use the exact endpoint from `doze status` (each instance has
  its own port, starting from your `listen` base).
- **S3 client fails with a DNS/host error.** Enable **path-style** addressing
  (localhost can't do virtual-host style) — see the [S3 recipe](../recipes/s3.md).
- **TLS.** If you set a `tls {}` block with `required = true`, plaintext clients
  are rejected; connect with `sslmode=require`.

## My database reaped while I was still working

That's by design — but it shouldn't interrupt you. doze reaps on **zero
connections**, never on query inactivity, so an open connection (or pool) keeps it
alive. If a short-lived script makes it sleep between runs and that bothers you,
raise the timeout:

```hcl
defaults { idle_timeout = "30m" }
```

(The next connection re-boots it instantly either way.)

## A backend lingered after a crash (macOS)

If the daemon was killed hard (`kill -9`, a crash), macOS can't auto-kill its
children the way Linux does. doze reclaims them automatically the next time the
daemon starts. To clean up immediately: `doze stop --all`.

## Start completely fresh

Reset one instance's data, or wipe the whole project's state — see
[Resetting & cleaning up](files-and-storage.md#resetting--cleaning-up).

## Still stuck?

Open an issue with `doze doctor` output, the relevant `doze logs` lines, your
`doze.hcl`, and your OS/arch: <https://github.com/NerdMeNot/doze/issues>.
