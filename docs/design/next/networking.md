# Contract 3: names, addresses and ports

Tier: core, with the dream items marked. Built in phase 3. Background in
[gap-analysis.md](gap-analysis.md#names).

## What exists

doze-names (`doze-names/`, standard library only) provides four things:

| Part | What it is |
|---|---|
| Registry | `<home>/names.json`: host to `{IP, PID, Owner, Tier, Target}`. File-locked, written atomically, pruned of dead PIDs on every access. |
| Resolver | A UDP DNS server on `127.0.0.1:5323` (macOS) or `192.0.2.53:5323` (Linux). Answers A records for the zone. First process to bind serves everyone; others retry every 2 s to take over. |
| Front door | One HTTP reverse proxy on `:80`, routed by Host to `Entry.Target`. Loopback peers only. |
| Setup | macOS: the network daemon (a launchd job holding one interface with `198.19.0.0/17` routed to it, decision D29), and `/etc/resolver/doze`. Linux: a sysctl for port 80, a managed block in `/etc/hosts`, and a systemd-resolved or dnsmasq route. Windows: nothing yet. |

Naming rules as implemented:

- Qualified names are `<service>.<stack>.doze`. Apex names (`aws.doze`,
  `kafka.doze`) are a fixed table for the standalone tools.
- A registered name owns its subtree. `sqs.ap-south-1.aws.shop.doze` resolves
  with no extra claim, and the most specific claim wins.
- The daemon claims every enabled endpoint once, at daemon start, when
  `defaults { domains = true }`. A sleeping service keeps its name and its
  listener, which is what makes wake-on-connect work.
- With setup done, each proxied service listens on its own loopback address at
  its canonical port. Without setup, everything listens on `127.0.0.1` and two
  services cannot share a port.

## The zone is shared with the standalone tools

`.doze` does not belong to doze alone. doze-aws and doze-kafka each ship as a
standalone binary and as a doze module, and the standalone binaries use
doze-names directly. Checked on 2026-10-10:

| Consumer | doze-names version | Names it claims | Allocation | Setup |
|---|---|---|---|---|
| doze | a September commit, marked indirect | `<service>.<stack>.doze`, and `aws.<stack>.doze` for an aws module | its own allocator, then `ClaimAt` | its own copy in `doze dns-setup` |
| doze-aws | v0.1.0 | `aws.<instance>.doze` | the library's `Claim` | `names.Install` |
| doze-kafka | the 2026-10-04 commit (HEAD) | `kafka.<instance>.doze`, `schema.<instance>.doze` | the library's `Claim` | `names.Install` |

Each of them also starts the resolver and the `:80` front door
(`names.Serve`, `names.ServeIngress`). Whichever process binds first serves
every other process's names, and another takes over when it exits.

What follows from that:

1. **`names.json` is shared between three programs.** Either doze, doze-aws
   or doze-kafka may be the process serving DNS and `:80` for all of them.
   A change to the file's shape or meaning has to land in all three.
2. **Nothing is live, so there is no older build to protect** (decision D14).
   The three tools move to each new doze-names version together, and no
   change here needs to stay readable by an earlier release. That stops being
   true at the first public release of any of them, so the file gets a format
   version now (N7) while it is free to add.
3. **A stack name and a standalone instance name share one namespace.** A
   doze stack `shop` with an aws module and a standalone doze-aws instance
   `shop` both want `aws.shop.doze`. First come wins; the loser is told who
   holds it. The design keeps that rule and makes the dash and `doze doctor`
   show it.
4. **The uncoordinated allocators are doze against the standalone tools.**
   The tools take addresses by hash from `names.json`; doze takes the lowest
   free one from its own file. Moving doze onto the library allocator (N1)
   fixes it on doze's side with no change to the tools.
5. **Setup is one installation for all three.** The address range, the
   resolver file and the Linux configuration are shared. On macOS setup installs
   the network daemon (decision D29), whichever tool runs it. A machine with
   no setup has no per-service addresses, and every tool falls back to
   `127.0.0.1` and distinct ports.
6. **The TLD binds three products.** It is `.doze` (decision D7). doze-aws is
   close to 1.0, and what it ships with is what `.env` files and CI configs
   will contain.
7. **The same engine has two shapes, on purpose** (decision D8). As a module,
   aws answers at `cloud.shop.doze` (the instance name) and `aws.shop.doze`
   (the front door). Standalone, it answers at `aws.<instance>.doze` and at
   `127.0.0.1:4566`. The split stays. What remains is to write it down as a
   table in the doze-aws and doze-kafka docs (unit 3.10).

Rules for every change to doze-names in this plan:

- The three repos move to the same doze-names version in the same phase.
- `names.json` carries a format version from now on.
- A cross-tool test runs doze, standalone doze-aws and standalone doze-kafka
  on one machine and checks that every name resolves whichever started first.

## What the design adds

### Ports

Today a service has one port and binds it itself, or (for a module) binds a
unix socket behind the proxy. The design needs two ports for anything that is
a plain command:

| | Address | Who binds it |
|---|---|---|
| Public | `<service address>:<port>` | doze's proxy, always, awake or asleep |
| Private | `127.0.0.1:<ephemeral>` | the command, from `$PORT` |

- `port = 27017` in the config is the public port.
- `$PORT` is the private one, picked by doze at each start
  (`netutil.FreePort` exists).
- The proxy forwards bytes between them. That is what lets a `service` sleep
  and wake on the first connection, the same way modules do today.
- `$HOST` is `127.0.0.1`.
- A command that ignores `$PORT` and binds the public port itself fails to
  start with a clear message, because doze already holds it.

This applies to `service` always, and to `app` when it declares a `port`
(decision D12). An app without a `port` is unproxied, as today.

What it buys for apps: two stacks can both use port 8080, connection counts
and `held by` work, and `$PORT` means the same thing for every kind. What it
costs: the app must listen on `$PORT`, and there is one more hop for dev
servers. Byte-level forwarding passes websockets and hot reload untouched.

Two details that come with the decision:

- **`localhost` keeps working.** People type `localhost:8080` from habit. For
  an app, doze also listens on `127.0.0.1:<port>` when that port is free, and
  forwards it the same way. When it is not free (another stack has it),
  `doze status` and the dash say the app is reachable by name only.
- **`direct = true` opts out.** The app binds its own port, as today, and doze
  only starts it. This covers anything the proxy cannot carry. The proxy
  forwards TCP only, so an app that needs UDP uses this.

### HTTP names without a port

`http://api.shop.doze` (no port) goes through the shared `:80` front door.
Today that needs `forward = 80` on a `process`. In the design:

- An app or service whose `ready` is `http(…)`, or that sets `console`, gets
  a front-door route automatically.
- The route's target is the public address, so a request through the front
  door wakes the service like any other connection. Today the ingress wakes
  nothing.

### More than one port on a service

`console = "http://127.0.0.1:8025"` names a second port on the same service.
doze adds a second proxy listener on the service's own address at that port,
forwarding to the address given.

- It shares the service's wake and hold accounting: opening the console wakes
  the service and keeps it awake.
- The dash shows it as `http://mailpit.shop.doze:8025`.
- Without per-service addresses (setup not run), the console is reachable only
  on its own port at `127.0.0.1`, and the dash says so.

### Per-service idle time

`sleep = "10m"` on one service. Today `idle_timeout` is global. The reaper
already runs per instance; it needs the per-instance value.

## Work inside doze-names

| # | Change | Why |
|---|---|---|
| N1 | One allocator, in doze-names, behind the registry lock. doze stops using `internal/loopback`'s own file. | Two allocators hand out `127.0.0.10` to `.65` from two files with no coordination. `ClaimAt` does no collision check. |
| N2 | Widen the pool from `127.0.0.10`–`.65` to `127.0.0.10`–`.254` (decision D13). macOS setup aliases the wider range; Linux needs nothing. The fixed addresses for `aws` and `kafka` do not move. | 55 addresses for the whole machine. A stack of ten services uses ten. |
| N2a | Superseded by decision D29: the pool is a block of about 32,000 on every system, and on macOS it is served by the network daemon and not by aliases. |
| N3 | Return an address when a service is removed. | Today it leaks until the daemon exits. |
| N4 | `Lease.Listen(port)` or equivalent, so extra listeners (the console port) are claimed with the name. | The console port needs to be released with the name. |
| N5 | Front-door routes for all ports live in `names.json`. Remove `ingress.json` and `aws-ingress.json`. | Route state is in two places, and the non-80 handler does exact match only. |
| N6 | README and the spec table in `notes/launch/07-names-spec.md` corrected to match the code. | They still describe `127.0.0.54:53` on Linux. |
| N7 | A format version in `names.json`. | Three products read and write it. Free to add now; needed the day any of them ships. |
| N8 | A cross-tool test: doze, doze-aws and doze-kafka together, each start order. | Nothing tests the shared zone across tools. |

N2, N4 and N5 change what the shared file or the shared servers do, so each
lands in doze, doze-aws and doze-kafka in the same phase.

## Work inside doze

| # | Change | Why |
|---|---|---|
| D1 | Bump the doze-names pin to HEAD and mark it a direct dependency. | The pinned commit lacks the loopback-only check on `:80`. |
| D2 | `doze doctor --fix` calls `names.Install`. `doze dns-setup` becomes an alias. | The CLI re-implements setup and installs nothing on Linux. |
| D3 | Fix the Linux resolver address in doctor and daemon messages. | They print `127.0.0.1:5323`. |
| D4 | Lock or remove `stacks.json` and `loopback.json`. | Unlocked read-modify-write. |
| D5 | Private ports and `$PORT` for services and apps. | New. |
| D6 | Console listeners. | New. |
| D7 | Honour `names.Disabled()` (`DOZE_ZONE=off`). | Added to doze-names, never called. |
| D8 | Refuse a stack named `aws` or `kafka`. | Nothing stops it colliding with the apex names. |
| D9 | When a name is held by a standalone tool, show who holds it in `doze status`, the dash and `doze doctor`. | Today the daemon logs one line and carries on. |
| D10 | Bring doze, doze-aws, doze-kafka and doze-modules to one doze-names version. | Three different versions today. |

## The TLD: `.doze` (decided)

Decided on 2026-10-10 as D7: keep `.doze`. The notes call this the one
decision that cannot change after v1, because addresses end up in `.env`
files and CI. The options that were weighed:

| Option | For | Against |
|---|---|---|
| `.doze` | Shortest. It is the brand. Already in code and on machines. The local resolver wins for the zone whatever the public DNS does. | Not reserved. If it were ever delegated publicly, browsers or OS resolvers could treat it specially, as happened with `.dev`. |
| `.doze.test` | `.test` is reserved for exactly this. No collision possible. | Longer. Every address changes. |
| `.doze.localhost` | Browsers treat `.localhost` as a secure context. | Some resolvers hard-wire every `.localhost` name to `127.0.0.1`, which defeats one address per service. |

The risk of `.doze` being delegated publicly is real but small, and it is the
only option that needs no migration. `notes/launch/07-names-spec.md` still
lists the TLD as undecided and should record this.

## Caller identity

Tier: dream. The canvas shows "api has 4 connections" and a caller column on
the aws wire. Nothing today records who connected.

Options, cheapest first:

| Option | What it gives | Cost and limits |
|---|---|---|
| Dependency holds | "held by api, migrate", from declared references | Free. Exists today. Not per connection. |
| Peer PID lookup | The process that opened a TCP connection, then the doze app it belongs to | OS-specific (`/proc` and sock_diag on Linux, `libproc` on macOS). A lookup per accepted connection, so it must be lazy and cached. |
| Per-app source address | Each app connects from its own loopback address | Cannot be forced on an arbitrary command. Not viable. |
| Unix socket peer credentials | Exact PID | Only for clients that use the unix socket. Most don't. |

Plan: ship dependency holds in phase 2. Prototype peer PID lookup as a
time-boxed experiment after phase 3, and keep or cut the caller column on the
result.

## Not in scope

- HTTPS for `.doze` names. The existing `tls` block terminates TLS for
  database clients only; its certificate has no `.doze` names, and nothing
  installs a trust root. Dream tier.
- Names on Windows. doze-names builds there but installs nothing.

## Measures

| Measure | How |
|---|---|
| A name resolves through the OS resolver | A CI job on macOS and Linux runs setup, starts the fixture stack, and resolves `db.shop.doze` with the system resolver. No such test exists today. |
| Two services share a port | The fixture stack runs two postgres instances on 5432. |
| First-connection latency through the proxy to a service | From the journal: `ready.Took` for a connection-caused wake. |
| Proxy overhead on a dev server | Request latency to an app with and without the proxy. Checks decision D12 with data; `direct = true` is the fallback. |
| Cost of the address block on macOS | Measured in doze-names CI on each push (decision D29): 2,000 services on one port, mDNSResponder CPU and lookup time before and after, throughput through the daemon. |
