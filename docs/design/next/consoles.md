# Contract 5: module consoles

Tier: core for kafka and aws, should for web links, dream for "keep in
doze.hcl". Built in phase 6. Background in
[gap-analysis.md](gap-analysis.md#module-consoles).

## The idea

A module describes its console as data. doze draws it. Every console then
looks and behaves like the rest of the dash, and the dash holds no knowledge
of any particular module.

Today that knowledge is hard-coded in the dash in at least six places, and
the aws page makes its own HTTP calls.

## What already exists

`doze-sdk/engine/admin.go`:

```go
type Admin interface {
    Resources(ctx, inst Instance, ep Endpoint) ([]Resource, error)
    Actions() []Action
    Run(ctx, inst, ep, action, resource, input string) (string, error)
}
type Resource struct { Kind, Name, Status string; Info map[string]string }
type Action   struct { ID, Label, Kind string; Destructive bool; InputHint string }
```

That is already tables and verbs. The kafka page is built on it. Three things
are missing: a description of how to lay the rows out, a way to stream, and a
console URL.

## The addition

One new optional interface, and one new streaming call.

```go
type Console interface {
    // Describe is static per module version. The host caches it.
    Describe() ConsoleSpec
    // Tail streams rows for a stream panel until ctx ends.
    Tail(ctx, inst Instance, ep Endpoint, panel, selector string, out chan<- Row) error
}

type ConsoleSpec struct {
    Front  string   // "console", "web" or "" for the host default
    WebURL string   // template, e.g. "http://{address}/_console"
    Panels []Panel
}

type Panel struct {
    ID, Title string
    Kind      string   // table | stream | counters
    Source    string   // for table and counters: a Resource.Kind
    Columns   []Column
    Select    string   // for stream: the table panel whose selection picks what to tail
}

type Column struct {
    Key, Title string  // Key is "name", "status" or an Info key
    Align      string  // left | right
    Role       string  // "", "count", "rate", "lag", "state", "time", "code"
}

type Row struct { Cells map[string]string; Tone string } // Tone: "", ok, warn, bad
```

- **table**: rows from `Resources` of one kind. The host polls while the panel
  is visible.
- **counters**: a row of small totals, one per resource of a kind.
- **stream**: rows pushed by `Tail`. The kafka message tail and the aws wire
  are both streams.
- `Role` lets the host format and colour without knowing the module. `rate`
  is computed by the host from successive `count` values, which is what the
  kafka page does client-side today.
- Actions keep working as they do. The host shows them in the key bar and the
  palette for the selected row; `Destructive` drives the confirmation.

### Protocol

- `doze-sdk/plugin/proto/engine.proto` gains `DescribeConsole` (unary) and
  `Tail` (server-streaming).
- `ProtocolVersion` goes from 1 to 2. A module built against version 1 has no
  console and gets the host default front for its kind.
- doze-modules is on doze-sdk v0.6.0 and doze is on v0.7.0. They converge on
  the new version in this phase.

### Through the daemon

The dash never talks to a module or a service directly. It subscribes to the
`console` topic for one instance ([daemon-api.md](daemon-api.md)), and the
daemon calls the plugin. A console only costs anything while someone is
looking at it.

## The two consoles

| | kafka | aws |
|---|---|---|
| Tables | topics (partitions, messages, rate), consumer groups (topic, lag, members) | resources (type, name, now) |
| Counters | none | services in use |
| Stream | tail of the selected topic | the wire: every API call |
| Web | the console the module already serves on port + 1 | `/_console` |
| Today | `Resources` via plugin; rates computed in the dash | direct HTTP to `/_console/api/glance` from the dash |
| Work | add `Describe`, add `Tail` over the broker | move the glance call into the module, expose it as resources and a stream |

The aws "caller" column depends on caller identity and is dream tier
([networking.md](networking.md#caller-identity)). The wire ships without it.

## Fronts

The host picks the first tab:

| Kind | Front | Source |
|---|---|---|
| app | logs | host |
| task | last run | host |
| module with a `ConsoleSpec` | whatever `Front` says, default console | module |
| module without one | overview | host |
| service with `console =` | web | config |
| service | logs | host |
| anything with `front = "…"` in config | that | config wins |

The overview tab (address, holders, vitals, recent journal) is drawn by the
host for every kind and needs nothing from the module.

## Declared versus made at runtime

Tier: should for the marker, dream for writing back.

- `Inventory.Objects` lists what the config declares. `Resources` lists what
  exists. A resource that exists but is not declared was made at runtime. The
  host can mark it today with no new interface.
- "Keep in doze.hcl" needs the module to render a resource as an HCL block. It
  would be a new optional call, `RenderObject(kind, name) string`. Not planned
  until the marker has proved useful.

## Web consoles

Tier: should.

- A module sets `WebURL`. A service sets `console = "http://…"`.
- The dash shows the address, and `o` opens it in a browser.
- The address is named and proxied as described in
  [networking.md](networking.md#more-than-one-port-on-a-service), so opening
  it wakes the service.

## Measures

| Measure | How |
|---|---|
| The dash has no module names in it | A test greps `internal/tui` for `"aws"` and `"kafka"`. |
| A version 1 module still loads | Run the fixture stack with one module pinned to the old protocol. |
| A console costs nothing when closed | Daemon CPU and plugin call count with the dash on home versus on the kafka console. |
