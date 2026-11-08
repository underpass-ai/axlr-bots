# Runbook: approve or deny a write

At the `act-with-approval` level every allowed write or command is a person's decision: AXLR stops at the first one with `needs_decision`, the task is `WaitingApproval`, and nothing runs until an operator decides or the wait expires. The level mapping is in the [runtime contract](../architecture/runtime-contract.md#the-policy); the bot fields are in the [bot definition](../reference/bot-definition.md#approvals).

```text
Running ──needs_decision──▶ WaitingApproval ──approve──▶ Running (decide execution) ──▶ Completed / Failed
                              │  deadline = stop + ttl     ──deny────▶ Running (decide execution) ──▶ …
                              │  notify each target
                              └─ deadline passed ──onExpire deny──▶ Running (a denial)  ──▶ Completed / Expired
                                                 └─onExpire cancel─▶ Expired
                                 a decision after the deadline: 409 approval_expired, nothing runs
```

## Run the example

[`cmd/axlr-bots/testdata/bot-act-with-approval.json`](../../cmd/axlr-bots/testdata/bot-act-with-approval.json) is the example bot one level up: `local_write` allowed, level `act-with-approval`, and an `approvals` block that waits 15 minutes, then denies.

```bash
axlr-bots bot register --file cmd/axlr-bots/testdata/bot-act-with-approval.json
axlr-bots bot enable bot-act-with-approval
axlr-bots task submit bot-act-with-approval --objective "Create hello.txt in your workspace containing hello" --key w-1
```

Check, a few seconds later: `axlr-bots task get task-…` shows `"status": "WaitingApproval"` and `approval` with the call id, the tool, `since`, the `deadline` and `onExpire`; the last execution is `"outcome": "DecisionRequired"` and its `result.pendingDecision` names the tool (`local_write`), the call, the session and the call's arguments (the path and the content that would be written). The file does not exist in the bot's workspace (`<node-state>/workspaces/bot-act-with-approval/`). The event `bot.task.waiting_approval` carries the deadline too.

## Decide

```bash
axlr-bots task approve task-…              # a new execution carries the decision
axlr-bots task deny task-…                 # or: the session goes on without the call
axlr-bots task approve task-… --call c-1   # refused unless the task waits on that call
```

