# Tracing: one OpenTelemetry trace per ended task

`axlr-bots serve --otlp-endpoint http://tempo:4318` (or `AXLR_BOTS_OTLP_ENDPOINT`) sends, for every task that ends, one trace to an OTLP/HTTP endpoint. Empty (the default): no exporter is created and nothing is recorded. The chart sets it with `tracing.otlpEndpoint`, or points it at its own optional Tempo (`tempo.enabled`).

## The trace

```text
axlr.task                      created .. ended         bot, task, correlation, outcome
├─ axlr.execution              started .. ended         node, outcome, tokens, cost
│  └─ axlr.tool <name>         tool.started .. ended    status; error text when it failed
├─ axlr.execution              (the one that carried the decision)
│  └─ axlr.tool <name>
└─ approval.wait               Since .. ClosedAt        outcome: approved, denied, expired, cancelled
```

Resource `service.name=axlr-bots-control-plane`. Attributes: `axlr.bot`, `axlr.task`, `axlr.execution`, `axlr.node`, `outcome`, `axlr.correlation_id`, and on an execution `axlr.cost_usd` (absent when the cost is unknown), `axlr.tokens.{prompt,cached,completion,reasoning}`, `axlr.requests`, `axlr.tool_calls`.

**Trace id.** A correlation id is free text (up to 128 characters), not a trace id. The trace id is the first 16 bytes of the SHA-256 of `correlation:<text>`, or of `root-task:<id>` of the chain's first task when there is no correlation id. The original text stays on the spans as `axlr.correlation_id`. Span ids are derived the same way from the task, execution, tool index or approval, so a rebuilt trace has the same ids.

**Delegation.** A task made by another (`domain.TaskLineage`) lives in its parent's trace and hangs under the parent's execution span that delegated it, when the parent task is still stored. When it is not, the task takes the trace id its own chain gives and carries a span link to the span the parent had.

## Why built after the fact

Tasks outlive the process: an approval can wait for hours, and the daemon restarts. Nothing is kept open in memory. `adapters/tracing` is a decorator of `application.EventPublisher`, wired in `adapters/daemon`. When the event that ends a task passes through (completed, failed, cancelled, preempted, expired), it first hands the event on, then, in the background, reads the task, its executions and its timeline (`bot.task.progress` items, paired by call id) through a three-method port and creates every span with its stored start and end timestamps. A trace that cannot be built is logged and never fails the event. The daemon waits for traces under way and flushes the exporter on the way out.

## Limits

- A task has a trace once it ends. Nothing is exported for a task still running or waiting.
- A daemon that stops between the save of the ended task and its event loses that task's trace.
- `approval.wait` shows the task's last wait: the task keeps one.
- A tool call with no `tool.ended` ends with its execution (`axlr.tool_unfinished=true`). Progress is bounded (400 events and 256 KiB per execution), so a very long run may show fewer tool spans than it made.
- The trace reads the newest 1000 events of the task, so a task with very many executions can lose its oldest tool spans.
- Usage that arrives after a cancel (a late report) is not in the trace.
- A task that ended while the daemon was down has no trace.
- Spans carry the tool name and the short summary the timeline keeps, never arguments or results.

## Why OTLP/HTTP

One request per batch over the standard library's client; it passes through any proxy and needs no stream to keep healthy. Tempo and every collector accept it on 4318.

It is not cheap in dependencies: the HTTP exporter's configuration package imports gRPC, so the binary links it anyway. Measured on a default `go build ./cmd/axlr-bots` (no tags, no strip flags):

| | before | after | added |
|---|---|---|---|
| modules in `go.sum` | 27 | 48 | 21 |
| linked packages | 322 | 459 | 137 (65 of gRPC) |
| binary | 23 890 718 B | 28 660 589 B | 4.77 MB (20 %) |

## Tempo in the chart

`tempo.enabled: true` renders a Deployment (one pod, Recreate, pinned image), a ClusterIP Service (4318 for OTLP, 3200 for Grafana's Tempo data source) and a ConfigMap (monolithic, local blocks, `tempo.retention`). Traces are on an emptyDir unless `tempo.persistence.enabled`. It is on no Ingress. With it on and `tracing.otlpEndpoint` empty, the control plane sends to it.
