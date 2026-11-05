---
title: "Your own domain, with https"
description: Give an app a real name under a public TLD, served over https on your machine, so sign-in callbacks, cookies and tenant subdomains work locally.
---

Every app already answers at its doze name, `http://web.shop.doze`. Some
work needs more than that: a sign-in provider such as Google sends a browser
back only to a name under a public top-level domain, over https, and a
tenant-per-subdomain product wants `*.dev.acme.com`. Those are a line in
`doze.hcl` and one admin prompt.

```hcl
app "web" {
  run     = "pnpm dev"
  port    = 3000
  domains = ["dev.acme.com", "*.dev.acme.com"]
}
```

`doze up` runs the stack as before. If this machine is not set up for the
domains yet, it says so in one line:

```
› dev.acme.com needs a one-time setup here: doze trust (one sudo)
```

`doze trust` does two things in one prompt: it routes each domain (and
everything under it) to doze's resolver, and it trusts the doze certificate
authority on this machine. From then on `https://dev.acme.com` reaches the
app, and so does `https://tenant.dev.acme.com`. `doze url web` prints the
https address; the app's Connect card shows it; ⌘K offers to open it.

## What the authority is

A certificate authority of this machine's own, made the first time a domain
needs it, under `~/.doze/ca`. Its key never leaves that folder. The front
door mints a short-lived certificate per domain from it on the first https
request, and renews it before it expires. The authority can sign only names
declared on this machine; `doze trust --check` shows it and when it expires,
`doze trust --rotate` makes a new one and trusts it in one prompt.

Processes inside the stack need to trust it too, for calls between services
over https. doze sets `SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS`,
`REQUESTS_CA_BUNDLE` and `DOZE_CA_FILE` on every process it runs, so Go,
curl, Node and Python verify without being told. Java reads its own store:
import the file from `DOZE_CA_FILE` with `keytool` if you need it.

## Bringing your own certificate

A team with a real certificate for the domain, or one from mkcert, keeps it:

```hcl
app "web" {
  run     = "pnpm dev"
  port    = 3000
  domains = ["dev.acme.com"]
  cert    = "certs/dev.acme.com.crt"
  key     = "certs/dev.acme.com.key"
}
```

The paths are relative to `doze.hcl`. doze serves that certificate for the
domain and trusts nothing of its own; `doze doctor` says whether the file
covers the name and when it expires. `doze trust` then installs the route
only.

## Where things can get in the way

- **Something else holds port 80 or 443** (Caddy, nginx). doze does not
  contend: `doze doctor` names what holds the port, and the domain still
  works with the app's port in the URL. If you keep your own proxy, point it
  at the address `doze url web --json` reports.
- **The domain exists on the internet.** While a stack declares it, this
  machine resolves it to the stack. When no stack is running, the resolver
  forwards the name to your usual DNS, so the real host is back.
- **A VPN or management profile overrides DNS.** macOS consults profiles
  before `/etc/resolver`; `doze doctor` reports a domain that does not resolve
  despite the route.
- **Firefox** before version 120 ignores the system keychain; newer versions
  read it. Chrome and Safari always do.

## When a domain goes away

Routes are machine-wide, so doze keeps a record of which project asked for
each. When a domain leaves `doze.hcl`, or the project folder is deleted, its
route is stale: `doze up` and `doze doctor` say so, and the next `doze trust`
removes it in the same prompt. `doze trust --remove` undoes this project's
routes at once, and the authority's trust with the last project that used it.
`doze dns-setup --uninstall` takes everything away.

## Rules for the name

A domain is lowercase labels under a public top-level domain, optionally
one leading `*.`: `dev.acme.com`, `*.dev.acme.com`, `app.acme.co.uk`.
Not `localhost` or anything under it, not an address, and not a `.doze`
name, which every app has already. A name that is not under a public TLD
(`dev.acme.test`) is accepted, and `doze doctor` warns that sign-in
providers will refuse it.
