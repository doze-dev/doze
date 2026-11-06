# doze.hcl and module compatibility: an audit (2026-11-06)

Two questions from the owner: is the configuration language as good as it
can be, and what happens when the doze app is older than the modules people
want (postgres 18 on an old doze)? Read from the code by two reviewers; the
claims below were checked against the source, and one was wrong (noted).

## Part 1. Compatibility between doze and its modules

### How it works today

- `module postgres "db" { version = 17 }`: `17` is the engine major. doze
  picks the module release itself (`internal/modules/modules.go`), from one
  signed index (`https://doze.nerdmenot.in/registry/doze/<name>/index.yaml`),
  and pins it per project in `doze.lock`.
- A release in the index records only `protocol` (the plugin wire version,
  still 1) and `engines` (the majors it supports)
  (`doze-sdk/modindex/modindex.go:48`). **Nothing records which doze it
  needs.**
- The module decodes its own block, strictly (`engine.DecodeStrict`). A new
  field in a newer module never troubles doze; an older module given a field
  it does not know fails with HCL "Unsupported argument", plus "a newer
  module is available" when there is one.
- Engine majors: no pin → doze picks a release that supports the major, or
  says "no release of postgres supports postgres 19; latest supports 14–18".

### Where it breaks

1. **A module that needs a newer doze, inside the same protocol.** It already
   happened: doze-sdk v0.10.0 moved engine downloads into doze. Those module
   releases still say protocol 1, so an older doze picks one, pins it in
   `doze.lock`, and then fails: "this doze is older than the module and can't
   download engine binaries for it: upgrade doze"
   (`doze-sdk/plugin/fetch.go:21`). The pin is now committed, so a teammate on
   the same doze fails too. The only record of the requirement is a comment in
   `doze-modules/modules.yaml`.
