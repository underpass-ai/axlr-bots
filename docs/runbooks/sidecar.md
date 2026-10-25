# Runbook: the node agent as a sidecar

Run `axlr-node` inside a service's pod on Kubernetes, beside the service, so the bots placed on that node read the service's files with AXLR's own tools. The control plane stays where it is (a host with `install --listen`, [install](install.md#one-control-plane-nodes-over-tls)); the pod is one more node of the fleet, enrolled with a one-time token like [a host](add-a-host.md), and every connection still goes out from the pod. The chart is [`charts/axlr-bots-node`](../../charts/axlr-bots-node); there are no CRDs.

```text
pod demo-log-service-0 (StatefulSet)                                    control plane (axlr-bots serve)
┌─────────────────────────────────────────────────────────────┐        on a host, --listen 0.0.0.0:7878
│ service ──writes──▶ service-logs (emptyDir)                 │
│                          │ read-only                        │  mTLS  register · heartbeat · poll
│ axlr-node ── axlr-tui ──┘ <state>/workspaces/<bot>          │ ─────▶ lease · report
│   state: PVC per replica (key, certificate, traces)         │
│   /tmp: tmpfs (AXLR's diagnostics and sessions)             │ ─────▶ model provider (HTTPS)
└─────────────────────────────────────────────────────────────┘
  NetworkPolicy: no ingress; egress to DNS, the control plane and the model provider only
```

## Before you start

- The control plane listens on an address the pods reach (`--listen 0.0.0.0:7878`) and its certificate names it: the IP is in it when it listens on all interfaces, a DNS name goes in `--server-names`. A pod's traffic leaving its node is usually SNATed to the node's IP (Calico's `natOutgoing`), so a firewall that already admits the nodes admits the pods; a pod on the control plane's own host arrives from the pod CIDR instead.
- `helm` and `kubectl` with rights to create a Secret, a StatefulSet, a Service, a ServiceAccount and a NetworkPolicy in one namespace, and a StorageClass that provisions volumes (or `sidecar.state.persistent: false`, below).
- The node image in a registry the cluster pulls from.

## 1. Build and push the node image

[`distribution/node.Dockerfile`](../../distribution/node.Dockerfile) builds `axlr-node` and takes `axlr-tui` from AXLR v0.15.0's release archive for the target, checked against the sha256 the release publishes; the image is distroless, runs as 65532 and holds nothing else. The build stages run on the build platform, so both architectures build without emulation. With Docker's default builder, build each one, push, and join them:

```bash
R=registry.example.internal/underpass-ai/axlr-bots-node
for arch in amd64 arm64; do
  docker buildx build -f distribution/node.Dockerfile --platform linux/$arch -t $R:0.1.0-$arch --load .
  docker push $R:0.1.0-$arch
done
docker buildx imagetools create -t $R:0.1.0 $R:0.1.0-amd64 $R:0.1.0-arm64   # prints the digest
```

Check: `docker run --rm --read-only $R:0.1.0-amd64 --help` lists `axlr-node check`; `docker buildx imagetools inspect $R:0.1.0` shows `linux/amd64` and `linux/arm64`. The chart takes the image by digest.

## 2. One token per replica

A token is bound to a node id, and a sidecar's node id is `<node.idPrefix><pod name>`: in the demo released as `demo`, `demo-log-service-0`, `-1`, … Mint one per replica and put each in one Secret, keyed by the node id, without printing it:

```bash
axlr-bots node enroll-token --node-id demo-log-service-0 --ttl 2h \
  | jq -j .token \
  | kubectl -n <namespace> create secret generic axlr-bots-enrollment --from-file=demo-log-service-0=/dev/stdin
```

The token carries the CA, so nothing else is copied; `controlPlane.ca.existingConfigMap` (a ConfigMap with `ca.crt` from `axlr-bots ca show`) pins the CA instead. The Secret is read at a first start only and may be deleted once every replica is enrolled.

## 3. The model key

The key is the pod's own, in a Secret only the sidecar reads:

```bash
grep '^OPENROUTER_API_KEY=' ~/.config/axlr-bots/env | cut -d= -f2- | tr -d '\n' \
  | kubectl -n <namespace> create secret generic axlr-bots-model --from-file=OPENROUTER_API_KEY=/dev/stdin
```

## 4. Install the demo

The demo is a StatefulSet: a small service that writes a log line every five seconds to an emptyDir, and the sidecar, which mounts that emptyDir read-only as the workspace of the bot `log-summariser`.

```bash
helm install demo charts/axlr-bots-node -n <namespace> \
  --set demo.enabled=true \
  --set image.digest=sha256:… \
  --set controlPlane.url=https://192.168.1.75:7878 \
  --set enrollment.existingSecret=axlr-bots-enrollment \
  --set model.apiKey.existingSecret=axlr-bots-model
```

`nodeSelector`, `tolerations` and `affinity` place the demo pod and the chart test. Checks:

```bash
kubectl -n <namespace> get pod demo-log-service-0                       # 2/2 Running
kubectl -n <namespace> logs demo-log-service-0 -c axlr-node             # enrolled as node demo-log-service-0 with token enr-…, then registered as …
axlr-bots node list                                                     # demo-log-service-0 Online, labels {app, namespace, pod}
helm test demo -n <namespace> --logs                                    # {"status": "ok", "control": "https://…"}
```

The sidecar registers **dedicated** (`node.dedicated: true`): it runs only the bots whose placement names it or selects its labels, never a bot placed on any node, whose tasks keep going to the other nodes (or run in process when none is Online). Without it, the first live run's sidecar took the example bots' timer tasks. A dedicated node needs a control plane that knows the field: an older one refuses its registration.

The sidecar's labels come from the Downward API: `namespace`, `app` (the pod's `app` label, `node.appLabelKey`) and `pod`, plus `node.labels`; values must be lowercase identifiers. The chart test (`axlr-node check`) asks the control plane's `/healthz` over TLS 1.3, verified against the fleet CA, from inside the namespace and through the NetworkPolicy; it changes nothing on either side.

