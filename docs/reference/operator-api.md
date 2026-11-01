# Operator API

The vision's minimal API (§11.1) plus the routes the CLI uses. Everything is HTTP+JSON; the JSON is the same the CLI prints.

## The contract

[`operator-api.json`](operator-api.json) is the operator address as OpenAPI 3.1: every route, every request and answer, every enum. Two tests hold it:

| Test | Checks |
|---|---|
| `adapters/unixapi/contract_test.go` | the routes are the server's; each schema has its Go type's JSON fields, required ones and kinds; each enum is the domain's constants |
| `panel/tests/operator_contract.rs` | the panel reads every answer it asks for: with every field, with the required ones only, and with every enum a value it does not know |

The enums are open (`x-extensible-enum`): a client reads a status, a trigger or an event type it does not know as unknown and goes on. A new status in `domain` fails the Go test until it is in the contract; the contract's change runs the panel's test.

## Listeners

| Listener | Flag | Serves | Credential |
|---|---|---|---|
| Unix socket `<state>/axlr-bots.sock` (mode 0660) | always | every route: the CLI's path | the socket's permissions (same host, same user); the caller is trusted |
| node address (TLS 1.3) | `serve --listen <addr>` | `/healthz`, enrollment and the node routes only ([protocol](node-protocol.md#messages)) | the node's client certificate |
| operator address (TLS 1.3) | `serve --api-listen <addr>` | the operator routes below and `/healthz`; no node route | `Authorization: Bearer <token>`; a webhook its own secret or signature |

The TLS certificate of both addresses is issued by the fleet CA (`ca init`; clients pass `--cacert <state>/ca/ca.crt`). The bearer token is generated at the first start with `--api-listen`, written to `<state>/api.token` (mode 0600, `--api-token-file` names another path) and never logged. A node certificate is no credential on the operator address, which keeps node identity and operator access apart (operator client certificates would need their own CA profile; the bearer token is the one credential). Bodies are bounded (1 MiB; a webhook 256 KiB) and unknown fields are refused.

Every CLI command tries the socket first (`--socket`, `AXLR_BOTS_SOCKET`, default `<state>/axlr-bots.sock`): when the daemon answers, the command is carried by it; when nothing answers, the command works on the state directory directly and a submitted task is spooled for the daemon's next start.

## Routes

| Route | Does | Idempotency |
|---|---|---|
| `POST /v1/bots` | register a bot from its definition | per id: a repeat is `409 conflict` |
| `GET /v1/bots`, `GET /v1/bots/{id}` | list, show (credentials redacted) | |
| `PATCH /v1/bots/{id}` | replace the fields the patch names | the patch is the whole value of each field |
| `POST /v1/bots/{id}/enable` | let the bot accept tasks (a Registered or Paused bot) | |
| `POST /v1/bots/{id}/pause` | put an Enabled bot on hold: it accepts no task, by hand or from a trigger, until it is enabled again; publishes `bot.paused` | another pause is `409 state` |
| `POST /v1/bots/{id}/stop` | stop the bot at once: pause it when it is Enabled and cancel every task it has not finished (Pending, Running, WaitingApproval), each with the reason `stopped by <actor>`; answers `{bot, cancelled, notCancelled}`. Enable undoes the pause | a second stop finds nothing to cancel |
| `POST /v1/bots/{id}/tasks` `{objective, correlationId?, priority?}` | submit a task; `Idempotency-Key` header (or `idempotencyKey` in the body; the header wins). `priority: urgent` makes it go first and cancels the bot's Running and WaitingApproval tasks that are not urgent (`bot.task.preempted`, reason `preempted by the urgent task …`) | same bot and key: same task, `200` instead of `201` |
| `GET /v1/bots/{id}/events?limit=100` | the bot's newest events, oldest first (at most 1000) | |
| `GET /v1/groups/{name}/events?after=&limit=100&wait=&tenant=` | the team's timeline: every event of its members, oldest first, after the event `after` names (absent, or older than the window: the newest `limit`, at most 1000). With `wait` (a duration up to 30 s) it answers as soon as there is a new event, or `[]` when `wait` passes: the panel's live view. The operator's own events (`group.event.published`, naming the team, no bot) are in it | |
| `POST /v1/groups/{name}/events?tenant=` with `{name, message, data?, to?}` and `Idempotency-Key` | the operator publishes on the team: the same checks and deliveries as a bot's event, as `operator`, less the rate and the chain; `error.*` and `done.*` are `400` (`not_allowed`), a `to` outside the team `400` (`not_member`); `stop.all` stops every Enabled member: each gets an urgent task | `201` with the event id, deliveries and skipped; `200` and nothing new for a repeated key |
| `GET /v1/groups/{name}/topics?tenant=` | the team's topics, derived: the names its newest 1000 events used (publishers seen, `lastAt`, `count`), the patterns its members subscribe to (listeners, mode, `pending`) or may emit (`declaredPublishers`); `nobodyListens` on a name published that no member listens to now | |
| `GET /v1/groups`, `GET /v1/groups/{name}?tenant=` | the teams the bots name, each with its members: id, name, status, purpose (the first sentence of the mission), tools, where it runs, its teams and whether it takes its teammates' delegations ([teams](bot-definition.md#teams)). A team exists while a bot names it; `tenant` is needed only when several tenants have a team of that name (else `400`); no bot naming it is `404` | |
| `POST /v1/incidents` `{tenant, botId, groupKey, title, severity?, source?, fingerprints?, objective?}` | report an alert group to the bot that triages it (it declares the `incident` trigger). A new tenant and group key opens an incident and submits the bot's triage task (key `incident:<id>:<cycle>`, the incident as the task's `input`, objective from the trigger's template or the default); the same key while the incident is open is a repeat (`incident.repeated`, alerts added, no task); the same key less than 30 minutes after its resolution reopens it (`incident.reopened`, cycle + 1, a new task); later, a new incident. When the triage task ends, the incident moves on by itself: Completed is its diagnosis (`incident.diagnosed`), Failed, Expired or Cancelled escalates it to a person (`incident.escalated`), and every execution's cost is added | `201` new, `200` a repeat (`task` absent) or a reopening (`reopened` true) |
| `GET /v1/incidents?tenant=&status=&limit=100`, `GET /v1/incidents/{id}` | the incidents, the last seen first (at most 1000); one incident: its alerts, cycle, tasks, diagnosis, reason, repeats, retries, cost (delegated children included) | |
| `POST /v1/incidents/{id}/resolve` `{reason?}` | close the incident (`incident.resolved`); its running triage task still settles and its cost is still added, but it moves the incident no further | a resolved incident is `409 state` |
| `POST /v1/triggers/{bot}/{trigger}` | fire a webhook with the body as the event; `Idempotency-Key` is the delivery id ([triggers](../runbooks/triggers.md)). On an `alertmanager` trigger the body is Alertmanager's payload and the answer names the incident and the `action` (opened, repeated, reopened, resolved, ignored); the task is absent for a repeat, a resolution or a group nobody had | same delivery (or same body): same task, `200`; an Alertmanager group: one incident, `201` only when it opens |
| `GET /v1/tasks?bot=&status=&since=&limit=100` | the tasks across bots, newest created first; each parameter optional: `bot` one bot (an unknown bot is `404`), `status` one status, `since` created at or after an RFC 3339 time, `limit` at most 1000 | |
| `GET /v1/tasks/{id}` | show a task: status, trigger, input, trace id, executions; a delegated task's `delegation` (parent task, execution and bot, depth, the chain's limit, the chain, its `grant`), a task that delegated its `children` and what it granted them (`grantedModelCalls`, `grantedToolCalls`) ([delegation](../architecture/delegation.md)) | |
| `GET /v1/tasks/{id}/events?limit=100` | the task's timeline: its newest events, oldest first (at most 1000) | |
| `GET /v1/tasks/{id}/executions` | the task's executions with their correlation, policy, usage, result and receipt; `result.artifacts` lists what each delivered, `stored: true` for the ones the control plane keeps ([artifacts](#artifacts)) | |
| `POST /v1/tasks/{id}/cancel` | stop a task: Pending never runs, Running ends `Cancelled` (the in-process run is interrupted, a node's run stops at its next lease renewal, the late report is dropped), WaitingApproval never gets its decision | a Cancelled task answers `200` with `changed: false`; Completed or Failed is `409` |
| `POST /v1/tasks/{id}/messages` | write to a Running task `{text}` (at most 4000 characters): the task's timeline gets `bot.task.message` naming the execution, and the node running it hands the text to the run between model steps (at once over the node's stream, else within one lease renewal, 10 s; then the run's next model step); what the run says back comes as `bot.task.said` in the same timeline; answers `{taskId, executionId, createdAt}` | any other task is `409 state`: send it a new task instead; a runtime without `--steer-file` drops the message (logged on the node) |
| `POST /v1/tasks` `{botId, objective, idempotencyKey?}` | the older form of submit, with the same header | as above |
| `POST /v1/tasks/{id}/approve?call=`, `POST /v1/tasks/{id}/deny?call=` | decide the call a WaitingApproval task waits on; no body. `call`, when given, must name the pending call (a notification's links carry it), else `409 state`; after the call's deadline `409 approval_expired` and nothing runs ([approvals](../runbooks/approvals.md)) | a task decided once is `409 state` |
| `POST /v1/tasks/{id}/decide` `{decision, callId?}` | the same with the decision in the body | as above |
| `POST /v1/tasks/{id}/run`, `GET /v1/tasks/{id}/report`, `GET /v1/executions/{id}` | the CLI's run, report and execution routes | |
| `GET /v1/executions/{id}/artifacts/{name}` | the bytes of an artifact the control plane keeps for the execution, as a download: `Content-Type` its media type, `X-Axlr-Artifact-Sha256` its hash; `404` for one not kept | |
| `GET /v1/overview?since=` | the dashboard's counts ([below](#the-overview)) | |
| `GET /v1/nodes`, `GET /v1/nodes/{id}`, `POST /v1/enrollments`, `POST /v1/nodes/{id}/revoke` | the nodes: list, show, mint a token, revoke; a node shows `certificateNotAfter`, its certificate's expiry, once the control plane has seen it (at enrollment, or on the node's next call over mutual TLS) | |

## The idempotency key

A manual submission carries the caller's key (`task submit --key`, the `Idempotency-Key` header). The same bot and key always name the same task: the second call answers `200` with the task the first created and nothing runs twice. A webhook fire derives its key from the trigger and the delivery id (or the body), a timer from the trigger and its slot, so a redelivery or a restarted scheduler finds the task it already created ([triggers](../runbooks/triggers.md)). A task also keeps a trace id (`correlationId`, minted as `trace-…` unless the caller passes one) that every execution and event repeats ([runtime contract](../architecture/runtime-contract.md#correlation)).

## The overview

`GET /v1/overview` answers what the control plane holds now and what happened since a moment: `since` (RFC 3339; absent, the start of today on the control plane's clock), and every status at zero when nothing is in it.

```json
{
  "since": "2026-10-25T00:00:00+02:00",
  "nodes": {"Registered": 0, "Online": 2, "Offline": 0, "Revoked": 0},
  "bots": {"Draft": 0, "Registered": 0, "Enabled": 3, "Paused": 1},
  "tasks": {"Pending": 0, "Running": 1, "WaitingApproval": 1, "Completed": 58, "Failed": 2, "Expired": 5, "Cancelled": 1},
  "tasksSince": {"Pending": 0, "Running": 1, "WaitingApproval": 1, "Completed": 4, "Failed": 0, "Expired": 4, "Cancelled": 0},
  "executionsSince": 6,
  "costUsdSince": 0.0123
}
```

`tasks` counts every task by its status now; `tasksSince` the tasks created since the moment. `Expired` is a task nobody decided in time ([approvals](../runbooks/approvals.md#expiry)), counted apart from `Failed`; `executionsSince` and `costUsdSince` the executions started since then and the cost they reported. The `axlr-bots overview` command prints the same.

## Artifacts

An execution that delivers files with AXLR's `axlr_deliver` lists them in its result with a name, a media type, a size and a SHA-256 ([runtime contract](../architecture/runtime-contract.md)). The control plane keeps a copy of each under `<state>/artifacts/executions/<execution-id>/<name>`: a node uploads the bytes from its workspace over its mutual-TLS channel before it reports ([node protocol](node-protocol.md#artifacts)), and an execution the control plane runs itself is read from its own workspace. A copy is kept only when its bytes are the size and hash the result lists; at most 32 MiB each, 64 and 256 MiB per execution; a later delivery of the same name replaces the earlier one. The execution lists every artifact and marks the kept ones `stored: true`; one that did not match (a file changed after its delivery) or came from an AXLR build without the hash is listed without it. `report.md` with `source: answer` is the control plane's own report of the answer, read with `GET /v1/tasks/{id}/report`.

```bash
axlr-bots execution artifact exec-… notes.md --out notes.md     # checks the SHA-256 on the way
curl --cacert $state/ca/ca.crt -H "Authorization: Bearer $(cat $state/api.token)" -OJ \
     https://cp.example.internal:7879/v1/executions/exec-…/artifacts/notes.md
```

The execution also keeps AXLR's receipt (`axlr.execution-receipt/v1`: every request with its model and cost, every tool call with the hashes of its arguments and outcome) as `receipt`, when the build writes one.

## Errors

`{"kind": "validation|not_found|state|conflict|forbidden|unauthorized|too_large|approval_expired|internal", "error": "…"}` with the matching status (`400`, `404`, `409`, `409`, `403`, `401`, `413`, `409`, `500`). `approval_expired` is a decision that came after the pending call's deadline: it authorises nothing.

## Examples

```bash
state=~/.local/state/axlr-bots
curl --cacert $state/ca/ca.crt -H "Authorization: Bearer $(cat $state/api.token)" https://cp.example.internal:7879/v1/bots
curl --cacert $state/ca/ca.crt -H "Authorization: Bearer $(cat $state/api.token)" -H "Idempotency-Key: deploy-42" \
     -d '{"objective": "List the files in your workspace"}' https://cp.example.internal:7879/v1/bots/bot/tasks
curl --cacert $state/ca/ca.crt -H "X-Axlr-Secret: $secret" -H "Idempotency-Key: gh-delivery-1" \
     -d '{"type": "deploy", "service": "web"}' https://cp.example.internal:7879/v1/triggers/bot/deploy
curl --cacert $state/ca/ca.crt -H "Authorization: Bearer $(cat $state/api.token)" -X POST \
     "https://cp.example.internal:7879/v1/tasks/task-…/approve?call=call-…"
```
