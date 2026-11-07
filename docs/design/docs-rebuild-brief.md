# Docs rebuild: the writers' brief (phase D3)

Approved plan: /Users/srini/.claude/plans/can-you-make-a-hidden-cloud.md (read it).
Slugs and titles: /Users/srini/Code/doze-dev/doze-website/design/docs-ia.json.
Boards (look at them): /private/tmp/claude-501/-Users-srini-Code-doze-dev/7b22fd62-a94d-4e3a-bd10-f71bb12a1385/scratchpad/web2/shots/Docs2*.png
Components: /Users/srini/Code/doze-dev/doze-website/src/components/docs/mdx/README.md
Screenshots (real, from the Larder demo, 1280×800 @2x): website/src/assets/app/*.webp
(list in website/docs-shots.json; re-make with `make docs-shots`).

## Rules

- Pages live in website/src/content/docs/<slug>.md(x), slugs from docs-ia.json.
  Use .mdx when a page uses components; import them with
  `import HowTo from "@docs/HowTo.astro";` (also Shot, Keys, Steps) and images
  relatively, e.g. `import map from "../../../assets/app/view-map.webp";`.
- App first: every task shows `<HowTo>` with the app slot first (a Shot and
  numbered Steps) and the terminal slot second. Reference pages don't.
- One job per page. First paragraph: one sentence saying what you'll be able to do.
- Larder is the example (doze-examples/larder, doze.hcl at
  /Users/srini/Code/doze-dev/doze-examples/larder/doze.hcl); always say it's made up.
- Ground every claim in code: commands/flags (`./doze <cmd> --help`), doze.hcl
  fields (website/public/schema/doze-hcl.json, internal/config/), defaults
  (internal/config/config.go: DefaultIdleTimeout = 2m), app screens
  (internal/desk/frontend/src/pages/*.tsx). `make ground` must report nothing
  for your pages. Never invent numbers; measured numbers say how.
- Moved or merged pages: the new page carries `redirect_from:` with every old
  slug it replaces, root-relative like the page's own links (e.g.
  `redirect_from: ["/start/getting-started/"]`). Delete the old file.
- Links between pages are root-relative to the docs root, e.g. `/hcl/sleep/`.
- Plain words, short sentences. No "honestly", "genuinely", "simply", no em-dash
  chains, no marketing filler. Second person.
- Registry-only: modules come only from the doze registry
  (registry.doze-dev.com for doze 0.2+); anything else runs as your own
  `service`. Never mention DOZE_<TYPE>_PLUGIN (being removed), DOZE_MODULES_MIRROR,
  `modules { source/mirror }`, outside publishers/namespaces, self-hosting a
  registry or mirroring binaries. Contributing = PR to doze-modules (see
  doze-modules CONTRIBUTING.md on branch contrib-docs, worktree in the
  scratchpad: ../../scratchpad/doze-modules).

## Facts the old pages get wrong (fix wherever they appear)

- Blocks: `module postgres "app" { version = 18 }`, never bare `postgres "app"`;
  `app`, `service`, `task` (not `process`); `after` (not `depends_on`);
  `sleep` (not `idle_timeout`); `stack` (not `name`); `names` (not `domains = true`).
- AWS is one module (`module aws "aws"`) with S3, SQS, SNS, DynamoDB, Lambda,
  EventBridge, KMS, SSM, Secrets Manager, STS and Step Functions — not "S3/SQS/SNS
  built into doze".
- Gone commands: `doze shell`, `doze binaries …`, `doze modules search/docs/…`;
  `doze dash` is hidden/deprecated (the app replaced it); `doze run` injects env
  only with `--env`.
- doze.lock layers: engines, modules, downloads, plus a top-level `doze:` line
  (internal/binaries/lock.go). No keys layer.
- Names: each service is `<name>.<stack>.doze` on its own address; the front door
  serves apps on :80/:443; `defaults { names = true }` (doze init writes it).
  States: awake, dozing, waking, asleep, crashing, failed, stopped. Default sleep 2m.
- Install: the app via `brew install --cask doze-dev/tap/doze` (DMG once a signed
  release exists), deb/rpm on Linux; the CLI is the same binary. No `go install`.
  doze installs a resolver, a network daemon and (with domains) a local CA.

## Content that exists nowhere yet (write it)

App-first quickstart; names & addresses; every app screen (Grid, notifications,
menu bar, Doctor/Machine, ⌘K, Activity, Logs/console tabs, task runs); your own
`service` (mailpit as Larder does it, `doze add --as installed|container|download`);
tasks & ordering (`task`, `inputs`, `manual`, `after` with `started()`/`lazy()`);
sleep & wake (2m, `sleep`, `"never"`, pin, on/off, `doze up --last/--all`);
upgrading from 0.1 (renames above); `doze migrate`; Linux (deb/rpm,
systemd-resolved, polkit Repair; undo: `doze dns-setup --uninstall`,
`doze trust --remove`); contribute via doze-modules; a new llms.txt.

## Found while writing (fix in D5 or after)

- `doze down --help` still says "use the dash: :sleep" (should point to `doze sleep` / the app).
- `RegistryBase` still doze.nerdmenot.in; docs say registry.doze-dev.com — the path layout (`/doze/<name>/index.yaml` at the root) must match what D5 serves.
- The old site's sidebar (website/astro.config.mjs) lists slugs the rebuild deletes; switch it to autogenerate or retire the old site after the merges.
- App `?` help sheet says "1 2 3 Tiles · List · Map"; code handles 1–4 (Grid) — fix the help text.
- Home's empty state mentions a Procfile; internal/detect doesn't read Procfiles — fix the copy or add detection.
- desk/app/menu.go `docsURL` points at /cli/app/ — point at /app/library/ on doze-dev.com.
- `doze init` (non-wizard) doesn't write `defaults { names = true }`; decide whether it should.
- doze-modules metas: postgres `grant` labelled `role` though `role` is a required field; aws meta lists only fifo/dlq/max_receives for queue and no fields for topic/table/function; ferret/mariadb/temporal nested blocks have no fields. (gen-engines works around with LABEL_IS_FIELD.)
- `doze url <app>` prints 127.0.0.1:<port> even with names on; references give <name>.<stack>.doze.
- `doze off --help` says off is "the row's switch in the app", but the switch sleeps; off is only in the sleep dialog / "Stop trying".
- `started()` still waits for ready on the app/service path.
- Temporal needs an explicit `port` to get an address, though its meta default is 7233.
- `doze add mailpit --as container` writes image `mailpit` (should be axllent/mailpit).
- No "add a service" flow in the app for an existing stack (only for an empty folder).
- README.md still links guides/engines.
