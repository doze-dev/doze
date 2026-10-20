# What doze already has that recall can use

Surveyed 2026-10-20, read-only. Local checkouts: doze `142af83` (75 ahead of origin/main), doze-sdk `2a1c39a`, doze-aws `383d06a`, doze-kafka `4c92161`, doze-modules `5b06880`, doze-binaries `a288ef3`. Every local checkout is ahead of origin, so it is newer than origin. Paths are relative to `/Users/srini/Code/doze-dev/`. `.claude/worktrees/` copies are ignored.

Design context: `doze/docs/design/recall/README.md` and `fit.md`. fit.md already plans recall as a synthetic built-in instance woken by the proxy, and lists six things to add. This report checks those claims against the code and adds what fit.md does not cover: helper processes, observation, downloads.

---

## 1. Proxy

### Does it understand any protocol?

By default it splices raw TCP. Protocol awareness is an optional capability on each engine.

- The package doc says so: "The generic path is protocol-agnostic (accept -> boot -> count -> splice); engines whose wire protocol needs a preamble, TLS termination, or out-of-band control routing implement engine.ProxyFilter" (`doze/internal/proxy/proxy.go:1-6`).
- `handle` (`proxy.go:127-209`) goes through these steps:
  1. A `engine.WireProxy` hand-off (`:138-155`): an out-of-process plugin owns the whole connection.
  2. Otherwise `ProxyFilter.Preamble` (`:158-171`).
  3. `bootBackend` (`:173`).
  4. `dialBackend`, which is TCP or a unix socket (`:180`, `:225-230`).
  5. A replay of the preamble bytes (`:188-193`).
  6. `ProxyFilter.Handshake` (`:196-204`).
  7. `Acquire`, then `splice`, then `Release` (`:206-208`).
- `splice` is two `io.Copy` calls (`proxy.go:241-262`). Nothing counts bytes or records anything per connection.
- The interfaces live in doze-sdk:
  - `ProxyFilter{Preamble, Handshake}`: `doze-sdk/engine/driver.go:293-306`.
  - `PreambleResult{Client, Replay, Handled}`: `:323-327`.
  - `WireProxy.Handoff` and `WireHost{Boot, Acquire, Release, TLS…}`: `doze-sdk/engine/wire.go:23-45`.
- The only protocol filter in use is Postgres (`doze-modules/modules/postgres/proxyfilter.go`).
  - `readStartup` (`:136-200`) parses `database` and `user` only (`parseParams` `:248`). It throws them away after checking that a database was named.
  - `Handshake` (`:75-96`) swaps the cancel key, and the rest is a plain splice.
  - It does not read `application_name`.
- HTTP is handled separately by the shared `:80` front door in doze-names, an `httputil.ReverseProxy` that routes on the Host header (`doze-names/ingress.go:260-270`). doze only publishes routes to it (`doze/internal/daemon/ingress.go:37-82`). AWS goes through it as a single host, `aws.<stack>.doze` (`doze/internal/daemon/awsingress.go:17-35`).

### Where a passive observer could hook in

| Hook point | What it sees | Latency | Notes |
|---|---|---|---|
| `splice` (`proxy.go:241`) | Raw bytes in both directions, for every engine | None, if the copy goes to a bounded channel or ring buffer that drops when full instead of blocking `io.Copy` | One place covers every protocol. Decoding has to run off the hot path. Wire-plugin engines (`WireProxy`) bypass it, because their bytes never cross into the host (`wire.go:16-22`). |
| `ProxyFilter.Preamble` / `Handshake` | Postgres startup parameters (user, database). With a small change, also `application_name`. | Already on the path | `PreambleResult` would need a metadata field (`driver.go:323`). That is an SDK change. |
| The doze-names `ReverseProxy` (`ingress.go:262`) | HTTP method, path, Host and status for apps fronted on `:80` | A `ModifyResponse` / `Transport` wrapper costs next to nothing | It lives in doze-names, shared by every doze program on the machine. Only portful apps reached by name go through it. Traffic on `127.0.0.1:port` does not. |
| The engine's own recorder (doze-aws, doze-kafka; see section 4) | Fully decoded calls | Already paid | This is the best source: it is already decoded, and it is already bounded. |

Recall's README cuts "observed wire traffic" from v1 (`recall/README.md`, "Cut from v1"). If observation comes back, the engine recorders are the cheap route. A generic tee in `splice` is the expensive one, because recall would have to write a decoder for each protocol.

### Caller identity (D17)

Nothing records which app opened a connection.

