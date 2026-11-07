# Names for modules: one name by default, more by asking

Status: agreed on 2026-11-07; not built. Touches doze-sdk (the contract), doze
(the daemon, endpoints, config), the aws and kafka modules, the standalone
doze-aws and doze-kafka binaries, and doze-names. Supersedes "The zone is shared
with the standalone tools" in [networking.md](networking.md).

## The problem in one paragraph

doze gives every instance one name, `<instance>.<stack>.doze`, on an address of
its own, and keeps its declared port there. That is right for postgres and for
anything else that speaks TCP. Some modules need more. AWS wants one port-less
HTTP host whose every subdomain reaches it (`<bucket>.s3.<region>.aws.shop.doze`)
and one name beside it (`sync-…`, which every SDK uses for two Step Functions
calls). Kafka has a TCP broker that must advertise its own name, plus an HTTP
schema registry and an HTTP console. Today doze knows AWS's needs by its engine
type string, and nothing at all of Kafka's. The SDK has no way for a module to
say what it needs, or to be told what it got.

## What doze does today (the facts this design starts from)

| | Where | What |
|---|---|---|
| One name per instance | `doze/internal/daemon/bindplan.go` | `<instance>.<stack>.doze`, claimed in the shared registry with an address from the shared pool; the declared port is kept on that address. Without machine setup, `127.0.0.1` and distinct ports. |
| Names own their subtree | `doze-names/registry.go` (lookup walks up) | `x.y.db.shop.doze` resolves to `db.shop.doze`'s address and is routed by the front door to `db.shop.doze`'s target. So a deeper name never needs claiming to *resolve*. A route, though, lives on a claimed entry (`RouteFor` refuses a name the process does not hold), so a child that must reach a *different port* is claimed as a twin at the same address, and the most specific entry wins. |
| Port-less HTTP | `doze-names` front door on `:80`/`:443` | Routes by Host to a target; most specific registered name wins. |
| AWS, special-cased | `doze/internal/daemon/awsingress.go`, `internal/config/aws.go`, `internal/endpoints/endpoints.go` | `aws.<stack>.doze` → the first `aws` instance; `AWS_ENDPOINT_URL` from it. Chosen by `decl.Type == "aws"`. A second aws instance in a stack gets no front door. `sync-aws.<stack>.doze` is never claimed, so SDK sync calls do not reach it. |
| Connection strings | `endpoints.go:80-82` | The module returns a URL built from the bound `ip:port`; doze string-replaces it with `name:port`. |
| Kafka | `doze-kafka/module/kafka.go:72` | Advertises `Endpoint.TCPAddr`, the bound address, so clients are sent to an IP and not the name. The registry and console have no names. |
| App domains | `domains.go` `claimAppDomains` | A person's own domains (`dev.acme.com`) are twinned onto an app's lease, with TLS from doze's CA. That is the user's power, in config; nothing here changes it. |

## Principles

1. **One name, by default, and nothing a module did not ask for.** A module
   that says nothing gets exactly what postgres gets today: its name, its own
   address, its declared ports on it. No front-door route, no extra names.
2. **A module describes its listeners; doze decides addresses.** The module
   says *what* it serves (this port is TCP, that one is HTTP, this one wants to be
   port-less, these host shapes go to that port). doze says *where* it ended up.
   A module never picks an address, a port it does not own, or a name outside its
   own.
3. **A module can only ask for names inside its own name**, plus a small, fixed
   set of shapes beside it, checked by doze for collisions before anything runs.
   It cannot ask for `kafka.doze`, another instance's name, or a real domain.
   Real domains stay in the person's config, where they are today.
4. **The plan is a pure function of the instance's config.** No I/O, no running
   process. `doze env`, `doze status` and `endpoints.yaml` are computed by the CLI
   without the daemon (`endpoints.For`), and they must come out the same as what
   the daemon serves.
5. **The module is told what it got, and builds every URL from that.** Names can
   be refused (machine not set up, a name held by someone else, the person turned
   it off). The module must never assume; it reads the grant and degrades to
   addresses. This replaces the string-patching of connection strings.
6. **Every listener goes through doze's proxy.** A request to any route, any
   port, any subdomain wakes a sleeping instance and is counted for idle time,
   the same as a connection to the main port today.
7. **The person can always say no.** Every opt-in has a config switch to turn it
   off for one instance (`routes = false`).

## The model

