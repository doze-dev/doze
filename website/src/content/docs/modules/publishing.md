---
title: "Publishing modules"
description: One registry, one key — how a module gets to where users can run it.
---

doze has **one registry**. A `module httpd "site"` block resolves to the module
`doze/httpd` there and nowhere else: there is no setting that points a project
at another registry or another publisher. So a module is published by getting
it into the doze registry, where it is built in public CI and signed with the
registry key that every doze binary carries.

## 1. Build and run it locally first

You do not need the registry to use your own module. Point doze at the plugin
binary you built:

```sh
DOZE_HTTPD_PLUGIN=$PWD/bin/httpd-plugin doze up
```

`DOZE_<TYPE>_PLUGIN` skips the registry and the lock entirely, for that engine
type. It is the whole module-author loop, and it is also how a team runs a
private module that will never be published. See
[testing](/modules/testing/) for the conformance suite to run before going
further.

## 2. Contribute it to the registry

Open a pull request against
[doze-modules](https://github.com/doze-dev/doze-modules) adding your module.
Once it is merged:

- CI builds the plugin for every supported platform from that source.
- The release is signed into
  [doze-registry](https://github.com/doze-dev/doze-registry): **each archive's
  checksum** and then **the index itself** (protocol, engine support, and
  channel metadata are attestable — a compromised CDN can't lie about
  compatibility).
- `validate` replays exactly the checks user doze binaries enforce;
  `validate:remote` additionally re-downloads every archive and confirms the
  signed checksums match reality.

Your module's registry page is generated from the module itself: tagline,
engine versions, the config reference rendered from your `Describe()`,
releases and signature status.

## 3. What users see

```hcl
module httpd "site" {
  port = 8080
  root = "./public"
}
```

First use pins the release in their `doze.lock`; from then on your module's
updates reach them only through `doze modules upgrade` — reviewable,
explicit, reversible. That is the same deal users get from every other
module, because the machinery is identical.
