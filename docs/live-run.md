# Live run

What the whole thing does on one host, user-level, against the real AXLR and a model. The script that runs it is `basics/demo.sh` in the private repo `axlr-bots-examples`, next to a clone of this one; the output below is from 24 October 2026 with `claude-haiku-5.5` through OpenRouter and an `axlr-tui` built from AXLR's execution-policy branch (the policy flag, not yet the correlation flags). The host is shown as `host-1`, the home directory as `~`.

## The demo script

```bash
export OPENROUTER_API_KEY=...
AXLR_TUI=/path/to/axlr-tui sh basics/demo.sh   # from axlr-bots-examples
```

It builds and installs both commands with the control plane listening for nodes on `127.0.0.1:7878` and for operators on `127.0.0.1:7879`, both TLS, beside its socket; creates the fleet CA; starts the control plane; enrolls this host as a node through the TLS path with a one-time token (so the installed node uses mTLS, as a node on another host would); starts it and shows it Online with its certificate; registers the example bot; submits a generic task and watches the node run it; reads the report; runs the approval path with a harmless write in the bot's workspace; restarts both services and shows the node, the bots and the tasks are still there; then the automatic triggers: a timer every ten minutes and a webhook on the example bot, the webhook fired with `curl` on the operator address with the trigger's secret, delivered twice (one task), refused without the secret, the operator route refused without the bearer token, both tasks and their executions with the trace ids, and a last restart that fires nothing twice. The installation stays running (`DEMO_UNINSTALL=1` removes it); a second run keeps the CA and the node's credentials (`DEMO_REENROLL=1` enrolls the node again, which rotates its certificate).

## The output

This was the third run of the demo within ten minutes on the same host. The CA, the node's credentials and the bots were there from the earlier runs; the timer `clock` was armed and fired by the first run (its slot `15:59:22Z`), and the two later runs, each re-applying the triggers and restarting both services, found that slot fired and created no second timer task, which is what step 8 counts. The webhook's delivery id carries the run's time stamp, so each run is one new webhook task.

