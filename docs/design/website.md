# Design: the doze documentation website

**Status:** shipped (2026-07-03) — live at doze.nerdmenot.in. The site source lives in this repo (`website/`); the
registry stays at `/registry`, served by doze-registry's own pipeline.

## Goals

One site at `doze.nerdmenot.in` that is the easiest, most unambiguous path for
every audience doze has:

1. **Evaluators** — why this project exists; why native processes beat
   containerization for development (including the debugger story); why HCL;
   honest alternatives.
2. **Users** — install → first project → daily loop → team/CI, with zero
   ambiguity at any step.
3. **Module authors** — from "I want engine X" to a signed, published module.
4. **Registry operators** — companies hosting their own registry/mirrors,
   air-gapped setups, the trust model.
5. **Contributors** — architecture, repo map, release process.

Non-goals: versioned docs (pre-1.0, "latest" only, revisit at 1.0); a blog;
translated docs.

## Hosting: can `doze/website` be the primary site with `/registry` separate?

Yes. The constraint is that a Cloudflare Pages custom domain attaches to **one**
project, and the registry must keep deploying from doze-registry (its publish
bot commits signed indexes there; that lifecycle is independent and must stay
so). Two workable shapes:

**(A) Two Pages projects + a tiny router Worker — recommended.**

- `doze-docs` (new Pages project) ← built from `doze/website` by a deploy
  workflow in this repo. Docs deploy when docs change.
- `doze` (existing Pages project) ← doze-registry, unchanged. Registry deploys
  when modules are signed.
- A ~15-line Worker bound to `doze.nerdmenot.in/*`: requests matching
  `/registry*` proxy to the registry project's `*.pages.dev`; everything else
  to the docs project. (Cloudflare serves Worker routes before Pages custom
  domains, so the domain moves to the Worker.)

Pros: fully decoupled lifecycles — a docs typo never redeploys the signed
registry and vice versa; each repo owns its site. Cons: one Worker to own
(trivial, versioned in doze-registry next to the deploy config).

**(B) One Pages project, composite build.** doze-registry's deploy checks out
doze and builds `website/` into the same `dist/` as the registry. Pros: no
Worker. Cons: docs deploys are gated on the registry repo's pipeline, doze
pushes need a cross-repo dispatch to publish docs, and the registry deploy
inherits the docs build (slower, more failure surface on the signing path).

Decision: **(A)**. The registry's deploy path is security-relevant; keep it
minimal and untouched.