## 5. Register the bot and run the task

[`examples/sidecar/bot-log-summariser.json`](../../examples/sidecar/bot-log-summariser.json) is an observe bot with AXLR's three read tools, `local_read` limited to `**/*.log`, and `"placement": {"selector": {"app": "demo-log-service"}}`: it runs on the demo's sidecar only.

```bash
axlr-bots bot register --file examples/sidecar/bot-log-summariser.json
axlr-bots bot enable log-summariser
axlr-bots task submit log-summariser --objective "Summarise the last lines of the service log" --key sidecar-1
axlr-bots task report task-…
```

Check: `task executions` shows `"nodeId": "demo-log-service-0"`, `"policy": {"level": "observe", "enforced": true}` and the `local_read` of `service.log`. The workspace is the log volume itself, so AXLR's `--root` confines every read to it; a read of another file or of `../pki/node.key` is refused by the policy before it runs ([live run](../live-run.md#the-sidecar-on-kubernetes)).

## The service over HTTPS with mutual TLS

`demo.https` turns the demo's service into what a real one looks like from outside: nginx on 8443 with TLS, every request needing a client certificate, behind a `LoadBalancer` Service (MetalLB on a bare-metal cluster), its access log on the volume the bot reads. The sidecar does not change; the bot now summarises who called, with which certificate, and what was refused.

```bash
helm upgrade demo charts/axlr-bots-node -n <namespace> --reuse-values \
  --set demo.https.enabled=true \
  --set demo.https.host=axlr-bots-demo.example.com \
  --set demo.https.certManager.enabled=true \
  --set demo.https.certManager.serverIssuer.name=letsencrypt-prod   # a ClusterIssuer (ACME, DNS-01 for a LAN address)
kubectl -n <namespace> get svc demo-log-service-https                  # EXTERNAL-IP: the address the DNS name must point at
```

With `certManager.enabled` the chart asks cert-manager for the server's certificate from that issuer, and makes a private client CA in the namespace (a self-signed root, an issuer that signs with it) and one client certificate (`demo-log-service-client`) for the test; the pod mounts the CA's `ca.crt` only, never its key. Without cert-manager, `serverTLS.existingSecret` (`tls.crt`, `tls.key`) and `clientCA.existingSecret` (`ca.crt`) name your own. Point the DNS name at the Service's address (an `A` record; an ACME issuer with a DNS-01 solver issues for a name whose address is private), then:

```bash
curl https://axlr-bots-demo.example.com/                                              # 400: no required SSL certificate
curl --cert client.crt --key client.key https://axlr-bots-demo.example.com/orders/1   # 200: hello CN=axlr-bots-demo-client
```

`externalTrafficPolicy: Local` keeps the client's address, in the access log and for the NetworkPolicy, which admits port 8443 from `demo.https.ingressFrom` (the LAN, `192.168.0.0/16`, by default) and nothing else. The certificate's name goes into the public Certificate Transparency logs when the issuer is a public CA.

## In your own chart

Depend on this chart with its demo off and include its templates in your pod; [`examples/helm/service-with-bots`](../../examples/helm/service-with-bots) is a complete one:

```yaml
# Chart.yaml
dependencies:
  - {name: axlr-bots-node, version: 0.1.0, repository: file://../charts/axlr-bots-node}
# values.yaml
axlr-bots-node:
  image: {digest: sha256:…}
  controlPlane: {url: https://192.168.1.75:7878}
  enrollment: {existingSecret: axlr-bots-enrollment}
  model: {apiKey: {existingSecret: axlr-bots-model}}
  sidecar:
    workspaces: [{bot: log-summariser, volume: app-logs}]   # your pod's volume, mounted read-only
```