2. **Changing the engine major on existing data.** Postgres refuses ("data
   directory is locked to Postgres 16 … Run `doze migrate db`"). `doze migrate`
   does not exist; the only way forward today is to delete the data.
3. **Offline with no pin:** the index fetch fails even with a usable module
   in the cache.
4. **A future protocol bump:** there is no negotiation yet (`VersionedPlugins`
   is planned in `PROTOCOL.md`, not built), so every older doze stops at once.
5. **A newer doze.hcl read by an older doze:** "An argument named X is not
   expected here" blames the person's file when the fix is to upgrade doze.

### What I recommend

The rule: **someone asking for postgres 18 gets the newest module that works
with the doze they have, and is told plainly, once, if they need a newer doze.**

| # | Change | Where | Size |
|---|---|---|---|
| C1 | Each release records `doze: ">= 0.9.0"` in the index, stamped at build from the doze-sdk the module uses (the SDK knows which doze its features need). `Select` skips releases this doze is too old for, and picks the newest that fits. | doze-sdk modindex, module build, doze modules | 1 day |
| C2 | When nothing fits, one sentence with the way out: "postgres 19 needs doze 0.11 or newer; you have 0.9.2. Update doze (one click in the app)". The app shows it in Needs you. | doze modules, desk | 0.5 day |
| C3 | Never write a pin that cannot run: check the release before writing `doze.lock`. | doze modules | 2 h |
| C4 | `doze.lock` records the doze version that wrote it; an older doze reading a newer lock says so instead of failing deeper. | doze binaries/lock | 2 h |
| C5 | Offline: with no pin, use the newest compatible module already in the cache, and say it is offline. | doze modules | 0.5 day |
| C6 | Engine majors: replace the `doze migrate` pointer with what works today (keep the old major, or move the data aside with `doze reset db` after a dump), and decide whether to build `doze migrate` (pg_upgrade or dump/restore). | postgres module, docs | 2 h now; migrate is 2–3 days |
| C7 | Protocol negotiation (`VersionedPlugins`) before the first protocol bump, so a bump never strands older doze. | doze-sdk, doze | 1 day, when needed |
| C8 | Keep the app current: auto-update (Sparkle, waiting on signed releases) and a quiet "doze X is available" in Settings; C2's sentence leads there. | desk | after signing |

The lasting answer is C1 + C8: modules say what doze they need, and doze stays
current without effort. C2–C5 make the edges kind.

## Part 2. The configuration language

### Verdict

The core is right, and better than Compose for the common case. Things to
keep exactly as they are:

- four nouns (module, app, service, task)
- references as the dependency graph, with wrong-kind and did-you-mean errors
- strict decoding everywhere
- old forms that name their replacement (`checkOldForms`)
- `ready = http("/healthz")`
- a required `version`, pinned by the lock

Postgres, valkey and a node app take 11 lines. It is not yet as good as it
can be: two bugs the docs lead people into, no forward-compatibility marker,
and drift at the edges.

### Findings, ranked (checked against the code)

| # | Finding | Evidence | Proposal | Size |
|---|---|---|---|---|
| L1 | **A dotted version written as a number loses digits**: `version = 10.10` reads as `"10.1"`. The docs themselves write `version = 11.4` and `version = 1.1`. | `internal/config/decode.go:20` (string field), configuration.md:393/423, engines.md:138/152 | Refuse a non-integer number for `version`: `write version = "10.10" (quoted)`. Quote the docs. | 2 h |
| L2 | **No language version marker.** An older doze reading a newer file blames the file. | load.go:193, kinds.go:110 | `doze = ">= 0.9"` at the root, read before anything else; `doze init` writes it. Pairs with C4. | 0.5–1 day |
| L3 | **Per-developer overrides are documented but impossible**: `local.doze.hcl` only adds blocks, and redeclaring `db` fails. | teams-ci.md:19, faq.md:96, load.go:89, instance.go:91 | `*.override.doze.hcl` merging arguments into a same-named block (core arguments first: `port`, `enabled`, `sleep`). Or fix the docs. | 1 day (core args) – 3 days |
| L4 | **Silent mistakes**: `--var` for an undeclared variable, a missing `env_file`, and `env("X")` unset all pass quietly; lint has no warnings. | vars.go:50, process.go:221, funcs.go:118 | Error on the first two; lint warnings for unused variables and orphan tasks. | 1 day |
| L5 | **Errors render three ways.** App errors are one flattened line with no snippet, and module (plugin) errors lose their position. | engine/process/config.go:173, doze-sdk plugin/server.go:502 | Return `hcl.Diagnostics` from decoders, carry them over the plugin wire, render one way. | 2 h (apps) – 2 days |
| L6 | **Reference typos are unhelpful**: `module.db.ulr` gives a bare "Unsupported attribute"; an app with no port has an empty `url` with no error. (The reviewer said every app's `url` is empty; wrong, `engine/process/attrs.go` fills it.) | hcl ops.go, eval.go:491–520 | "db has no attribute ulr; did you mean url? It has: url, address, host, port…"; refuse `url` on a portless app. | 0.5 day |
| L7 | **Two words for ordering**: `after` (references) and `depends_on` (string map, accepts an undocumented `lazy`, module and app only). | instance.go:31, foreach.go:107 | One: `after = [module.db, started(app.cloud), lazy(module.aws)]`; `depends_on` becomes an old form. | 1 day |
| L8 | **One word for two ideas, and two for one.** `domains = true` (turn on .doze names) vs `domains = [...]` (your own hostnames); `idle_timeout` vs `sleep`, with "never" spelled two ways; docs say "reap". | domains.go:168, runtime.go:1022 | `defaults { names = true, sleep = "2m" }`; `"never"` everywhere; one verb in the docs. Old spellings point at new ones. | 0.5 day + docs |
| L9 | **Root `listen` does nothing** but is documented as the base port. | load.go:251, configuration.md | Remove it, as an old form pointing at `port`. | 2 h |
| L10 | **Each kind takes a different set of shared arguments**: `enabled`/`count`/`for_each` on module and app only (a service cannot be paused). | instance.go:31, runnable.go:88 | One shared command schema from runspec; meta-arguments on every kind. | 1 day |
| L11 | **The `$PORT` trap**: an app listening on its own port instead of `$PORT` only shows as a ready timeout. | process docs | On timeout, find its listener: "web listens on :5173, not $PORT; pass --port $PORT or set direct = true". | 0.5–1 day |
| L12 | **`defaults` is thin**: no `ready_timeout`, `restart`, shared `env`. | shop example | Allow them in `defaults`. | 0.5 day |
| L13 | **Docs drift**: `download` services documented as refused (they run), several common fields missing, `lazy` and `output` undocumented. | configuration.md | Fix; test examples for meaning, not only shape. | 0.5 day |
| L14 | **Module knowledge in core**: conventional ports hard-coded in doze. | detect/render.go:215 | Take the port from the module's Describe. | 0.5 day |
| L15 | **Env sugar**: modules know their env var. | eval.go | `env = merge(module.db.env, module.cache.env)`, erroring on a collision. | 3 h |
| L16 | Smaller: number outputs print as Go syntax; a portless service may sleep with nothing to wake it (to check); no `doze fmt`; no editor schema; no stop grace period or several named ports. | vars.go:286, process/config.go:418 | Each small. | 2 h – 1 day |

Rightly left out: images, volumes, networks, resource limits, build graphs,
Tilt-style live sync.

### Suggested order

1. **Now (about 1 day):** L1, L13, L9, the C6 message, C3. Bugs and wrong
   docs.
2. **Compatibility (about 2 days):** C1, C2, C4, L2, C5. This is the
   owner's worry, done properly.
3. **Vocabulary, while the user base is small (2–3 days):** L7, L8, L10,
   with old spellings pointing at new ones.
4. **Kindness (2–3 days):** L5, L6, L4, L11.
5. **Later:** L3 overrides, L12, L14, L15, C7 when needed, C8 with signing.

## To do (the owner said "do them all", 2026-11-06)

Ticked as each lands, with its commit.

Bugs and docs
- [x] L1  — 30de227a dotted `version` written as a number is refused; docs quote versions
- [x] L13  — cb8c51ddocs drift fixed
- [x] L9  — cb8c51droot `listen` removed (old form pointing at `port`)
- [x] C6 the — doze-modules 7b45fcf (points at doze migrate, which now exists) `doze migrate` pointer replaced with what works
- [x] C3  — folded into C1: the pin carries the release's doze need and is checked (3d9aca1); a pin cannot be held back until the daemon runs, since `up` writes it and the daemon fails latera pin is checked before it is written

Compatibility
- [x] C1  — doze 3d9aca1, doze-sdk 75b5f9f, doze-registry 8694281; the field is outside the signature so older doze still verifyreleases record the doze they need; selection skips what this doze cannot run
- [x] C2  — 3d9aca1one plain sentence when nothing fits, with the way out
- [x] C4  — 3d9aca1doze.lock records the doze that wrote it
- [x] L2  — 5b36a67`doze = ">= x"` in doze.hcl, read first
- [x] C5  — 30145dfoffline with no pin uses a compatible cached module
- [x] C7  — doze-sdk b0345d5protocol negotiation (VersionedPlugins)
- [x] C8  — 81d7531; installing waits for signed releasesthe app says when a newer doze is out (auto-install waits for signing)
- [x] C6b  — 1255047, run end to end (16 → 17, data intact)`doze migrate` for a postgres major (dump and restore)

Vocabulary
- [x] L7 one word for ordering: `after = [..., started(x), lazy(y)]`
- [x] L8 `defaults { names, sleep }`, "never" everywhere, one verb in the docs
- [x] L10 shared meta-arguments on every kind

Kindness
- [ ] L5 one error rendering, with positions, across the plugin wire
- [ ] L6 reference typos get did-you-mean and the list; portless `url` refused
- [ ] L4 silent mistakes become errors; lint warns
- [ ] L11 the `$PORT` trap is named on a ready timeout

Later items
- [ ] L3 override files for core arguments
- [ ] L12 richer `defaults`
- [ ] L14 ports from the module's Describe
- [ ] L15 `module.<x>.env`
- [ ] L16 outputs render, portless service sleep, `doze fmt`, editor schema

Not done by me, for the owner (publishing): tag and push doze-sdk (v0.11.0:
MinHost, the release field, SelectFor, VersionedPlugins), bump doze-modules and
the aws/kafka modules to it so new releases carry `doze:`, deploy the
registry, and publish the postgres module with the new message.