```text
## 0. build and install (user-level: ~/.local, ~/.config, systemctl --user; nodes on 127.0.0.1:7878 and operators on 127.0.0.1:7879, both TLS)
  "mode": "user",
  "binary": "~/.local/bin/axlr-bots",
  "nodeBinary": "~/.local/bin/axlr-node",
    "~/.config/systemd/user/axlr-bots.service",
    "~/.config/systemd/user/axlr-node.service"
  "envFile": "~/.config/axlr-bots/env",
  "socket": "~/.local/state/axlr-bots/axlr-bots.sock",
  "control": "https://127.0.0.1:7878",
  "listen": "127.0.0.1:7878",
  "apiListen": "127.0.0.1:7879",
    "~/.local/bin/axlr-bots --state ~/.local/state/axlr-bots ca init  # once per fleet, before the service starts",
    "the operator bearer token is written to ~/.local/state/axlr-bots/api.token at the first start",
    "systemctl --user enable --now axlr-bots.service axlr-node.service",
    "loginctl enable-linger $USER  # keep the services running after logout"

## 1. the fleet CA (once per fleet; kept across runs)
(the CA exists: ~/.local/state/axlr-bots/ca)
  "fingerprint": "dcb7e7d6b66e604bc768f9932f621c1803972be2cd3e63e0d6c5ba7fe1cfd2cc",

## 2. start the control plane; enroll this host as a node over TLS with a one-time token; start the node
(the node is enrolled: ~/.local/state/axlr-node/pki; DEMO_REENROLL=1 enrolls it again and rotates its certificate)
    "id": "host-1",
    "hostName": "host-1",
      "host": "host-1"
      "policySupported": true,
    "status": "Online",
    "certificateSerial": "32a865fa9c3309f8c98ce2f3032ae8f0"
2026/10/24 18:04:28 node: control plane https://127.0.0.1:7878, client certificate ~/.local/state/axlr-node/pki/node.crt for node host-1 (serial 32a865fa9c3309f8c98ce2f3032ae8f0, until 2027-10-24)

## 3. register the example bot (observe: AXLR's read tools, manual trigger, any node)
(bot already registered: the state survived an earlier run)
  "id": "bot",
  "level": "observe",
      "type": "manual",
      "type": "timer",
      "type": "webhook",
  "status": "Enabled",

## 4. a generic task: the control plane assigns it to the node over TLS, the node runs it with AXLR
task task-708a26189b5bf886b9c785f3 is Completed after about 8s
    "costUsd": 0.00122603
    "enforced": true,
  "nodeId": "host-1",
  "outcome": "Completed",
    "requests": 2,
    "runtimeLevel": "observe",
    "toolCalls": [
    "toolCalls": 1,
report:
    The workspace has **0 files**. The recursive listing of the workspace root (`.`, up to 8 levels deep) came back empty.

    The listing skips `.git` and the console's `.axlr` and `.kernel` stores by design, so any files there wouldn't appear.

## 5. the approval path: a write runs only after task approve
task task-c31765eca100f22cb1ed470d is WaitingApproval after about 4s
  "status": "WaitingApproval",
  "outcome": "DecisionRequired",
        "callId": "toolu_01LwYwj9yNxWLCxYWjmEcNfu",
      "tool": "local_write",
      "callId": "toolu_01LwYwj9yNxWLCxYWjmEcNfu",
      "arguments": {
  "nodeId": "host-1",
the file does not exist yet: nothing ran without approval
approving...
    "status": "Running",
    "kind": "decide",
    "nodeId": "host-1"
task task-c31765eca100f22cb1ed470d is Completed after about 10s
    Nothing is missing: I read the file back and it contains exactly `hello`, so the request is complete.
-rw------- 1 user user 5 Oct 24 18:04 ~/.local/state/axlr-node/workspaces/bot-act-with-approval/hello-20261024T160422Z.txt
hello
## 6. restart both services: node, bots and tasks are still there
    "id": "host-1",
    "status": "Online",
    "certificateSerial": "32a865fa9c3309f8c98ce2f3032ae8f0"
    "id": "bot-act-with-approval",
    "status": "Enabled",
    "id": "bot",
    "status": "Enabled",
  "id": "task-708a26189b5bf886b9c785f3",
  "status": "Completed",
  "id": "task-c31765eca100f22cb1ed470d",
  "status": "Completed",
service logs (last lines):
Started axlr-bots.service - AXLR Bots control plane.
2026/10/24 18:04:50 daemon: operator token read from ~/.local/state/axlr-bots/api.token
2026/10/24 18:04:50 daemon: serving on ~/.local/state/axlr-bots/axlr-bots.sock and https://127.0.0.1:7878 (nodes) and https://127.0.0.1:7879 (operators, bearer token), tick 2s
axlr-node.service: Consumed 3.685s CPU time.
Started axlr-node.service - AXLR Bots node agent.
2026/10/24 18:04:50 node: control plane https://127.0.0.1:7878, client certificate ~/.local/state/axlr-node/pki/node.crt for node host-1 (serial 32a865fa9c3309f8c98ce2f3032ae8f0, until 2027-10-24)
2026/10/24 18:04:50 node: registered as host-1 (host-1), axlr "" policy true correlation false

## 7. automatic triggers: a timer every 10 minutes and a webhook on the example bot
  "name": "bot",
      "type": "manual",
      "name": "manual"
      "type": "timer",
      "name": "clock",
      "objective": "Say the current time of this host (the trigger fired at {{.firedAt}}).",
      "every": "10m0s"
      "type": "webhook",
      "name": "deploy",
      "objective": "Handle event {{.type}} for {{.service}}: say in one sentence what you would check first, without running anything.",
      "secret": "<redacted>"
(the secret is generated per run and shown redacted; the timer is armed and fires at once)
curl --cacert ~/.local/state/axlr-bots/ca/ca.crt -H 'X-Axlr-Secret: <secret>' -H 'Idempotency-Key: deploy-20261024T160422Z' -d '{"type": "deploy", "service": "web"}' https://127.0.0.1:7879/v1/triggers/bot/deploy
{"created":true
"task":{"id":"task-dc002e74d9f2a08d0f42aa6e"
"idempotencyKey":"webhook:deploy:1476c6218f02cdcd11c12fc3acc42bd1"
"trigger":{"type":"webhook"
the same delivery again: "created":false, task task-dc002e74d9f2a08d0f42aa6e (the same)
without the secret: HTTP 403
operator route without the token: HTTP 401
operator route with the token: HTTP 200
timer task: task-462b9058850cd655a0b0e0a5
task task-dc002e74d9f2a08d0f42aa6e is Completed after about 6s
task task-462b9058850cd655a0b0e0a5 is Completed after about 0s
  "id": "task-dc002e74d9f2a08d0f42aa6e",
  "objective": "Handle event deploy for web: say in one sentence what you would check first, without running anything.",
  "status": "Completed",
  "idempotencyKey": "webhook:deploy:1476c6218f02cdcd11c12fc3acc42bd1",
  "correlationId": "trace-8f1cf2d09f22bf4a441c59e3",
    "type": "webhook",
    "name": "deploy"
    "type": "deploy",
    "id": "exec-840bc8f94fb89071e514f9e8",
    "outcome": "Completed",
      "costUsd": 0.000907775
      "traceId": "trace-8f1cf2d09f22bf4a441c59e3",
      "carried": false,
    "nodeId": "host-1",
report:
    I would first check the web deploy's target environment and the build artifact it will ship: confirm the intended branch or commit, that the production build passes locally, and that required env vars and config for that environment are set.
  "id": "task-462b9058850cd655a0b0e0a5",
  "objective": "Say the current time of this host (the trigger fired at 2026-10-24T15:59:22Z).",
  "status": "Completed",
  "idempotencyKey": "timer:clock:1792857562",
  "correlationId": "trace-b3291e374a270360e9a61f6f",
    "type": "timer",
    "name": "clock"
    "id": "exec-c5b43e95c519018452e2276d",
    "outcome": "Completed",
      "costUsd": 0.0014355799999999999
      "traceId": "trace-b3291e374a270360e9a61f6f",
      "carried": false,
    "nodeId": "host-1",
report:
    Nothing is missing: the only time I have is the trigger timestamp, 2026-10-24T15:59:22Z (UTC). None of my tools can read the host's live clock, so the host's current time may be somewhat later.
timer state (the next fire is persisted):
  "schedule": "every 10m0s",
  "nextFireAt": "2026-10-24T16:09:22.594343227Z",
  "lastScheduledAt": "2026-10-24T15:59:22.594343227Z",
  "lastTaskId": "task-462b9058850cd655a0b0e0a5"

## 8. restart both services: the timer fires nothing twice, the webhook delivery stays one task
timer tasks for the slot 2026-10-24T15:59:22.594343227Z: 1; timer tasks in all: 1 (one per slot)
fires of the delivery deploy-20261024T160422Z in the events: 2 (two: the delivery and its redelivery); tasks for it: 1
2026/10/24 18:04:57 daemon: operator token read from ~/.local/state/axlr-bots/api.token
2026/10/24 18:04:57 daemon: serving on ~/.local/state/axlr-bots/axlr-bots.sock and https://127.0.0.1:7878 (nodes) and https://127.0.0.1:7879 (operators, bearer token), tick 2s

## installed and running
check:     systemctl --user status axlr-bots.service axlr-node.service
uninstall: ~/.local/bin/axlr-bots uninstall [--purge]
```

