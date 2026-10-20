# Research 4: the stack join, from code and from the wire

Question: what is the best way to build the cross-service picture (app → queue/topic,
app → table/column, app → route of another app, app → external service), resolved
back to resources declared in `doze.hcl`, accurately and with low effort for users,
from code (static) and from what doze's proxy and backends can see (dynamic)?

Date: 2026-10-20. Code references checked against `doze` at `142af83`, plus
`doze-modules`, `doze-aws` and `doze-kafka` working trees on this machine.
Numbers marked *measured* were computed for this report; numbers marked
*estimate* are mine and should be treated as hypotheses for `eval.md`.

---

## 0. The short answer

1. **Doze already holds the hard half of the join.** Generic architecture-recovery
   tools spend most of their effort working out which config value points where
   (docker-compose, `application.properties`, Kubernetes manifests). In doze, that
   half is *declared*: `doze.hcl` says `app.worker` env `JOBS_QUEUE_URL` comes from
   `module.cloud:queue.jobs.url`. Static analysis only has to carry a value from the
   call site back to `os.Getenv("JOBS_QUEUE_URL")` (or a literal name), which is
   usually a short, intra-app path. This is why doze can beat published tools.
2. **The operation catalogue can be mostly generated, not hand-written.** For AWS,
   Smithy models plus AWS's Service Authorization Reference (SAR) resolve which input
   member names the resource for 72% of all resource-scoped operations across ten
   services and for every common data-plane call except four, which need a ~10-line
   override table (*measured*, §2.3). Redis/Valkey publishes the same thing as
   *key specs*. Kafka, SQL and HTTP have a handful of client libraries per language.
3. **The dynamic side is mostly already built, and it is in doze's own servers, not
   in a generic proxy.** `doze-aws` parses SigV4 on every request (~200 ns) and knows
   the operation and resource; `doze-kafka` already has an `Observer` hook that
   records client id, software, principal, remote address, topics and groups per
   request; the Postgres proxy filter already parses the startup packet (`user`,
   `database`) and terminates TLS. What is missing is **per-app attribution** (a
   per-app AWS access key id, `PGAPPNAME`, a source-port → pid lookup), and a
   read-only tee of Postgres `Query`/`Parse` messages.
4. **Static and dynamic find different edges, so merge them.** Static finds code
   paths never run in a dev session; dynamic finds what static cannot resolve and
   proves what static only claims. The literature on URL extraction shows the
   overlap between the two can be small (§5). Both are needed, and each is the
   other's test oracle.
5. **Order:** static Go join first (as `README.md` plans), then observations from the
   doze-owned backends (AWS, Kafka, Postgres startup + statements, the :80 ingress),
   because they are cheap, have precision near 1.0 once attribution exists, and give
   the eval a free ground truth for static recall. eBPF, TLS interception and
   requiring OpenTelemetry are not worth it for doze.

---

## 1. What prior art reaches, with numbers

### 1.1 Microservice architecture recovery (static)