```yaml
{{- $bots := dict "values" (index .Values "axlr-bots-node") "root" $ }}
    containers:
      - name: web
        …
      {{- include "axlr-bots-node.inject.container" $bots | nindent 6 }}
    volumes:
      {{- include "axlr-bots-node.inject.volumes" $bots | nindent 6 }}
  volumeClaimTemplates:
    {{- include "axlr-bots-node.inject.volumeClaimTemplate" $bots | nindent 4 }}
```

The workload must be a StatefulSet whose pods carry an `app` label. A NetworkPolicy applies to the whole pod, so the chart cannot add one without restricting your service: put `axlr-bots-node.inject.egress` in your own policy, beside your service's rules. `sidecar.native: true` adds `restartPolicy: Always`, for a native sidecar under `initContainers`.

## What the sidecar keeps where

| Path | Volume | Survives |
|---|---|---|
| `/var/lib/axlr-node`: `pki/` (key 0600, certificate, CA, `enrollment.json` with the token id), traces, policies, correlation | the replica's PVC (`axlr-node-state`) | the pod's deletion and rescheduling |
| `/var/lib/axlr-node/workspaces` | an emptyDir of its own: scratch space for the bots without a volume | a container's restart, not the pod's |
| `/var/lib/axlr-node/workspaces/<bot>` | the service's volume, read-only (`sidecar.workspaces`), mounted inside the one above | whatever that volume does |
| `/tmp`: AXLR's `XDG_STATE_HOME` (diagnostics, sessions) | tmpfs (emptyDir, medium Memory) | a container's restart, not the pod's |
| `/etc/axlr-node/enroll/<node id>` | the token Secret, read-only, optional | read at a first start only |

Root's filesystem is read-only, the container runs as 65532 without privilege escalation and with every capability dropped, and the pod mounts no service-account token.

## Restarts, a wrong token, rotation

- **A restart** keeps the certificate: `keeping the certificate for node demo-log-service-0 (token enr-… already used)`, the same serial in `node list`.
- **A wrong token** (for another node id, spent, expired, not a token) is never sent when it names another node, and the sidecar does not exit: it logs `first start: … (retrying in 10s)` and reads the file again, so updating the Secret's key (kubelet refreshes the mounted file within a minute or so) is enough. A sidecar that exited would mark the pod not Ready and take the service out of its endpoints; for the same reason it has no readiness probe.
- **Rotate** the certificate: mint a fresh token for the same node id, replace the Secret's key, delete the pod. The first start finds a token other than the one the certificate came from and enrolls again (`enrolled as node … with token <new id>`); the old serial is denied. A rotation that fails keeps the certificate the node has.
- **A lost volume** (a new PVC) needs a fresh token: the old one is spent.
- **Retire**: `helm uninstall demo`, then `kubectl delete pvc axlr-node-state-demo-log-service-0` (a StatefulSet keeps its claims) and `axlr-bots node revoke demo-log-service-0`.

## Trade-offs

- **A StatefulSet, not a Deployment.** The node id is the pod's name, the certificate names the node id and the replica's volume holds the certificate: a StatefulSet keeps all three together across restarts and rescheduling, and gives each replica its own volume and so its own key. A Deployment's pod names change at every rollout (a new node id each time, no token minted for it) and its replicas would share one volume, and so one identity.
- **A PVC per replica.** The identity lives on the volume. With a node-local StorageClass (`local-path`) the replica stays on the node that holds its volume; losing that node means a fresh token for a new volume. A networked StorageClass lifts that. `sidecar.state.persistent: false` uses an emptyDir: no provisioner needed, the certificate survives a container's restart but not the pod's, and every new pod needs a fresh token.
- **One Secret for every replica's token** is mounted whole in every replica, so one replica could enroll as another before that one does. For strict separation, delete each key once its replica is enrolled, or install one release per replica.
- **AXLR's sessions on tmpfs.** AXLR refuses a state directory with an ancestor other accounts may write unless it is sticky, and a volume's root is 0777, or group-writable under `fsGroup`; a tmpfs is 1777. An approval still waiting when the pod is deleted cannot be continued on the new pod.
- **The NetworkPolicy cannot name a host.** The control plane is an IP (`<ip>/32` taken from `controlPlane.url`, or `networkPolicy.controlPlaneCIDR`); the model provider is HTTPS to the public ranges (`networkPolicy.modelProvider`). Narrow those to the provider's ranges or route through an egress proxy.
- **Executions share the sidecar.** Each execution is an `axlr-tui` process inside the sidecar container: it has the container's user, filesystem and limits, and its own workspace and policy file. One identity, filesystem and set of limits per execution (vision §9.2), a Job per execution, is not done.
