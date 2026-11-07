---
title: "The trust model"
description: How doze checks every module and binary it downloads, what doze.lock pins, and what the signatures do not prove.
---

You'll know how doze decides that what it downloads is safe to run, and what
that does and does not promise.

`doze up` downloads programs and runs them. A tool that does that owes you an
account of why you can trust what it runs, especially a development tool:
your machine holds source code, credentials and access to production.

## Where things come from

- **Modules** (the plugins that run each engine) come only from the doze
  registry, `registry.doze-dev.com`. There is one registry and one publisher;
  modules are added by pull request to
  [doze-modules](https://github.com/doze-dev/doze-modules) and built in public
  CI. See [How the registry works](/contribute/registry/).
- **Engine binaries** (Postgres, Valkey, MariaDB and the rest) are built from
  upstream source in public CI, in
  [doze-binaries](https://github.com/doze-dev/doze-binaries). A published
  version is never rebuilt. doze downloads them itself and checks each one.
- **Anything else** you run as your own `service` with `download` comes from
  the URL you wrote. doze cannot vouch for it, but it pins what it got the
  first time, and refuses different bytes later.

## How a module is checked

1. **The registry's key is compiled into doze.** It is an ed25519 public key.
   doze does not fetch it and does not learn it on first use, so a compromised
   registry host cannot hand a new install a key of its own. Changing the key
   takes a doze release.
2. **Each module's index is signed as a whole.** The index says which releases
   exist, which engine versions each supports, which plugin protocol it
   speaks, and where the channel points. Signing all of it means a compromised
   server cannot claim false compatibility or roll the channel back to an
   older release.
3. **Each archive's checksum is signed.** Where the archive is stored is not
   trusted: changed bytes fail the check whoever serves them.

## doze.lock is the contract

`doze.lock`, committed beside `doze.hcl`, has a `doze:` line and three layers:

```yaml
doze: 0.2.0   # the newest doze that wrote this file
engines:      # each engine version you declared, resolved to an exact build, with a checksum per platform
modules:      # the module release that runs each engine: version, protocol, checksums
downloads:    # each URL a service downloads, with the checksum of what it served the first time
```

What that gives you:

- **The same bytes everywhere.** A teammate's clone and your CI resolve to the
  same software, checked, not assumed.
- **No drift.** A newer release in the registry does not change what a locked
  project runs. With the downloads already on disk, it resolves offline.
- **Change on purpose.** Module pins move only with `doze modules upgrade`, and
  the new lock goes through review like any change.
- **Nothing rebuilt upstream.** A published version and platform is never
  rebuilt, so a lock committed today resolves the same later.

See [Files and storage](/guides/files-and-storage/#dozelock) and the
[doze.lock reference](/reference/lockfile/).

## Two versions, on purpose

You write the **engine** version, `version = 18`: the software your app talks
to. The **module** version, the plugin's own release, is chosen for you: the
newest release that this doze can run and that supports every engine version
you declared. Then it is locked. You meet it only in `doze modules` output and
in an error that names its own fix, such as
`run 'doze modules upgrade postgres'`.

That split lets a module add an argument or a new Postgres major without
changing what any locked project runs; your project takes it in one reviewed
commit.

## The local certificate authority

For an app with `domains`, `doze trust` makes a certificate authority on your
machine and asks the system to trust it. doze only makes certificates for names
your stacks declare, but like any root it could sign any name, so its key stays
in `~/.doze/ca`, readable by you alone. `doze trust --remove` stops trusting it
when no stack declares a domain any more, and `doze trust --rotate` replaces
it. See [Domains and HTTPS](/app/domains/).

## What this does not claim

A signature proves who published something, not that it is harmless. What
makes the registry's modules trustworthy is that each one is built in public
CI from public source, through a reviewed pull request, and each module's
registry page shows its source and what it accepts. And the compiled-in key is
only as trustworthy as the doze that carries it: get doze from its releases or
the doze Homebrew tap.
