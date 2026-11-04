---
title: "Install"
description: One static binary — Homebrew, mise, direct download, or built from source.
---

doze is **one static binary** with no runtime dependencies — no Docker, no
toolchain. It fetches everything else (engines, modules) itself, verified and
pinned.

**Platforms:** macOS on Apple Silicon, Linux on x86-64 and arm64. Intel Macs
are not supported (use a Linux devcontainer or VM there); on Windows, WSL2
works.

## Homebrew (macOS & Linux)

```sh
brew install doze-dev/tap/doze
```

## mise

```sh
mise use -g ubi:doze-dev/doze
```

## Direct download

Grab the archive for your platform from the
[releases page](https://github.com/doze-dev/doze/releases), extract, and put
`doze` on your `PATH`:

```sh
curl -fsSL https://github.com/doze-dev/doze/releases/latest/download/doze_0.1.1_darwin_arm64.tar.gz \
  | tar -xz && sudo mv doze /usr/local/bin/
```

(Checksums ship as `checksums.txt` alongside every release.)

## From source (Go 1.27+ and bun)

```sh
git clone https://github.com/doze-dev/doze && cd doze
make build
```

`go install …@latest` is not a way in: the module carries a `replace` for a
patched Wails, and the app's page is built into the binary by `make build`.

## Verify

```sh
doze version
# doze 0.1.1 (darwin/arm64, go1.26.x)
```

Then head to [Getting started](/start/getting-started/) — your first backend is
about three commands away.

## Upgrading

`brew upgrade doze-dev/tap/doze` / `mise up` / re-download. doze itself is
versioned independently of your projects: upgrading the CLI never changes what
a project runs — that's pinned in each project's `doze.lock`.

## Uninstall

Remove the binary (`brew uninstall doze-dev/tap/doze`), then optionally the
shared cache and all project data: `rm -rf ~/.doze`. Nothing else is touched —
doze installs no services, agents, or kernel extensions.
