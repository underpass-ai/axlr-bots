# Runbook: the control plane on Kubernetes

Run `axlr-bots serve` as a pod from the chart [`charts/axlr-bots-control-plane`](../../charts/axlr-bots-control-plane): a first install, moving a control plane that runs on a host (systemd or [compose](control-plane-compose.md)) into the cluster with its state, going back, and updating. Nodes stay where they are and dial the new address; the [node chart](sidecar.md) is for nodes in pods.

## What changes against a host

| On a host | In the cluster |
|---|---|
| `serve --listen 0.0.0.0:7878 --api-listen 0.0.0.0:7879` | the same ports in the pod, a `Service` in front; nodes outside the cluster need a `LoadBalancer` (or `NodePort`) |
| the certificate names the host's addresses | it names the pod's, plus `serverNames` and the Service's cluster names: put the load balancer's address there |
| `~/.local/state/axlr-bots` | a PersistentVolumeClaim mounted at `/var/lib/axlr-bots`, owned by 65532 |
| `<state>/api.token`, readable on the host | a Secret (`apiToken.existingSecret`): the image has no shell to read the volume |
| env file `~/.config/axlr-bots/env` | a Secret (`env.existingSecret`) for the keys; the bus as `bus.*` values and a Secret for its client certificate |
| the CLI and input adapters on the socket | `kubectl exec deploy/<release> -- axlr-bots …`, or containers in the same pod (`extraContainers`) on `/var/lib/axlr-bots/axlr-bots.sock` |
| targets of type `command` | not possible: move them to `webhook` targets before the move ([approvals](approvals.md)) |

## Before you start

1. Helm 3.8 or newer, and a cluster that pulls `ghcr.io/underpass-ai/axlr-bots:<version>`.
2. A storage class with `ReadWriteOnce`. On a class bound to one node (local-path), the pod always runs on that node.
3. An address nodes reach: a load balancer's (MetalLB: an address from a pool, by annotation) or a node port.
4. Find what only works on a host:

   ```bash
   grep -l '"command"' <state>/bots/*.json <state>/outputs.json
   ```

   Each match is a notification target or an output that runs a host program: change it to a `webhook` target, check a delivery arrives, and only then move.

## A first install (empty state)

```bash
ns=axlr-bots
kubectl create namespace $ns
kubectl -n $ns create secret generic axlr-bots-api-token --from-literal=token="$(openssl rand -hex 32)"
kubectl -n $ns create secret generic axlr-bots-env --from-env-file=<(printf 'OPENROUTER_API_KEY=%s\n' "$KEY")
helm install axlr-bots charts/axlr-bots-control-plane -n $ns \
  --set ca.init=true \
  --set apiToken.existingSecret=axlr-bots-api-token \
  --set env.existingSecret=axlr-bots-env \
  --set service.type=LoadBalancer \
  --set 'serverNames={192.0.2.10}' --set apiURL=https://192.0.2.10:7879 \
  --set-string 'service.annotations.metallb\.io/loadBalancerIPs=192.0.2.10'
helm upgrade axlr-bots charts/axlr-bots-control-plane -n $ns --reuse-values --set ca.init=false
```

The init container writes the fleet CA on the first start; the upgrade removes it, since a second `ca init` fails on a CA that is there.

## Move a running control plane in

The state moves whole: the fleet CA is in it, so every node certificate stays valid and nodes need only the new address. Never start the pod on a copy while the host's daemon still runs: two control planes on copies of one state fire the same timers and run the same tasks.

**1. Before the window.** Nodes on the host that dial the socket (`unix://…`) cannot follow the control plane: enroll each over TLS now, against the host's own TLS address, with `axlr-bots node enroll-token --node-id <id>` and `axlr-node enroll --control https://<host>:7878`, then change its `--control` ([add a host](add-a-host.md)). Their certificates come from the fleet CA, which moves. Input adapters on the socket move into the pod (`extraContainers`) or wait for the operator API.

