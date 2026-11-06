# Registry redesign

Status: proposal, 2026-11-07, for the owner's review. Nothing built.

## What is there (doze-registry, doze.nerdmenot.in/registry)

Looked at the running site: a Modules grid (8 cards, category chips, search)
and one page per module (usage, versions, full configuration tables, a
sidebar with "add to your stack", platforms, updates, and a folded "under the
hood"). The signing, the immutability checks and the machine catalog are
sound and stay as they are.

What is wrong with it:

1. **It teaches syntax doze now refuses.** The postgres page's examples read
   `postgres "app" { … }` (now `module postgres "app"`), the sidebar too; the
   prose says "reaps when idle". A copy-paste from the registry fails. The text
   comes from each module's Describe (`dzm meta`), so it is fixed at the
   source and checked: every example on the site is parsed by the current
   doze in CI, as the website's docs already are.
2. **The first screen is a wall.** Usage is every option at once (40 lines
   for postgres). Most people need three lines and one connection string.
3. **Extensions are a footnote**: one row in a table, and the only way to get
   pgvector is a bundle URL you find yourself.
4. **Nothing says what doze you need.** Releases now carry the oldest doze
   that runs them (the `doze` field); the site shows none of it.
5. **Search finds modules only.** "pgvector", "redis", "s3", "mongodb" find
   nothing, though doze runs all of them.
6. **It looks like a second product**: amber, its own type and layout, while
   the app and the website share one brand.

## The redesign

**Home (the catalog).**
- One search over modules, extensions and what-it-replaces words: "redis"
  shows Valkey and Kvrocks, "pgvector" shows postgres › vector, "mongodb"
  shows FerretDB, "s3" shows AWS. Results say why they matched.
- Cards: name, one line, the versions you can declare, platforms, and the
  three-line snippet with Copy. Category chips stay.
- A strip at the top: "Every module is signed; doze pins what it fetched in
  doze.lock" with a link to how, instead of a footer line.

**A module page, top to bottom.**
1. Header: name, one line, signed, versions, platforms, "needs doze 0.2 or
   newer" from the newest release.
2. **Start**: the smallest block that works (`module postgres "db" {
   version = 17 }`), the variable your app reads (`DATABASE_URL`), and the
   connection string in three languages (Go, Node, Python), each with Copy.
3. **Recipes**: the two or three setups people actually write, each a small
   complete block with a sentence: for postgres, "a role and a schema for
   your app", "vectors with pgvector", "time series", "geo".
4. **Extensions** (postgres): a searchable grid from the module's catalog:
   name, what it is for, licence, the majors it supports, "preloads, restarts
   once", a one-line snippet. Contrib extensions listed apart ("ship with
   Postgres"). Wave 2 ones appear as they publish.
5. **Versions**: the table as today, plus per module release the oldest doze
   it needs.
6. **Reference**: the full argument tables, folded by default, with search
   ("connection_limit").
7. **Under the hood**: releases, signatures, protocol, as today.

**Machine catalog.** `index.json` gains the extensions and the `doze` field,
so `doze modules search vector` finds postgres › vector and `doze modules
docs postgres` prints the same Start and Recipes the page shows.

**Look.** The website's tokens and type (shared with the app), light and
dark,
the same header as doze.dev with Registry as a section, not a separate brand.

**Checks in CI.** Every HCL example on the site parsed by the current doze;
every snippet's module and extension resolvable in the published index; the
pages screenshotted at desktop and phone width.

## Files

- `doze-modules` (each module's `describe.go`) and `dzm meta`: Describe gains
  `start`, `recipes`, `env` and `connect` snippets; meta.yaml regenerated.
- `doze-registry`: `src/pages/registry/index.astro` (search over modules,
  extensions, aliases), `src/pages/registry/[ns]/[name].astro` (the sections
  above), components `Start.astro`, `Recipes.astro`, `Extensions.astro`,
  `Reference.astro`; `scripts/prepare.mjs` (catalog with extensions, aliases,
  doze field); `scripts/check-examples.mjs` (bun: runs `doze lint --offline`
  on every snippet); styles from the website's tokens.
- `doze`: `doze modules search` and `docs` read the new fields.

## Open questions for the owner

- **Address**: stay at doze.nerdmenot.in/registry, or move under doze.dev
  (doze.dev/registry) now that the app and docs say doze.dev?
- **Third-party modules**: the "Publish" button is there; keep it prominent,
  or hide it until outside publishers exist?

## Size

3–4 days: Describe fields and meta for eight modules (1), the two pages and
search (1.5), extensions grid and catalog (0.5, after the extensions work),
example checks and screenshots (0.5–1).
