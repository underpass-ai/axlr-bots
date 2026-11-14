# Runbook: extended observability

Per-bot metrics, JSON logs and one trace per task, for a fleet on Kubernetes. Every layer is optional and off by default. Nothing here is needed to run a bot; it is what lets you answer "which task, on which bot, is slow, failing, waiting or expensive" without opening pods.

## When you need it

| Setup | Advice |
|:--|:--|
| One host, one bot, systemd or [compose](control-plane-compose.md) | Text logs and `axlr-bots task list` are enough. Leave it off. |
| One host, a few bots | The overview metrics ([control plane on Kubernetes](control-plane-kubernetes.md#observability)) and `logFormat: json` are cheap. Optional. |
| **Several bots in Kubernetes**: control plane in the cluster, nodes as [sidecars](sidecar.md) in other pods | **Recommended: turn on every layer.** |

Why, with several bots in the cluster:

| Problem | What you see without it | What you see with it |
|:--|:--|:--|
| Several tasks run at once, on nodes in different pods | Interleaved lines in many `kubectl logs` | One query on `correlationId` across the control plane and every node |
| A task is slow | A start line and an end line | Queue, execution, approval wait and tool time apart, per bot |
| A bot waits for a person | Nothing: a wait writes no log line | `WaitingApproval` count and `axlr_bots_approval_wait_seconds` |
| Cost | A total per day | Cost and tokens per bot, and executions whose cost is unknown |
| A tool call fails inside a run | A failed task, maybe | The tool span with its error text |

## Layers

| Layer | Switch | Needs | Detail |
|:--|:--|:--|:--|
| JSON logs, to Loki | `logFormat: json`; `--log-format json` on `axlr-bots serve` and `axlr-node` | a log collector that ships pod logs | [Log fields](control-plane-kubernetes.md#log-fields) |
| Metrics per bot | `metrics.enabled`, `metrics.podMonitor.enabled`; `--metrics-listen` | Prometheus Operator for the PodMonitor | [Task metrics](control-plane-kubernetes.md#task-metrics-per-bot) |
| Traces per task | `tracing.otlpEndpoint`, or `tempo.enabled`; `--otlp-endpoint` | an OTLP/HTTP endpoint (port 4318) | [Tracing](../architecture/tracing.md) |
| Dashboard | `grafanaDashboard.enabled` | Grafana with its dashboard sidecar | [The dashboard](#the-dashboard) |

## Turn it on

### The control plane

`values-observability.yaml`:

```yaml
logFormat: json

metrics:
  enabled: true
  podMonitor:
    enabled: true
    labels:
      release: kube-prometheus-stack   # must match the Prometheus' podMonitorSelector

tracing:
  otlpEndpoint: ""                     # your collector, e.g. http://otel-collector.observability:4318
tempo:
  enabled: true                        # or false, with tracing.otlpEndpoint set

grafanaDashboard:
  enabled: true
```

```bash
helm upgrade axlr-bots charts/axlr-bots-control-plane -n $ns --reuse-values -f values-observability.yaml
kubectl -n $ns rollout status deploy/axlr-bots-axlr-bots-control-plane
```

The pod restarts: nodes reconnect on their own. Counters start at zero.

| Value | Default | Notes |
|:--|:--|:--|
| `logFormat` | `text` | `json` sets `AXLR_BOTS_LOG_FORMAT` |
| `metrics.enabled`, `metrics.port` | `false`, `9464` | plain HTTP `/metrics` and `/healthz` on the pod, on no Service |
| `metrics.podMonitor.enabled`, `.labels`, `.interval`, `.scrapeTimeout` | `false`, none, `30s`, `10s` | needs the PodMonitor CRD |
| `tracing.otlpEndpoint` | empty | empty and no Tempo: no exporter exists |
| `tempo.enabled`, `tempo.retention`, `tempo.persistence.*` | `false`, `168h`, off | one pod; with it on and no endpoint, the control plane sends to it |
| `grafanaDashboard.enabled`, `.namespace`, `.labels` | `false`, release namespace, `grafana_dashboard: "1"` | a ConfigMap for Grafana's sidecar |

### The nodes (sidecars)

The node logs are the ones that carry `node`, `execution` and `correlationId` from the run. Give the node chart the variable:

```yaml
# values-observability.yaml of the node release
sidecar:
  env:
    - {name: AXLR_NODE_LOG_FORMAT, value: json}
```

```bash
helm upgrade demo charts/axlr-bots-node -n $ns --reuse-values -f values-observability.yaml
```

Keep your other `sidecar.env` entries in the list: Helm replaces a list whole. On a host node, `axlr-node --log-format json`. See [sidecar values](sidecar.md#values).

### Loki and Tempo

| Piece | What to do |
|:--|:--|
| Loki | Nothing is pushed from the pods. Run a collector (Promtail, Alloy, the Grafana agent) that ships pod logs, and give Grafana a Loki data source. |
| Tempo of the chart | Add a Tempo data source in Grafana: `http://axlr-bots-axlr-bots-control-plane-tempo:3200` (`<fullname>-tempo`). |
| Your own collector | Set `tracing.otlpEndpoint` to its OTLP/HTTP address, port 4318. |

## Check it works

```bash
# 1. Metrics: the series exist (a bot shows up after its first task)
kubectl -n $ns port-forward deploy/axlr-bots-axlr-bots-control-plane 9464:9464 &
curl -s localhost:9464/metrics | grep axlr_bots_task_
curl -s localhost:9464/healthz
kill %1

# 2. Prometheus: the target is up
#    up{pod=~"axlr-bots-.*"} == 1   and   axlr_bots_overview_up == 1
```

| Check | Query | Expect |
|:--|:--|:--|
| Logs | `{namespace="$ns"} \| json \| correlationId="<id>"` in Loki | lines from the control plane and from the node, as JSON |
| Trace | Grafana Explore, Tempo, TraceQL: `{ span.axlr.correlation_id = "<id>" }` | one trace, root `axlr.task` |
| Trace by task | `{ span.axlr.task = "<task id>" }` | the same trace |
| Dashboard | *AXLR Bots · control plane* (uid `axlr-bots-control-plane`) | loads with both data sources picked at the top |

To make a task to look at: `axlr-bots task submit my-bot --objective "say hello" --key check-1 --correlation check-1`, then search `check-1`. A trace appears when the task **ends**.

## The dashboard

*AXLR Bots · control plane* (uid `axlr-bots-control-plane`) has only template variables, so it fits any fleet:

| Variable | Is |
|:--|:--|
| `$prometheus`, `$loki`, `$tempo` | the data sources; Tempo is optional and nothing else uses it |
| `$namespace` | the release's namespace |
| `$bot` | the bots, from `axlr_bots_tasks_finished_total`; several or *All* |
| `$container`, `$component`, `$task` | narrow the logs |
| `$correlation` | a correlation id (a regular expression; default `.*`) for the logs panel |

Rows: **Estado** and **Tareas** (the fleet), **Tareas por bot** (p50 and p95 of task, queue, execution and approval wait; tasks finished by state; failures; cost and tokens per bot over 24 hours; executions of unknown cost; tool calls by status and their p95), **API** (the gRPC operator API by service, method and code with its latency; the HTTP row is only the node protocol and the webhooks) and **Logs** (`| json | bot=~"$bot" | correlationId=~"$correlation"`). The dashboard link *Trazas (Tempo) de la correlación* opens Tempo in Explore with the `$correlation` value; without a Tempo data source it opens an empty Explore and the rest works.

## Follow one task

Start from a correlation id, or a task id from `axlr-bots task list`.

```bash
# every line, control plane and node, oldest first
# LogQL:  {namespace="$ns"} | json | correlationId="check-1"
# only the task, any correlation:
# LogQL:  {namespace="$ns"} | json | task="<task id>"
```

The whole activity of a task, one line per step, from the operator API over gRPC (`ActivityService`):

```bash
axlr-bots task activity <task id>              # the tree: delegated tasks, each execution with duration and cost, the tools with their state
axlr-bots task activity <task id> --depth 2    # how deep the delegated tasks go
axlr-bots task activity <task id> --watch      # follow it live while it runs
axlr-bots task activity <task id> --json       # the same as the machine reads it
```

Against a control plane in the cluster add `--control <address>:7880 --token-file … --cert-file … --key-file … --ca-file …` ([command reference](../reference/cli.md)). The desktop panel shows the same tree on a task's **Activity** page ([panel](../../panel/README.md#screens)): tools with ✓ and ✕, how long each took, the bots it asked, and the call that waits for a person.

Then the trace: `{ span.axlr.correlation_id = "check-1" }`. Reading it:

| Span | Tells you |
|:--|:--|
| `axlr.task` | creation to end, `outcome`, `axlr.bot` |
| `axlr.execution` | one run: `axlr.node`, tokens, `axlr.cost_usd` |
| `axlr.tool <name>` | one tool call, `outcome`, error text when it failed |
| `approval.wait` | the wait for a person, with its outcome |

## Diagnose

In each query replace `my-bot` and the window.

### A task is slow: queue, run, approval or tool?

| Question | PromQL |
|:--|:--|
| Whole task, p95 | `histogram_quantile(0.95, sum by (le) (rate(axlr_bots_task_duration_seconds_bucket{bot="my-bot"}[1h])))` |
| Waiting for a node to start it (queue) | `histogram_quantile(0.95, sum by (le) (rate(axlr_bots_task_queue_seconds_bucket{bot="my-bot"}[1h])))` |
| Running | `histogram_quantile(0.95, sum by (le) (rate(axlr_bots_execution_duration_seconds_bucket{bot="my-bot"}[1h])))` |
| Waiting for a person | `histogram_quantile(0.95, sum by (le) (rate(axlr_bots_approval_wait_seconds_bucket{bot="my-bot"}[1h])))` |
| Tool calls | `histogram_quantile(0.95, sum by (le) (rate(axlr_bots_tool_call_duration_seconds_bucket{bot="my-bot"}[1h])))` |

The largest one is the answer. A high queue means no free node for the bot: `axlr-bots node list`. For one task, the trace shows the same split as bars; the longest `axlr.tool` span names the tool.

### A task fails

| Question | Query |
|:--|:--|
| Failures per bot | `sum by (bot) (increase(axlr_bots_tasks_finished_total{state="Failed"}[24h]))` |
| Expired or cancelled | `sum by (bot, state) (increase(axlr_bots_tasks_finished_total{state=~"Expired\|Cancelled"}[24h]))` |
| Tool calls that failed | `sum by (bot) (increase(axlr_bots_tool_calls_total{status="failed"}[24h]))` |
| The error line (LogQL) | `{namespace="$ns"} \| json \| bot="my-bot" \| level="ERROR"` |
| The failed tool span (TraceQL) | `{ name =~ "axlr.tool.*" && status = error && span.axlr.bot = "my-bot" }` |
| Failed tasks of a bot (TraceQL) | `{ span.axlr.bot = "my-bot" && span.outcome = "Failed" }` |

### Where is it waiting

| Question | Query |
|:--|:--|
| Tasks waiting now | `sum by (state) (axlr_bots_tasks{state="WaitingApproval"})` |
| How long people take | `histogram_quantile(0.95, sum by (le, outcome) (rate(axlr_bots_approval_wait_seconds_bucket{bot="my-bot"}[24h])))` |
| Waits that expired | `sum by (bot) (increase(axlr_bots_approval_wait_seconds_count{outcome="expired"}[24h]))` |
| The wait of one task (TraceQL) | `{ name = "approval.wait" && span.axlr.task = "<task id>" }` |

A wait ends in `approved`, `denied`, `expired` or `cancelled`. See [approvals](approvals.md) for who may decide and the expiry.

### Cost per bot

| Question | PromQL |
|:--|:--|
| Dollars per bot, last day | `sum by (bot) (increase(axlr_bots_cost_usd_total[24h]))` |
| Tokens per bot and kind | `sum by (bot, kind) (increase(axlr_bots_tokens_total[24h]))` |
| Cost that is not in the total | `sum by (bot) (increase(axlr_bots_executions_cost_unknown_total[24h]))` |

`executions_cost_unknown_total` counts runs that ended before they said what they used. A late report adds that cost to `cost_usd_total` afterwards, so a non-zero value is a bound, not a hole.

## Costs and limits

| Item | Limit |
|:--|:--|
| Cardinality | Labels are only `bot`, `state`, `outcome`, `kind` and `status`. Never a task, execution or correlation id. Series grow with bots, not with work. |
| Binary | OTel adds 4.77 MB to `axlr-bots` (23.9 to 28.7 MB, 20 %) and 21 modules; the HTTP exporter links gRPC anyway. |
| Counters | They restart at zero with the process: use `rate` and `increase`, never the raw value. |
| Traces | One per **ended** task. None for a task still running or waiting, for one that ended while the daemon was down, or when it stops between the save and the event. |
| Trace content | The newest 1000 events of the task; progress is bounded (400 events and 256 KiB per execution), so a long run may show fewer tool spans; `approval.wait` is the task's last wait; late usage after a cancel is not in it. |
| Privacy | Spans carry the tool name and a short summary, never arguments or results. |
| Metrics port | Plain HTTP, counts only, no secret, on no Service: keep it in the cluster. |
| Tempo of the chart | One pod, `Recreate`, an `emptyDir` unless `tempo.persistence.enabled`. To start with, not for a large fleet: use your own Tempo or collector there. |

Full list: [Tracing](../architecture/tracing.md#limits).

## Turn it off

Set the values back and upgrade:

```yaml
logFormat: text
metrics: {enabled: false, podMonitor: {enabled: false}}
tracing: {otlpEndpoint: ""}
tempo: {enabled: false}
grafanaDashboard: {enabled: false}
```

```bash
helm upgrade axlr-bots charts/axlr-bots-control-plane -n $ns --reuse-values -f values-off.yaml
```

With the endpoint empty and no Tempo, no exporter is created and nothing is recorded. Tempo's `emptyDir` goes with its pod; a claim from `tempo.persistence.enabled` stays until you delete it. On the nodes remove `AXLR_NODE_LOG_FORMAT` from `sidecar.env`. Task state is untouched by any of this.