- D17: "Per-app connection counts and the aws caller column are deferred… A two-day experiment after phase 3 decides keep or cut" (`doze/docs/design/next/README.md:108`).
- `networking.md:202-220` lists the options:
  - dependency holds, which exist today;
  - peer PID lookup, using `/proc` and sock_diag on Linux and `libproc` on macOS, lazy and cached;
  - a per-app source address, which is not viable;
  - unix peer credentials, which only work for unix-socket clients.
- `phases.md:582` schedules "Caller identity by peer PID lookup" as a dream-tier item after phase 7.
- `gap-analysis.md:23` says: "No per-connection record at all."
- `daemon-api.md:41-46` says the same: an open proxy connection is known only as a count, and a named connection is dream tier.
- `runtime/holders.go:9-40` gives holders as dependant, pin or connection count, with no names.
- Each service gets its own loopback IP as its destination (`daemon/daemon.go:166-175`, via the doze-names zone). Apps still connect from 127.0.0.1, so the source address does not identify the app.
- A cheap partial route that the docs do not list: doze writes every app's env, so it could stamp the caller into it.
  - For AWS, set a per-app `AWS_ACCESS_KEY_ID`. Today every app gets the constant `doze` (`doze/internal/endpoints/endpoints.go:156-166`). doze-aws already parses the key (`doze-aws/internal/sigparse/sigparse.go:28-35`) but does not record it.
  - For Postgres, add `application_name=<app>` to the DSN.
  - For Kafka, `client.id` is already recorded but is set by the app.
  - All of this needs env rendered per app rather than one `module.x.url` value. That is a config change.

### TLS

- An optional `tls` block makes the proxy terminate client TLS for engines that support it, via the ProxyFilter or plugin TLS material. The proxy-to-backend hop stays plaintext (`proxy.go:40-53`, `:55-74`).
- The certificate comes from user files, or is self-signed and cached under `<home>/tls` (`proxy/tls.go:24-47`). It is wired at `daemon/daemon.go:146-162`.
- Generic splices never terminate TLS. If an app speaks TLS to an upstream, a tee would only see ciphertext.
- HTTPS for `.doze` names is dream tier (`networking.md`, "Not in scope").

---

## 2. On-demand processes (service, download, run, port, sleep)

### Contract

`doze/docs/design/next/lifecycle.md`:

- One command engine with three presets (`:9-18`):
  - A service starts on first connection or when a dependant starts.
  - It sleeps after its idle time.
  - It is always proxied.
  - It restarts with backoff after a crash.
- Moments (`:20-34`): `download` runs once per URL, `setup` once per data folder, `run` on every wake, then `ready`, `on_ready` and `stop`. Each step is a journal event, and its output goes to the log store with a step tag (`:36-37`).

### Code path

- **Config.**
  - `ServiceDecl` (`doze/internal/config/runnable.go:33-57`) has fields including `Download map[string]string` keyed by `mac`, `mac_intel`, `linux`, `linux_arm` or `""`, plus `Port`, `Sleep`, `Setup`, `OnReady` and `Stop`.
  - `parseDownload` (`:402-448`) accepts http(s) URLs only. It has no sha attribute.
  - `addService` (`:305-331`) turns the block into a synthetic `InstanceDecl` of the command engine through `runspec.CommandBuilder`. That makes services a "synthetic instance" already.
- **Sleep.**
  - `runspec.ParseSleep` takes a duration or `"never"` (`doze/internal/runspec/runspec.go:132-151`).
  - The registry's `Reapable` skips pinned (`KeepAwake`) instances. It returns instances that are `Idle`, have `Conns == 0`, and have been idle for at least their limit, which is the per-instance `IdleAfter` or else the default (`doze/internal/registry/registry.go:315-338`).
  - The runtime's reaper ticks at a minimum of 250 ms and stops each reapable instance with cause `idle` (`doze/internal/runtime/runtime.go:985-1000`).
- **States** are reaped, booting, active, idle, crashing, failed and done (`registry.go:14-35`). `Acquire` is at `:279` and `Release` at `:294`.
- **Proxied?** Only when there is a declared port and `direct` is not set (`doze/engine/process/config.go:423`). A portless service has no listener, so nothing wakes it except a dependant or a `wake`/`boot` command on the control socket (`control/server.go:212`).
- **Download at boot.** `runtime.go:337-357` calls `dl.Download(inst)`, then `lock.GetDownload(url)`, then `fetchDownload`. That re-execs `doze __fetch` so the daemon never carries an HTTPS stack (`runtime/steps.go:153-200`, `cmd/doze/fetch.go:14-39`). On first fetch it calls `RecordDownload` (trust on first use), then `SetDownloadRoot`.
- **Logs.** Each boot begins a log-store run (`runtime.go:362-366`).