They are `POST /v1/tasks/{id}/approve?call=` and `/deny?call=` (no body), or `POST /v1/tasks/{id}/decide` with `{"decision": "approve", "callId": "…", "decidedBy": "…"}`; `task decide <id> approve|deny` is the same. The CLI decides as its global `--actor`, the API as `X-Axlr-Actor` or `decidedBy` ([who may decide](#who-may-decide)). The decision is a new execution of the task. When the stopped execution ran on a node, the decision is queued for that node (the AXLR session lives there); the node runs `axlr-tui --session <id> --decide approve --policy <file>` on its next poll and reports. A node carries one execution at a time and polls only when it is free, so a decision queued for a node busy with another task waits for that task's end: `bot.task.decided` says so with `busyWith` (the task ids the node is running). A decision queued for a node that is Offline (or gone) waits for it: the session lives on that node and no other can take it, and a node back from a reboot or a reinstall runs it. The reconcile pass tells the task's timeline once per state of the node, with a `node.offline` event (`nodeId`, `nodeStatus`, `waiting: waiting for node <id>, offline since HH:MM UTC`); only the bot's timeout and a minute fail the task, with `reconciled: its decision (…) waited for node <id>, which is Offline since …`. In single-process mode the control plane runs it itself.

Check after `approve`: `task get` shows `approval.outcome: approved`, a second execution of `"kind": "decide"`, then `"status": "Completed"`; the file exists with its content; `task report` prints the answer. After `deny`: `approval.outcome: denied`, the task ends with the outcome of that execution and the file never appears. Either way the write never ran before the decision: `bot events bot-act-with-approval` shows `bot.task.decided` between the two executions.

## Expiry

The deadline is when the task stopped plus the bot's `approvals.ttl` (default `1h`), kept on the task, so a restart of the control plane keeps it. The daemon looks at every tick (2 s): a wait past its deadline is closed `expired`, `bot.approval.expired` is published with the call, the deadline and the action, and the action is carried out:

| `onExpire` | What happens | Events |
|---|---|---|
| `deny` (default) | a new execution hands the session a denial, exactly as `task deny` would (queued for the node, or run in process); the session goes on without the call. A run that completes ends the task `Completed`; one that fails ends it `Expired`, with the run's error as its `failureReason` | `bot.approval.expired`, `bot.task.decided` (`decision: deny`, `reason: approval expired`), then `bot.task.completed` or `bot.task.expired` |
| `cancel` | the task ends `Expired` (`failureReason`: the call and the deadline); the session stays where it is | `bot.approval.expired`, `bot.task.expired` (`action: cancel`) |

`Expired` is its own end, apart from `Failed`: nobody decided the call in time, so the call never ran. It is counted apart in the [overview](../reference/operator-api.md#the-overview) and listed with `task list --status Expired`. A delegated task still waiting when its delegation's deadline comes ends `Expired` too. A task that failed after a later wait was decided ends `Failed`: only the last wait counts. Tasks that ended before this release keep the status they were stored with.

A paused bot cannot carry a denial: its expired task ends `Expired` at once. A task that waited before approvals had deadlines gets one at the daemon's first pass: when it began to wait plus the bot's `ttl`.

**A late decision authorises nothing** (vision §14). After the deadline, `approve` and `deny` are refused with `409 approval_expired` and the call never runs, whether the daemon has carried out the expiry yet or not; the refusal is audited (`task.decide`, denied).

```text
$ axlr-bots task approve task-…
axlr-bots: approval expired: task task-… (call c-1) had to be decided by 2026-10-25T18:02:11Z; the decision authorises nothing
```

## Notifications

When a task starts to wait, the control plane queues one delivery per target in `approvals.notify`, in `<state>/deliveries/` (durable: a restart resumes the retries), and the daemon makes the attempts. Each delivery has an id derived from the stopped execution and the target, the same on every attempt, so a receiver drops a repeat. An attempt that fails is retried after 10 s, 30 s, 1 min, 5 min and 15 min, six attempts in all; a delivery whose wait ended first (decided, expired, cancelled) or whose target the bot no longer has is skipped. Every attempt, retry, give-up and skip is in the audit log as `approval.notify` with the delivery, the task, the target and the reason; the daemon logs one line per attempt.

The payload (`schema: axlr-bots.approval-request/v1`):

```json
{
  "schema": "axlr-bots.approval-request/v1",
  "deliveryId": "dlv-6f0c…", "event": "bot.task.waiting_approval", "target": "ops",
  "tenantId": "default", "botId": "bot-act-with-approval", "botName": "bot-act-with-approval",
  "taskId": "task-…", "executionId": "exec-…", "correlationId": "trace-…", "nodeId": "gx10-ff95",
  "objective": "Create hello.txt in your workspace containing hello",
  "call": {"tool": "local_write", "callId": "c-1", "sessionId": "…", "arguments": {"path": "hello.txt", "content": "hello"}},
  "requestedAt": "2026-10-25T18:00:11Z", "deadline": "2026-10-25T18:02:11Z", "onExpire": "deny",
  "correlation": {"botId": "…", "taskId": "…", "executionId": "…", "traceId": "trace-…"},
  "actions": {
    "approve": {"method": "POST", "path": "/v1/tasks/task-…/approve?call=c-1", "url": "https://cp.example.internal:7879/v1/tasks/task-…/approve?call=c-1"},
    "deny":    {"method": "POST", "path": "/v1/tasks/task-…/deny?call=c-1",    "url": "https://cp.example.internal:7879/v1/tasks/task-…/deny?call=c-1"},
    "task":    {"method": "GET",  "path": "/v1/tasks/task-…"}
  }
}
```

Arguments above 64 KiB are left out (`argumentsOmitted: true`; `task executions` has them). The `url`s are on the operator API (`serve --api-url`, default `https://<api-listen>`; absent when the control plane serves no operator address) and need its bearer token; the call id in them makes a link decide only the call it was sent for.

### Webhook target

`{"type": "webhook", "name": "ops", "url": "https://…", "secret": "<16 to 256 characters>"}`: a `POST` of the payload with `Content-Type: application/json`, `X-Axlr-Delivery` and `Idempotency-Key` (the delivery id), `X-Axlr-Event: bot.task.waiting_approval` and `X-Axlr-Signature: sha256=<hex>`, the HMAC-SHA256 of the body under the secret (the convention of a webhook trigger and of GitHub). Any 2xx is a delivery; a redirect is not followed; each attempt has 10 s. The receiver checks the signature over the raw body before it reads it:

```python
import hashlib, hmac
expected = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
assert hmac.compare_digest(expected, request.headers["X-Axlr-Signature"])
```

**Slack, Teams and the rest.** There is no Slack or Teams adapter: a chat incoming webhook takes its own message shape and cannot check a signature. Put a small relay between them, a function or a service that receives the webhook, checks the signature, drops a repeated `deliveryId`, and posts a message (the bot, the objective, the tool and its arguments, the deadline) to the chat's incoming webhook. To approve from the chat, the relay holds the operator token and turns a button into the `POST` of `actions.approve.url` or `actions.deny.url` with the chat's user as `X-Axlr-Actor`: that name is who decides, checked against the bot's `approvers` ([who may decide](#who-may-decide)); the control plane trusts the relay to say it truly.

### Command target

`{"type": "command", "name": "desktop", "command": ["/usr/local/bin/axlr-notify", "--urgency", "critical"]}`: the program (an absolute path) runs on the control plane's host with the payload on its stdin and `AXLR_BOTS_DELIVERY_ID`, `AXLR_BOTS_TASK_ID`, `AXLR_BOTS_BOT_ID` and `AXLR_BOTS_EVENT` in its environment; exit status 0 is a delivery, anything else a failed attempt with the last line of its stderr. It has 10 s. A desktop notification:

```sh
#!/bin/sh
# /usr/local/bin/axlr-notify: read the request, show it.
jq -r '"\(.botId) asks for \(.call.tool) on \(.taskId) before \(.deadline)"' | xargs -0 notify-send "$@" "AXLR approval"
```

A user-level control plane needs `DISPLAY` or `DBUS_SESSION_BUS_ADDRESS` in its environment for `notify-send` to reach the desktop.

## Without a person

`approvals.autoApprove` names the calls the control plane approves itself, such as a worker's `rollback` of its own job: `{"autoApprove": [{"tool": "rollback"}]}` ([reference](../reference/bot-definition.md#approvals)). The task still stops on the call for a moment (`bot.task.waiting_approval`), then `bot.task.decided` follows at once with `decidedBy: policy:autoApprove`, and nobody is notified. Keep every other call, starting a new job or changing something new, a person's. A bot's call of `axlr_bots_emit_event` (telling its team) is approved the same way without any rule, as `policy:team-event`.

## Cancel instead

```bash
axlr-bots task cancel task-…
```

A `WaitingApproval` task ends `Cancelled` with `approval.outcome: cancelled`; the decision is never asked, pending notifications are skipped, and the session stays where it is.

## An incident's apply

The apply phase of an incident ([triggers](triggers.md#incidents)) waits here like any task: its bot runs at `act-with-approval`, its write call stops it in `WaitingApproval`, `warn.approval` and the notifications go out, and `task approve` or `task deny` (or the panel's **Approve** and **Deny**, on the task's page or the incident's) decides it. The decision moves the incident at once, before the call runs: approved is `Applying`, denied is back to `Open` with the reason, and the new cycle's triage starts at once. An expiry is a denial. Every return to `Open` counts (a denial, an expiry, a failed apply, a red verify): the second since the incident was last reopened escalates it. Only the task the incident stands on now can be approved: a task of a cycle the incident left, or of an incident resolved meanwhile, is refused (`409 state`) and may only be denied, and a superseded apply that asks again is cancelled. A wait of a task its incident no longer stands on (resolved meanwhile) notifies nobody; its expiry ends it. `incident get` counts the incident's `approvals`, one per task by its last wait: the product's rule is one per incident, shown and not enforced.

## Who may decide

`approvals.approvers` lists who may approve **and** deny the bot's calls ([reference](../reference/bot-definition.md#approvals), [ADR](../architecture/approvers.md)). The name is the actor a decision carries: the CLI's global `--actor` (default `cli:$USER`), the panel's **Decide as** (`controlPlane.actor` in its settings), or `X-Axlr-Actor` / `decidedBy` on the API. It is compared without the CLI's `cli:` prefix and without case (`tirso`, `cli:tirso` and `Tirso` are one person), so the CLI's default `cli:tirso` matches `"approvers": ["tirso", "oncall"]`.

```bash
axlr-bots task approve task-…                  # as cli:tirso: decided by tirso
axlr-bots --actor mallory task approve task-…  # refused
axlr-bots: forbidden: mallory is not among the approvers of bot fix
```

A refusal changes nothing and is audited `approval.refused` with the actor and the decision; a bot that lists approvers refuses a decision that names nobody too (an old panel, a relay that sends no header). `bot.task.decided` says `decidedBy`, and every audit line of the decision has the named actor. A bot without `approvers` takes anyone, as before; a decision that names nobody is then the control plane's own actor. `autoApprove` and the team's events are the control plane's policy, not a person, and are not checked against the list; no person may decide as `policy:…`, nor, on a daemon of a bot with approvers or two persons, as the control plane's own actor.

**The name is asserted, not proven.** Whoever holds the operator token can type any name; `approvers` stops mistakes and keeps an honest audit, not a token holder who lies. Per-actor tokens are [#137](https://github.com/underpass-ai/axlr-bots/issues/137).

## Two persons

`approvals.twoPersons` makes a call of an incident wait for two different people, and needs `approvers` listing at least two: `{"namespaces": ["prod", "shop-prod"]}` reads the incident's namespaces (every `namespace` label its alerts carried, or `incident open --namespace`; any one listed is enough), `{"severities": ["sev1", "sev2"]}` its severity; both may be given, one match is enough. Set it on the bot that applies (the apply phase's).

```text
WaitingApproval (approvalsNeeded: 2) ──approve by tirso──▶ WaitingApproval, approvedBy [tirso]     bot.approval.first, nothing runs
                                     ──approve by tirso──▶ 409 state (the same person)            approval.refused
                                     ──approve by oncall─▶ Running (decide execution)              bot.task.decided {approvedBy: [tirso, oncall]}
                                     ──deny by either───▶ the wait ends denied                    incident back to Open
                                     ──deadline─────────▶ expired, approvedBy kept                 as any expiry
```

`task get` and `incident get` show `approval.approvalsNeeded: 2` and `approval.approvedBy: [{"actor": "tirso", "at": "…"}]`; the panel's Needs-you card on the incident says **1 of 2 approvals · approved by tirso**. The first approval exits 3 (the task still waits) and answers with the stopped execution; the incident stays `Proposed`. Both approvals name their actor: one that names nobody is refused. The deadline is the wait's from the start: the second person decides within the same `ttl`. An `autoApprove` rule never decides such a call; the team's events (emit, subscribe) are approved outright as before and are not one of the two. A task of no incident waits for one person whatever the lists say: two persons reads the incident.

**Production.** Production stays at `propose` by default (axlr-sre Q6). The lab runs `act-with-approval` with `approvers` set to the judge's actor. Production goes to `act-with-approval` only with `approvers` and `twoPersons` on the bot that applies.

## Limits today

The actor is the token holder's word: one token, any name, until per-actor tokens ([#137](https://github.com/underpass-ai/axlr-bots/issues/137)). `approvers` is one list per bot, not per namespace. Two persons is for incidents only, and two at most. There is no queue view of pending approvals beyond `task list --status WaitingApproval`. Notifications go to webhooks and local commands; chat services are reached through a relay, which should send the chat user as `X-Axlr-Actor`. The `observe`, `propose` and `act-within-policy` levels never stop for a decision: the policy either allows the call or refuses it before any card.