Two ideas: listeners, and routes for HTTP listeners. There is no alias: the person names the instance, so an instance called `aws` simply is `aws.<stack>.doze`.

### Listeners (every module has at least one)

A listener is one port the instance serves, and the protocol on it.

| Field | Meaning |
|---|---|
| `Name` | The listener's id within the instance: `main`, `registry`, `console`, `smtp`. |
| `Port` | The port clients use on the instance's address (the public port). The backend side stays as today: a socket or a private port behind the proxy. |
| `Protocol` | `tcp` or `http`. Only `http` can be fronted or routed by host. |

Today's single endpoint is a module with one `tcp` listener named `main`; that
is the default when a module declares nothing, so every existing module keeps
working unchanged. Listeners are what `networking.md`'s "more than one port on a
service" (D6, the console port) needs anyway; this gives modules the same thing
on purpose rather than through a `console = "http://…"` string.

TCP listeners keep their port on the instance's address: `db.shop.doze:5432`,
`events.shop.doze:9092`. They are never port-less and never routed by host;
plain TCP carries no host to route on.

### Routes (opt in: HTTP listeners only)

A route says: requests for this host shape go to this HTTP listener, through the
front door, without a port.

| Shape | Written | Example for instance `cloud` in stack `shop` |
|---|---|---|
| The name and everything under it | `@` | `aws.shop.doze`, `bucket.s3.eu-west-1.aws.shop.doze` |
| One child (and everything under it) | `registry` | `registry.events.shop.doze` |
| A sibling with a prefix | `sync-@` | `sync-aws.shop.doze` |

- Most specific wins, which the front door already does: Kafka can send `@` to
  its console and `registry` to the schema registry.
- How doze carries each shape, with what doze-names already has:
  - `@` is the instance's own name with its route; every child falls through to
    it by the subtree rule, so it claims nothing more.
  - A named child (`registry`) is claimed as a twin of the instance's lease, at
    the same address, with its own route; most specific wins, so it takes
    `registry.events.shop.doze` and everything under it, and the rest of the
    subtree keeps the parent's route.
  - A sibling (`sync-@`) is outside the subtree, so it is claimed as a twin too.
  - Twins are released with the instance's lease (`Lease.Twin`, which exists and
    already does this for app domains).
- Sibling prefixes are the one shape that can collide inside a stack (`sync-` +
  `cloud` against an instance named `sync-cloud`). doze checks that when the
  config loads, and the error names both.
- An HTTP listener with no route is reached on its port only
  (`http://events.shop.doze:8081`), which is what most HTTP things want.

## Who holds the ports