**2. The Secrets**, from the host's files (nothing is written to disk on the way):

```bash
kubectl -n $ns create secret generic axlr-bots-api-token --from-file=token=<state>/api.token
grep -E '^(OPENROUTER_API_KEY|TYPESAFE_API_KEY)=' ~/.config/axlr-bots/env | kubectl -n $ns create secret generic axlr-bots-env --from-env-file=/dev/stdin
```

With a broker, a Secret with the control plane's client certificate as `tls.crt` and `tls.key` and the broker's CA as `ca.crt`.

**3. A claim and a loader pod** on the node the pod will run on:

```bash
kubectl -n $ns apply -f - <<'EOF'
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: axlr-bots-state, annotations: {helm.sh/resource-policy: keep}}
spec: {accessModes: [ReadWriteOnce], resources: {requests: {storage: 2Gi}}}
---
apiVersion: v1
kind: Pod
metadata: {name: axlr-bots-loader}
spec:
  nodeSelector: {kubernetes.io/hostname: <node>}
  containers:
    - {name: loader, image: busybox:1.37.0, command: [sleep, "3600"], volumeMounts: [{name: state, mountPath: /state}]}
  volumes: [{name: state, persistentVolumeClaim: {claimName: axlr-bots-state}}]
EOF
kubectl -n $ns wait --for=condition=Ready pod/axlr-bots-loader
```

**4. The window.** Stop the host's daemon so nothing writes the state, then copy it in, owned by 65532, without the socket:

```bash
systemctl --user disable --now axlr-bots.service
systemctl --user mask axlr-bots.service        # the node units' Wants= would start it again
tar -C <state> --exclude=./axlr-bots.sock --owner=65532 --group=65532 -cf - . \
  | kubectl -n $ns exec -i axlr-bots-loader -- tar -C /state -xf -
(cd <state> && find . -type f ! -name axlr-bots.sock -print0 | sort -z | xargs -0 sha256sum) > /tmp/host.sums
kubectl -n $ns exec axlr-bots-loader -- sh -c 'cd /state && find . -type f -print0 | sort -z | xargs -0 sha256sum' > /tmp/pod.sums
diff /tmp/host.sums /tmp/pod.sums && echo identical
kubectl -n $ns delete pod axlr-bots-loader
```

**5. Start it** on the claim:

```bash
helm install axlr-bots charts/axlr-bots-control-plane -n $ns \
  --set persistence.existingClaim=axlr-bots-state \
  --set apiToken.existingSecret=axlr-bots-api-token --set env.existingSecret=axlr-bots-env \
  --set service.type=LoadBalancer --set 'serverNames={<address>}' --set apiURL=https://<address>:7879 \
  --set-string 'service.annotations.metallb\.io/loadBalancerIPs=<address>' \
  --set 'nodeSelector.kubernetes\.io/hostname=<node>'
```

**6. Point everything at the address**: each node's `--control https://<address>:7878` (restart it), a node chart's `controlPlane.url` and its NetworkPolicy CIDR, the panel's `api_url`, every client of the operator API. A node that already has a certificate reconnects with it.

Leave the host's state, unit file and binary in place: they are the way back.

## Check

```bash
kubectl -n $ns exec deploy/axlr-bots-axlr-bots-control-plane -- axlr-bots version
kubectl -n $ns exec deploy/axlr-bots-axlr-bots-control-plane -- axlr-bots node list
kubectl -n $ns exec deploy/axlr-bots-axlr-bots-control-plane -- axlr-bots ca show   # the same fingerprint as on the host
curl --cacert ca.crt https://<address>:7879/healthz
curl --cacert ca.crt -H "Authorization: Bearer $(kubectl -n $ns get secret axlr-bots-api-token -o jsonpath='{.data.token}' | base64 -d)" https://<address>:7879/v1/build
```

