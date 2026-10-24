# Live run

What the whole thing does on one host, user-level, against the real AXLR and a model. The script that runs it is [`examples/demo.sh`](../examples/demo.sh); the output below is from 24 October 2026 with `claude-haiku-5.5` through OpenRouter and an `axlr-tui` built from AXLR's execution-policy branch (the policy flag, not yet the correlation flags). The host is shown as `host-1`, the home directory as `~`.

## The demo script

```bash
export OPENROUTER_API_KEY=...
AXLR_TUI=/path/to/axlr-tui sh examples/demo.sh
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

A build of AXLR with the result line and the `--correlation` and `--budget` flags (now on AXLR's `main`) was run the same day with the files this adapter writes: the run completed, every event line and the result carried the four ids unchanged, and `TestLiveExec` passed through the adapter with both flags on the command line (5 s, $0.00114). A run that reached its `tool_calls` budget ended `budget_exceeded` and was recorded as `Failed` with the limit as the reason. Switching an installation to such a build is the `AXLR_TUI=` line of the environment file and a restart ([install](runbooks/install.md#change-the-model-key-or-the-axlr-build)).

## The live tests

Three tests run the real binary when asked; everything else runs against the stand-ins.

| Test | Command | Last live run |
|---|---|---|
| the adapter against `axlr-tui --exec` | `AXLR_LIVE=1 AXLR_TUI=… OPENROUTER_API_KEY=… go test ./adapters/axlr -run TestLiveExec -v` | 24 Oct 2026, with the correlation and budget flags passed |
| two nodes over TLS, a bot placed by selector | `AXLR_BOTS_LIVE=1 AXLR_TUI=… OPENROUTER_API_KEY=… go test ./adapters/daemon -run TestLiveTwoNodesOverTLS -v` | 24 Oct 2026, 7 s: the task of the bot whose selector names `us` ran on `node-us` ($0.00095), both agents enrolled with tokens against a control plane on `127.0.0.1:0` |
| a webhook against the installed daemon, run by its node | `AXLR_BOTS_LIVE=1 go test ./cmd/axlr-bots -run TestLiveWebhookRunsOnTheNode -v` | 24 Oct 2026, 6 s, Completed on `host-1` ($0.00094) |

## The offline demo

[`examples/phase0-demo.sh`](../examples/phase0-demo.sh) runs the single-process flow with no daemon and no node: register, enable, submit, `task run`, read the result, resubmit the same key and see that no second execution happens. Without `AXLR_TUI` it uses [`examples/fake-axlr-tui`](../examples/fake-axlr-tui), a shell stand-in that prints a finished turn and its result object, so it needs no model provider:

```bash
sh examples/phase0-demo.sh
```