## Against the vision's criteria

The bot, the node, the tasks and the timer's next fire survived the restarts of both services (§14.1); tasks were started by the API and by two automatic triggers, a timer and a webhook (§14.2); AXLR received the policy and produced the result object, enforced (§14.3, §14.5); the write ran only after `task approve` (§10.4); the redelivered webhook and the restarted scheduler created no second task, and both fires are in the events with `deduplicated` (§14.7); every task has a trace id and every execution and event names the bot, the task, the execution and the trace (§14.8; this build of the runtime did not carry them yet, `carried: false`); the executions keep the node, the policy, the cost and the trace reference (§12.3); nothing needed root (§9.1); the node authenticated with a client certificate the fleet CA issued against a one-time token, the control plane with a certificate from the same CA, over TLS 1.3; the operator's commands stayed on the socket and the operator address refused a request without the bearer token and a webhook without its secret (§9.4). The installation was left running with the node on the TLS path and the timer firing every ten minutes; `loginctl show-user $USER -p Linger` says `yes`, so it survives a logout. The `axlr ""` in the node's log is the capability the agent reads from `axlr-tui --version`, which that build does not print.

## The correlation flags

A build of AXLR with the result line and the `--correlation` and `--budget` flags (released in AXLR v0.14.0) was run the same day with the files this adapter writes: the run completed, every event line and the result carried the four ids unchanged, and `TestLiveExec` passed through the adapter with both flags on the command line (5 s, $0.00114). A run that reached its `tool_calls` budget ended `budget_exceeded` and was recorded as `Failed` with the limit as the reason. Switching an installation to such a build is the `AXLR_TUI=` line of the environment file and a restart ([install](runbooks/install.md#change-the-model-key-or-the-axlr-build)).

## The sidecar on Kubernetes

On 25 October 2026 the node agent ran as a sidecar in a pod of a real cluster, beside the two hosts already enrolled. The control plane is the systemd one on `host-1` (`--listen 0.0.0.0:7878`, reached at `192.168.1.75`); the cluster's control-plane node is `host-2`, which is also enrolled as a node over systemd. The procedure is the [sidecar runbook](runbooks/sidecar.md).

What the cluster needed, each approved by the operator first: the node image pushed to the cluster's own registry (on the LAN; nothing public); the demo pod and the chart test pinned to `host-2` with a toleration of `node.kubernetes.io/unschedulable`, because every node was cordoned (the cordon stayed; only these pods tolerate it); and the `local-path` provisioner's helper image made fully qualified (`docker.io/library/busybox:1.37.0`), because CRI-O on `host-2` enforces short names and refused `busybox`, so no volume could be provisioned. That last change also let the provisioner finish deleting a volume released months before.

Before the cluster, the image ran under Docker against a throwaway control plane, as the pod runs it (`--read-only`, a tmpfs on `/tmp`, user 65532, every capability dropped, the log directory mounted read-only as the bot's workspace): the first start enrolled, the restart kept the certificate, the summary completed ($0.0014), and a task told to read `secret.txt` and `../pki/node.key` got both `local_read` calls `forbidden` by the policy. The first run had failed with `diagnostic directory /var/lib/axlr-node belongs to uid 1000 …`: AXLR checks the ancestors of its state directory, which is why the image sets `XDG_STATE_HOME=/tmp/axlr-state` on a tmpfs.

The cluster run, condensed to the lines that matter (the hosts shown as `host-1` and `host-2`):

```text
$ axlr-bots node enroll-token --node-id demo-log-service-0 --ttl 2h | jq -j .token | kubectl -n underpass-labs create secret generic axlr-bots-enrollment --from-file=demo-log-service-0=/dev/stdin
token id enr-25454831d681d5f2dbd876c6 for demo-log-service-0
secret/axlr-bots-enrollment created
$ helm install demo axlr-bots-node-0.1.0.tgz -n underpass-labs -f values.live.yaml
STATUS: deployed
$ kubectl -n underpass-labs get pod,pvc -l app.kubernetes.io/instance=demo
pod/demo-log-service-0   2/2   Running   host-2
persistentvolumeclaim/axlr-node-state-demo-log-service-0   Bound   1Gi   RWO   local-path
$ kubectl -n underpass-labs logs demo-log-service-0 -c axlr-node
node: enrolled as node demo-log-service-0 with token enr-25454831d681d5f2dbd876c6 (serial e9f55a309b14da46be5d6c7a0a054dcf, until 2027-10-24)
node: control plane https://192.168.1.75:7878, client certificate /var/lib/axlr-node/pki/node.crt for node demo-log-service-0 (serial e9f55a30…, until 2027-10-24)
node: registered as demo-log-service-0 (demo-log-service-0), axlr "" policy true correlation true
$ axlr-bots node list
demo-log-service-0  Online  {"app": "demo-log-service", "namespace": "underpass-labs", "pod": "demo-log-service-0"}  e9f55a309b14da46be5d6c7a0a054dcf
host-1              Online  {"host": "host-1"}
host-2              Online  {"host": "host-2", …}
$ axlr-bots bot register --file cmd/axlr-bots/testdata/bot-log-summariser.json && axlr-bots bot enable log-summariser
$ axlr-bots task submit log-summariser --objective "Summarise the last lines of the service log" --key k8s-sidecar-1
task-3c30871d5c7287cdb80bea1b Pending trace-eea9af13794c5c730555aef9
$ axlr-bots task report task-3c30871d5c7287cdb80bea1b
The last six lines of `service.log` (23:48:35Z to 23:49:00Z) show a healthy service. All six requests returned HTTP 200, for `/orders/1` through `/orders/6`, at roughly one request every 5 seconds.
- **Latency:** between 461 ms and 884 ms, with most under 650 ms. …
- **Warnings:** one `level=warn` "slow request" line, for `/orders/5` at 461 ms. It was still a 200 response.
- **Errors:** none.
$ axlr-bots task executions task-3c30871d5c7287cdb80bea1b
nodeId demo-log-service-0, outcome Completed, policy {level observe, runtimeLevel observe, enforced true}, correlation carried true,
tool calls local_list, local_list, local_read; 4 model requests, $0.00103
$ kubectl -n underpass-labs delete pod demo-log-service-0          # the StatefulSet recreates it on the same volume
node: keeping the certificate for node demo-log-service-0 (token enr-25454831d681d5f2dbd876c6 already used)
node: registered as demo-log-service-0 (demo-log-service-0), axlr "" policy true correlation true
$ helm test demo -n underpass-labs --logs
TEST SUITE: demo-log-service-check   Phase: Succeeded
{"ca": "the token at /etc/axlr-node/enroll/demo-log-service-0", "control": "https://192.168.1.75:7878", "status": "ok"}
```

A probe pod with the demo's labels, so under its NetworkPolicy, asked the cluster's API server (`https://10.96.0.1:443`) and got `context deadline exceeded`: the policy lets the pod reach DNS, the control plane and the model provider, and nothing in the cluster's private ranges.

### The sidecar took other bots' tasks

The first sidecar registered like any node, so the control plane also gave it the tasks of the example bots placed on any node: two timer tasks of `bot` and one of `bot-act-with-approval` failed there with `axlr: mkdir /var/lib/axlr-node/workspaces/bot: permission denied`. Two fixes, both live since 00:54 UTC. First, the runtime had created `workspaces/` on the state volume as root when it mounted the log volume inside it; the workspaces now have a volume of their own. Second, the sidecar registers `dedicated`, and the control plane gives it only the tasks of bots whose placement names it or selects its labels. That needed the control plane rebuilt with the field, and the image `0.1.1`, each approved first. Afterwards a task of `bot` ran on `host-1`, and one of `log-summariser` on `demo-log-service-0`:

```text
$ axlr-bots node list
demo-log-service-0  Online  dedicated
host-1              Online
host-2              Online
bot -> host-1 Completed
log-summariser -> demo-log-service-0 Completed
```

### Over HTTPS with mutual TLS

Later the same night the demo's service became an HTTPS server with mutual TLS (`demo.https`, [sidecar runbook](runbooks/sidecar.md#the-service-over-https-with-mutual-tls)), each step again approved first. The Service took `192.168.1.240` from MetalLB; cert-manager issued the server's certificate from the cluster's Let's Encrypt issuer (DNS-01 on Route53), and the client CA and a client certificate in the namespace; the pod was recreated with nginx in place of the log writer, and the sidecar kept its certificate.

```text
$ kubectl -n underpass-labs get svc demo-log-service-https
demo-log-service-https   LoadBalancer   192.168.1.240   443:31234/TCP
$ openssl s_client -connect 192.168.1.240:443 -servername axlr-bots-demo.underpassai.com | openssl x509 -noout -subject -issuer
subject=CN = axlr-bots-demo.underpassai.com
issuer=C = US, O = Let's Encrypt, CN = YE2
$ curl https://axlr-bots-demo.underpassai.com/                                      # verify=0 against the system's CAs
400
$ curl --cert client.crt --key client.key https://axlr-bots-demo.underpassai.com/orders/1
hello CN=axlr-bots-demo-client from axlr-bots-demo.underpassai.com
$ curl --cert rogue.crt --key rogue.key https://axlr-bots-demo.underpassai.com/     # self-signed, not from the client CA
400
$ axlr-bots task report task-d781cd27caacc97277a74fd6                                # "Summarise the last lines of the service log"
The service log is `access.log` in the workspace. It has six lines, all from 2026-10-25 00:15:20 UTC, and `error.log` is empty.
- **Client:** every request comes from 192.168.1.75 over TLSv1.3.
- **Successful requests:** three `curl` calls with a client certificate (CN=axlr-bots-demo-client) returned 200 for `/orders/1`, `/orders/2` and `/orders/3`, each 67 bytes.
- **Rejected requests:** two `GET /` requests returned 400. One was a plain `curl` with no client certificate (verify=NONE). The other presented a self-signed certificate for `CN=rogue`, which failed verification.
```

The execution ran on `demo-log-service-0` (observe, enforced, $0.0009).

## Approvals that expire, notifications and artifacts (gx10, 25 Oct 2026)

The installed control plane and the gx10 node were replaced by this build (the old binaries kept as `*.before-approvals`); `gx10-ff95`, `wrx80` and `demo-log-service-0` stayed Online through both restarts.

**Expiry.** `bot-act-with-approval` was patched to `{"ttl": "2m", "onExpire": "deny", "notify": [{"type": "command", "name": "file", "command": ["/bin/sh", "-c", "cat > …/last-approval-request.json"]}]}` and given a write:

```text
18:21:26.513 bot.task.waiting_approval {"deadline": "2026-10-25T18:23:26.5Z", "callId": "toolu_01Gvgz…"}   on gx10-ff95
18:21:28     daemon: notification dlv-1fb5f03cc48c6742c380e6a6 of task task-1831b5d5… to file: delivered (attempt 1)
             (the file holds axlr-bots.approval-request/v1: local_write expiry-check.txt, the deadline, actions.approve.url
              https://127.0.0.1:7879/v1/tasks/task-1831b5d5…/approve?call=toolu_01Gvgz…)
18:23:28.194 bot.approval.expired {"action": "deny", "deadline": "2026-10-25T18:23:26.5Z"}
18:23:28.196 bot.task.decided {"decision": "deny", "reason": "approval expired", "nodeId": "gx10-ff95"}
18:23:30.974 bot.task.completed   "I didn't create expiry-check.txt. You denied the write approval, so nothing was written."
$ axlr-bots task approve task-1831b5d5…            # exit 1
axlr-bots: approval expired: task task-1831b5d5… (call toolu_01Gvgz…) had to be decided by 2026-10-25T18:23:26Z; the decision authorises nothing
$ curl … -X POST https://127.0.0.1:7879/v1/tasks/task-1831b5d5…/approve?call=toolu_01Gvgz…
{"kind":"approval_expired", …}  HTTP 409
```

The task ends with `approval.outcome: expired`; `expiry-check.txt` never appeared in the workspace; `audit.jsonl` has the delivery as `approval.notify` (queued, then delivered on attempt 1). The bot's `ttl` was set back to `15m` afterwards.

**Artifacts.** A temporary dedicated node, `gx10-artifacts`, enrolled with a one-time token and ran an `axlr-tui` built from AXLR PR #180 (`axlr_deliver`, the receipt); the bot `bot-artifacts` (`act-within-policy`, `local_read`, `local_list`, `axlr_deliver`) was placed on it. One task, haiku-5.5, $0.0012:

```text
node: execution exec-587ace1e…: artifact findings.md uploaded (405 bytes)
result.artifacts[0] = {"name": "findings.md", "mediaType": "text/markdown", "size": 405, "sha256": "f02be984…f980",
                       "reference": ".axlr/deliverables/3b4c06…/findings.md", "source": "inline", "stored": true}
receipt = axlr.execution-receipt/v1, completed, complete: true, 2 requests, tool calls [axlr_deliver]
$ curl … https://127.0.0.1:7879/v1/executions/exec-587ace1e…/artifacts/findings.md
HTTP/2 200, content-type: text/markdown, content-length: 405, x-axlr-artifact-sha256: f02be984…f980
sha256 of the download = sha256 of the file in the node's workspace = f02be984…f980
$ axlr-bots execution artifact exec-587ace1e… findings.md --out findings.md     # the hash checked on the way
```

The temporary node was stopped and revoked and the bot paused afterwards.

**Found on the way.** Every task on `gx10-ff95` had failed since the node's restart at 08:04 (`axlr-tui: jev: TYPESAFE_API_KEY is required for Jev`): AXLR's settings turn on the Jev judge and the key reached the user's systemd manager only after the node had started. Restarting the node with the new binary gave it the key; the `bot` timer completed again at 18:25.

## Delegation between bots (gx10, 25 Oct 2026)

The installed control plane was replaced by this build (the old binary kept as `axlr-bots.before-delegation`); `gx10-ff95`, `wrx80` and `demo-log-service-0` stayed Online and their node binaries were not touched. A temporary dedicated node, `gx10-deleg`, enrolled with a one-time token and ran this build's `axlr-node` with an `axlr-tui` built from AXLR `main` (`81a327a`, 0.14.0 plus #179–#182). The two example bots, [`bot-delegating.json`](../cmd/axlr-bots/testdata/bot-delegating.json) (`bot-a`) and [`bot-delegate-target.json`](../cmd/axlr-bots/testdata/bot-delegate-target.json) (`bot-b`), were placed on it; both `observe`, haiku-5.5.

**A task that needs the other bot's answer.** `bot-a` may ask `bot-b`, `maxDepth: 1`:

```text
$ axlr-bots task submit bot-a --objective "What is the codename of release 0.15? Only bot-b knows it: ask bot-b, then answer with the codename." --key deleg-live-1
20:24:30 node: execution exec-68e01f6d… (run) assigned                       policy allow: axlr_bots_delegate, local_list, local_read
20:24:30 node: execution exec-68e01f6d… may delegate to bot-b (depth 0 of 1)
20:24:36 node: execution exec-68e01f6d…: delegated task-f4380d7e… to bot-b at depth 1 (created true, wait true)
20:24:36 node: execution exec-aa6383fb… (run) assigned                       the child, on the same node, while the parent waits
20:24:39 node: execution exec-aa6383fb… reported Completed                   "The codename of release 0.15 is Lighthouse."
20:24:40 node: execution exec-68e01f6d…: delegated task task-f4380d7e… ended Completed
20:24:41 node: execution exec-68e01f6d… reported Completed                   "The codename of release 0.15 is Lighthouse, according to bot-b."
bot.task.delegated {"childTaskId": "task-f4380d7e…", "childBotId": "bot-b", "depth": 1, "maxDepth": 1, "chain": ["bot-a"],
                    "grant": {"maxModelCalls": 5, "maxToolCalls": 5, "deadline": "2026-10-25T19:28:33Z"}, "wait": true}
```

The child shares the parent's trace id (`trace-6124dd89…`); `GET /v1/tasks/task-f4380d7e…` shows `delegation` with its parent and grant, the parent's shows `children` and `grantedModelCalls: 5`. The grant is half of `bot-a`'s 10 calls (under `bot-b`'s own 6) and half of the 474 s `bot-a` had left. The model reached the tool through AXLR's `axlr_tools` and `axlr_call_tool`; the execution's policy record names the manifest (`"delegation": "…/mcp/exec-68e01f6d….json"`), and the socket was gone once it ended.

**A loop.** `bot-b` was patched to ask `bot-a` back before it answers (`delegation: {to: [bot-a], maxDepth: 1}`):

| `bot-a` | What cut `bot-b`'s ask | `bot.delegation.refused` |
|---|---|---|
| no `delegation` trigger | its consent | `target_unavailable`: "bot bot-a takes no delegated task: its triggers do not include delegation" |
| `delegation` trigger, `maxDepth: 1` | the depth | `depth_exceeded`: "a task of bot-a would be at depth 2; this chain allows 1" |
| `maxDepth: 2` on both | the visited set | `cycle`: "bot bot-a is already on this chain (bot-a → bot-b): asking it again would loop" |

Each time `bot-b` read the refusal as the tool's error, answered without it ("Breakwater. The confirmation from bot-a was refused with reason \"cycle\" …"), and `bot-a`'s task completed with one child; `audit.jsonl` has `task.delegate` `denied` with the reason. No third task was created.

The temporary node was stopped and revoked and both bots paused afterwards. Not run live: a delegating bot at `act-with-approval` (the card on the delegation call is covered by the executor's tests only), and `wait: false`.

## The live tests

Three tests run the real binary when asked; everything else runs against the stand-ins.

| Test | Command | Last live run |
|---|---|---|
| the adapter against `axlr-tui --exec` | `AXLR_LIVE=1 AXLR_TUI=… OPENROUTER_API_KEY=… go test ./adapters/axlr -run TestLiveExec -v` | 24 Oct 2026, with the correlation and budget flags passed |
| two nodes over TLS, a bot placed by selector | `AXLR_BOTS_LIVE=1 AXLR_TUI=… OPENROUTER_API_KEY=… go test ./adapters/daemon -run TestLiveTwoNodesOverTLS -v` | 24 Oct 2026, 7 s: the task of the bot whose selector names `us` ran on `node-us` ($0.00095), both agents enrolled with tokens against a control plane on `127.0.0.1:0` |
| a webhook against the installed daemon, run by its node | `AXLR_BOTS_LIVE=1 go test ./cmd/axlr-bots -run TestLiveWebhookRunsOnTheNode -v` | 24 Oct 2026, 6 s, Completed on `host-1` ($0.00094) |

## The offline demo

`basics/phase0-demo.sh` in the private repo `axlr-bots-examples` runs the single-process flow with no daemon and no node: register, enable, submit, `task run`, read the result, resubmit the same key and see that no second execution happens. Without `AXLR_TUI` it uses `basics/fake-axlr-tui`, a shell stand-in that prints a finished turn and its result object, so it needs no model provider:

```bash
sh basics/phase0-demo.sh   # from axlr-bots-examples
```