`running.version` is the image's; every node is `Online` within a heartbeat; a task fired by hand runs on its node and ends.

> Several bots in the cluster? Turn on extended observability: per-bot metrics, JSON logs and a trace per task. See the [observability runbook](observability.md).

## Observability

With `metrics.enabled`, `metrics.podMonitor.enabled` (its `labels` matching the Prometheus' `podMonitorSelector`; kube-prometheus-stack's is `release: <its release>`), `logFormat: json` and `grafanaDashboard.enabled`:

| Piece | Check |
|---|---|
| Prometheus | target `podMonitor/<ns>/<release>-axlr-bots-control-plane` is up; `axlr_bots_overview_up == 1` |
| Loki | `{namespace="<ns>", container="control-plane"} \| json \| level="ERROR"` (any log collector that ships pod logs; nothing in the pod pushes) |
| Grafana | dashboard *AXLR Bots · control plane* (uid `axlr-bots-control-plane`); pick the Prometheus and Loki at the top |

`axlr_bots_overview_up 0` means the state could not be read at that scrape. The `…_today` gauges go back to zero at midnight on the control plane's clock. The node stream and the long poll are counted but not timed.

### Log fields

With `logFormat: json` (`--log-format json` or `AXLR_BOTS_LOG_FORMAT=json` on `axlr-bots serve`; `--log-format json` or `AXLR_NODE_LOG_FORMAT=json` on `axlr-node`) each line is one JSON object: `time`, `level`, `msg`, `component` (the `daemon:`, `node:` or `bus:` prefix of the message) and, when the line is about one of these, the fields below. They are filled from the data the line is about, not parsed from the text; a field that does not apply is not in the line.

| Field | Is | On lines about |
|---|---|---|
| `bot` | the bot id | a timer firing, a run, an approval that expired, a notification, a node's execution |
| `node` | the node id | a node's own lines (register, heartbeat, stream, certificate), an execution a node carries, a push on the bus |
| `task` | the task id | a run, an approval, a notification, a node's execution |
| `execution` | the execution id | a run, a node's execution |
| `correlationId` | the task's correlation id, when it has one | a run, a node's execution |

Lines that name a task or execution only in their text (a message from code that has no subject yet) still get `task` and `execution` pulled out of the text as a fallback, so filter on the fields, not on the text: `{container="control-plane"} | json | bot="<bot>" | level="ERROR"`. The text format is unchanged.

## Back to the host

While nothing has run in the pod yet, or when what ran there may be lost:

```bash
helm uninstall axlr-bots -n $ns                 # the claim stays
systemctl --user unmask axlr-bots.service
systemctl --user enable --now axlr-bots.service
```

and point the nodes back. When the pod has run for a while, its state is the newer one: copy it out of the claim (a loader pod, `tar` the other way, owner the host account) into a **new** directory, check it, and start the service on that directory; never overwrite the original until the new one works.

## Update

```bash
helm upgrade axlr-bots charts/axlr-bots-control-plane -n $ns --reuse-values --set image.tag=<version>
```

`Recreate` stops the old pod before the new one starts: a few seconds without a control plane, which nodes ride out. Take the new chart's values from its release; read its notes for anything the state needs.

## When it does not work

| Symptom | Cause |
|---|---|
| the pod restarts with `the state … is in use` | a second pod on the claim: the Deployment is Recreate; check nothing else mounts it |
| `ca-init` fails with `already initialised` | `ca.init` is still true on a state with a CA: `--set ca.init=false` |
| `serve --listen/--api-listen: …ca…` | an empty state without `ca.init=true` |
| a node says `certificate is valid for …, not <address>` | the address is missing from `serverNames` |
| `permission denied` in the state | the copy kept the host's owner: copy with `--owner=65532 --group=65532` (or `chown -R 65532:65532` in the loader) |
| the API answers 401 | the client has another token than `apiToken.existingSecret` |
| an approval notification never arrives | a `command` target: see what changes against a host |