### Could recall run helper processes (a language server per app, an indexer) as hidden services?

Mostly yes. What exists:

- `Config.Add` exists "for synthetic instances that are not declared in the config file" (`config/config.go:118-126`).
- `addService` shows how to build one from a `runspec.Command` (`runnable.go:310-330`). A recall-owned `InstanceDecl{Kind: service, Type: AppEngine, …}` therefore gets the following with no new lifecycle code:
  - registry states and crash backoff;
  - journal events (`wake`, `ready`, `crash`, `sleep`);
  - logs under `<ProjectDir>/logs/<name>/`;
  - idle reaping;
  - a pinned download.
- `Daemon.AddInstance` (`doze/internal/daemon/dynamic.go:131+`) is the live path, which parses one HCL block. It also starts the proxy listener (`dynamic.go:47-120`). That path cannot resolve references to siblings (`:136-140`).

Gaps:

1. **No hidden flag.** Nothing marks an instance internal. A helper would show up in the dash, `doze status`, `doze up` and `doze env`. fit.md section 2 already lists "hidden from instance rows, excluded from `doze up`" as new work. The same flag would cover helpers.
2. **Language servers speak stdio, not TCP.** rust-analyzer only speaks stdio. `tsgo --lsp --stdio` and gopls can also listen on a socket. The proxy wakes on a TCP or unix accept, so a stdio server needs one of two things:
   - a tiny stdio-to-socket bridge, which could be a `doze __lspbridge` hidden subcommand in the style of `__fetch`;
   - or recall spawns the server itself as a child.
   The second loses the shared logs, journal and dash visibility.
3. **One server per app per language** means names such as `recall.lsp.api`. Instance names must avoid colliding with user names. fit.md reserves `recall`, and a `recall.*` or `recall-*` prefix would need reserving too.
4. **Idle accounting.** Recall can hold a helper awake while it is indexing, either with an open proxied connection or with `pin` (`keepawake`, `control/server.go:235`). After that it sleeps through the normal `Reapable` path. Pins are in memory only (`daemon-api.md:44`), which is fine here.
5. **Budget.** A helper's memory counts toward `idle_tree_kb` only while it is awake (`test/bench/budget.json`: darwin `idle_daemon_kb` 34000, `idle_tree_kb` 250000). fit.md already wants a "peak while indexing" entry.

Synthetic or internal instances to copy:

- `service` blocks: `runnable.go:305-331`.
- `task` blocks: `runnable.go:359-386`, the second `cfg.Add`.
- The AWS unified instance, which gets a front-door route rather than a listener of its own (`daemon/awsingress.go`, `config/aws.go:4-17`).
- Hidden self-exec helpers are an established pattern for out-of-process work: `doze __daemon` (`cmd/doze/daemonctl.go:34-40`) and `doze __fetch` (`cmd/doze/fetch.go`).

---

## 3. Downloads and pinning

### doze-binaries, for engine binaries

These are postgres, valkey, kvrocks, ferret, documentdb, mariadb and temporal.

- **Catalog.** `versions.yaml:29-32` maps three triples (`x86_64-unknown-linux-gnu`, `aarch64-unknown-linux-gnu`, `aarch64-apple-darwin`) to runners. There is no Intel mac. Each engine entry has `ref`, `archive_suffix`, `archive_name`, a subset of `triples` and an explicit `versions:` list (`versions.yaml:10-24`).
- **Build.**
  - `.github/workflows/release.yml` runs one `build-engine.yml` per engine (`release.yml:46-56`).
  - `dzb plan` emits the matrix (`build-engine.yml:46`).
  - `recipes/<engine>/build.sh` runs on native runners (`:77-78`).
  - `dzb manifest` computes the sha256 (`cmd/dzb/manifest.go:112-116`, `:141`).
  - The output is uploaded to a rolling GitHub release tagged with the engine name (`build-engine.yml:109,137`).
  - The archive is `<engine>-<full>-<triple>.tar.gz` with a `.sha256` sidecar (`scripts/package.sh:12-13`).
- **Index.** Each release carries an `index.yaml` shaped `engines.<e>.{versions, artifacts: full→triple→{url, sha256}}` (`doze-sdk/binaries/manifest.go:19-45`).
  - The default root is `https://github.com/doze-dev/doze-binaries/releases/download`.
  - Engine archives are unsigned. Only modules from the registry carry ed25519 signatures.
