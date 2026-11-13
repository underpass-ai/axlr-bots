# axlr-bots-control-plane

The AXLR Bots control plane (`axlr-bots serve`) as one pod on Kubernetes: the node protocol (mutual TLS) on 7878, the operator API (bearer token) on 7879, and the state (bots, tasks, events, the fleet CA and its key, the socket) on a persistent volume. The runbook is [`docs/runbooks/control-plane-kubernetes.md`](../../docs/runbooks/control-plane-kubernetes.md): a first install, moving a running control plane's state in, rollback and update.

## What it renders

| Object | |
|---|---|
| Deployment | one replica, `strategy: Recreate` (the state lock refuses a second `serve`), user 65532, read-only root, `/tmp` on a memory `emptyDir`, probes `axlr-bots health` |
| PersistentVolumeClaim | `<release>-state`, kept on `helm uninstall` (`helm.sh/resource-policy: keep`); or `persistence.existingClaim` |
| Service | ports `node` 7878 and `api` 7879, passed through (TLS ends in the pod); `LoadBalancer` for nodes outside the cluster |
| ServiceAccount | no API rights, no token mounted |
| PodMonitor | with `metrics.podMonitor.enabled`: Prometheus reads `/metrics` on the pod's `metrics` port (9464), which is on no Service |
| ConfigMap | with `grafanaDashboard.enabled`: the dashboard, for Grafana's sidecar (label `grafana_dashboard: "1"`) |

No Ingress: the node protocol authenticates nodes by their client certificate, so a proxy that ends TLS would break it. Expose it as a `LoadBalancer` (MetalLB, a cloud load balancer) or a `NodePort`.

Several bots in the cluster? Turn on metrics, JSON logs and traces: [`docs/runbooks/observability.md`](../../docs/runbooks/observability.md).

## Values that matter

| Value | |
|---|---|
| `image.tag` | the release without `v` (`0.4.0`); empty takes `appVersion`. The panel compares nodes and bots with it |
| `serverNames` | the addresses and DNS names nodes and operators dial; the certificate carries them and the Service's cluster names. Required with a `LoadBalancer` |
| `apiURL` | `https://<address>:7879`, the base approval notifications link to |
| `apiToken.existingSecret` | the operator token as a Secret (`token`), so the panel and the CLI outside the pod can be given it; without it the daemon writes `<state>/api.token`, which a distroless image gives no way to read |
| `env.existingSecret` | every key as a variable: `OPENROUTER_API_KEY` and `TYPESAFE_API_KEY` for the model and Jev proxies |
| `bus.kind`, `bus.nats.*` | `direct`, or `nats` with `tls://` and a Secret holding `tls.crt`, `tls.key` (from `axlr-bots ca issue-client --name control-plane`) and `ca.crt` (the broker's CA) |
| `ca.init` | true for the first start of an empty state only: an init container runs `ca init`, which fails when a CA is there |
| `persistence.existingClaim` | a claim you filled with an existing state |
| `metrics.enabled`, `metrics.podMonitor.*` | `/metrics` and `/healthz` in plain HTTP on 9464: the overview's counts (`axlr_bots_tasks`, `axlr_bots_nodes`, cost today…), the requests per listener and route, Go and process. The PodMonitor's `labels` must match the Prometheus' `podMonitorSelector` |
| `logFormat` | `text`, or `json` for Loki: level, `component`, and, when a line is about one, `bot`, `node`, `task`, `execution` and `correlationId` as fields |
| `grafanaDashboard.*` | the dashboard (`files/grafana-dashboard.json`); `namespace` when the sidecar does not look in every namespace |
| `extraContainers`, `extraVolumes`, `service.extraPorts` | input adapters in the same pod, reaching the daemon through `/var/lib/axlr-bots/axlr-bots.sock` (volume `state`) |

`values.schema.json` checks the types; `values.example.yaml` is a load balancer with a fixed address and a broker.

## What the pod cannot do

A notification target of type `command` runs its program inside the container, which has no shell and none of a host's scripts: move those to `webhook` targets first ([approvals](../../docs/runbooks/approvals.md)). The control plane's own runs (a task no node can take) see only the image's `axlr-tui`; place bots on nodes.
