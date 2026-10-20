# Research 7: the eval stack, port or build (decision V4)

> **Superseded in part, 2026-10-20.** Three later decisions change this
> report's conclusions; the survey and the risks in §4 still stand.
>
> - **No live backends** (R94): tasks are scored by unit and contract tests
>   with nothing started, so the porting costs below for wiring Postgres,
>   Kafka and testcontainers to doze no longer apply.
> - **E2 is sized at about 325 cross-app tasks** ([eval.md](../eval.md) §8),
>   not the 100 to 300 below.
> - **Agents write the tasks** (V3), so the "2–4 hours per task" person-hours
>   in §4 become agent time plus about a day of owner review per 20 tasks.
> - **V4 is "build our own"**, not the hybrid recommended in §5. The stack is
>   `parcel` ([eval.md](../eval.md) §2), built with §4's mitigations. Jitsu
>   and Hatchet are used as the realism yardstick and as the first candidates
>   for an external-validity run after E2.

Question: should the recall eval run on a stack built from scratch by AI agents,
or on an existing open-source system ported into a `doze.hcl`? Which open-source
systems fit?

Date: 2026-10-20. Scope as revised on the same day: recall v1 indexes **Go,
TypeScript/JavaScript (one pack), Python and Rust**. The pilot (E1) runs once
Go and TS/JS are done, so its stack needs **Go and TS/JS services that talk to
each other**. Python and Rust services join for E2. Services in C#, Java,
Kotlin, Ruby, PHP, C++ or Elixir are not indexed and count against a candidate.