The best quantitative source is the 2024/2025 comparison of static recovery tools
(Schneider et al., MSR'24 registered report, EMSE 2025). Nine of thirteen tools ran,
on 17 small and medium Java/Spring applications with hand-built ground truth.

Connections (inter-service links), Table 5b of arXiv 2412.08352:

| Tool | Approach | Precision | Recall | F1 |
|---|---|---|---|---|
| Code2DFD | keyword search over code + config, iterative "snowballing" of new keywords | 0.92 | 0.83 | **0.87** |
| MicroDepGraph | docker-compose `depends_on` | 0.98 | 0.51 | 0.67 |
| Attack Graph Generator | deployment files | 0.39 | 0.86 | 0.54 |
| RAD-source | | 0.78 | 0.02 | 0.04 |
| Prophet | annotations, naming conventions | 0.67 | 0.01 | 0.02 |
| MicroGraal, RAD, ContextMap | | 0 | 0 | 0 |

- Combining tools: Code2DFD OR AGG reached recall 0.97 on connections; the best
  all-characteristics combination reached P 0.92 / R 0.89 / F1 0.91.
- Components (services) are easy: Code2DFD P 0.97 / R 0.98. **Edges are the hard
  part**, which is exactly the stack join.
- Effort lessons from the same paper: several tools needed debugging or
  produced nothing ("despite us having confirmed that the tools executed
  correctly"); tools that read only deployment files miss most connections;
  ContextMap only finds Feign/HttpExchange clients and "would need manual annotation"
  otherwise; heuristics such as "a component per folder" backfire.
- Code2DFD's own paper reports P 93% / R 85% overall on the same 17 apps, with
  traceability from each model item back to the code snippet: the same evidence
  shape recall wants (`file:line`).
- Prophet-style REST matching (Cerny's group): endpoints from JAX-RS/Spring
  annotations, calls from `RestTemplate`, matched by path, parameter count and
  method; on train-ticket it found 135 calls and missed 18 (88% recall), the misses
  being calls choosing between several possible URLs.
- LLM-only recovery (ESEM 2026, GPT-4o agent exploring the repo, same 17 apps, 20
  runs each): mean F1 0.604 (Llama4: 0.254); precision higher than recall;
  "service-to-service connections are the most challenging" element. An agent alone
  is clearly worse than rules plus resolution, which supports the README's "AI only
  adds" principle.

Caveat for doze: all of this is Java/Spring, small apps, homogeneous patterns. The
numbers show the *shape* (keyword + config resolution gets ~0.9 precision and ~0.85
recall; combining sources pushes recall into the 0.9s) rather than what doze will get
on Go/TypeScript stacks.

### 1.2 CodeQL: the framework-model approach

CodeQL's "models as data" are the most complete public catalogue of *which call
consumes which argument* for a library:

- `sinkModel(package, type, subtypes, name, signature, ext, input, kind, provenance)`,
  `input` an access path such as `Argument[0]`, `Argument[receiver]`, `ReturnValue[1]`.
- Source kinds include `remote` and the local threat models `environment`, `file`,
  `commandargs`, `database`; sink kinds include `sql-injection`, `nosql-injection`.
- The Go pack ships 100+ model files under `go/ql/lib/ext`, including
  `database.sql`, `github.com.jmoiron.sqlx`, `github.com.masterminds.squirrel`,
  `github.com.go-pg.pg.orm`, `github.com.beego.beego.client.orm`,
  `github.com.gogf.gf.database.gdb`, `github.com.rqlite.gorqlite`, and config/env
  sources `github.com.caarlos0.env`, `github.com.kelseyhightower.envconfig`,
  `github.com.joho.godotenv`, `github.com.gobuffalo.envy`,
  `github.com.hashicorp.go-envparse`; routers `go-chi.chi`, `gin-gonic.gin`,
  `labstack.echo`, `gorilla.mux`, `gofiber.fiber`.
- JavaScript has a `ClientRequest` concept with `getUrl()`/`getHost()` per library
  (`FetchUrlRequest`, `AxiosUrlRequest`, `GotUrlRequest`, `SuperAgentUrlRequest`, …).

What CodeQL does **not** have is the resource dimension: no model says "argument
`QueueUrl` names a queue". Its models are about taint, not about identity. The
lesson for recall is the *format*: a flat, versioned table of
`(package, receiver, method, argument path, kind)` rows, with provenance, that a
generic engine applies. Recall's operation catalogue should be exactly that, with
`kind` being `sqs.send`, `sqs.receive`, `sql.query`, `http.client`, `kafka.produce`,
… and a `resource` access path. CodeQL's Go sink list is a good checklist of which
SQL libraries to cover; licensing (MIT for the CodeQL libraries) should be checked
before copying rows rather than re-deriving them.

### 1.3 Semgrep, Sourcegraph, Backstage

- **Semgrep** rules can express "call to `sqs.SendMessage` with `QueueUrl: $X`", and
  constant propagation folds Boolean, numeric and string constants; but the
  community edition is single-file (intrafile) only, interprocedural and interfile
  propagation is the commercial platform. For Go, recall has `go/types` (exact
  constant values) and `x/tools/go/ssa` in-process, which is strictly better.
  Semgrep is relevant as a *pattern format* for the non-Go languages on tree-sitter.
- **Sourcegraph / SCIP** give precise symbol navigation within and across repos but
  no resource or service edges (protoc-gen-scip was excluded from the 2024 tool study
  as not an architecture tool).
- **Backstage** relationships (`dependsOn`, `providesApis`, `consumesApis`) are
  hand-written in `catalog-info.yaml`; the graph is drawn automatically, the edges are
  not discovered. This is the "told" layer, not the "proven" one, and it is the
  failure mode recall exists to avoid (edges people forget to update).

### 1.4 Configuration linking

CfgNet (TSE 2023) links configuration options across technologies (Docker, Maven,
CI, …) with plugins, and found by literature study that *equality of values* is the
most common cross-artifact dependency. Doze needs none of that machinery: the
references in `doze.hcl` are already explicit edges from resource attributes to env
vars, handed to recall in the stack JSON (`fit.md` §3).

---

## 2. Static: how to build it

### 2.1 The pipeline

```
call site (catalogue rule)  →  resource argument expression
    →  value resolution  →  {Const "jobs" | Env JOBS_QUEUE_URL | ARN … | Unknown}
    →  join with stack JSON (env var → reference → declared resource)
    →  edge app.worker —sends→ module.cloud:queue.jobs   (evidence: file:line, chain)
```

Each edge keeps the resolution chain as evidence ("`SendMessage` at
`worker/jobs.go:41`, `QueueUrl` = `cfg.JobsQueue`, set from env `JOBS_QUEUE_URL` at
`config.go:12`, which `doze.hcl:18` wires to `module.cloud:queue.jobs.url`"). This is
what lets an agent trust or check it.

### 2.2 Value resolution: what is achievable

A small abstract domain is enough; full string analysis is not needed:

| Value form | Example | Resolution | Achievable |
|---|---|---|---|
| Literal / `const` | `"orders"`, `const Q = "jobs"` | `go/types` `TypeAndValue.Value` gives the exact constant | ~100% |
| Env read | `os.Getenv("JOBS_QUEUE_URL")`, `os.LookupEnv` | model as source kind `environment` | ~100% at the read |
| Config struct tags | `envconfig:"JOBS_QUEUE_URL"`, `env:"…"` (caarlos0), `koanf`, `viper.GetString("jobs.queue")` + `AutomaticEnv` | per-library rule: tag or key → env var name (viper with its key replacer) | high for tag libraries, medium for viper |
| Field flow | `cfg.JobsQueue` set in `Load()`, read in `worker.go` | field-based (flow-insensitive) summary: "field `Config.JobsQueue` ⊇ Env(JOBS_QUEUE_URL)"; cheap and good enough for config structs written once | high |
| Concatenation / `fmt.Sprintf` | `base + "/queue/" + name`, `fmt.Sprintf("%s/orders/%d", base, id)` | `Concat(parts)` with unknown parts as wildcards; enough for HTTP templates and ARNs | medium |
| Two-step lookup | `GetQueueUrl(QueueName: "jobs")` then `SendMessage(QueueUrl: out.QueueUrl)` | catalogue marks `GetQueueUrl.QueueName` as resource and `QueueUrl` output as "same resource" | high |
| Passed through function params | `func send(ctx, url string)` called from 3 places | context-insensitive: the parameter takes the union of call-site values (`OneOf`) | medium, may give 2–3 candidates |
| Loaded at run time | queue name read from a DB row, a JSON file, a flag | `Unknown` | not achievable statically; dynamic only |

Published evidence on how far string resolution gets: Stringoid (Android, 30,000
apps) improved over plain constant extraction by +5% for domains, +45% for paths,
+134% for query keys by modelling concatenation, but against dynamic traces of 20
apps the overlap was small (only ~23% of observed domains and ~7% of path pairs were
also found statically), because many URLs come from responses or user input.
Meurice et al. (CAiSE 2016) extracted SQL for 71.5–99% of database access points in
three Java systems with 87.9–100% of extracted queries valid. Doze's case is easier
than Android's: the interesting values are resource *names*, which in a doze stack
come from env vars that doze itself sets.

*Estimate* for a Go app in a doze stack: 80–90% of AWS/Kafka call sites resolve to
exactly one declared resource, 5–10% to a small candidate set (shown as such), the
rest Unknown. Unknown sites are listed, not dropped: "worker calls SendMessage at
x.go:41, queue not resolved" is still useful to an agent.

### 2.3 AWS: generate the operation catalogue from AWS's own data

Two machine-readable sources exist and are already used in `doze-aws`
(`dzaudit iam`):

1. **Smithy models** (vendored in `doze-aws/.audit-models/`): input shapes,
   `smithy.api#required`, and the endpoint-rules traits
   `smithy.rules#contextParam` / `smithy.rules#operationContextParams`, which mark
   the members that identify the resource for routing.
2. **Service Authorization Reference** JSON
   (`servicereference.us-east-1.amazonaws.com/v1/<svc>/<svc>.json`): per action,
   the resource types it acts on (`queue`, `topic`, `bucket`, `table`, …) and
   `IsWrite`/`IsList`/`IsPermissionManagement`, which gives the read/write
   direction of the edge.

*Measured* with a bun script over the vendored models and the downloaded SAR files
(10 services, 637 operations, 441 resource-scoped):

| Service | resource-scoped ops | resolved by `contextParam` | resolved by member name ↔ resource type | unresolved |
|---|---|---|---|---|
| sqs | 19 | 0 | 16 | 3 (message-move tasks) |
| sns | 18 | 0 | 9 | 9 (subscriptions, `ResourceArn`) |
| s3 | 51 | 51 | 0 | 0 |
| dynamodb | 47 | 44 | 1 | 2 (`BatchGetItem`/`BatchWriteItem`, but `operationContextParams` gives `keys(RequestItems)`) |
| kinesis | 36 | 34 | 2 | 0 |
| lambda | 76 | 0 | 56 | 20 (layers, ESM `UUID`) |
| events | 43 | 1 | 9 | 33 (generic `Name`) |
| states | 30 | 0 | 24 | 6 |
| secretsmanager | 20 | 0 | 20 | 0 |
| ssm | 101 | 0 | 52 | 49 |
| **total** | **441** | **130** | **189** | **122 → 72.3% resolved** |

For the data-plane calls apps actually make, the picture is better. The resource
member is generated correctly for `SendMessage(QueueUrl)`, `ReceiveMessage`,
`DeleteMessage`, `GetQueueUrl(QueueName)`, `PublishBatch(TopicArn)`,
`Subscribe(TopicArn)`, all S3 object calls (`Bucket`, `Key` for prefix detail),
`PutItem/GetItem/Query/Scan/UpdateItem(TableName)`,
`BatchWriteItem` (`keys(RequestItems)`), `TransactWriteItems`
(`TransactItems[*].[…TableName]`), `Invoke(FunctionName)`,
`StartExecution(stateMachineArn)`, `GetSecretValue(SecretId)`,
`GetParameter(Name)`. The exceptions that need a hand-written override are few:

- SNS `Publish`: `TopicArn` *or* `TargetArn` (neither required).
- Kinesis `PutRecord(s)`: `StreamName` *or* `StreamARN`.
- EventBridge `PutEvents`: `Entries[*].EventBusName`, defaulting to `default`.
- Kinesis `GetRecords`: identified only by an opaque `ShardIterator` (dynamic only,
  or via the `GetShardIterator` that produced it).

So: **generate the catalogue from Smithy + SAR, ship a ~10-row override file, and
test it against the doze-aws handlers** (doze-aws already knows the resource for
every request, which is a free conformance check of the catalogue).

Per-SDK naming is mechanical from the Smithy member name, so one catalogue serves
every language:

| SDK | Call shape | Resource argument |
|---|---|---|
| Go v2 | `client.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: …})` | struct field `QueueUrl` |
| JS v3 | `client.send(new SendMessageCommand({QueueUrl: …}))` | object key `QueueUrl` |
| boto3 client | `sqs.send_message(QueueUrl=…)` | kwarg `snake_case(member)` |
| boto3 resource | `sqs.Queue(url).send_message(...)` | resource identifier from boto3's `resources/*.json` |
| Java v2 | `SendMessageRequest.builder().queueUrl(…)` | builder method `lowerCamel(member)` |

Resource values then resolve against doze-aws naming: queue URLs
(`http://sqs.<stack>.doze/000000000000/jobs`), ARNs (`arn:aws:sns:…:orders`),
bucket and table names, all of which doze can map back to declared resources
because doze-aws mints them.

### 2.4 SQL: tables and columns

**Where the SQL is.** In priority order of precision and effort:

1. **sqlc**: queries live in `.sql` files with named statements, schema in
   migration files, both listed in `sqlc.yaml`. Exact: parse the files, map each
   generated Go method (`q.GetOrder`) to its query. Highest value per hour of work.
2. **Literal SQL at a modelled sink**: `db.QueryContext(ctx, "SELECT …")`,
   `pgx.Conn.Query`, `sqlx.Select`, `squirrel` builders (partial). Use the CodeQL
   Go `sql-injection` sink list as the checklist of functions and argument
   positions; resolve the string with §2.2 (constants and concatenation of
   constants cover most hand-written SQL).
3. **ORMs by convention**: GORM (snake_case plural of the struct name unless
   `TableName()`; columns from `gorm:"column:…"` tags), Prisma (`model` names,
   `@@map`/`@map`), Django (`app_model` unless `Meta.db_table`), SQLAlchemy
   (`__tablename__`, `Column`), TypeORM (`@Entity('name')`), Rails (pluralised class
   name). Table-level edges are reliable; column-level edges from ORM calls are
   only as good as the field → column mapping, and `SELECT *`-style loads are common.
4. **Dynamic SQL** built from fragments at run time: `Unknown` statically; this is
   exactly where the wire observation (§3.2) fills in.

**Schema.** Prefer the live catalogue over parsing migrations: doze runs the
database, so `information_schema`/`pg_catalog` give the exact schema after
migrations. But reading it would wake the database, so snapshot it opportunistically
(when the db is already awake, or when it is about to idle) and fall back to
migration files / `schema.sql` / sqlc schema when no snapshot exists. Column
resolution (`SELECT o.id FROM orders o JOIN …`, `SELECT *`) needs the schema to
resolve aliases and stars.

**Parsers** (all checked 2026-10):

| Parser | Pure Go? | Fidelity | Notes |
|---|---|---|---|
| `pganalyze/pg_query_go` v6 | **No, cgo** (compiles parts of Postgres; "expect up to 3 minutes" first build) | Postgres's own grammar (PG 17); `Parse`, `Fingerprint`, `Normalize` | Used by sqlc |
| `wasilibs/go-pgquery` | **Yes** (libpg_query compiled to wasm, run on wazero) | Same as above, same AST types | 4–5x slower than cgo: `ParseSelect1` 24.6 µs vs 6.6 µs, `FingerprintSelect1` 16.0 µs vs 2.5 µs; fine for indexing, not for a hot path; actively updated (pseudo-version dated 2026-10-20) |
| `cockroachdb/cockroachdb-parser` | Yes | CockroachDB dialect, close to Postgres but not identical | **Archived 2026-10-06**, read-only |
| `ValkDB/postgresparser` | Yes (ANTLR) | Not full Postgres; extracts tables, aliases, columns with roles (projection/filter/join/group/order), CTEs | 70–350 µs per query; built for exactly this extraction use |
| Vitess `sqlparser` forks (`xwb1989`, …) | Yes | MySQL; DDL partial; forks lag | Vitess itself is not self-contained |
| TiDB `parser` | Yes | MySQL, full | Large dependency |

Recommendation: `go-pgquery` in the `doze-recall` binary (no cgo, real Postgres
grammar, identical AST to pg_query so fingerprints match what the wire side would
compute). Its wazero runtime and embedded wasm add binary size that I have not
measured; check it against the recall binary budget before committing. For MySQL
(if a module exists), the TiDB parser.

### 2.5 HTTP: client calls to routes of another app

Three steps, each mechanical:

1. **Route table per app** (server side): Go 1.22 `ServeMux` patterns
   (`"GET /orders/{id}"`), chi/gin/echo/fiber/gorilla registrations (CodeQL already
   models these packages), Express/Fastify/Hono, FastAPI/Flask decorators, Spring
   annotations; or an `openapi.yaml` / `.proto` if present (exact).
2. **Client calls**: `http.NewRequest(method, url, …)`, `http.Get`, `resty`,
   `fetch`, `axios`, `got`, `requests`, generated OpenAPI clients (by
   `operationId`, exact), gRPC/Connect/Twirp stubs (fully-qualified
   `package.Service/Method`, exact). The URL expression resolves via §2.2 to
   `Concat(Env(ORDERS_URL), "/orders/", ?)`.
3. **Join**: `Env(ORDERS_URL)` → `doze.hcl` says `app.orders.url` → target app
   `orders`; then match the path *template* `/orders/*` against `orders`' route
   table segment by segment (literal segments must be equal, a wildcard matches a
   `{param}`; method must match when known). This is the "regex-like signature
   matching" the Cerny-group papers use (path + method + parameter count). When
   two routes match (`/orders/{id}` and `/orders/export`), keep both as candidates
   with lower confidence, never pick one silently.

Expected quality: Prophet-style matching found 88% of calls on train-ticket; the
misses were calls choosing between URLs at run time. With base URLs coming from
doze-declared env vars the target-app step is close to exact; the path step is the
weak one when paths are assembled in helpers.

### 2.6 Kafka, Redis, external services

- **Kafka**: four Go clients (franz-go `kgo.Record{Topic}` / `ConsumeTopics`,
  Sarama `ProducerMessage{Topic}`, segmentio `kafka.Writer{Topic}` /
  `ReaderConfig{Topic, GroupID}`, confluent-kafka-go `TopicPartition{Topic}`), plus
  KafkaJS, the Java client and Spring `@KafkaListener(topics=…)`. Topics are
  usually literals or env vars, so resolution is easy. Consumer group ids are a
  strong "this app consumes" signal.
- **Redis/Valkey**: the server publishes its own operation catalogue. Since
  Redis 7, `COMMAND DOCS`/`COMMAND INFO` give *key specs* per command: where keys
  start (`begin_search` by index or keyword), how to find the rest (`find_keys`),
  and exactly one access flag (`RO`, `RW`, `OW`, `RM`). Generate the client rule
  table from it once. Keys are rarely declared resources, so the useful edge is
  "app → valkey instance, key pattern `session:*` (RW)": normalise key literals the
  way Beyla normalises paths (digits/UUIDs → `*`).
- **External services** (Stripe, Twilio, SendGrid, OpenAI…): static detection by
  SDK import path (`github.com/stripe/stripe-go`, `stripe` on npm/PyPI) is precise
  and cheap; a curated table of ~50 vendor SDKs plus host literals
  (`api.stripe.com`) and conventional env var names (`STRIPE_SECRET_KEY`) covers the
  common cases. Dynamically, doze cannot see these calls today (they go straight to
  the internet over TLS; the `.doze` resolver never sees other names).

### 2.7 Beyond Go

Tree-sitter for syntax (a pure-Go runtime, `gotreesitter`, now exists with 206
grammars; its speed claims are self-reported), then the same catalogue and resolver
over a simpler, syntax-level value domain (literals, `process.env.X`,
`os.environ["X"]`, module-level constants, same-file concatenation). That is
roughly Semgrep-CE-level power and is enough for resource names, which are mostly
literals or env reads. Doubtful cases go to a language server on demand
(`README.md`, open question).

---

## 3. Dynamic: what doze can see cheaply

### 3.1 Where the bytes already pass

| Traffic | Path today | Already parsed | Missing for the join |
|---|---|---|---|
| AWS (SQS, SNS, S3, DynamoDB, …) | app → `aws.<stack>.doze` :80 → doze-aws | everything: SigV4 header (`sigparse`, ~200 ns), service, operation (model route table), resource | per-app identity (all apps share `AWS_ACCESS_KEY_ID=doze`, `internal/endpoints/endpoints.go:158`) |
| Kafka | app → proxy → doze-kafka (own broker) | every request decoded; `protocol.Observation` has `Remote`, `Principal`, `ClientID`, `API`, topics, group, software (`client_software_name`), and an `Observer` interface | mapping connection → app |
| Postgres | app → proxy (`doze-modules/modules/postgres/proxyfilter.go`) → postgres unix socket | startup packet (`user`, `database`), TLS terminated at the proxy; after the handshake the proxy is a plain `io.Copy` splice | `application_name` (one more key in `parseParams`), a read-only tee of `Q`/`P` messages |
| App HTTP | app → :80 ingress (Host routing) → app | Host, method, path | caller identity; route template (comes from the static route table) |
| Valkey/Redis | app → proxy → server (generic splice) | nothing | RESP tee (trivial framing), caller identity |
| External HTTPS | app → internet directly | nothing | only via an opt-in `HTTPS_PROXY` (CONNECT gives host name, no path, without MITM) |

The key point: for AWS and Kafka, doze *is* the server, so "observing the wire"
costs nothing new; the decoding is already paid for. Doze is unusually well placed
here compared with service meshes, which have to re-decode protocols they do not
own.

### 3.2 Per-protocol extraction and cost

- **Postgres.** Messages are framed as a type byte plus an int32 length, so a
  tee that reads frames on the client→server direction costs a header read per
  message. Only `Q` (simple query) and `P` (Parse, extended protocol) carry SQL. With
  the extended protocol, a statement is parsed once per connection and executed many
  times (`B`/`E` carry only the statement name), so SQL text appears rarely; pgx,
  node-postgres with named statements, JDBC all prepare. Simple-protocol queries with
  inlined literals make each text unique, so strip literals before hashing (PgDog
  notes this is the costly case and normalises first). Design: the proxy records only
  `(app, sha of normalised text, first N bytes, count, first/last seen)` into a
  bounded table, never parses; `doze-recall` parses lazily with go-pgquery
  (fingerprint ~16 µs in wasm, once per distinct statement). This keeps pg_query out
  of the daemon and the idle-heap budget. Prior art: Envoy's Postgres filter parses
  `Query`/`Parse` messages and emits `table.db` + operation metadata, "best effort"
  because its parser does not handle all SQL, and does not decode encrypted sessions;
  PgBouncer 1.21+, ProxySQL 3.0.3+ and PgDog all track `Parse` messages for prepared
  statements, so the protocol detail is well trodden.
  Zero-proxy alternative: `pg_stat_statements` inside postgres gives normalised
  queries and counts, but keyed by user and database, not app; useful only with
  per-app database users.
- **Kafka.** Request header = `api_key, api_version, correlation_id, client_id`.
  Produce up to v12 names topics; v13 (Kafka 4.1, KIP-516) uses topic ids, Fetch uses
  ids since v13 too; a third-party proxy must map ids through Metadata, doze-kafka
  already owns the mapping. Envoy's Kafka broker filter only counts per API key; it
  does not expose topics. `client.id` is a poor app identifier by default (Java
  `producer-1`, librdkafka `rdkafka`, franz-go `kgo`), so attribution must come from
  the connection (§3.3); the group id is a good secondary signal.
- **Redis/Valkey.** RESP arrays of bulk strings; command name is element 0, keys
  located by the key specs above. Parsing is a few branches per command. Pipelining
  means many commands per read; the tee must handle partial frames and give up (keep
  splicing) on anything it does not understand.
- **HTTP.** Method, Host, path at the ingress. Map the raw path to a route by
  matching against the callee's static route table (exact, like Beyla/OBI's
  explicit `routes` mode); fall back to a heuristic (segments with digits or
  non-words → `*`, OBI's default "heuristic" decorator, which its docs call best
  effort) only when no table exists. Postman Insights does the same inference with
  user hints and samples traffic with rate limits, a reminder that this needs bounds.
- **AWS.** Operation and resource are known inside doze-aws. JSON-protocol services
  carry `X-Amz-Target: AmazonSQS.SendMessage`; query-protocol carry `Action=`; REST
  services (S3) are route-matched; doze-aws already does all three.

**Risk of breaking traffic.** Keep observation strictly read-only and off the data
path: tee bytes into a bounded buffer handed to a goroutine, drop observations when
it is full, never block or rewrite, and stop observing a connection at the first
framing surprise while continuing to splice. Then the worst case is a missing
observation, not a broken connection.

**TLS.** Not an issue for doze-owned traffic: apps talk plaintext or TLS terminated
at the doze proxy (`internal/proxy/tls.go`). It is the reason eBPF tools (Pixie)
call out TLS and HTTP/2 as limits, and why external calls are invisible without
MITM, which doze should not do.

### 3.3 Attributing traffic to the calling app

Ranked by cost and reliability:

1. **Per-app identity in the env doze already injects** (cheapest, works for
   containers too):
   - AWS: `AWS_ACCESS_KEY_ID=doze-<app>` per app; every SDK signs with it and
     doze-aws parses the `Credential=` scope on every request already. Optionally
     `AWS_SDK_UA_APP_ID=<app>` (supported by Go v2, JS v3, boto3, .NET, Ruby, Rust,
     PHP, C++; Java 2.x only via JVM property; not Go v1/Java 1/JS v2).
     doze-aws ignores the key value, so no breakage.
   - Postgres: `PGAPPNAME=<app>` (libpq and drivers that follow it; an
     `application_name` in the URL wins over it), read from the startup packet the
     proxy already parses. Adding `?application_name=` to the generated URL is
     stronger but riskier (some ORMs validate URL parameters).
   - Kafka: no standard env var for `client.id`; rely on 2.
   - This is "per-app credentials", which `README.md` cuts from v1. In this light,
     only the *identity* half is needed (a different key id string, no
     enforcement), and it is a few lines in `EnvAssignments` once env is per app.
2. **Source port → pid** for loopback TCP, when credentials say nothing (Kafka,
   Redis, HTTP between apps). Doze supervises every app and knows its pid tree.
   macOS: `proc_pidinfo(PROC_PIDLISTFDS)` + `proc_pidfdinfo(PROC_PIDFDSOCKETINFO)`
   over the supervised pids, matching the local port (what `lsof` does; same-user
   processes, no root). Linux: `/proc/net/tcp` inode → `/proc/<pid>/fd`, or netlink
   `sock_diag`. Do it once per connection, asynchronously after accept; connections
   are pooled and long-lived, so even a slow lookup is amortised. For unix-socket
   clients, `LOCAL_PEERPID` (macOS) / `SO_PEERCRED` (Linux) gives the pid directly.
   Fails for apps in Docker on macOS (the source is the VM's address), where 1 still
   works.
3. **Protocol hints** (`client.id`, `client_software_name`, User-Agent, consumer
   group): useful as tie-breakers and for showing the library, not as identity.

eBPF (Pixie: kprobes on send/recv, <2% overhead on non-trivial servers; Caretta,
Kindling, Beyla/OBI) is Linux-only and needs privileges; doze's main platform is
macOS. Not worth it when the proxy and the servers already sit in the path.

### 3.4 OpenTelemetry as an optional input

If an app already exports OTLP, its spans carry exactly the join attributes:
`messaging.destination.name`, `db.collection.name` / `db.query.text` /
`db.query.summary`, `http.route` on server spans, `service.name`. The collector's
servicegraph connector builds edges from client/server span pairs and "virtual
nodes" for uninstrumented peers. Doze could set `OTEL_EXPORTER_OTLP_ENDPOINT` and
accept spans (doze-kafka already speaks OTLP), giving call-site-level attribution
for free where it exists. It should not be required: most dev stacks are not
instrumented, and auto-instrumentation (`NODE_OPTIONS=--require …`,
`-javaagent`) is intrusive.

---

## 4. Merging static and dynamic

Every edge is `(app, verb, resource[, column|route])` with a set of evidence:
static (`file:line`, rule id, resolution chain, commit) and observed (first/last
seen, count, attribution method, the app's commit at the time).

| Static | Observed | Shown as | Notes |
|---|---|---|---|
| resolved | yes, same resource | **proven** | highest trust |
| resolved | not (yet) | **in code** | not exercised in this environment; may be dead code |
| candidates {A, B} | only A | **proven A**, B kept as "in code, unconfirmed" | dynamic disambiguates |
| Unknown at a call site | exactly one resource for that (app, operation) | **likely** binding of the site to the resource | only when the app has a single call site for that operation |
| none | yes | **observed, source not found** | a gap in the rules or a runtime-built name; surfaced to the agent and to the eval as a recall miss |
| resolved to A | B only | **conflict** | usually an env override or drift between `doze.hcl` and the running process; flag, do not merge |

Rules that follow from "no edge rather than a wrong edge":

- Observed edges are facts about behaviour and need no confidence score once
  attribution is by identity (1) or pid (2); attribution by hint (3) is "likely".
- Observations age: keep them per app commit and mark them stale when the code that
  explains them changes (`freshness.md`), and expire unseen ones after N days.
- Observed-only edges are the **free oracle for static recall**: the share of
  observed edges that static also found is a recall estimate measured on the user's
  own stack with no labelling. That is worth more to `eval.md` than any corpus.

---

## 5. Expected accuracy

*Estimates* for a Go stack in doze, to be replaced by eval numbers:

| Edge kind | Static precision | Static recall | Observed precision (with identity) | Observed recall | Merged |
|---|---|---|---|---|---|
| app → AWS resource | 0.95+ | 0.80–0.90 | ~1.0 | what ran (often 40–70% of edges in one dev session) | recall 0.9+, precision ~0.98 |
| app → Kafka topic | 0.95+ | 0.85–0.95 | ~1.0 with pid lookup | what ran | ~0.95 |
| app → table (sqlc / literal SQL) | 0.95+ | 0.70–0.95 depending on ORM | ~1.0 | what ran | 0.9+ |
| app → column | 0.85–0.9 (needs schema) | 0.5–0.8 (ORMs, `SELECT *`) | 0.95 (from parsed wire SQL + schema) | what ran | 0.8+ |
| app → route of app | 0.85–0.9 | 0.75–0.9 | ~1.0 against a known route table | what ran | 0.9 |
| app → external SaaS | 0.95 (SDK import) | 0.8 (raw HTTP clients missed) | n/a without `HTTPS_PROXY` | n/a | static only |

These are consistent with the literature: keyword/rule extraction with config
resolution reaches P ~0.92 / R ~0.83 on connections in Java (Code2DFD), combining
complementary sources lifts recall to ~0.97, and LLM-only recovery sits at F1 ~0.6.
Doze should land above the published static numbers because its env→resource half
is declared rather than inferred, and above any single source because it has both.

---

## Recommendation for recall

**What to do statically (v1, the "proven" layer):**

1. A data-driven **operation catalogue** in CodeQL's row format
   (`package, receiver, method, argument path, kind, resource path, direction`),
   versioned and tested:
   - AWS rows generated from the vendored Smithy models + SAR (72% of all
     resource-scoped operations and all common data-plane calls automatically), with a
     ~10-row override file (SNS `Publish`, Kinesis `PutRecord(s)`, EventBridge
     `PutEvents`, …), checked against doze-aws's own request decoding.
   - Kafka rows for franz-go, Sarama, segmentio, confluent-kafka-go.
   - SQL rows for `database/sql`, pgx, sqlx, GORM, squirrel, sqlc (CodeQL's Go
     `sql-injection` sinks as the checklist).
   - HTTP server rows for `ServeMux` 1.22 patterns, chi, gin, echo; client rows for
     `net/http`, resty; gRPC/Connect stubs.
   - Redis rows generated from `COMMAND DOCS` key specs.
   - External SaaS by SDK import path (~50 vendors).
2. A **value resolver** over `go/types` + SSA with the domain
   `Const | Env | Concat | OneOf | Unknown`, config-library rules (envconfig,
   caarlos0/env, viper, koanf) and field-based summaries for config structs.
3. The **join** with the stack JSON from `fit.md` §3: `Env(X)` → the reference that
   lands in `X` for that app → the declared resource. This is the step no other tool
   has.
4. **SQL** through `wasilibs/go-pgquery` (pure Go, real Postgres grammar); schema from
   an opportunistic live snapshot, falling back to migrations/sqlc schema.
5. Every edge carries its resolution chain as `file:line` evidence; unresolved call
   sites are kept and listed, never guessed.

**What to do dynamically (v1.1, small, mostly in existing code):**

1. Per-app identity in injected env: `AWS_ACCESS_KEY_ID=doze-<app>`,
   `PGAPPNAME=<app>`. Identity only, no enforcement.
2. Record `(app, service, operation, resource, count, first/last seen)` in doze-aws;
   hook doze-kafka's existing `Observer`; add `application_name` to the Postgres
   startup parse and a read-only, bounded `Q`/`P` tee that stores normalised-SQL
   hashes and text for recall to parse later; record `(caller, host, method, path)`
   at the :80 ingress.
3. Source port → pid lookup over supervised pids, once per connection, off the data
   path, for Kafka, HTTP and later Redis.
4. Merge as in §4; report "observed, source not found" edges to the agent and use
   them as the eval's recall oracle.

**Later, only if the eval asks:** RESP tee for Valkey; OTLP ingest for apps that
already export spans; opt-in `HTTPS_PROXY` to see external hosts. **Not at all:**
eBPF, TLS interception, requiring OpenTelemetry, LLM extraction in the proven layer.

**Order:** catalogue + resolver + join for Go AWS/Kafka (the differentiator, and
the easiest to get right) → SQL tables (sqlc and literals) → HTTP routes → per-app
identity and backend observations → columns → other languages over tree-sitter.

**Expected accuracy:** static alone around P 0.95 / R 0.8–0.9 for queue, topic and
table edges in Go, lower for columns and HTTP paths; with observations merged,
recall above 0.9 on edges exercised during development, with precision staying near
1.0 because observed edges are attributed by identity, not inferred. These are
estimates; the first eval run on a real doze stack should replace them, using
observed edges as the ground truth for static recall.

---

## Sources

Architecture recovery
- Schneider et al., *Comparison of Static Analysis Architecture Recovery Tools for Microservice Applications* (EMSE 2025): https://arxiv.org/html/2412.08352 , registered report https://arxiv.org/pdf/2403.06941 , https://link.springer.com/article/10.1007/s10664-025-10686-2
- Schneider & Scandariato, *Automatic Extraction of Security-Rich Dataflow Diagrams for Microservice Applications written in Java* (Code2DFD, JSS 2023): https://arxiv.org/abs/2304.12769
- MicroDepGraph: https://github.com/imranurr/microservices-comm-pattern-check
- *The Microservice Dependency Matrix*: https://arxiv.org/html/2309.02804v1
- Cerny group (Prophet, holistic reconstruction): https://par.nsf.gov/servlets/purl/10393890 , https://www.scitepress.org/Papers/2022/110321/ , search summary of Prophet REST matching (135 found / 18 missed) via https://www.researchgate.net/publication/357990564_Using_Static_Analysis_to_Address_Microservice_Architecture_Reconstruction
- Microvision: https://arxiv.org/pdf/2207.02974
- MDPI 2024, *Assessing Evolution of Microservices Using Static Analysis*: https://www.mdpi.com/2076-3417/14/22/10725 (403 to fetch; cited from search summary)
- Liljas et al., LLM architecture recovery (ESEM 2026): https://drops.dagstuhl.de/entities/document/10.4230/LIPIcs.ESEM.2026.62

Static analysis frameworks
- CodeQL Go models-as-data: https://codeql.github.com/docs/codeql-language-guides/customizing-library-models-for-go
- CodeQL Go model files: https://github.com/github/codeql/tree/main/go/ql/lib/ext
- CodeQL JS `ClientRequest`: https://codeql.github.com/codeql-standard-libraries/javascript/semmle/javascript/frameworks/ClientRequests.qll/type.ClientRequests$ClientRequest.html
- CodeQL Go SqlInjection: https://codeql.github.com/codeql-standard-libraries/go/semmle/go/security/SqlInjection.qll/module.SqlInjection.html
- Semgrep constant propagation: https://docs.semgrep.dev/writing-rules/data-flow/constant-propagation ; taint mode: https://docs.semgrep.dev/writing-rules/data-flow/taint-mode/overview
- Backstage Component entity: https://backstage.io/api/stable/interfaces/_backstage_catalog-model.index.ComponentEntity.html
- CfgNet (TSE 2023): https://sws.informatik.uni-leipzig.de/wp-content/uploads/2023/06/CfgNet_main_2023.pdf
- gotreesitter: https://pkg.go.dev/github.com/odvcencio/gotreesitter@v0.6.0

String and SQL extraction
- Stringoid, *Who you gonna call?*: https://ar5iv.labs.arxiv.org/html/1705.06629
- Meurice et al., *Static Analysis of Dynamic Database Usage in Java Systems* (CAiSE 2016): https://www.academia.edu/144649372/Static_Analysis_of_Dynamic_Database_Usage_in_Java_Systems
- SQL identifier concatenation study: https://cse.usf.edu/~ligatti/papers/sql-idivs-jrnl.pdf
- SQLInspect: https://computer.org/csdl/proceedings-article/icse-companion/2018/566301a093/13bd1gzWkRM

SQL parsers
- pg_query_go: https://github.com/pganalyze/pg_query_go
- go-pgquery (wasm): https://pkg.go.dev/github.com/wasilibs/go-pgquery
- cockroachdb-parser: https://github.com/cockroachdb/cockroachdb-parser
- ValkDB postgresparser: https://dev.to/eitamring/building-a-pure-go-postgresql-sql-parser-no-cgo-no-server-no-runtime-dependencies-goi , https://github.com/ValkDB/postgresparser
- Vitess parser forks: https://pkg.go.dev/github.com/88250/vitess-sqlparser , https://best-of-web.builder.io/library/xwb1989/sqlparser
- sqlc and pg_query: https://golab.io/talks/sqlc-deep-dive-how-sql-becomes-type-safe-go-code

AWS
- Service Authorization Reference JSON: https://servicereference.us-east-1.amazonaws.com/v1/sqs/sqs.json (and sns, s3, dynamodb, kinesis, lambda, events, states, secretsmanager, ssm)
- Smithy models vendored at `doze-aws/.audit-models/`
- Application ID (`AWS_SDK_UA_APP_ID`): https://docs.aws.amazon.com/sdkref/latest/guide/feature-appid.html

Wire protocols and proxies
- Envoy Postgres filter: https://www.envoyproxy.io/docs/envoy/latest/configuration/listeners/network_filters/postgres_proxy_filter , https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/other_protocols/postgres
- Envoy Kafka broker filter: https://www.envoyproxy.io/docs/envoy/latest/configuration/listeners/network_filters/kafka_broker_filter
- PgBouncer prepared statements: https://github.com/topicusonderwijs/pgbouncer-ps-patch ; ProxySQL: https://proxysql.com/documentation/pgsql-extended-query-architecture ; PgDog: https://docs.pgdog.dev/features/prepared-statements/ , https://docs.pgdog.dev/features/sharding/dry-run/
- Kafka protocol: https://kafka.apache.org/0110/generated/protocol_messages.html , Produce versions: https://kafka-options-explorer.conduktor.io/wire-format/Produce/changelog
- Redis key specs: https://redis.io/docs/latest/develop/reference/key-specs/ , Valkey: https://valkey.io/topics/key-specs/
- libpq env vars (`PGAPPNAME`): https://www.postgresql.org/docs/16/libpq-envars.html ; precedence: https://postgres.ai/docs/postgres-howtos/development-tools/client-tools/how-to-set-application-name-without-extra-queries
- Node env proxy: https://nodejs.org/learn/http/enterprise-network-configuration

Observability
- Pixie eBPF protocol tracing (LPC 2021): https://lpc.events/event/11/contributions/945/ , https://docs.px.dev/about-pixie/pixie-ebpf/
- Caretta: https://www.cncf.io/blog/2023/01/23/navigate-your-way-to-production-bliss-with-caretta/
- Beyla / OBI routes decorator: https://grafana.com/docs/beyla/next/configure/routes-decorator/ , https://opentelemetry.io/docs/zero-code/obi/configure/routes-decorator/index.md
- Postman Insights endpoint inference: https://learning.postman.com/docs/live-insights/live-insights-early-access
- OTel servicegraph connector: https://grafana.com/docs/alloy/next/reference/components/otelcol/otelcol.connector.servicegraph.md
- OTel semantic conventions summary: https://uptrace.dev/opentelemetry/semconv
- macOS socket → pid: https://developer.apple.com/forums/thread/728731 , https://github.com/ohadravid/netstat2-rs/blob/master/README.md

Doze code read for this report
- `doze/internal/proxy/proxy.go` (handle, splice), `doze/internal/endpoints/endpoints.go:111-170` (shared AWS credentials), `doze/internal/daemon/awsingress.go`, `daemon.go:277` (:80 ingress)
- `doze-modules/modules/postgres/proxyfilter.go` (startup parse, `parseParams`)
- `doze-aws/internal/sigparse`, `doze-aws/awsident/ident.go`
- `doze-kafka/internal/protocol/observe.go` (`Observation`, `Observer`), `doze-kafka/internal/traffic`
- Measurement scripts (bun): scratchpad `opcat.ts`, `opcat2.ts`