The registry site keeps its module-browse pages (they're good, and generated);
the docs site links into them for per-module reference rather than duplicating.
Shared look: extract the palette/typography tokens from doze-registry's
`global.css` into the docs theme so the two halves feel like one site.

## Tech

**Astro Starlight** in `doze/website`. Rationale: the org already runs Astro
(registry site — one mental model, shared styling); Starlight gives sidebar
IA, full-text search (pagefind, static — no service), dark/light, MD/MDX,
last-updated stamps from git, and good defaults for exactly this shape of site.
Content lives as Markdown in `website/src/content/docs/` — ported from
`docs/`, which then becomes thin pointers (or is moved wholesale; see
"Source of truth" below).

**Source of truth:** the website content **replaces** `docs/` as the canonical
prose (a `docs/README.md` pointer remains for GitHub browsers, and
`CONTRIBUTING`/`SECURITY` stay repo files). One copy, no drift. Generated
surfaces stay generated: per-module config references live on `/registry`
(from `Describe()`); the CLI reference should eventually be generated from the
cobra tree (`doze docs-gen`, later — hand-ported at launch).

## Information architecture

Top navigation = the four audiences + reference:

```
Why doze · Docs (user guide) · Modules (authors) · Registry (operators) · Reference
```

### Why doze — the case (mostly NEW writing; seeds exist)

The section the project's voice lives in. Five essays, each standalone and
linkable:

1. **Why this project exists** `[port+expand: guide/why-doze.md]`
   The three taxes (Docker VM, brew drift, LocalStack weight); "real engines
   that sleep"; declare-don't-orchestrate; structure-not-data; the whole model
   in five ideas. Ends with "is doze for you?" including the honest not-for
   (production, HA, data you can't lose).

2. **Native processes vs containers, for development** `[NEW; seed: resource-footprint.md]`
   The core argument, made concretely:
   - *The debugger story (the centerpiece).* Your backend is a native process
     on your kernel: attach `lldb`/`delve` to your app AND inspect the engine
     with `ps`/`lsof`/Activity Monitor; strace/dtruss the real syscalls;
     `perf`/Instruments profile without a VM boundary; core dumps land on your
     disk. No `docker exec` indirection, no port-forward mazes, no "works in
     the container" filesystem skew, no waiting for a VM to share your files
     (bind-mount I/O tax on macOS). Breakpoint your app while `psql` sits in
     the real server — both are just processes.
   - *The resource story.* Measured numbers (port the footprint doc): idle
     doze ≈ one ~15 MB daemon vs an always-on VM holding RAM; per-engine RSS;
     Apple Silicon native vs emulation.
   - *The fidelity story.* Real upstream binaries — every extension, wire
     feature, and client behaves exactly as production; no reimplementation
     drift (contrast: LocalStack API coverage).
   - *What containers are still for* — honest section: production packaging,
     team-identical Linux userlands, things doze deliberately doesn't do.

3. **Why HCL (and not YAML or JSON)** `[NEW]`
   Argued from doze's actual features, not taste:
   - Config is a *program with a schema*, not a data dump: typed fields with
     strict unknown-key errors and did-you-mean (show the real `doze lint`
     output) vs YAML's silent-typo/indentation traps and JSON's no-comments.
   - **References between blocks** (`sqs.jobs.name`) build the dependency
     graph — SNS boots its SQS because the config *says so*; YAML needs string
     conventions and a second tool.
   - Expressions, variables/locals, `for_each`/`count` for real projects
     (three tenant databases from a list) without templating YAML with YAML.
   - Blocks-with-labels read like the infrastructure they declare
     (`postgres "app" { role "app" {…} }`); familiar to every Terraform user.
   - The counterargument acknowledged: one more syntax to learn — and why
     that's a smaller cost than debugging whitespace at 5pm.

4. **Alternatives, honestly** `[NEW]`
   A comparison page that respects the reader: docker-compose, Testcontainers,
   LocalStack, Nix/devenv/Flox, Tilt/Skaffold (dev-on-k8s), plain
   `brew services` — one paragraph each on what it's genuinely good at, when
   to pick it over doze, and where doze wins (a table for scan-readers, prose
   for deciders). This page earns trust the marketing page can't.

5. **The trust model** `[port+expand: concepts #modules + registry README]`
   Signed modules, TOFU keys, the lockfile's three layers, two version axes —
   why supply-chain rigor in a *dev* tool (because `doze up` runs binaries).

### Docs — the user guide `[mostly PORT, freshly updated]`

- **Install** — brew / mise / binaries / go install; platform policy (Apple
  Silicon + Linux; Intel-Mac guidance); uninstall. `[expand README install]`
- **Getting started** `[port guide/getting-started.md]`
- **Core concepts** `[port guide/concepts.md]` — daemon, lazy boot, reap,
  convergence, endpoints, engines-are-modules.
- **The engines** `[port guide/engines.md]` + per-engine recipe pages
  `[port recipes/*]`, each linking to its `/registry` page for full config.
- **Daily use** `[port recipes/workflows.md]` — run, shell, status, dash, logs.
- **Coming from docker-compose** `[NEW]` — a *mental-model mapping*, not a
  converter: services→blocks, depends_on→references, volumes→data dirs,
  healthcheck→readiness, .env→variables. Teaches the doze way to people who
  think in compose.
- **Teams & CI** `[expand faq + files-and-storage]` — commit doze.hcl+lock;
  the `~/.doze` cache recipe for CI; `doze modules upgrade --check` as a bot;
  `doze fetch`-style prewarm guidance.
- **Modules for users** `[port cli.md modules + configuration.md modules]` —
  search/docs/info/upgrade; the modules{} block; version-gated arguments.
- **Files & storage** `[port]` · **Troubleshooting** `[port]` · **FAQ** `[port]`.

### Modules — the author guide `[NEW, seeded by module-template README + SDK docs]`

1. **How modules work** — driver contract, capabilities by type-assertion,
   plugin protocol, the two version axes from the author's side.
2. **Write your first module** — the template walkthrough end to end (the
   proven httpd loop: template → build → `DOZE_<TYPE>_PLUGIN` → `doze up`).
3. **Real-engine modules** — Resolve against a binaries mirror, version
   normalization, convergence, templating, wire filters (postgres as the
   worked example; valkey as the minimal one).
4. **Describe(): docs and gates from code** — config args, blocks,
   Since/Until, RequireVersion; the drift guards.
5. **Testing** — decode tests, the enginetest harness, acceptance CI.
6. **Releasing** — modtool, immutability rules (never rebuild a published
   version; -buildvcs), cumulative indexes, the release workflow.
7. **Publishing** — namespaces, keys, signing, getting into a registry.

### Registry — the operator guide `[NEW, seeded by doze-registry README]`

1. **The architecture of trust** — index signature vs artifact signatures,
   TOFU, what the CDN can and cannot lie about.
2. **Host your own registry** — it's static files: layout, keygen, publish,
   validate, serve it. (Withdrawn: doze has one registry — see docs/design/next, decisions D1 and D11.)
3. **Mirror engine binaries** — doze-binaries format, `DOZE_<ENGINE>_MIRROR`,
   air-gapped end-to-end (registry + binaries + module cache).
4. **Operations** — key rotation (and what it breaks), CI wiring
   (dispatch/sign/deploy), the provisional-index lifecycle.

### Reference `[PORT]`

CLI `[cli.md]` · Configuration `[configuration.md]` · doze.lock format
`[files-and-storage extract]` · Environment variables `[cli.md extract]` ·
Module index schema (schema-1) `[registry README extract]` · Architecture
`[ARCHITECTURE.md]` — plus Contributing/Security links to the repo files.

### Cross-cutting

- Every page ends with "next" links (tutorial rail).
- Landing page: the pitch, a 60-second terminal cast (asciinema or styled
  static), the three-beat trust strip (mirrors the registry landing), audience
  cards ("Use doze / Build a module / Host a registry / Why?").
- `llms.txt` + clean markdown URLs: this product's users will ask AI tools
  about it; make the docs trivially ingestible.
- Every code sample must be executable as written; recipes CI-checked later
  (post-launch: a docs-test harness that runs the HCL snippets under
  `doze lint`).

## Deployment wiring

1. `website/` (Starlight) + `.github/workflows/docs.yml` in this repo: build +
   `wrangler pages deploy` to the new `doze-docs` project on pushes touching
   `website/`. Secrets: reuse `CLOUDFLARE_API_TOKEN`-style pair (needs Pages
   edit on the new project — same account token already works).
2. Router Worker (source in doze-registry, `worker/router.js` + wrangler
   config): `/registry*` → `doze.pages.dev`, else → `doze-docs.pages.dev`;
   route `doze.nerdmenot.in/*`. Deployed once, rarely touched.
3. Registry site: remove its root landing page's duplicate pitch eventually
   (the docs landing supersedes it); `/registry` remains the browse+machine
   layer. Its internal links point at the docs site for "what is doze".

## Phasing

- **P1 — launch-worthy:** infra (site + worker + deploy), landing, Install,
  Getting started, Core concepts, Why doze essays #1–#3, Reference (CLI +
  configuration), FAQ/Troubleshooting. Everything else stubs with links to
  current markdown.
- **P2 — the ecosystem:** Modules author guide, Registry operator guide,
  Alternatives (#4), Trust model (#5), engines/recipes ported, compose
  mental-model page.
- **P3 — polish:** terminal casts, llms.txt, CLI reference generation from
  cobra, docs-snippet CI, redirect old GitHub docs links via docs/README
  pointers.

## Decisions (resolved 2026-07-03)

1. **Worker router (A).** A ~15-line Worker on `doze.nerdmenot.in/*` forwards
   `/registry*` to the registry Pages project and everything else to the docs
   project — two independent deploy pipelines behind one domain.
2. **Registry landing retires.** Under the router only `/registry*` reaches the
   registry site, so its root hero page naturally stops being served. Its good
   content (pitch, install strip, trust strip) migrates to the docs landing —
   one pitch, one place; `/registry/` (the browse grid) becomes the registry's
   front page.
3. **Domain: `doze.nerdmenot.in` stays** for now. The Worker makes a future
   move a one-line change.
4. **Dark AND light mode, day one.** Starlight ships both (toggle + system
   preference). The palette is defined as paired light/dark tokens: the
   registry's amber-on-dark becomes the dark theme; a proper light counterpart
   is designed, not inverted. The registry pages adopt the same tokens (and
   gain light mode) in P2 so mode-switching never reveals a seam.
