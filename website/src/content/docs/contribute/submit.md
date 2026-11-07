---
title: "Submit and release"
description: Open the pull request, pass the checklist, and follow your module from the merge to a signed release in the registry.
redirect_from: ["/modules/publishing/", "/modules/releasing/"]
---

This page shows you how to submit a module to doze-modules and what happens, step by step, between the merge and the moment users can declare it.

You don't build, upload or sign anything yourself. After the merge, CI in
doze-modules builds and publishes the module, and CI in doze-registry signs it.

## Open the pull request

Open a pull request against `main` in
[doze-modules](https://github.com/doze-dev/doze-modules). Say what it changes
and why, and link the [Propose a module](/contribute/propose/) issue for a new
module. Then fill in the checklist from the pull request template:

- [ ] The module is in `modules.yaml`, and its **version is bumped** (a
  published version is immutable; an unchanged version does not release).
- [ ] `Describe()` is complete: `Source = "doze/<name>"`, title, tagline,
  category, every config argument or block, `Versions`, and an `Example` that
  converges.
- [ ] New module: registered in `cmd/dzm/meta.go` (`describers`) and the README
  table regenerated (`go run ./cmd/dzm readme`).
- [ ] New module: added to the `matrix.module` list in
  `.github/workflows/acceptance.yml`, with a `case` arm in
  `scripts/backend-env.sh`.
- [ ] The backend is published in
  [doze-binaries](https://github.com/doze-dev/doze-binaries) for all three
  platforms (link the release or PR).
- [ ] `go vet ./...` and `go test -race ./...` pass.
- [ ] Acceptance passes locally: `go test -tags acceptance ./modules/<name>/...`
  with `DOZE_<NAME>_BINDIR` set (it skips when unset).
- [ ] Licence: the engine's licence is named, and allows doze to download and
  run it.

[Write it in doze-modules](/contribute/write/) and [Test it](/contribute/test/)
cover each item.

On the pull request, `test.yml` runs `go vet` and `go test -race ./...` on
Linux and macOS, and a maintainer reviews it. Acceptance does not run on the
pull request, because it needs the backend download. It runs after the merge,
as the release gate, which is why the checklist asks you to run it first.

### Licence

doze-modules is Apache 2.0, and so is your contribution. The engine's own
licence is a separate question: doze downloads and runs the engine on users'
machines, so it must allow that. Name it in the pull request. If users should
know something about it, such as a source-available licence, say so in the
module's `Description`.

## After the merge

Nothing is manual. A push to `main` that touches `modules.yaml`, `modules/`,
`cmd/`, `go.mod`, `go.sum` or the workflows starts `release.yml`:

1. **test**: `go test ./...` must pass before anything is built.
2. **bumped**: compares `modules.yaml` with the previous commit and lists the
   modules whose version changed. A version bump is the release intent; a module
   whose version did not change is not released.
3. **acceptance**: for each bumped module, fetches the published backend from
   doze-binaries (`scripts/backend-env.sh`) and runs its acceptance suite. No
   version publishes without passing it.
4. **build-and-publish**: downloads each module's published `index.yaml`, so
   older releases stay in it; builds the archives for the three platforms with
   `dzm`; generates `meta.yaml` from `Describe()`; and uploads them to the
   module's rolling GitHub release, whose tag is the module name.
5. **notify the registry**: sends a `modules-published` event to
   [doze-registry](https://github.com/doze-dev/doze-registry), carrying the
   module names and the versions it should expect.

Then doze-registry's `publish.yml` takes over:

6. waits until the GitHub release serves the expected version (a release asset
   can be stale for a short while after an upload);
7. signs each new archive's checksum and then the whole index with the registry
   key, which lives only in doze-registry;
8. validates every signature, checks that nothing already published was dropped
   or changed, and downloads every archive again to confirm its checksum;
9. commits `registry/doze/<name>/index.yaml` and deploys the registry.

`publish.yml` also runs every hour and signs any release it has not seen yet,
so a lost notification only delays a release.
[How the registry works](/contribute/registry/) explains what each signature
protects.

## Live in the registry

A few minutes after the merge, the module is listed at
[doze-dev.com/registry](https://doze-dev.com/registry/), and any `doze.hcl` can
declare it:

```hcl
module valkey "cache" {
  version = 9
}
```

The first time doze resolves the module, on `doze up` for example, it pins the release in the project's `doze.lock`.
From then on a new release reaches a project only when its owner runs
`doze modules upgrade` and commits the new lock. That keeps every update
reviewable and reversible.

## Changing a published module

Every change is a new release: a bug fix, a new field, a new engine major. The
rules are enforced, not suggested:

1. **Bump the module version in `modules.yaml`.** Without a bump, CI rebuilds
   nothing new and nothing reaches users.
2. **Published bytes never change.** `dzm` skips any `(version, platform)`
   already in the published index, and stops with an error if a rebuild would
   produce different bytes. A changed checksum would break every `doze.lock`
   that pinned the original. Never republish a version; bump it.
3. **Old releases stay.** The release job merges into the published index, so a
   project locked last year, or an older doze, keeps resolving. The registry's
   validation refuses an index that drops a published release.
4. **The protocol is stamped for you** from the SDK the module is built with. If
   the plugin protocol ever changes, an older doze keeps choosing your older
   releases, which is one more reason they must stay.

To support a new engine major, add it to `Describe().Versions`, do the work it
needs in `Resolve` and convergence, cover it in acceptance, and bump the
version.
