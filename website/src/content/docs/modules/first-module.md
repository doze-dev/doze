---
title: "Write your first module"
description: Template to running engine in fifteen minutes — the httpd walkthrough.
---

The fastest path is the official template — a complete, working engine
(`httpd`, a static file server) with tests, generated docs, and release CI,
that you reshape into yours.

## 1. Take the template

On GitHub: **[doze-dev/module-template](https://github.com/doze-dev/module-template)
→ "Use this template"**. You get:

```
httpd/httpd.go       # the driver: Type, Resolve, Provision, Plan, DecodeConfig, ConnString
httpd/describe.go    # Describe(): docs + engine-support — generated, never hand-written
httpd/httpd_test.go  # config decode tests + the Describe/Config drift guard
plugin/main.go       # the plugin binary (plugin protocol / hidden __serve mode)
cmd/release/main.go  # packages archives + signed-index + meta.yaml via modtool
```

## 2. Run the dev loop — no registry required

```sh
go build -o /tmp/httpd-plugin ./plugin

mkdir /tmp/demo && cd /tmp/demo
cat > doze.hcl <<'EOF'
httpd "site" {
  port = 8080
  root = "."
}
EOF

DOZE_HTTPD_PLUGIN=/tmp/httpd-plugin doze up
curl localhost:8080
```

`DOZE_<TYPE>_PLUGIN` points doze at your local binary, skipping fetch and
signature checks entirely — the whole author inner loop is *build, run,
curl*. `doze lint` exercises your `DecodeConfig` (your typos become positioned
errors); `doze logs site` shows your server's output; `doze down` reaps it.

## 3. Make it yours

Rename the package, `Type()`, and `Config`, then pick your engine's shape:

**Self-contained** (what httpd is): the plugin binary *is* the server —
`Plan` self-execs with a hidden `__serve` argument, and the driver is
`Versionless`. Right for anything you can implement in Go (doze's own
S3/SQS/SNS are this).

**Real upstream engine** (postgres/valkey shape): delete `Versionless`,
implement `Resolve` against a binaries mirror, and advertise supported engine
majors in `Describe().Versions`. Covered in
[real-engine modules](/modules/real-engines/).

The template's TODOs walk every decision; each driver method carries a
doc-comment explaining its half of the contract.

## 4. The habits that keep you honest

- **`Describe()` is not optional.** The release tool refuses to package
  without it; docs, the registry page, and the signed
  engine-support gate all generate from it.
  [Details](/modules/describe/).
- **The drift-guard test** (in the template) fails when `Describe()` and your
  decode schema diverge. Grow both together.
- **Strict decode.** `gohcl` rejects unknown attributes — keep it that way; a
  silently-ignored typo is the YAML world you left.

## 5. Onward

Test it properly ([testing](/modules/testing/)), cut a release
([releasing](/modules/releasing/)), and put it where users can trust it
([publishing](/modules/publishing/)).
