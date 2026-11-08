# ADR: incident outputs to a webhook, Slack or a command

Status: accepted, 8 November 2026. axlr-sre design §4 (the Incident aggregate), §6 (the brigade), §9 (the panel), §10 (outputs: "Slack / Teams: incident opened, cause, card with a link to the panel, resolved"), §12 (phase 2). Builds on the incident phases ([ADR](incident-phases.md)) and on the approval notifications' outbox ([approvals](../runbooks/approvals.md#notifications)).

## Context

An incident moves by itself: opened, diagnosed, proposed, applied, verified, resolved or escalated, each move an `incident.*` event on the bot that triages it. A person sees it only in the panel or over the operator API. The design asks for an output adapter, the mirror of the input adapters ([ADR](input-adapters.md)): the incident told where the people are, Slack first.

Two things already exist that look close and are not the same. The approval notifications (`approvals.notify` of a bot) announce a task that waits for a person's decision, and their runbook refuses a Slack adapter on purpose: the person answers through the notification, and a chat's incoming webhook can neither sign nor be checked. The event bus carries `incident.*` to bots that subscribe, inside a team. An output does neither: it only posts what happened, to a place a person reads, and nothing comes back. For that a chat's incoming webhook is fine: its URL is its credential.

## Decisions (8 November 2026)

