# Research behind the graph and context design

Six reports written on 2026-10-20 to settle what the graph and context layer
must hold and how to build it without installs, failures or heavy machines. The
synthesis and the decisions it asks for are in
[../context-system.md](../context-system.md).

| Report | Question |
|---|---|
| [1-agent-context-needs.md](1-agent-context-needs.md) | What context makes coding agents better, where they fail without it, what hurts them |
| [2-precision-sources.md](2-precision-sources.md) | Language servers on demand against SCIP, stack-graphs and syntax only, per language, with measurements |
| [3-breadth-speed-memory.md](3-breadth-speed-memory.md) | gotreesitter against C, `go/types` loading modes on doze and doze-aws, incremental designs, change detection |
| [4-stack-join.md](4-stack-join.md) | Finding resource use and service-to-service calls in code, and what doze's proxy and servers can observe |
| [5-doze-capabilities.md](5-doze-capabilities.md) | What doze already has that recall can lean on, with file references |
| [6-answer-assembly.md](6-answer-assembly.md) | Anchoring, ranking, packing to a budget, MCP tool design and adoption |

Each report lists the pages it opened. Numbers marked measured were run on an
M5 Max (18 cores, 36 GiB, Go 1.27.2). Vendor results are marked as such; none
has been replicated independently.

The programs used for the measurements are in [bench/](bench/). The reports
refer to them by their original scratch paths; the files are the same. Each Go
bench is its own module, so `go build ./...` in doze does not pick them up.
Corpora (hono, pydantic, ripgrep, jq, Kubernetes) were shallow clones and are
not kept.