- **Consumer.**
  - `doze-sdk/binaries/binaries.go`: `targetTriple` (`:74-93`; darwin/amd64 is an error), `ResolveMajor` (`:139`), `Ensure` (`:161-181`).
  - The engine cache is `<home>/<engine>/<full>-<triple>/bin`.
  - The engine path requires tar.gz with a `bin/` directory, and reads the archive into memory (`:212-276`).
- **Mirror** (`binaries.go:99-110`). The order is: `Manager.MirrorRoot`, then `DOZE_<ENGINE>_MIRROR`, then `DOZE_MIRROR` + `/<engine>`, then the default. **Mirrors apply only to engine binaries, not to `download` URLs.**
- **doze.lock** (`doze-sdk/binaries/lock.go:19-56`) holds `engines` (`{resolved, source, hashes: triple→"sha256:…"}`), `modules` and `downloads`. `GetDownload` and `RecordDownload` are in `doze-sdk/binaries/download.go:21-52`.
- **Modules** come from the signed registry (`doze-registry`, a static Cloudflare Pages site) through `EnsureArtifact` (`doze/internal/modules/modules.go:35,41,224,249`).
- **Home.** `$DOZE_HOME`, else `~/.doze` (`doze/internal/config/paths.go:13-37`).
  - Projects live at `~/.doze/projects/<slug>` (`paths.go:37`, `:68-75`).
  - The cache is `<home>/cache` (`:43`).

### Service `download` path

This is the one recall would use.

- `EnsureDownload` (`doze-sdk/binaries/download.go:71-170`):
  - caches at `<home>/downloads/<hex(sha256(url))[:16]>`;
  - streams to disk while hashing;
  - refuses a mismatch against the lock before unpacking;
  - re-checks the cached archive's sha against the lock on every wake.
- `unpack` (`download.go:174-208`) sniffs magic bytes: gzip is treated as **tar**.gz, `PK` as zip, and anything else as a bare executable (mode 0755, named after the URL basename).
- The platform pick is `platformKey()` and `Download()` (`doze/engine/process/command.go:97-127`).

### How hard is it to pin a language server?

- **tsgo** (`@typescript/native-preview-<platform>` npm tarballs). The `.tgz` URLs work **today** through the tar.gz path, with `run = "lib/tsgo --lsp --stdio"` (the single top-level `package/` directory becomes the working directory, `download.go:123-130`).
  - The sha256 is trust-on-first-use per platform.
  - npm's sha512 `integrity` is not used.
- **rust-analyzer** publishes per-triple `.gz` files: gzip of a single binary, not a tar. These **fail today**, because `1f 8b` always goes to `untar` (`download.go:186-187`). The fix is a gunzip fallback to a single file, about 15 lines.
- **Pinning in config.** There is no `sha256` attribute on `download`. Hashes only enter `doze.lock` on first fetch, for the platform that fetched (`lifecycle.md:76-88`). For helpers that recall ships itself, recall should carry a compiled-in table of `{url, sha256}` per platform and pass it as `expected` to `__fetch` (the `--sha` flag already exists, `cmd/doze/fetch.go:37`). That needs no user config and no lock churn.
- **Going through doze-binaries instead** would need a `versions.yaml` entry, a `recipes/<tool>/build.sh` that repackages into `bin/`, and a new consumer path (only engine drivers call `Ensure`). It is only worth it for mirroring, the major-to-version index, or signing.
- **Estimate.** A pinned helper on the service-download path is roughly 50 lines plus tests: the gunzip fallback and an optional inline sha. Distribution is not the hard part. The hidden flag and the stdio bridge from section 2 are.

---

## 4. Observation in the modules

### doze-aws: records every external call, not the caller

- **Recorder.**
  - `Recorder` is gateway middleware with an in-memory ring buffer of `trafficCap = 500` entries (`doze-aws/internal/console/traffic.go:23-38`, `:140`, `:225-235`). Nothing is written to disk.
  - The console's own calls are skipped.
  - `TrafficEntry` (`traffic.go:71-117`) holds Seq, At, Service, Action, Resource (a name, not an ARN), Status, Millis, redacted ReqBody/RespBody up to 8 KB, ErrType, Denied, Method, Path, Host, Target, RequestID, and cascade fields Parent/Via.
  - The classification comes from `classify` (`:421`). Resource names come from `jsonResource` (`:871`) or `queryResource` (`:852`).