doze holds every public port, as it holds a module's main port today: it
listens on each, wakes the instance on a connection, and forwards to the
listener's backend. Main's backend is the socket `BackendSocket` names; every
other listener's is `<listener>.sock` in the instance's socket directory, given
to the module in the grant (`Backend`). So opening the console of a sleeping
Kafka starts it, which today it does not (the module binds its console itself).
The main listener's port is the person's (`port = …`, or the module's default):
a module leaves it 0 and doze puts it there; another listener on that port is a
configuration error naming both.

## What the module is told back (the grant)

At provision, spawn and converge, `Instance` carries what doze actually set up:

| Field | Example | Empty when |
|---|---|---|
| `Host` | `cloud.shop.doze` | names are off or the machine is not set up |
| `Listeners[name].Addr` | `cloud.shop.doze:4566`, or `127.0.0.1:41233` without names | never: there is always an address |
| `Listeners[name].Backend` | `/…/run/events/console.sock` | never: where the module listens for it |
| `Listeners[name].URL` | `http://cloud.shop.doze` (fronted), `http://cloud.shop.doze:4566` (not) | not an HTTP listener |
| `Routes` | the granted shapes, as full host patterns | none granted |

`ConnString` is computed from the grant, so the module writes
`postgres://…@db.shop.doze:5432/…` itself and doze stops rewriting URLs.
The Kafka module advertises `Listeners["main"].Addr`, so clients follow the name.

## What it looks like for the four cases that matter

| Module | Declares | Gets |
|---|---|---|
| postgres | nothing (the default) | `db.shop.doze:5432`. Exactly today. |
| kafka | `main` tcp 9092; `registry` http 8081 with route `registry`; `console` http with route `@` | `events.shop.doze:9092` advertised as the broker; `http://registry.events.shop.doze`; `http://events.shop.doze` is the console. |
| aws (instance `aws`) | `main` http 4566 with routes `@`, `sync-@` | `http://aws.shop.doze`, every SDK subdomain under it, and Step Functions' sync calls. `awsingress.go` and `config/aws.go` are deleted. |
| mailpit-like | `smtp` tcp 1025; `ui` http 8025 with route `@` | `mail.shop.doze:1025` for the app, `http://mail.shop.doze` for a person. |

## The contract (doze-sdk)

No change is needed in doze-names: twins at the same address, routes on entries,
subtree lookup and most-specific-wins all exist.

One optional capability, in the style of the others (`PortBinder`, `Attributer`):

```go
// Networked is implemented by a module that serves more than one port, or
// wants HTTP names. Without it, an instance has one tcp listener, "main", on
// its declared port, and no routes. Network must be a pure function of inst:
// the CLI calls it without the daemon to print endpoints.
type Networked interface {
    Network(inst Instance) Network
}

type Network struct {
    Listeners []Listener
}

type Listener struct {
    Name     string   // "main", "registry", "console"
    Port     int      // public port; 0 for main, whose port is the declared one
    Protocol Protocol // TCP or HTTP
    Routes   []string // HTTP only: "@", "<label>", "<prefix>@"
}

// Grant is what doze set up, carried on Instance.
type Grant struct {
    Host      string
    Listeners map[string]GrantedListener
    Routes    []string // full host patterns
}
```

Over the plugin boundary it is one more advertised capability (`capNetworked`)
and one RPC, `Network(InstanceRequest) → Network`, plus the `Grant` on the
`Instance` message that already travels on every call. A module built against an
older SDK does not advertise it and gets the default.

## Rules doze enforces (config load, before anything runs)

- A route's shape is one of the four forms; anything else is refused with the
  form it should take.
- `Routes` on a `tcp` listener: refused (a module bug, named as such).
- Two listeners on one port: refused.
- A sibling prefix that equals another instance's name in the stack: refused,
  naming both.
- A port another instance in the stack uses on `127.0.0.1` (no machine setup):
  the error that exists today, per listener.

## The person's switches (HCL)

```hcl
module aws "cloud" {
  routes = false  # no port-less routes; reach it on its ports
}
```

Defaults are the module's ask. The person can only take away, never add a shape
the module did not declare; anything more is an app domain, which already exists.

## What this removes

- `doze/internal/daemon/awsingress.go`, `internal/config/aws.go`
  (`AWSUnifiedType`, `AWSEndpoint`, `AWSHost`) and the `decl.Type == "aws"`
  branches in `domains.go` and `endpoints.go`.
- The connection-string rewrite in `endpoints.go`.
- With the standalone tools no longer naming themselves, the reason for three
  writers of `names.json`: doze becomes its only writer, which ends the version
  coupling (`networking.md` rules 1–5, N8, D10).

## Plan

Six steps, each a pull request (or one per repo) that leaves every stack working.
A step starts when the one before it has merged and its CI is green.

### 1. doze-sdk: the contract

- `engine.Networked`, `Network`, `Listener`, `Protocol`, and `Grant` on
  `Instance`, as above.
- The plugin protocol: the `capNetworked` capability, the `Network` RPC, and the
  grant on the `Instance` message. A module that does not advertise the
  capability gets the default (one `tcp` listener, `main`, on its declared port).
- `enginetest.CheckNetwork(drv, inst)`: the declaration is valid (route shapes,
  no routes on `tcp`, no two listeners on one port) and pure (two calls, same
  answer).
- Tests: the protocol round trip, with and without the capability; the checker
  refuses each invalid form.

Done when: a release of doze-sdk with the contract, used by nothing yet.

### 2. doze: everything built from `Network`

- The bind plan, claims and front-door routes come from each instance's
  `Network` (the default for modules without it): one address per instance,
  every listener's port on it, a twin claim for each named child and sibling, a
  route for each HTTP route.
- The grant is filled and sent with every call that carries `Instance`.
- `endpoints.For` and `doze env` read the grant through `ConnString`; the
  `ip:port` → `name:port` rewrite in `endpoints.go` goes.
- The config rules (route forms, sibling against instance names, ports) at load,
  and `routes = false` in HCL (not `front`, which was once a service argument
  that did nothing and is refused with that message).
- Every listener goes through the proxy, so any route or port wakes the instance.
- The AWS special case stays in this step, so nothing changes for aws yet.
- Tests: a golden test over the fixture stacks: `endpoints.yaml` and `doze env`
  are byte-identical before and after for every module that does not implement
  `Networked`. A fake module with two HTTP listeners, a child and a sibling route:
  each name resolves, each route reaches its port through the front door, and a
  request to a sleeping instance wakes it.

Done when: doze on `main` builds every name from the contract, and existing
stacks are unchanged.

### 3. aws module

- doze-aws `module/`: declare `main` (http, its port) with routes `@` and
  `sync-@`; build URLs from the grant.
- doze: delete `awsingress.go`, `config/aws.go` and the `decl.Type == "aws"`
  branches, once the aws module with `Networked` is the one doze's index
  resolves.
- Docs and examples name the instance `aws` (doze-examples' `larder` declares
  `module aws "cloud"` today, which would answer at `cloud.<stack>.doze`).
- Tests, end to end through `aws.<stack>.doze`: an S3 request by virtual host,
  `StartSyncExecution` from an SDK with default settings, the console.

### 4. kafka module

- doze-kafka `module/`: declare `main` (tcp 9092), `console` (http, route `@`)
  and `registry` (http, route `registry`); advertise the granted address of
  `main`, so a client bootstrapped on the name is sent back to the name.
- Tests: a client bootstrapped on `events.<stack>.doze:9092` gets that name in
  Metadata; `http://events.<stack>.doze` is the console;
  `http://registry.events.<stack>.doze/subjects` answers.

### 5. Standalone doze-kafka and doze-aws without doze-names

- doze-kafka: the default is `127.0.0.1:9092`, console and registry on their
  usual ports; `--listen 127.0.0.1:0` stays the way to run two at once. Remove the
  zone, `dns-setup` and the sudo prompt; `doctor` and `env` say addresses; the
  `names` CI job goes; docs (about 70 mentions) and the CHANGELOG.
- doze-aws: the default is `127.0.0.1:4566`; S3 path-style and the Step
  Functions host prefix are documented as client settings for standalone use;
  `apply` and `export` find the server by its address; `COMPATIBILITY.md`'s
  promise of `aws.<instance>.doze` is withdrawn (it is v0.3.0, nothing to keep
  compatible with); docs.
- The website's tool docs follow (they are pulled from the tool repos).
- Tests: each binary starts with no machine setup, writes nothing under
  `~/.doze`, and binds only loopback.

### 6. doze-names into doze

- Move the package into doze (see below), with its tests and its macOS end-to-end
  job; archive the doze-names repo.
- Rewrite what says the zone is shared with the standalone tools: this
  section's predecessor in `networking.md`, `doze dns-setup`'s help, `doze
  doctor`, the website's CLI reference.
- `names.json`'s format version and the peer code stay as they are in this step;
  simplifying them is its own change, after.

## doze-names after this

It should not stay a separate repo. It was split out so three programs could
share the zone as peers; after step 5 doze is the only program that uses it.

| Kept separate | Folded into doze |
|---|---|
| Every change to the registry, the resolver or the front door is two pull requests, a tag and a version bump. | One pull request; the format of `names.json` and the code that reads it change together. |
| Version skew between its users is possible (today three pins differ, and fixes like `heal.go` exist because of it). | Not possible: one binary. |
| Its own CI, including the macOS end-to-end job. | The same jobs, in doze's CI. |
| Reusable by another program. | Nothing else needs it; the desktop app is part of doze. |

Where: `doze/internal/names`, unless something outside doze's module must
import it, in which case `doze/names`. History can be kept with `git subtree`
or `git filter-repo`. It moves last (step 6), because until step 5 the standalone
tools still import it from its own repo.

## Left out on purpose

- **Host routing for TCP.** Plain TCP carries no host. TLS with SNI could, but no
  module needs it, and it would mean doze terminating or peeking TLS for
  databases. A TCP listener gets its port on the instance's address, always.
- **Names a module invents at runtime** (a bucket created later, a queue). The
  `@` shape already covers them, since it includes everything under the name;
  nothing needs registering per resource.
- **HTTPS for module routes.** Later, with the decision about trusting doze's CA.

## Decided

- No alias: the instance name is the address; name the instance `aws` to get
  `aws.<stack>.doze`.
- Kafka's console is the instance name (`http://events.shop.doze`); the schema
  registry is the child `registry`.
- HTTPS for module routes comes later.