Criteria are from [eval.md](../eval.md) §2 ("three or more services, two kinds
of link, tests that run under doze, a history of commits that changed more
than one service, and a license that allows it"), plus size, contamination and
porting effort. §8 sets the scale: 20 cross-app tasks for E1, 100 to 300 for E2.

Numbers marked *measured* were computed for this report from blobless clones
taken on 2026-10-20. Numbers marked *estimate* are mine. GitHub stars and
licences come from the GitHub API on the same day.

---

## 0. The short answer

1. **No open-source system covers what the main corpus needs.** That means Go
   and TS services linked through doze's own modules: SQS/SNS/S3 through
   doze-aws, Kafka topics and Postgres tables. Of the 24 repositories checked,
   none uses the AWS emulator surface in a meaningful way (Highlight uses S3; OpenMeter's
   AWS imports come in through a connector library). Most polyglot showcases
   put their queue consumers in languages recall doesn't index.
2. **Three real systems are good Go + TS fits.** All are MIT or Apache, active
   or recently frozen, mid-to-large, with real tests and many multi-component
   commits:
   - **Jitsu** (Go bulker/ingest/syncctl and TS console/rotor, linked by Kafka
     and Postgres). It is the best fit for E1.
   - **Hatchet** (Go engine and API, TS frontend and SDK, Python SDK, linked by
     REST, gRPC and Postgres). It is the easiest to port and brings Python
     for E2.
   - **Highlight** (Go backend and TS frontend, with the most Go↔TS
     co-changes, Kafka, Postgres, Redis and S3). It needs ClickHouse.
3. **Real history yields fewer tasks than it seems.** Commits that touch two or
   more components, are not chores and include a test file number 160–200 per
   repository (*measured*, upper bound). Many will fail the validity checks
   (hidden test in the consuming app, a partial patch passing visible tests).
   Commits that change Go and TS together are rare where the link is a topic:
   59 of 3,704 in Jitsu. Mined tasks can't reach E2's 230–300. Written tasks
   are needed whichever way V4 goes, and at 2–4 hours each (V3) they cost more
   than the stack.
4. **Recommendation: a hybrid.** Port **Jitsu** now as the E1 stack: real Go↔TS
   Kafka contracts and real mess, in days rather than weeks. Build the **main
   corpus stack with agents** (Go + TS first, Python + Rust for E2) around
   doze-aws, Kafka and Postgres, with the guards in §4. Keep **Hatchet** (and
   optionally Highlight) as the real-stack external-validity set for E2. Mine
   tasks only from commits newer than the tested model's training cutoff, and
   report them apart from written ones (§3).

---

## 1. Candidates, measured

How these were measured:

- Every repository was cloned blobless at HEAD on 2026-10-20.
- **LOC** counts hand-written lines per language. Files under `vendor/`,
  `node_modules/`, `dist/`, `build/`, `generated/` and `migrations/` are
  skipped, as are files whose header says "Code generated", "@generated" or
  "auto-generated", and the usual generated names (`*.pb.go`, `*_gen.go`,
  `*.d.ts`, `*_pb2.py`, mocks).
- **Multi-component commits** are non-merge commits whose changed source files
  fall in two or more of the components listed in the table.
- **Feature-like with tests** is the subset whose subject has none of chore,
  docs, ci, build, bump, deps, lint, format, refactor, rename, license,
  revert or release, and which changes at least one test file. It is an upper
  bound on minable tasks.
- **Test funcs** counts `func Test…` in `_test.go` files.

The Bun scripts were run from the session scratchpad and are not kept. They
are short enough to redo from this description.

### 1.1 Comparison

| # | Repository | Indexed languages (hand LOC, +tests) | Unindexed | Services and links | Backends: doze fit | Tests | History: commits / multi-component / feature-like with tests | License | Stars | Port effort (*estimate*) |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | **jitsucom/jitsu** | Go 73k (+15k), TS 113k (+38k) | none | Go `bulker`, `ingest`, `syncctl`, `admin`, reprocessing worker. TS `console` (Next.js), `rotor`, `retl-runner`. **Kafka** ingest→rotor→bulker (Go→TS→Go), **HTTP** console↔bulker/syncctl, **shared Postgres** config | Postgres ✓, Kafka (Redpanda) ✓. Mongo and ClickHouse are optional in rotor's env schema (`serverEnv.ts`). Mongo could go to ferret (compat unverified) | 82 Go `_test.go`, bulker integration through testcontainers. 160 TS test files, Jest e2e for rotor | 3,704 / 507 / **166**; Go+TS together 59; 1,631 in the last 12 months | MIT | 5.1k | **4–8 days**: kafka + postgres modules, topics in `doze.hcl`, point testcontainers tests at doze endpoints, pnpm/Next dev for console |
| 2 | **hatchet-dev/hatchet** | Go 196k (+72k), TS 143k (+13k), Py 120k (+13k) | Ruby SDK 100k (drop it) | Go `hatchet-engine` (dispatcher, scheduler, controllers, ingestor, ticker), `hatchet-api`. TS frontend (REST, OpenAPI client) and TS SDK workers (**gRPC**). Py SDK workers (gRPC). **Shared Postgres**. MQ is Postgres (lite default), RabbitMQ or NATS | **Postgres only** ✓ (`SERVER_MSGQUEUE_KIND=postgres`) | 1,503 Go test funcs: 84 untagged files, 13 `integration` (Postgres), 11 `e2e`. TS and Py SDK e2e run against a live engine, which suits hidden tests | 3,738 / 626 / **172**; api+frontend 61, api+repo+frontend 52; 1,803 in the last 12 months | MIT | 8.2k | **3–6 days**: engine + api + frontend + example workers. Large codebase; Go↔TS links go through generated clients |
| 3 | **highlight/highlight** | TS 213k (+8k), Go 64k (+12k), small Py, Rust and Go SDKs | Java, .NET, Ruby, PHP SDKs (drop them) | One Go backend binary run as `public-graph`, `private-graph` and `worker` (**Kafka** between them). TS frontend (**GraphQL**, codegen). SDKs post to public-graph | Postgres ✓, Kafka ✓, Redis ✓, **S3 via doze-aws** ✓. **ClickHouse required** (service command, heavy) | Go 12k test LOC, TS 8k. Cypress e2e | 4,591 since 2022 / 740 / **204**; **Go+TS 617**. Wound down: 21 commits in 12 months, landing page redirects to LaunchDarkly (Aug 2026) | Apache-2.0 outside `highlight.io/` and `enterprise/` | 9.4k | **1–2 weeks**: ClickHouse service, OTel collector, frozen upstream |
| 4 | hitesh22rana/chronoverse | Go 33k (+56k), TS 14k (+10k) | none | 13 Go services and workers: HTTP gateway, users, workflows, jobs, notifications, analytics, scheduler, executor, outbox relay. **gRPC + Kafka (franz-go) + Postgres + Redis**. TS dashboard | Postgres ✓, Kafka ✓, Redis ✓. **ClickHouse, Meilisearch and a Docker-socket runtime** needed. mTLS everywhere | 839 Go test funcs | 302 / 52 / 38. One person (two author names). Recent history looks agent-assisted (12 commits with AI trailers, uniform "test: cover …" stream) | MIT | **16** | 1–2 weeks: three service commands, TLS stripping. Best shape and lowest contamination, but carries much of build-with-agents' uniformity risk |
| 5 | goharbor/harbor | Go 127k (+93k), TS 52k (+23k, Angular) | Robot Framework tests (Python) | `core`, `jobservice`, `registryctl`, exporter, plus upstream `distribution` registry and Trivy. **HTTP + Redis job queue (gocraft/work) + shared Postgres** | Postgres ✓, Redis ✓. Registry, S3 storage and scanners as services | 2,798 Go test funcs, many need Postgres + Redis | 1,858 since 2022 / 229 / 161. Go+TS only 7 in 12 months | Apache-2.0 | 29.6k | 1–2 weeks. CNCF graduated, very likely in training data |
| 6 | openmeterio/openmeter | Go 290k (+299k). TS and Py are generated API clients | none | `server`, `sink-worker`, `balance-worker`, `billing-worker`, `notification-service`, `jobs`. **Kafka + shared Postgres** | Kafka ✓, Postgres ✓. **ClickHouse required**, Svix, Redis | Very large Go suite | 6,149. Go+TS commits are client regeneration | Apache-2.0 | 2.4k | 1–2 weeks. Go-only in practice, and too big for E1 |
| 7 | open-telemetry/opentelemetry-demo | Go 2k (checkout, product-catalog), TS 6k (frontend, RN app), JS payment, Py (agent, chatbot, mcp, load-gen), Rust 1k (shipping) | **C#** accounting and cart, **Java** ad and recommendation, **Kotlin** fraud-detection, **C++** currency, **Ruby** email, **PHP** quote, **Elixir** flagd-ui: about half the services | gRPC everywhere. **Kafka checkout(Go)→accounting(C#), fraud-detection(Kotlin)**: the queue link ends in unindexed code. Postgres used by product-catalog (Go) and accounting (C#). Valkey only through C# cart | Kafka ✓, Postgres ✓, Valkey ✓. flagd, collector, OpenSearch, Jaeger and Grafana as services | 16 Go test funcs, trace-based tests in `compose.tests.yaml` | 2,371 / Go-Go 17. Mostly dependency bumps (the last 8 commits are all bumps) | Apache-2.0 | 3.4k (plus every OTel tutorial) | 1 week. Tiny per service, famous, the queue's far side unindexed. **Poor** |
| 8 | GoogleCloudPlatform/microservices-demo | Go 3k (frontend, checkout, productcatalog, shipping), Py 1k (email, recommendation, shopping assistant, load-gen), JS 1k (currency, payment) | **C#** cart, **Java** ad | **gRPC only**. Redis only through C# cart. No queue | Valkey ✓ | 41 Go test funcs | 2,699 / 44 Go multi. Mostly bumps and infra | Apache-2.0 | 21k | 3–5 days. One link kind, very famous. **Poor** |
| 9 | ThreeDotsLabs/wild-workouts-go-ddd-example | Go 5k (+2k), TS/JS 5k (Vue web) | none | `trainer`, `trainings`, `users` + web. **HTTP (OpenAPI) + gRPC** | **Firestore** (emulator service, Java/gcloud) + MySQL (mariadb ✓) | 51 Go test funcs, component tests need the Firestore emulator | 65 / 19 / 11 | MIT | 6.5k, backs a well-known blog series and ebook | 3–5 days, or rewrite storage to Postgres. Too small, too famous |
| 10 | temporalio/reference-app-orders-go | Go 3k (+1k) | none | billing, order, shipment, fraud in one `oms` binary. **HTTP + Temporal workflows**. Web UI is a separate repo (TS, 11 stars) | **Temporal module ✓**, SQLite/Mongo | 15 Go test funcs | 434 / 22 | MIT | 84 | 2–3 days. Small. Useful as the Temporal module's smoke stack, not as the corpus |
| 11 | Mikaelemmmm/go-zero-looklook | Go 5k (+8.5k goctl-generated) | none | 8 app dirs (usercenter, travel, order, payment, mqueue…). **zrpc gRPC + Kafka (go-queue) + asynq/Redis + MySQL** | mariadb ✓, valkey ✓, kafka ✓ | **2 test funcs** | 252 / ~24 / 0 | MIT | 5.2k | 3–5 days, but tests would all have to be written |
| 12 | mehdihadeli/go-food-delivery-microservices | Go 31k (+12k) | none | catalog-write, catalog-read, orders. **gRPC/HTTP + RabbitMQ + Postgres + Mongo + EventStoreDB** + Elasticsearch + Redis | Postgres ✓, Redis ✓, ferret?. **RabbitMQ, EventStoreDB, Elasticsearch** as services | 196 Go test funcs (unit, integration, e2e through testcontainers) | 244 / 17 / 9. Dormant since 2024-09 | MIT | 1.1k | 1–2 weeks. Three heavy services, thin history |
| 13 | rasadov/EcommerceAPI | Go 4k (+3k), Py 1k (recommender) | none | account, order, payment, product, recommender + GraphQL gateway. **gRPC + Kafka (sarama) + Postgres + Elasticsearch** | Postgres ✓, Kafka ✓. Elasticsearch as a service | 46 Go test funcs | 114 / 47 | Apache-2.0 | 696 | 3–5 days. Small |
| 14 | minghsu0107/go-random-chat | Go 6k, JS 1k | none | chat, forwarder, match, uploader, user, web. **gRPC + Kafka (Watermill) + Redis + Cassandra + S3** | Kafka ✓, Valkey ✓, S3 ✓. **Cassandra** | **0** | 364 / 29 | MIT | 426 | Tests would all have to be written |
| 15 | PacktPublishing/Event-Driven-Architecture-in-Golang | Go ~12k per chapter (127k across 11 chapter copies) | none | MallBots, 9 modules. **gRPC + NATS JetStream + Postgres** | Postgres ✓. **NATS** as a service (single binary, low effort) | 129 test funcs incl. contract and component tests | **35 commits (chapter snapshots): no usable history** | MIT | 460 (a book) | 3–5 days. No history |
| 16 | jaegertracing/jaeger | Go 65k (+105k), Py 4k | UI is a separate repo (TS) | v2 is one binary (OTel collector) configured as collector, ingester or query. **Kafka** collector→ingester, gRPC remote storage | Kafka ✓, memory/Badger storage ✓. Cassandra, ES and ClickHouse optional | 2,931 Go test funcs | 4,920 / role-level co-change hard to separate | Apache-2.0 | 23.3k | 3–5 days. One binary, infrastructure rather than business apps, famous |

Also opened and set aside:

| Repository | Why it was set aside |
|---|---|
| ThreeDotsLabs/watermill | A library. `_examples/` are one-to-three-program demos, not a stack |
| go-kit/examples | 8k LOC, 2 test funcs, last commit 2021-11 |
| temporalio/samples-go | 96 independent samples, not interacting services |
| delimitrou/DeathStarBench (hotelReservation) | Go 11k, 10 gRPC services + Mongo + memcached + consul, **0 Go tests** |
| stackus/ftgogo | **No license**, 18 commits |
| AleksK1NG/Go-CQRS-Kafka-gRPC-Microservices | **No license**, 0 tests, 41 commits in three weeks of 2021 |
| formancehq/stack | Emptied; components moved to separate repos (141 Go LOC left) |
| aws-containers/retail-store-sample-app | MIT-0, but Java 15k vs Go 1k (catalog) and TS 3k (checkout) |
| PostHog/posthog | MIT outside `ee/`. Python + TS + Rust with Kafka, Postgres, Redis, ClickHouse and S3, but Go is only `livestream` (183 Go files vs 24,736 Python). ~40k commits in 12 months. ClickHouse required. Far too big. Only worth a look as an E2 Python/Rust source |
| SigNoz/signoz | Go 243k + TS 421k, but essentially one server + frontend; ClickHouse required; ingest collector in another repo |
| Grafana Tempo / Loki / Mimir, coder/coder, zitadel/zitadel | **AGPL-3.0** |
| openreplay/openreplay | AGPL-3.0 outside the MIT directories |
| kubeshop/testkube | Mixed Testkube Community License + MIT |
| inngest/inngest | GitHub reports no OSI licence (NOASSERTION); not examined further |
| woodpecker-ci, kubeshop/tracetest, netbird, bluesky atproto+indigo | Checked by metadata only: weak TS share in-repo, no queue, or split across repos (no shared history) |

### 1.2 Go unit tests run locally (*measured*)

Run with `go test -count=1` on this machine: Go 1.27.2, no Docker, no
backends.

| Repository | Scope | Result |
|---|---|---|
| Jitsu | `bulker/{jitsubase,kafkabase,bulkerlib/types,ingest}` with `-short` | 13 packages ok, 0 failed |
| Chronoverse | `./...` | 50 packages ok. One failed: `TestDNSPollBuilder_ResolvesAddresses` timed out resolving `localhost`, a sandbox or DNS quirk |
| Hatchet | `./pkg/... ./internal/...`, no build tags | At least 29 packages ok (output was truncated to its last 400 lines). Four failed (`pkg/repository`, `pkg/testing/harness`, `internal/services/grpc`, `internal/services/ticker`). The failures shown are testcontainers calls ("rootless Docker not found"): even Hatchet's *untagged* tests start Postgres through testcontainers. The port must point that helper (`pkg/repository/task_partition_test.go:48`) at doze's Postgres |

Testcontainers is the common obstacle. Jitsu's bulker integration tests,
Hatchet's repository tests and food-delivery all start their own containers.
Rewiring them to doze endpoints is a fixed cost of every port. It is also the
point: the rewired tests become the visible and hidden tests that run against
the live stack.

### 1.3 Scores

0 = fails, 1 = weak, 2 = adequate, 3 = strong. "Languages" is the share of
services in indexed languages and whether Go↔TS links exist (the revised
scope). "Contamination" is scored high when the risk is *low*. "Effort" is
scored high when the port is *cheap*.

| Repository | 1 Services | 2 Link kinds | 3 Tests | 4 History | 5 License | 6 Size fit | 7 Contamination | 8 Effort | Languages | Total /27 |
|---|---|---|---|---|---|---|---|---|---|---|
| **Jitsu** | 3 | 3 (Kafka, HTTP, shared PG) | 2 | 2 | 3 | 3 | 2 | 2 | 3 (Go↔TS over Kafka) | **23** |
| **Hatchet** | 3 | 3 (REST, gRPC, shared PG, PG-MQ) | 3 | 3 | 3 | 2 (large) | 2 | 3 | 2 (Go, TS, Py; Ruby SDK dropped) | **24** |
| **Highlight** | 2 (one binary, three roles + frontend) | 3 (Kafka, GraphQL, PG, S3) | 1 | 3 (frozen) | 3 | 2 | 2 | 1 (ClickHouse) | 3 | **20** |
| Chronoverse | 3 | 3 | 3 | 1 | 3 | 3 | 3 | 1 | 2 (TS = dashboard only) | 22 |
| Harbor | 3 | 3 | 3 | 2 | 3 | 2 | 0 | 1 | 2 | 19 |
| OpenMeter | 3 | 2 | 3 | 2 | 3 | 1 (huge) | 2 | 1 | 0 (Go only) | 17 |
| OTel demo | 3 | 3 | 1 | 1 | 3 | 1 (tiny) | 0 | 2 | 1 (half unindexed, queue ends in C#/Kotlin) | 15 |
| microservices-demo | 3 | 1 (gRPC only) | 1 | 1 | 3 | 1 | 0 | 2 | 1 | 13 |
| wild-workouts | 2 | 2 | 2 | 1 | 3 | 1 | 0 | 2 | 2 | 15 |
| food-delivery | 2 | 3 | 2 | 1 | 3 | 2 | 2 | 1 | 0 | 16 |
| looklook | 3 | 3 | 0 | 0 | 3 | 1 | 1 | 2 | 0 | 13 |
| Temporal OMS | 2 | 2 | 1 | 1 | 3 | 1 | 3 | 3 | 0 | 16 |

Hatchet edges Jitsu on points, but Jitsu fits E1 better. Recall's distinctive
claim is the stack join: an app → topic declared in `doze.hcl` → another app in
another language. Jitsu's Go→TS→Go Kafka pipeline exercises exactly that.
Hatchet's Go↔TS links go through generated OpenAPI and protobuf clients, and
its queue is internal to its own Postgres schema, invisible as a doze resource.

---

## 2. The three real fits, in more detail

### Jitsu (MIT, github.com/jitsucom/jitsu)

- **Shape.** An event pipeline, an open-source Segment alternative.
  - `ingest` (Go) accepts events and produces to Kafka.
  - `rotor` (TS) consumes them, runs user functions and produces again.
  - `bulker` (Go) batches into destinations.
  - `console` (Next.js, TS) owns configuration in Postgres and serves it to
    the Go services over HTTP.
  - `syncctl` (Go) runs connector syncs.
  - The Go side has been in the main repo since 2023-06, when `bulker` was
    merged in. The standalone jitsucom/bulker repo is archived.
- **Doze fit.** Postgres and Kafka are core. Mongo (profiles) and ClickHouse
  (metrics) are `optional()` in rotor's environment schema. A first port can
  leave them out, or run Mongo on ferret, whose compatibility is untested here.
  The compose file's dev profile already runs Go and Node from source, which is
  close to how doze runs apps. `ingest` reads its stream configuration from
  `REPOSITORY_URL`, the console's repository endpoint
  (`bulker/ingest/repository.go`). That is a Go→TS HTTP dependency declared
  in env, the kind `doze.hcl` wiring makes explicit.
- **Tasks.** About 166 feature-like multi-component commits with tests is the
  upper bound. Go+TS co-changes are rare (59), so cross-language tasks will be
  mostly *written*, for example "add a field to the ingest event and use it in
  rotor's function context and bulker's table schema". The real code supplies
  the realism; the written task supplies the cross-app change.
- **Contamination.** 5.1k stars and public since 2022, so earlier code is
  likely in training data. 1,631 commits landed in the last 12 months, so a
  post-cutoff mining window exists.
- **Watch.** One maintainer writes most recent commits (1,686 of the last
  two years' ~2,350), and the repo carries `AGENTS.md` and `CLAUDE.md`, so
  part of the recent code is agent-assisted.

### Hatchet (MIT, github.com/hatchet-dev/hatchet)

- **Shape.**
  - `hatchet-engine` (Go) holds several internal services: dispatcher,
    scheduler, controllers, ingestor, ticker.
  - `hatchet-api` (Go, REST) shares Postgres with the engine (sqlc).
  - The React frontend (TS) calls the API through a generated client.
  - Worker SDKs in TS, Python and Go connect to the engine over gRPC.
  - Example workers in `examples/` and `sdks/*/examples` are ready-made "user
    apps".
- **Doze fit.** Runs on **Postgres alone** (`SERVER_MSGQUEUE_KIND=postgres`,
  which `hatchet-lite` sets by default). This is the cheapest port in the set.
- **Tests.** Go unit tests are guarded by build tags; `integration` needs
  Postgres. The TS and Python SDKs have e2e suites that run against a live
  engine, the natural form of a hidden test in the consuming app.
- **For E2.** The Python SDK (120k LOC) adds an indexed language with a real
  gRPC contract to Go. The Ruby SDK (100k) must be removed from the port, or
  it pollutes the unindexed share.
- **Watch.**
  - Large: Go 196k plus TS 143k.
  - Commit history starts 2023-12, so most of it is new.
  - The repo has `AGENTS.md` and an AI policy.

### Highlight (Apache-2.0 outside two directories, github.com/highlight/highlight)

- **Strengths.**
  - The richest Go↔TS co-change history: 617 commits changed both the Go
    backend and the TS frontend since 2022, typically a GraphQL schema change
    plus its UI.
  - It uses S3, which exercises doze-aws.
  - The upstream is frozen, so a pin never drifts.
- **Weaknesses.**
  - ClickHouse is required.
  - The backend is one Go binary started in three roles. Recall would see one
    app unless `doze.hcl` declares three apps with different args, which is
    realistic but thin.
  - Go↔TS links go through GraphQL codegen.
  - Test suites are modest (Go 12k test LOC).

---

## 3. What porting means, in general

For any of these, a port is:

1. **Pin a commit** and fork into a private eval repo, keeping history so
   tasks can be mined.
2. **Write `doze.hcl`**: one `app` per binary or role, modules for Postgres
   and Kafka (topics declared, so recall can join on them), `service` blocks
   for anything doze lacks (ClickHouse, NATS, RabbitMQ, Elasticsearch,
   Cassandra, Meilisearch). Each service block costs half a day to two days
   to get a reliable binary and health check (*estimate*). ClickHouse and
   Elasticsearch are the heavy end.
3. **Replace testcontainers and docker-compose test setup** with doze
   endpoints from env. This is the biggest hidden cost in Jitsu, food-delivery
   and Chronoverse.
4. **Strip what recall can't index** (other-language SDKs, Java/C# services)
   or note it as noise.
5. **Strip production hardening** that fights local runs (mTLS in
   Chronoverse, Kubernetes-only paths).
6. **Check the licence obligations.** All three fits are MIT or Apache-2.0.
   Using them for an internal eval is fine. Publishing tasks or patches
   derived from them needs the licence and NOTICE carried along. Highlight's
   `highlight.io/` and `enterprise/` directories are under other terms and
   should be deleted from the fork.

**Contamination handling for any port.**
- Mine tasks only from commits **after the tested model's training cutoff**.
  Hatchet (1,803) and Jitsu (1,631) each landed well over a thousand commits
  in the last 12 months; Highlight (21) did not, so Highlight's mined tasks
  are all pre-cutoff for current models.
- Write new tasks against the pinned commit, not reconstructed from public
  PRs.
- Report mined, written and built-stack tasks separately (§11 of eval.md
  already asks for this).

---

## 4. The build-with-agents option, assessed for Go + TS (later Python + Rust)

What it would be: 5–8 services, Go and TS first, then a Python and a Rust
service, around doze-aws (SQS, SNS, S3, maybe DynamoDB), Kafka and Postgres.
Real queues, topics and tables, declared in `doze.hcl`, tests that run under
doze. This is the only way to cover doze-aws and to control size, link mix and
language mix exactly.

### Risks, frankly

| Risk | Why it matters for this eval |
|---|---|
| **Uniform code** | One model family writes the services in one style: the same names, layering, error wrapping and env-var reading. Finding the consumer of a topic becomes a grep for a predictable name. The A0 baseline does better than it would on real code, and recall's measured lift shrinks. The bias is conservative, but the result says less about real stacks |
| **Too clean** | Real stacks have duplicated helpers, two clients for the same queue, config read in three places, stale dead code, generated clients, half-finished migrations. These are exactly where an agent misses a required site. A clean stack has fewer of them, so fewer tasks fail at baseline, and the effect is harder to detect at a given n |
| **Matches what agents expect** | If the writing model and the tested model share a family (Claude writing, Claude Code on Sonnet testing), the stack is in-distribution. The agent "guesses right" where a human-written system would surprise it |
| **Synthetic history** | No organic multi-service commits. Mined tasks are really written tasks (eval.md already reports them apart). There is no evidence about change-coupling patterns in real teams |
| **Unverified realism** | Without a yardstick, "realistic" is self-judged. Memory: *self-judged fidelity is too generous* (21/21 became 3/19 under an independent audit) |
| **Contamination** | Low, which is the main advantage, as long as the stack stays private |

### Mitigations

1. **Measure realism against the real fits.** Compute the same structural
   metrics on the built stack and on Jitsu and Hatchet: file-size
   distribution, functions per file, duplication ratio, distinct ways of
   reaching the same resource (env var, config struct, constant, generated
   client), call depth from handler to queue call, share of generated code.
   Set targets from the real repos and have someone other than the builders
   audit them.
2. **Mix authors.** Write different services with different models (Claude,
   Codex/GPT, Gemini), different prompts and different library choices: sarama
   vs franz-go vs kafkajs, pgx vs database/sql vs Prisma vs Drizzle,
   aws-sdk-go-v2 vs AWS SDK v3. Have one person hand-write at least one service.
3. **Inject mess on purpose, with a catalogue.**
   - A second consumer of a topic in an unexpected app.
   - A queue URL built from parts.
   - A topic name in a shared constants package *and* a literal.
   - A generated OpenAPI client between Go and TS.
   - A table read by two apps through different ORMs.
   - A feature flag that hides a call site.
   Each item is a known trap that a required-site list can target.
4. **Grow it as history, not as one drop.** Build v0 small, then evolve it
   through 50–150 feature changes written from issue-style specs. Each lands
   as a commit with tests, as a team would. This yields a real-looking log
   (still synthetic) and makes the codebase layered by time, with older
   patterns left behind.
5. **Keep the tested model out of authorship** where possible. At least, don't
   let the exact model and harness under test write the services its tasks
   touch.
6. **Keep it private** and never publish its task set, or the contamination
   advantage disappears.

### Effort (*estimate*)

| Piece | Effort |
|---|---|
| Design: services, contracts, mess catalogue, realism targets measured on Jitsu and Hatchet | 3–5 days |
| v0: 5–6 Go + TS services, aws + kafka + postgres, tests running under doze | 1.5–3 weeks of agent work plus review |
| Evolution: 50–150 feature commits with tests | 2–4 weeks |
| Python + Rust services for E2 | +1.5–3 weeks |
| Independent realism audit and fixes | 3–5 days |
| **Total** | **about 6–11 weeks to an E2-ready stack**. Go + TS, enough for E1, is about 4–6 weeks |

For comparison: porting Jitsu is about 1–2 weeks to E1-ready, including test
rewiring. **Task writing dominates either way.** 230–300 tasks at 2–4 hours is
460–1,200 person-hours. 100 tasks is 200–400 hours.

---

## 5. Recommendation

**A hybrid, staged by eval step.**

| Step | Stack | Why |
|---|---|---|
| **E1 (pilot, Go + TS)** | **Port Jitsu** (pinned; Postgres + Kafka; Mongo and ClickHouse left out). 20 cross-app tasks, mostly written against real Go↔TS Kafka contracts, plus any post-cutoff mined commits that pass the validity checks | Real, messy, non-uniform code where recall's stack join matters most, in 1–2 weeks rather than 4–6. It also produces the realism yardstick the built stack needs |
| **E2 main corpus** | **Build with agents** (Go + TS first, then Python + Rust), with mitigations 1–6. It is the only stack exercising doze-aws, sized to the task count V1 settles on | Covers SQS/SNS/S3 and the full language set. Low contamination. Tasks can be written to any required-site pattern |
| **E2 external validity** | **Jitsu + Hatchet** (Hatchet adds Python and Postgres-only links), optionally **Highlight** for Go↔TS GraphQL co-change and S3 | eval.md §2's "real stacks". Results reported per stack |
| Keep in view | **Chronoverse** (MIT, 16 stars, 13 Go services, Kafka + Postgres + Redis + outbox) | Lowest contamination of any real candidate, and close to the doze shape. Costs ClickHouse, Meilisearch and a Docker runtime. Its agent-assisted recent history makes it half "built" |

If only one option can be funded, port **Hatchet** rather than build. It runs
on Postgres alone and has real tests in three indexed languages, but its links
won't exercise doze's Kafka or AWS modules. If doze-aws coverage is the
priority, build, and accept the uniformity risk with the audit in mitigation 1.

Decisions this asks for:
- V4a: Jitsu for E1 (or Hatchet if the Kafka module port is a concern).
- V4b: whether the built stack's services are written by models other than the
  one under test.
- V4c: the mining window, i.e. which model cutoff the "post-cutoff only" rule
  uses.

---

## Sources

Repository pages and API records opened (GitHub API `repos/<name>` and
`repos/<name>/languages`, then a blobless clone reading README, LICENSE,
go.mod / package manifests, tree, compose files, test files and `git log`):

- https://github.com/jitsucom/jitsu: README, LICENSE, `bulker/LICENSE`, `docker/README.md`, `docker/docker-compose.yml`, `services/rotor/src/serverEnv.ts`, tree, log
- https://github.com/jitsucom/bulker (archived; metadata)
- https://github.com/hatchet-dev/hatchet: README, LICENSE, `.gitattributes`, `docker-compose.yml`, `Taskfile.yaml`, `pkg/config/server/server.go`, `cmd/hatchet-lite/main.go`, `sdks/`, tree, log
- https://github.com/highlight/highlight: LICENSE, `docker/compose.yml`, `backend/go.mod`, `sdk/`, tree, log
- https://github.com/hitesh22rana/chronoverse: README, tree, log
- https://github.com/goharbor/harbor: tree, go.mod, log
- https://github.com/openmeterio/openmeter: tree, go.mod, log
- https://github.com/open-telemetry/opentelemetry-demo: `src/` tree, `src/checkout/kafka/producer.go` location, compose files, log
- https://github.com/GoogleCloudPlatform/microservices-demo: `src/` tree, go.mod files, log
- https://github.com/ThreeDotsLabs/wild-workouts-go-ddd-example: tree, go.mod, log
- https://github.com/ThreeDotsLabs/watermill: tree, go.mod files
- https://github.com/go-kit/examples: tree, log
- https://github.com/temporalio/reference-app-orders-go: README, tree, log
- https://github.com/temporalio/reference-app-orders-web (metadata)
- https://github.com/temporalio/samples-go: tree, log
- https://github.com/Mikaelemmmm/go-zero-looklook: tree, go.mod, log
- https://github.com/mehdihadeli/go-food-delivery-microservices: tree, go.mod, log
- https://github.com/rasadov/EcommerceAPI: tree, go.mod, log
- https://github.com/minghsu0107/go-random-chat: tree, go.mod, log
- https://github.com/PacktPublishing/Event-Driven-Architecture-in-Golang: tree, go.mod, log
- https://github.com/stackus/ftgogo: tree, log (no license)
- https://github.com/AleksK1NG/Go-CQRS-Kafka-gRPC-Microservices: tree, log (no license)
- https://github.com/delimitrou/DeathStarBench: `hotelReservation/` tree, log
- https://github.com/formancehq/stack: tree (emptied)
- https://github.com/aws-containers/retail-store-sample-app: tree
- https://github.com/PostHog/posthog: LICENSE, tree (no checkout), log
- https://github.com/SigNoz/signoz: LICENSE, tree
- https://github.com/jaegertracing/jaeger: tree, `cmd/`, go.mod, log
- https://github.com/openreplay/openreplay: LICENSE
- https://github.com/kubeshop/testkube: LICENSE
- Metadata only (stars, licence, languages): grafana/tempo, coder/coder, zitadel/zitadel, inngest/inngest, woodpecker-ci/woodpecker, kubeshop/tracetest, netbirdio/netbird, bluesky-social/atproto, bluesky-social/indigo, instana/robot-shop, microservices-demo/microservices-demo, go-kratos/beer-shop, hatchet-dev/hatchet
- GitHub repository search (API), queries: `topic:microservices topic:kafka language:Go`, `topic:microservices topic:postgres language:Go`, `topic:event-sourcing language:Go`, `topic:saga language:Go`, `topic:outbox language:Go`, `microservices kafka postgres in:readme language:Go`, `topic:microservices-demo`, `topic:polyglot topic:microservices`

Internal: [../eval.md](../eval.md) §2, §3, §8, §10; [5-doze-capabilities.md](5-doze-capabilities.md) (modules: postgres, valkey, kvrocks, mariadb, ferret, temporal, aws, kafka).