- **Fan-out across services** (S3 to SQS to Lambda) is traced by `doze-aws/trace/trace.go` (`Event` `:57-84`, `Sink` `:110-116`) into the same ring (`traffic.go:246`). Causes are carried in `AWSTraceHeader` (`trace.go:205-252`).
- **Wiring.** As a module, the recorder is always on (`doze-aws/module/serve.go:30`). Standalone, it is on only when the console is (`cmd/doze-aws/main.go:494-503`). The module path never calls `SetOpResolver`, so S3, Lambda and API Gateway action names fall back to a guess from the HTTP method (`traffic.go:40-55`).
- **Exposure.**
  - The HTMX partials under `/_console/traffic*` are HTML, not JSON (`internal/console/routes.go:46-61`, `handlers_surfaces.go:118-252`).
  - The only JSON is `GET /_console/api/glance` (`glance.go:19-73`). It returns a `wire` list of up to 60 entries as `{seq,t,svc,action,res,code,ms,err}`.
  - There is no SSE or websocket. doze's dash polls glance every second through `awslocal.TailWire` (`module/awslocal/console.go:148-194`), exposed as the `engine.Console.Tail` "wire" panel (`module/console.go:16-23`).
- **Caller.** Unknown. Every app gets `AWS_ACCESS_KEY_ID=doze` (`doze/internal/endpoints/endpoints.go:156-166`). Only IAM reads the key (`iam/authorize.go:245-268`).
- **Inventory.**
  - The block types are bucket, queue, topic, table, function, rule, key, secret, parameter and state_machine (`module/config.go:28-39`). They are converged through `provision.Apply` (`module/converge.go:20-39`).
  - doze-aws does not implement `engine.Inventory`.
  - `GET /_console/api/resources` returns `[{s,n,u}]` (`handlers_create.go:97-110`).
  - `Admin.Resources` merges glance data with resources (`module/admin.go:14-16`, `module/awslocal/console.go:90-121`).

### doze-kafka: the richest recorder, including client.id

- **Recorder.**
  - `doze-kafka/internal/traffic/traffic.go` keeps everything in memory, bounded (package doc `:1-23`): 2,000 rows, identical requests folded within 10 minutes, 100 groups.
  - `Event` (`:116-176`) holds Conn, Remote, **ClientID**, Software, **Principal** (SASL), API key and version, Topics, Group, TxnID, partitions, error, Duration, bytes, and decoded Facts.
  - The source is `protocol.Observation` (`internal/protocol/observe.go:78-113`), an in-process observer interface.
  - It exists only when the console is on (`server/server.go:117`, `:339-343`).
- **Exposure.** HTML partials (`console/routes.go:126-129`), plus one JSON endpoint, `/traffic/new?since=`, which returns `{"n":N,"atLeast":bool}` (`console/traffic.go:1296-1312`).
  - The module's `Console.Tail` streams topic messages, not traffic (`module/console.go:20-103`).
  - `Admin.Resources` lists topics and groups (`module/admin.go:19+`).

### doze-modules

- There is no statement logging in any module: no pg_stat_statements, `log_statement`, MONITOR or slowlog hook.
- Postgres accepts arbitrary `settings = {}` (`modules/postgres/config.go:127`), so `log_min_duration_statement` is possible. The postgres example sets it (`describe.go:33`). The output would land in the instance's log store.
- The postgres console takes snapshots of `pg_stat_activity`, including `application_name` and the query up to 600 characters (`postgres/console.go:75-121`).
- valkey and kvrocks have no Admin or Console.

### doze-sdk: no "facts", no observe hook

- **Admin.** `Resources`, `Actions` and `Run`, with `Resource{Kind,Name,Status,Info}` (`doze-sdk/engine/admin.go:15-43`).
- **Inventory.** `Inventory.Objects(inst) []Object` is computed from config with no live query (`engine/driver.go:112-119`). `Object{Kind,Name,Hash}` is at `engine/types.go:22-26`.
- **Console.** `DescribeConsole` and `Tail` (`engine/console.go:16-107`).
- **Plugin capabilities.** `CAP_INVENTORY`, `CAP_ADMIN` and `CAP_CONSOLE` (`plugin/proto/engine.proto:15-43`).
- There is no "facts" or "observe" concept. "Facts" exists only inside doze-kafka (`internal/protocol/facts.go`).
- Which modules implement what:
  - **Inventory:** postgres (roles, database, schemas, extensions; `postgres/inventory.go:24-50`), mariadb, ferret and temporal. Not aws or kafka.
  - **Admin and Console:** postgres, aws and kafka.
- **Recall's way in.** Control op `resources` (`control/server.go:237`) returns `ResourceView{Kind,Name,Status,Info}` (`control/types.go:92-97`). It works for postgres, aws and kafka, but **it needs the instance awake**, so it would wake the module.
  - For a sleeping stack, the declared names come from config. For the plugin modules (aws, kafka) the nested blocks are decoded inside the plugin and are opaque to doze (see section 5).

---

## 5. Resource declarations and env wiring