| # | Decision |
|:--|:--|
| D1 | Outputs belong to the **control plane**, not to a bot: one list of targets, kept in `<state>/outputs.json` (mode 0600, written atomically: a chat webhook's URL and a webhook's secret are in it), managed by `axlr-bots output add\|list\|remove` and `GET`/`POST /v1/outputs`, `DELETE /v1/outputs/{name}`. `list` and every answer show a fingerprint (`sha256:` and 12 hex digits over the type, URL, secret and command) and the host, never the URL, the secret or the command |
| D2 | A target has a `name`, a `type` (`webhook`, `slack` or `command`), what the type needs, and three filters: `events` (default `opened`, `reopened`, `diagnosed`, `proposed`, `applying`, `verifying`, `resolved`, `escalated`), `tenants` (default all) and `severities` (default all). `repeated` is left out of the default (a repeat comes every repeat interval of the source) and may be named; `reopened` is in it, since it is how a proposal sent back (a denied, expired or failed apply, a red verify) is announced (review of #136) |
| D3 | `webhook`: a POST of the incident event as JSON (`axlr-bots.incident-event/v1`) with `X-Axlr-Event: incident.<event>`, `X-Axlr-Delivery`, `Idempotency-Key` and `X-Axlr-Signature: sha256=` the HMAC-SHA256 of the body under the target's secret: the approval webhook's conventions |
| D4 | `slack`: a POST of a Block Kit message to the incoming webhook, unsigned: a header with the severity and the title, the facts (status, bot, cycle, cost), the cause once diagnosed, the proposal and "Needs you" when proposed, the end with its duration when resolved, the reason and "Needs you" when escalated, and a button to the panel's incident page when a panel URL is known (`serve --panel-url`, or the target's `panelUrl`); without one there is no button. One message per event: no threading, no update of an earlier message |
| D5 | `command`: the approval command's rules (an absolute program, the body on its stdin, a minimal environment, 10 s): the relay to Teams or to anything else |
| D6 | A delivery per (incident event, target), durable, in the approval notifications' outbox directory (`<state>/deliveries/outputs/`), made by the daemon on a loop of its own, beside the notifications', with the same retries (10 s, 30 s, 1 min, 5 min, 15 min; six attempts) and audited as `incident.output` with the target, the incident, the event, the attempt and the outcome. A delivery whose target was removed, or replaced by one of another type, is skipped |
| D7 | No secret in a log, an audit entry, an error or an API answer: an attempt's error names the target and says why (`output chat: connection refused`, `HTTP 404: no_service`), never the URL that net/http puts in its errors |
| D8 | Delivery is **at least once**. An attempt is made, then recorded: a control plane that stops between the two sends the same delivery again at its next start. A webhook or a command drops a repeat by `deliveryId` (`Idempotency-Key`); Slack cannot, and the channel may read a message twice. Queueing is idempotent: one event is one delivery per target, however many times it is queued |
| D9 | Order and isolation (review of #136): a pass goes oldest first (by creation, then id); an incident's delivery to a target waits while an earlier one of the same incident to the same target is still pending, so a channel never reads "resolved" before "opened"; a target whose attempt failed gets no other attempt in that pass, so a host that does not answer costs one timeout per pass; and the outputs run on their own loop, so they never delay an approval notification a person waits for |

## How the decisions are carried out

### Where it hooks in

Every incident event goes through one function, the recorder's `incidentEvent` (opened, repeated, reopened from `open_incident.go`; diagnosed to escalated from `incident_advancer.go`; resolved from `resolve_incident.go`, `fire_alertmanager.go` and `delete_bot.go`). Once the event is published, `incidentOutputQueue` reads the targets, keeps those whose filters want the event, builds the body once (the incident as it stands after the move, the diagnosis and the proposal cut to 2 KiB), and queues one delivery per target. The delivery id is `dlv-` and 24 hex digits of the SHA-256 of `incident.output`, the event id and the target name: the same on every queueing, so the outbox holds one delivery per event and target, and never an approval notification's id. A receiver can still see it twice (D8).

A queueing that fails (the targets file unreadable, a disk full) is audited `incident.output` with outcome `failed` and the reason, and the incident's move goes on: an announcement never undoes or stops what it announces.

The body is fixed when the delivery is queued, so every attempt sends the same bytes (the signature is over them): the JSON event for a webhook and a command, the Block Kit message for slack. The panel link is `<base>/incidents/<id>`, from the target's `panelUrl` or `serve --panel-url`; the operator API's link is `GET /v1/incidents/<id>`, with its URL when `serve --api-url` (or `--api-listen`) gives one. A command that queues an event without a daemon (no socket answers, so it works on the state directory) knows neither base, and its deliveries carry the paths alone.

### The layers

| Layer | Files |
|:--|:--|
| domain | `output_kind.go`, `output_name.go`, `output_target.go` (validation, filters, `Wants`, `PanelLink`, `Host`, `Fingerprint`, `Sign`), `output_target_snapshot.go`, `output_delivery.go` and its snapshot (the attempt state machine) |
| application | ports `OutputRepository`, `OutputDeliveryRepository`, `OutputSender`; DTOs `OutputDefinition`, `OutputView`, `IncidentOutputEvent`, `SlackIncidentMessage`, `OutputDeliveryOutcome`; `incident_output_queue.go`; use cases `AddOutput`, `ListOutputs`, `RemoveOutput`, `DeliverOutputs` |
| adapters | `filestore/output_repository.go` (`outputs.json`), `filestore/output_delivery_repository.go` (`deliveries/outputs/{pending,done}`), `notify/output_sender.go` (the posts and the command, the approval sender's client and environment), `daemon` (`outputLoop`: one pass of `DeliverOutputs` per `deliverEvery`, on its own goroutine beside the notifications' loop), `unixapi` (the three routes, `Service`, `Local`, `Client`) |
| commands | `cmd/axlr-bots/output_commands.go`; `serve --panel-url` / `AXLR_BOTS_PANEL_URL` |

### Why a delivery of its own

The approval notification's `Delivery` names a task, an execution and a pending call, and its restore refuses one without them; its sender sets `X-Axlr-Event: bot.task.waiting_approval`. Widening it to carry an incident event would have touched the approval path, whose skip rule (the wait ended) means nothing for an output. `OutputDelivery` is a type of its own that shares the id, the statuses, the attempt limit, the backoff, the directory and the loop. The two attempt state machines are the same code twice; folding them into one shared value is the follow-up.

### The CLI and secrets

`output add <name> --type slack --url-file <file>` (or `-` for stdin) and `--secret-file` keep the URL and the secret out of the shell's history and the process list; `--url` exists for a URL that is no secret. A command target's program and its arguments come after `--`, so their own flags are never read as the CLI's. `--file <output.json>` takes the whole definition.

## What an operator reads

The [outputs runbook](../runbooks/outputs.md): how to add a Slack target, how to try one with a local receiver, the body shapes, what the audit and the log say.

## Left out

- **Threading and updating.** One Slack message per event; the next one does not reply in the first one's thread nor edit it. An incoming webhook cannot do either: it needs a bot token and `chat.postMessage`/`chat.update`, a different credential and a different adapter.
- **Teams natively.** A Teams workflow webhook takes an Adaptive Card; until it is asked for, a `command` target relays the JSON.
- **Deciding from the chat.** An output only tells; the approval stays the task's, from the panel, the CLI or the approval notification's relay ([approvals](../runbooks/approvals.md#webhook-target)).
- **A test send.** There is no `output test`; the runbook opens a manual incident to try a target.
- **Per-bot outputs and a status page, PagerDuty** (design §10, phase 3).
- **The panel.** It shows no Outputs line yet; `GET /v1/outputs` is there for it.