**Declarations.** Nested resource blocks live inside a module block (`doze/examples/shop/doze.hcl:36-50`):

```hcl
module kafka "events" { version = 4  port = 9092  topic "orders" { partitions = 3 } }
module aws "cloud"    { port = 4566  bucket "uploads" {}  queue "orders" {} }
```

**Apps reference attributes** (`shop/doze.hcl:84-117`):
- `DATABASE_URL = module.db.url`
- `KAFKA_BROKERS = module.events.address`
- `AWS_ENDPOINT = module.cloud.url`
- literal strings such as `ORDERS_QUEUE = "orders"`, which do not reference the queue block
- `API_URL = "http://${app.api.address}"`

**Evaluation** (`doze/internal/config/eval.go`):

- `evaluate` (`:139-207`) runs three steps:
  1. It collects references per instance with `instanceDeps` (`:300-354`) and `referencedTraversals` (`:370-390`).
  2. It sorts topologically (`:406-442`).
  3. It decodes each body in order, growing the eval context with `module.<name>` and `app.<name>` objects (`:178-206`).
- `attributesFor` (`:447-506`) builds each object:
  - `name`, `engine`, `address`, `url`, `env_var`, and `host`/`port` or `socket`;
  - the domain name in place of the IP in domains mode (`:461-466`);
  - the shared AWS endpoint URL (`:480-482`);
  - extra attributes from `engine.Attributer` (`:500-504`).
- Plugin modules (aws, kafka) decode their own body remotely (`decodeBody`, `:226-246`), so doze core holds their spec as opaque bytes. **Core has no list of a module's nested queues, topics and buckets.** Only the plugin's `Inventory.Objects`, `Admin.Resources` or console API knows them.

**What survives as data.**

- **Resolved values: yes.** An app's `env` is decoded into `map[string]string` (`doze/engine/process/config.go:29`, `:84`) with every reference already resolved to a concrete string, such as `postgres://…@db.shop.doze:5432/…`. `mergedEnv` layers OS env, then `env_file`, then `env` (`process.go:195-215`).
- `endpoints.EnvAssignments` gives the default var-to-URL table for each service (`doze/internal/endpoints/*.go:97-166`, behind `doze env --json`, `cmd/doze/env.go:32-80`). It covers only each service's default var, such as `DATABASE_URL`, not every app's env.
- **Provenance: only partly.** `InstanceDecl.Refs`/`After` keep `Ref{Kind, Name}` (`runnable.go:24-29`, `config.go:110-112`), meaning "api references module.db". They do not keep which attribute or which env key.
  - The traversal-to-env-key map exists only during evaluation. That is why fit.md section 3 wants a stack hand-off JSON built from `internal/config`. Building it means re-walking each app's `env` attribute expressions (`hcl.Expression.Variables()`) in `referencedTraversals`, which is a small addition.
- **Literals are where code matching matters.** `ORDERS_QUEUE = "orders"` names `queue "orders"` only by matching strings. Recall could match env literals and string constants in code against the declared resource names, once the plugin's resource names are available.

**Matching code to the stack, already half-built.** `internal/detect` (`doze init`) scans source for env reads with `reGetenv` (`os.Getenv|LookupEnv|getenv|env("X")`, `detect.go:138`) and for imports (`:139`). It maps driver packages and env var names to module kinds (`kinds`, `detect.go:86-150`; for example `DATABASE_URL` → postgres `url`). It is regex-level, but it is the same join recall needs: which app reads which env var, and what that var is wired to.

---

## 6. Journal and log store: inputs for a diagnose tool

### Journal

- Code: `doze/internal/journal/journal.go`. The file is `<ProjectDir>/run/journal.jsonl` (`daemon/daemon.go:134`).
- It is capped by `DefaultMaxBytes = 1 MiB` and rotated once to `.1` (`journal.go:64`, `:111`, `:150`).
- JSON lines of `Event{seq, time, kind, instance, cause{kind,by}, took(ns), detail}` (`:52-60`).
- **Kinds:** wake, ready, sleep, **crash** (detail is how it exited), restart, give_up, **failed** (detail is why), step, pin (`:21-31`).
- **Causes:** connection, dependency (by names the dependant), command, restart, idle, shutdown, watch (by names the path) (`:34-42`).
- **API:** `Since(seq)`, `Tail(n)`, `Subscribe(ctx, buf)`, and the static `Read(path)` (`:163`, `:185`, `:196`, `:261`). `Read` works on the file without the daemon.

### Log store

- Code: `doze/internal/logstore/logstore.go`. Path: `<ProjectDir>/logs/<instance>/r%06d.s%03d.log` (`:117`; opened at `runtime/runtime.go:109`).
- Plain text, one line per printed line: `2026-10-10T09:15:02.418305Z o listening on :8080` (`:3-10`).
  - The timestamp is microsecond UTC (`:46`).
  - The stream is `o` (stdout), `e` (stderr) or `s:<step>` (`:34-38`).
- It keeps 5 runs or 10 MB per instance by default (`:41-44`).
- **API:** `Runs`, `Read(cursor)`, `Tail(run, n)`, `Follow` (pushed by the writer, not polled) (`:366-496`). Logs of sleeping and crashed instances stay readable.
- **For a diagnose tool:** recall can read a crash (journal `crash`/`failed`/`give_up` and its `detail`) together with the matching run's last N stderr lines. It can do this either directly from the files (same user, no daemon needed) or through the control socket (section 7).
- **Missing:** no level detection (left to clients), no structured app logs, and no stack-trace parsing.

---

## 7. Daemon API and how a sidecar talks to it

### Transport

- Newline-delimited JSON on the unix socket `<ProjectDir>/run/doze.admin.sock` (`daemon/daemon.go:38`).
- The client is `control.NewClient(path)` (`control/client.go:17-25`).
- **Protocol 2** (`control/types.go:281-291`):
  - It opens with `{"hello":{"proto":2,"client":"…"}}`.
  - Requests carry an `id`, so many can share one long-lived connection (`:18-23`).
- There is no authentication beyond socket file permissions.

### Ops

`control/server.go:200-241` and the `Handler` interface at `types.go:216-255`:

| Group | Ops |
|---|---|
| State | `status` |
| Lifecycle | `up`, `down`/`sleep`, `boot`/`wake`, `restart`, `reset`, `task` |
| Logs | `logs` (with `run`, `tail`, `since_time` and `follow`) |
| Live config | `add`, `remove` (live instances) |
| Pinning | `keepawake`/`pin` |
| Data | `resources`, `admin` |
| Streaming | `subscribe` |

### Subscriptions

`types.go:294-300`, implemented in `daemon/subscribe.go:25-232`:

| Topic | What it carries |
|---|---|
| `instances` | A snapshot, then changes. Not lossy. |
| `stats` | CPU, RAM and connection count per instance, plus child processes (`StatView`, `:303-312`). Sampled only while subscribed. |
| `journal` | From `Since`, or `JournalFromStart` (`:333`). |
| `logs` | For the named instances, with a backlog. |
| `console` | A module's panels. |

- **There is no file or source-change topic.** Today's watcher (`doze/internal/watch/poll.go:1-11`, `:72`) polls size and mtime of an app's `watch` globs once a second, and only while that app is awake. fit.md section 4 already flags the need for a daemon-owned change feed.
- **There is no config topic.** A `config_changed` kind is planned in `daemon-api.md:61-62` but not implemented in `journal.go:21-31`.

### How recall would talk to the daemon

- As the planned synthetic instance, recall dials the socket. fit.md's hand-off passes the socket path, plus the stack JSON and a contract version. It then calls `subscribe` for `journal`, `instances` and `logs`, and makes unary `logs`, `resources` and `wake`/`pin` calls.
- `cmd/doze/parity_test.go` enforces that every dash action has a CLI form. fit.md notes `doze recall check` needs to be added there.

---

## 8. Other pieces recall can reuse

| Piece | Where | What recall gets | Limits |
|---|---|---|---|
| Glob match and content hash | `doze/internal/inputs/inputs.go:24` (`Match`, with `**` support), `:74` (`Files`), `:124` (`Hash`: sha256 over path, NUL, then the file's sha256) | Content-hash keys for extraction caches (`~/.doze/recall/cache/`), and include/exclude globs | It skips only `.git`, `node_modules` and `.doze` (`:70`). It does not respect `.gitignore`, which fit.md requires. It reads every file on every call, with no stat cache. |
| Poll watcher | `doze/internal/watch/poll.go:35-72` | Size and mtime snapshots, debounced. Already shared with task inputs. | Only while the app is awake, and only for its globs. freshness.md and fit.md section 4 plan the daemon-owned feed. |
| Block fingerprints | `doze/internal/config/fingerprint.go:15-47` (`InstanceDecl.Fingerprint`, `BlockFingerprints`) | A stable hash for each block, so recall can tell which stack blocks changed since the last index | |
| Setup-once marker | `<DATA>/.doze-setup` (`lifecycle.md:49-53`) | A pattern for "re-index only when the hash changed" | |
| Project detection | `doze/internal/detect/detect.go:25-150` | App directories, Go `main` packages (`goMains` `:381`), package manager (`:478`), env reads, driver imports | Regex-level |
| Config loader | `config.Parse` and `loadConfig` (as in `cmd/doze/env.go:49`) | The full resolved stack in-process, without the daemon. `Lookup`, `Instances` and `Refs` serve `doze recall check`. | Plugin modules must be resolvable (fetched), or the loader's shape pass uses stand-in attributes (`eval.go:249-293`). |
| `__fetch` self-exec | `cmd/doze/fetch.go` | Pinned, cached downloads of helper binaries, with no HTTPS stack resident in the daemon | |
| Console framework | `doze-sdk/engine/console.go`, `doze/internal/daemon/console.go` | A way to put recall panels in the dash | Recall is not a module, so it would need a core-side adapter. |
| Git helpers | none in doze or doze-sdk (no `exec.Command("git")`, no go-git) | | freshness.md's "stale since which commit" needs new code. |

---

## What recall can lean on, and what's missing

| Need | Lean on (exists) | Missing / to build | Size |
|---|---|---|---|
| Sleep until called, wake on connect | Proxy lazy boot and hold-while-connected (`proxy.go:127-221`); registry and reaper (`registry.go:315`, `runtime.go:985`) | Nothing for a TCP-served recall | — |
| Recall as a daemon-owned process | `Config.Add` synthetic instances (`config.go:118`); service→`InstanceDecl` pattern (`runnable.go:305-331`); supervisor, journal, logs for free | A `hidden`/internal flag (dash, status, up, env); reserving the `recall` name; adding `recall` to `reservedBlocks` | S–M |
| Helper processes (an LSP per app, an indexer) that sleep | The same synthetic-service path; `pin` to hold while indexing; idle reap; `download` | A stdio-to-socket bridge, since LSPs speak stdio and the proxy wakes on accept; a naming scheme; budget entries | M |
| Pinned helper binaries | `__fetch` and `EnsureDownload` (stream, sha, cache under `~/.doze/downloads`, re-verify on wake); tsgo `.tgz` works today | A gunzip-single-file fallback (rust-analyzer `.gz`); a recall-compiled `{url, sha256}` table passed as `--sha`; `DOZE_MIRROR` does not cover downloads; no Intel-mac builds in doze-binaries | S |
| Declared stack: instances, kinds, deps | `config` loader, `Refs`, `Deps`, topology, fingerprints | Per-attribute provenance (env key ← `module.x.attr`) must be captured during `evaluate` for fit.md's hand-off JSON | S–M |
| Resolved values to match against code strings | App `env` resolved to concrete strings (`process/config.go:29`); `doze env --json` | The nested resources of plugin modules (queue and bucket names) are opaque to core. They are available only from the plugin (`Admin.Resources` when awake; `Inventory.Objects` not implemented by aws or kafka). Need a cheap offline "declared resources" RPC or a field in the decode result. | M |
| Who calls what at runtime (observed edges) | doze-aws recorder (service, action, resource, latency, cascades); doze-kafka recorder (client.id, topics, group, principal); postgres `pg_stat_activity` snapshots | No caller identity anywhere (D17 deferred). The recorders are in-memory rings (500 and 2,000 entries) exposed as HTML, plus one 60-entry JSON glance. No SDK-level observe hook. Proxy splice has no tee. Cut from recall v1 anyway. | L (if revived) |
| Cheap caller identity | doze writes every app's env | A per-app `AWS_ACCESS_KEY_ID`, a per-app `application_name` in the Postgres DSN, recorders storing it. Otherwise, the peer-PID experiment. | M |
| Diagnose: crashes and logs | Journal JSONL with crash, failed and give_up detail; log store with runs, streams, tail and follow, readable while asleep; both through the socket or directly from the files | Level detection, stack-trace parsing, correlating a crash with code locations (recall's job) | S for access |
| Event subscriptions | `subscribe`: instances, stats, journal, logs, console; proto 2 with multiplexed ids | No source-file change topic, no `config_changed` event | M (the file feed is the big one, per freshness.md) |
| Content hashing, globs | `inputs.Match`, `Files`, `Hash` | `.gitignore` support; stat-based incremental hashing | S |
| Git (commit for "stale since") | Nothing | Everything | S |
| HTTP view of app routes as served | doze-names `ReverseProxy` on `:80` (`doze-names/ingress.go:262`) | A logging hook (shared across doze programs); misses direct `127.0.0.1:port` traffic | S–M, optional |
| TLS-terminated traffic | Proxy TLS for DB clients (`proxy/tls.go`) | Nothing generic; HTTPS for `.doze` is dream tier | — |
