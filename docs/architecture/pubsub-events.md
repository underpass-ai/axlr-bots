# ADR: publish/subscribe events between bots

Status: accepted, 28 October 2026 (the product owner settled each question below in turn: topics per team, «C» for who subscribes, the modes and the kinds, the panel «Aprobado», the limits and the migration «déjalo todo preparado»). Vision principle 13; builds on [teams, events and urgent tasks](groups-and-events.md) and [delegation](delegation.md). This document is written so that a session can implement it in the ordered PRs at the end, each with its acceptance checks, without asking again.

## Context

Today a bot tells its team something with `axlr_bots_emit_event {name, message, severity, data, to}`: the emitter names the receivers (`to`), or broadcasts; a receiver hears what is addressed to it and the team's `warning` and `critical` broadcasts; every event heard is a new task. The product owner wants bots that talk **by publish/subscribe**: the emitter names a topic, not the receivers; whoever subscribed hears it; the operator sees and controls who hears what from the panel. The guide principles hold: "desde el panel siento que controlo mis bots", "no se traslada la complejidad al usuario", "los bots se conocen y pertenecen a grupos". Bots stay generic (a shop, a cluster, a fleet); the vehicle fleet is only the live demo.

Most of pub/sub already exists. The event's `name` is a dotted topic (`pedido.nuevo`); an `event` trigger with `events: ["pedido.*"]` is a subscription; an event without `to` is a broadcast; the control plane is the bus. What is missing, and what this ADR adds:

| Missing today | Added |
|---|---|
| subscriptions live only in a definition file, under *Advanced* | a `subscriptions` field, shown and edited on the bot's page; a bot may ask for one and the operator approves |
| `info` broadcasts reach nobody | a subscriber hears every event its topic pattern matches |
| the emitter chooses the severity | the topic's **kind** (its first segment) fixes the severity |
| a heard event is always a new task | a **mode** per subscription: `task`, `message` (into the running task) or `note` (into the next prompt) |
| the control plane's own facts (a task failed, finished, waits) never reach a teammate | the control plane publishes `error.bot`, `warn.approval` and `done.task` on the team's bus |
| the operator cannot publish | `POST /v1/groups/{name}/events` and a **Publish an event** button |
| nobody can see which topics exist | `GET /v1/groups/{name}/topics` and a **Topics** tab |
| the emitter's rate is the only limit | a subscriber's intake rate, coalescing of events into a pending task, chain depth 2 |

**There is no broker.** The control plane is the bus, as it is today: an event is a node→control plane call, a delivery is a task, a message or a note the control plane makes. NATS, Kafka or any queue would add a second source of truth for nothing this fleet needs.

```text
bot A (its run) ──axlr_bots_emit_event {name "pedido.nuevo", message, data}──▶ node ──▶ control plane
                                                                                            │ team of A: who subscribes to pedido.nuevo?
                                                                                            ├─▶ bot B, subscription pedido.* mode task  ──▶ a task of B
                                                                                            ├─▶ bot C, subscription pedido.* mode note  ──▶ in C's next prompt
                                                                                            ├─▶ history: bot.event.emitted on A, bot.event.heard on B and C
                                                                                            └─▶ panel: team timeline, Topics tab, B's and C's pages
operator ──POST /v1/groups/{name}/events {name "stop.all", message}──▶ control plane ──▶ an urgent task for every member that hears stop.*
control plane (B's task failed) ──error.bot on B's teams──▶ bot S, subscription error.* mode task ──▶ a task of S
```

## Decisions

| # | Question | Decision |
|---|---|---|
| 1 | Where a topic lives | **In a team.** A topic is the event's `name`; it exists in the team the emitter and the subscribers share. Only members of that team hear it. No global topics; a bot in two teams hears each team's topics; an event of a bot in two teams goes to both. |
| 2 | Who subscribes | **The operator commands; the bot may ask.** A subscription is a field of the bot's definition, shown on its page («Listens to»). A bot may ask for one with the tool `axlr_bots_subscribe {topic, why}`; the ask is a *pending* subscription the operator approves or denies in the panel. Nothing is heard until it is approved. |
| 3 | What a heard event does | **A mode per subscription**: `task` (a new task, as today), `message` (into the bot's running task through mid-run chat; a new task when it has none), `note` (kept and put in the prompt of the bot's next task; no task). The default follows the severity: `info` → `note`, `warning` → `message`, `critical` → urgent `task`. |
| 4 | Severity | **The topic's first segment is its kind, and the kind fixes the severity.** The emit tool no longer takes `severity`. Six kinds: `error`, `danger`, `stop` (critical), `warn` (warning), `done`, `info` (info). `error` and `done` are the control plane's only; a name whose first segment is no kind (`recall`) is `info`. |
| 5 | The panel | Bot page → **Listens to** (its subscriptions: topic, mode, from whom; **Listen to a topic**; a card for each pending ask with **Approve** / **Deny**). Team → **Topics** (each topic: who publishes, who listens, last event; **nobody listens** in red). Team → **Publish an event**. |
| 6 | Limits on the hearing side | Intake: **10 events a minute per bot**; past it, a delivery degrades to `note` (nothing is lost). Coalescing: a **Pending task not yet started absorbs** the later events of the same subscription as notes. Loops: cycle cut as today; the default depth of an event chain goes from 1 to **2**. Budget: the receiver spends **its own**, as today. |
| 7 | Migration | `to` stays: an addressed event is heard by the named teammates whether or not they subscribe. `severity` leaves the tool (accepted and ignored from an older node). A bot with no subscriptions hears what is addressed to it and the team's `danger.*` and `stop.*` (not `error.*`: that would wake every teammate on every failure; a supervisor subscribes to it). `event` triggers in a definition are read as subscriptions of mode `task` for one release. **Two behaviours change**: an addressed `warn.*` no longer interrupts (it is a `message`, or a normal task when the receiver is idle): to stop a teammate, emit `stop.*`; an addressed `info.*` or kindless name makes no task (a `note`). For one release the kindless name `recall` is read as `stop.recall`, so the deployed pilots keep recalling until their definitions move. |

## What the operator sees and does

Panel words are English, as the rest of the panel; the API names are in code.

| In the panel | How | Feedback |
|---|---|---|
| see what a bot listens to | bot page → **Listens to**: one line per subscription: `pedido.*` · `task` · from `recepcion` | live; a pending ask shows as a card above the list |
| make a bot listen | **Listen to a topic**: topic (pattern), mode (default by the kind's severity, shown), from (default anyone) → `PATCH /v1/bots/{id}` with `subscriptions` | the line appears; `bot.subscription.changed` in the timeline |
| stop it listening | the line's **Remove** | same |
| answer a bot's ask | the card «`almacen` asks to listen to `error.*`: "para reintentar si recepcion falla"» → **Approve** (status `active`) or **Deny** (removed) | `bot.subscription.decided`; the Overview's "need you" count includes pending asks |
| see the team's topics | Team → **Topics**: `pedido.nuevo` · publishes `recepcion` · listens `almacen`, `supervisor` · 2 min ago; `info.turno` · **nobody listens** | derived live from `GET /v1/groups/{name}/topics` |
| publish an event | Team → **Publish an event**: name, message, optional `to` → `POST /v1/groups/{name}/events` | the event and its deliveries in the timeline within a second |
| watch events flow | the team timeline, as today, now with the mode of each delivery: «`almacen` got a task», «told `recepcion` while it worked», «noted for `supervisor`» | long-poll as today |
| see why a bot did something | a task made by an event shows **Told by** `recepcion` · `pedido.nuevo` and the message; a message delivery shows in the task's chat as the teammate's line; the notes a task was given show in its detail under **Knew** | task detail |
| change the defaults | *Advanced*: `events.emit`, `events.perMinute`, `events.intakePerMinute`, `events.maxDepth`, `events.interruptible`; a subscription's `objective`, `urgent` | — |

## The rules underneath

### Topics and kinds

| Rule | Decision |
|---|---|
| name | dot-separated segments `[a-z0-9-]`, ≤ 64 chars (as today). The first segment is the **kind**. A pattern takes `*` (one segment) and `**` (any), as today |
| kinds | `error` critical, control plane only · `danger` critical, bots and the operator · `stop` critical, bots and the operator · `warn` warning, bots, the operator and the control plane · `done` info, control plane only · `info` info, bots and the operator. Any other first segment is `info`, except `recall`, read as `stop.recall` for one release (the deployed pilots emit it) |
| who may emit what | by default a bot may emit any name that is not `error.*` or `done.*`, 6 a minute (`events.perMinute`, ≤ 60). *Advanced* `events.emit` lists names or patterns and narrows it; `maxSeverity` on a rule is accepted and ignored (the kind decides) |
| the operator | may publish any name but `error.*` and `done.*`; `stop.all` to a team is the generalised **Recall** |
| the control plane | publishes on the bus, as the bot the fact is about and `by: control-plane`, only when a teammate of that bot subscribes to the name: `error.bot` (the task failed or expired; `data`: `taskId`, `reason`), `warn.approval` (the task waits for a person; `data`: `taskId`, `tool`), `done.task` (the task completed; `data`: `taskId`, `answer`: the task's result text, cut at 280 chars). The bot the fact is about never hears its own |
| event id | `evt-` + sha256 of the task (or the operator's idempotency key), name, message, data and `to`: the same call again is the same event and delivers nothing twice (as today, less the severity) |

### Subscriptions

A subscription is one entry of the bot's `subscriptions` (at most 16):

```json
"subscriptions": [
  {"topic": "pedido.*", "mode": "task", "from": ["recepcion"]},
  {"topic": "warn.stock"},
  {"topic": "done.*", "mode": "note", "objective": "Note what {{.from}} finished: {{.message}}"},
  {"topic": "error.*", "status": "pending", "askedBy": "almacen", "why": "para reintentar si recepcion falla", "askedAt": "2026-10-28T10:00:00Z"}
]
```

| Field | Meaning |
|---|---|
| `topic` | a name or pattern; one subscription per pattern per bot |
| `mode` | `task`, `message` or `note`; absent: by the event's severity (`info` → `note`, `warning` → `message`, `critical` → `task`) |
| `from` | only events from these bots, or `operator`, or `control-plane`; absent: anyone in the team |
| `urgent` | `true`: a `warn.*` heard by this subscription makes an urgent task (mode forced to `task`); a critical makes an urgent task unless `events.interruptible` is `false` |
| `objective` | a template over the event for mode `task`, as an `event` trigger's today; absent: `DefaultEventObjective` |
| `status` | `active` (absent) or `pending`: asked by the bot, not yet approved; a pending subscription hears nothing |
| `askedBy`, `why`, `askedAt` | who asked and why, for the card; the operator's own subscriptions have none |

| Rule | Decision |
|---|---|
| who hears an event | every Enabled teammate of the emitter, never the emitter: **addressed** (in `to`) → heard, by its matching active subscription when it has one, else by the implicit subscription (mode by severity); **not addressed** → heard only by a matching active subscription whose `from` allows the emitter; **no subscriptions and not addressed** → heard only when the kind is `danger` or `stop` (the team's default) |
| `to` | at most 16 teammates; a bot outside the emitter's teams refuses the whole event (`not_member`), as today. `not_listening` disappears: an addressed event is always heard |
| mode `task` | a task of the receiver with trigger `event` (name: the subscription's topic, or `team` for the implicit one), objective from the template, key `event:<event id>:<receiver>`, lineage as today (`EventID`, depth + 1, chain), on the receiver's own budgets and timeout; urgent as the `urgent` rule says. Preemption as today |
| mode `message` | when the receiver has a Running task with a leased execution: a `bot.task.message` on that task, `from: event:<event id>`, text `Your teammate <from> says (<name>): <message>` plus the data as compact JSON on a second line; the node relays it as the operator's messages ([mid-run chat](../reference/node-protocol.md)). Otherwise the delivery is a `task` |
| mode `note` | recorded only (`bot.event.heard`, mode `note`); the receiver's next prompt carries its notes: the last 8 `note` deliveries of the last 24 hours, newest last, ≤ 1200 chars, under "Recent events in your team". The notes are cut before the directory; the mission and the objective are never cut. A Paused bot's notes are recorded too; it reads them when it is enabled |
| intake | the receiver's `bot.event.heard` of the last minute with mode `task` or `message` are counted (`note` deliveries are free); from the 11th (`events.intakePerMinute`, 1 to 60, default 10) a `task` or `message` delivery is recorded as `note` with `degraded: "intake_rate"`. Criticals are exempt: a `stop` always lands |
| coalescing | a `task` delivery whose receiver already has a **Pending** task (not started) made by an event of the **same name** makes no second task: it is recorded as `note` with `coalescedInto: <task id>`, and that task's prompt, built when it starts, carries it in its notes. Criticals are never coalesced: a second `stop.*` is a second urgent task |
| depth and cycle | `events.maxDepth` (1 to 8, default **2**) is the chain limit of a chain an event starts; a task made by an event and a delegation share one lineage, and the limit along a chain is the least of its hops, as today. A receiver on the chain is skipped (`cycle`), as today. `message` and `note` deliveries carry no lineage: the rates cut a ping-pong of messages (6 out, 10 in, a minute) |
| budget | the receiver's own, as today (a share of the emitter's pool would starve a broadcast) |
| the bot's ask | `axlr_bots_subscribe {topic, why}` on the execution's socket, next to emit and delegate, offered to every bot in a team. The control plane adds a `pending` subscription (`bot.subscription.asked`, audit `subscription.ask`), refuses a second ask for a pattern the bot already has (`already_subscribed`, pending or active) and a 17th subscription (`too_many`), and answers the model «asked; your operator decides». The operator approves or denies with `PATCH /v1/bots/{id}` (the panel writes the list with the status changed, or without the entry) |
| events | `bot.event.emitted` on the emitter (as today, `deliveries[].mode` added, `by` for the control plane's own), `bot.event.heard` on each receiver (`eventId`, `name`, `from`, `by`, `mode`, `taskId` or `messageId`, `degraded`, `coalescedInto`), `bot.subscription.asked`, `bot.subscription.changed` (the list after the change, who changed it), `group.event.published` for the operator's events (tenant, group, name, message, to, deliveries, skipped; `botId` empty) |

### Routes

| Route | Who | Does |
|---|---|---|
| `PATCH /v1/bots/{id}` with `subscriptions` | operator | replaces the list (validation: patterns, modes, `from` are teammates or `operator`/`control-plane`, ≤ 16); a pending entry may only be kept as is, made `active` or dropped |
| `GET /v1/groups/{name}/topics?tenant=` | operator | the team's topics, derived: the subscriptions of its members (listeners, with the mode), the `events.emit` rules of its members (declared publishers), and the names seen in the newest 1000 events of the timeline (publishers seen, `lastAt`, `count`). `nobodyListens: true` when a name seen in the timeline has no active listener now and the last event was not addressed |
| `POST /v1/groups/{name}/events` with `{name, message, data?, to?}` and `Idempotency-Key` | operator | publishes as `operator` to the team: the same checks and deliveries as a bot's event, less the rate and the lineage (depth 0); `error.*` and `done.*` refused (`not_allowed`); answers `201` with the event id, deliveries and skipped, `200` on a repeat |
| `POST /v1/nodes/{id}/executions/{exec}/subscriptions` with `{topic, why}` | node | the bot's ask ([node protocol](../reference/node-protocol.md)) |
| `GET /v1/groups/{name}/events` | operator | as today; the new event types appear in it |

CLI: `bot subscribe <bot> <topic> [--mode task|message|note] [--from a,b]`, `bot unsubscribe <bot> <topic>`, `bot subscriptions <bot>` (pending ones marked), `group topics <team>`, `group publish <team> <name> --message "…" [--data '{…}'] [--to a,b]`.

### The tools

`axlr_bots_emit_event` keeps `name`, `message`, `data`, `to`; `severity` leaves the schema and is ignored when an older node sends it. Its description tells the model the kinds («`stop.` and `danger.` are critical: the teammates that hear them stop what they do; `warn.` reaches a teammate while it works; `info.` is for the record and for whoever subscribed») and lists the names it may use and its teammates, as today.

```json
{
  "name": "axlr_bots_subscribe",
  "description": "Ask your operator to let you hear a topic of your team. You hear nothing until a person approves it. Say in why what you would do with it.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "topic": {"type": "string", "minLength": 1, "maxLength": 64, "description": "a name or pattern, e.g. error.* or pedido.nuevo"},
      "why":   {"type": "string", "minLength": 1, "maxLength": 280}
    },
    "required": ["topic", "why"],
    "additionalProperties": false
  }
}
```

### The definition

Nothing written but the team, as today:

```json
"groups": ["tienda"]
```

With every field this ADR adds:

```json
"groups": ["tienda"],
"subscriptions": [{"topic": "pedido.*", "mode": "task", "from": ["recepcion"]}, {"topic": "done.*", "mode": "note"}],
"events": {"emit": [{"name": "info.*"}, {"name": "warn.*"}], "perMinute": 6, "intakePerMinute": 10, "maxDepth": 2, "interruptible": true}
```

`triggers` of type `event` are still accepted in a definition and are read as subscriptions (`events` → one subscription per pattern, mode `task`, `interrupt` → `urgent`, `objective` kept); `bot get` answers them as `subscriptions`. The next release refuses them.

## Where it goes in the code

Hexagonal, one type per file, as the rest of the repository; every new domain type has its tests next to it; coverage stays ≥ 80 % (`go test -race -coverprofile`).

| Layer | New or changed |
|---|---|
| `domain/` | `topic_kind.go` (`TopicKind`, `KindOf(EventName)`, `Severity()`, `PublishedBy`), `delivery_mode.go` (`DeliveryMode`, `DefaultMode(Severity)`), `subscription.go` + `subscription_snapshot.go` + `subscription_status.go` + `subscriptions.go` (the list, `Hearing(name, from, addressed)`), `event_note.go` (one note for a prompt) + `event_notes.go` (render, cut), `event_policy.go` (+ `intakePerMinute`, `maxDepth`; default emit rules by kind), `bot.go` (`Subscriptions()`, `Subscribe`, `Hears` rewritten over subscriptions, `InterruptedBy` over the kind), `bot_snapshot.go` (`Subscriptions`), `trigger.go` (the `event` trigger kept as a task's trigger ref; the conversion from `TriggerSnapshot` to subscriptions), `event_type.go` (`bot.event.heard`, `bot.subscription.asked`, `bot.subscription.changed`, `group.event.published`), `event.go` (`GroupName` for an event about a group), `prompt.go` (`NewTeamPrompt` takes the notes; cut order notes → directory) |
| `application/` | `emit_event.go` split: `event_publication.go` (what a bot, the operator and the control plane share: checks, deliveries, records), `emit_event.go` (the node's call), `publish_group_event.go` (the operator's), `publish_platform_event.go` (the control plane's own, called from `report_execution.go`, `expire_approvals.go`, `approve_task.go` and the waiting-approval path), `event_delivery.go` (+ `Mode`), `deliver_task.go` / `deliver_message.go` / `deliver_note.go` (one per mode), `event_intake.go` (the intake count and coalescing), `event_notes_query.go` (the notes for a prompt), `ask_subscription.go` (the node's ask), `list_group_topics.go` + `group_topic.go`, `update_bot.go` (subscriptions in a patch; the pending rules), `next_assignment.go` and `run_task.go` (notes in the prompt), `events_offer.go` (+ `Subscribe`), `event_reader.go` (`ListGroupEvents` by name for `group.event.published`) |
| `adapters/nodeagent/` | `emit_tool.go` (no `severity`; kinds in the description), `subscribe_tool.go`, the relay's dispatch |
| `adapters/unixapi/` | the three routes, `publish_request.go`, `subscribe_request.go`, `topics_response.go`; `contract_test.go` |
| `adapters/filestore/` or `adapters/jsonl/` | events about a group readable by group name |
| `cmd/` | the CLI verbs |
| `docs/reference/` | `operator-api.json` + `.md`, `bot-definition.md` (Subscriptions section replaces the event-trigger rows), `node-protocol.md` (subscribe, `bot.event.heard`), `cli.md` |
| `panel/` | `domain/subscription.rs`, `delivery_mode.rs`, `topic.rs`; `application/{list_topics,publish_event,save_subscriptions}.rs`; `ui/view/bot_detail.rs` (Listens to, the pending card), `ui/view/teams.rs` (Topics tab, Publish), `ui/view/task_detail.rs` (Told by, Knew); the contract test knows the new event types |

## Order of PRs

Each from `origin/main`, base `main`, one concern each; the control plane before the nodes before the panel, as the groups ADR says. Checks are run against a local control plane (`axlr-bots serve` with the socket) and the fake node of the tests; the live fleet only in PR 9. Build on gx10 with `GOFLAGS=-p=2` and `CARGO_BUILD_JOBS=2`.

### PR 1 — this ADR

Docs only. Check: the links resolve; `docs/architecture/groups-and-events.md` gets one line under *Left out* pointing here.

### PR 2 — topics have kinds; the emit tool loses `severity`

Domain `TopicKind`, severity derived, default emit rules by kind, `error.*`/`done.*` refused to bots, the event id without the severity, the tool's schema and description, `maxSeverity` ignored.

| Check | Expected |
|---|---|
| a bot in a team emits `info.turno` | `bot.event.emitted` with `severity: info`; delivered as today (nobody hears a broadcast `info` yet: no deliveries, no skips) |
| emits `stop.recall` addressed to a teammate | the teammate's task is urgent (`severity: critical` derived) |
| emits `error.bot` | `200` with `refused: {reason: "not_allowed"}`, `bot.event.refused` |
| an old node sends `severity: "critical"` with `info.x` | accepted; the event is `info` |
| `events.emit: [{"name": "warn.*", "maxSeverity": "critical"}]` | accepted; `warn.x` is `warning`; `info.x` refused `not_allowed` |
| `tools/list` from the node's socket | `axlr_bots_emit_event` has no `severity` property; its description names the kinds |
| coverage | ≥ 80 % total; `domain/topic_kind_test.go` covers every kind and a kindless name |

### PR 3 — subscriptions, mode `task`

`subscriptions` on the bot (`Subscription`, `Subscriptions`, snapshot, status), the conversion of `event` triggers, `Hears` over subscriptions, the implicit subscription for `to`, the team default (`danger.*`, `stop.*`), the `recall` alias, deliveries of mode `task` only (`message` and `note` are recorded as `task` deliveries for now: this PR keeps every event a task), `bot.event.heard`, `PATCH` validation, CLI `bot subscribe/unsubscribe/subscriptions`, OpenAPI, bot reference.

| Check | Expected |
|---|---|
| `bot subscribe almacen 'pedido.*' --from recepcion`; recepcion emits `pedido.nuevo` | almacen: a task, trigger `{type: event, name: "pedido.*"}`, **not** urgent, objective `Your teammate recepcion says (pedido.nuevo): …`; `bot.event.heard` on almacen with `mode: task`; `bot.event.emitted` on recepcion lists the delivery with `mode: task` |
| another teammate without subscriptions | no delivery, no skip (a broadcast `info`) |
| the same event from a bot not in `from` | not heard; no skip |
| a bot with no subscriptions; a teammate emits `danger.fire` | an urgent task (team default); with `events.interruptible: false` a normal task |
| `stop.recall` addressed to a bot whose subscriptions do not match `stop.*` | heard anyway (implicit subscription), urgent |
| a teammate emits `recall` addressed to a bot | read as `stop.recall`: an urgent task |
| a teammate emits `warn.x` addressed to a bot with no matching subscription | a task, **not** urgent |
| `PATCH` with 17 subscriptions, a bad pattern, `from` naming a stranger | `400 validation` each, with the field |
| a definition with `triggers: [{"type": "event", "events": ["a.*"], "interrupt": true}]` | registered; `bot get` shows `subscriptions: [{"topic": "a.*", "mode": "task", "urgent": true}]` |
| the panel on `main` against this control plane | still runs: unknown event types render as `Unknown` (#23) |
| coverage | ≥ 80 % |

### PR 4 — modes `message` and `note`

`DeliveryMode` in full: `deliver_message.go` over `bot.task.message` and the node relay, `deliver_note.go`, the notes in the prompt (`event_note.go`, `event_notes.go`, `NewTeamPrompt` with the cut order notes → directory), the default mode by severity, the task detail's **Knew** data (the notes a task was given, kept on the task as `input.notes`).

| Check | Expected |
|---|---|
| `bot subscribe supervisor 'warn.*'` while supervisor has a Running task with a leased execution; someone emits `warn.stock` | `bot.task.message` on that task with `from: event:<id>`; the fake node reads it with `GET …/messages?after=`; `bot.event.heard` `mode: message` |
| the same with supervisor idle | a task (fallback), `bot.event.heard` `mode: task` |
| `bot subscribe supervisor 'info.*' --mode note`; a teammate emits `info.turno` | heard `mode: note`, no task; supervisor's next task's prompt (the assignment's objective the fake node receives) carries "Recent events in your team" with that line; the mission and the objective are intact |
| 9 notes in a minute | the prompt carries the last 8 |
| an addressed `info.x` to a bot with no matching subscription | `note`, no task |
| a Paused bot subscribed as `note` | the note is recorded; its first task after Enable carries it |
| a prompt where mission + objective + 8 notes + directory pass 8000 chars | the notes are cut first, then the directory; mission and objective intact |
| coverage | ≥ 80 % |

### PR 5 — intake, coalescing, depth

`event_intake.go`: the receiver's rate and coalescing by event name; `events.intakePerMinute`; `events.maxDepth` (default 2) as the chain limit of a chain an event starts.

| Check | Expected |
|---|---|
| the 11th event to one bot within a minute, mode `task` | recorded `mode: note`, `degraded: "intake_rate"`, no task; a `stop.*` addressed as the 12th still makes an urgent task |
| 11 `note` deliveries in a minute, then a `task` one | the task is made: notes do not count |
| two `pedido.nuevo` while almacen's first task is still Pending | one task; the second is `note` with `coalescedInto`; when the task starts, its prompt carries the second as a note |
| two `stop.halt` while the first urgent task is still Pending | two urgent tasks: criticals are never coalesced |
| `pedido.nuevo` then `pedido.cancelado`, both by `pedido.*`, the first still Pending | two tasks: different names |
| a task an event made emits to a third teammate with a subscription | delivered (depth 2 by default); a fourth hop is `depth_exceeded`; a receiver on the chain is `cycle` |
| `events.maxDepth: 1` on the emitter | the second hop is `depth_exceeded` |
| coverage | ≥ 80 % |

### PR 6 — the control plane's own events on the bus

`publish_platform_event.go`; `error.bot` on failure and expiry, `warn.approval` when a task waits for a person, `done.task` on completion; only when a teammate subscribes; the bot the fact is about never hears its own.

| Check | Expected |
|---|---|
| supervisor subscribes `error.*` mode `task`; recepcion's task fails | supervisor: a task, urgent, objective names recepcion, `error.bot`, the reason; `bot.event.emitted` on recepcion with `by: control-plane` |
| the same with no subscriber in the team | no `bot.event.emitted`: the history keeps only `bot.task.failed` |
| recepcion's task completes; almacen subscribes `done.*` (default mode `note`) | `bot.event.heard` on almacen `mode: note`; almacen's next prompt carries it; recepcion itself hears nothing |
| recepcion waits for an approval; supervisor subscribes `warn.approval` mode `message` and runs | the message lands in supervisor's run |
| a task an `error.bot` made fails itself | the next `error.bot` goes to the subscriber at depth 2; a third fails: `depth_exceeded` skip, no fourth task |

### PR 7 — the bot asks; the operator publishes; the topics view

`axlr_bots_subscribe` and its node route, pending subscriptions and their rules, `POST /v1/groups/{name}/events`, `group.event.published`, `GET /v1/groups/{name}/topics`, CLI `group topics` and `group publish`, node protocol and API docs.

| Check | Expected |
|---|---|
| the model calls `axlr_bots_subscribe {topic: "error.*", why: "…"}` | `bot get` shows the entry with `status: pending`, `askedBy`, `why`, `askedAt`; `bot.subscription.asked`; the tool answers "asked; your operator decides"; an `error.bot` meanwhile is **not** heard |
| the same ask again, or an ask for a pattern it already has | `refused: {reason: "already_subscribed"}` |
| `PATCH` the list with that entry `active` | heard from then on; `bot.subscription.changed` |
| `PATCH` the list with a pending entry edited (another mode) | `400 validation`: keep, approve or drop |
| `group publish tienda stop.all --message "Everyone stop"` | every Enabled member gets an urgent task; `group.event.published` in `group events tienda` with the deliveries; a repeat with the same `Idempotency-Key` answers `200` and makes nothing |
| `group publish tienda error.bot …` | `400` `not_allowed` |
| `group topics tienda` after the above | `pedido.nuevo` (publishers seen `recepcion`, listeners `almacen` task), `stop.all` (publisher `operator`), `info.turno` with `nobodyListens: true` |

### PR 8 — the panel

Listens to, Listen to a topic, Remove, the pending card with Approve/Deny (and the Overview count), Topics tab with **nobody listens**, Publish an event, the timeline lines by mode, Told by / Knew in the task detail, the contract test.

| Check | Expected |
|---|---|
| `cargo fmt --check`, `cargo clippy --all-targets --locked -- -D warnings`, `cargo test --locked` | green; the contract test knows `bot.event.heard`, `bot.subscription.asked`, `bot.subscription.changed`, `group.event.published` |
| the fake control plane with a bot that has an active, a pending and no subscription | the bot page lists the active one with its mode, shows the pending card, and **Listen to a topic** opens the form with the default mode by kind |
| Approve on the card | one `PATCH` with the entry `active`; the card leaves; the Overview count drops |
| Topics tab with the fake's events | one row per topic, red **nobody listens** on the one without listeners; **Publish an event** posts and the timeline shows it within a poll |
| a task made by an event, a message delivery, a task with notes | **Told by**, the teammate's line in the chat, **Knew** with the notes |
| an old control plane (no `/topics`) | the Topics tab says the control plane is older, nothing else breaks |

### PR 9 — the fleet and the runbook

The pilots emit `stop.recall` instead of `recall` with a severity; `examples/drone-fleet` definitions get a subscription or two that show the three modes (the plane: `info.*` as notes; a quad: `warn.*` as messages; the operator's `stop.all` as the generalised Recall); `docs/runbooks/teams-and-events.md` extended with "Listen to a topic", "Publish an event" and "When it does not work" rows; `docs/runbooks/drone-fleet.md` updated.

| Check | Expected |
|---|---|
| the guide case live on port-fleet (images rebuilt, with the user's OK to deploy) | the plane's bot emits `stop.recall` to quad-1; quad-1's urgent task, `return_home` by `policy:autoApprove`, RTL; the timeline shows the mode |
| the operator publishes `stop.all` to `port` from the panel | every pilot gets an urgent task and returns home |
| `info.ship-seen` from the plane with the quads subscribed as `note` | no task; the quads' next task's prompt carries it |

## Consequences

- An emitter no longer needs to know who listens: it names the topic. The team is still the trust boundary (the groups ADR); the operator sees every subscription on the bot's page and every topic on the team's tab.
- Three new things reach a bot's prompt or run: a task (as today), a teammate's line in its chat, a block of notes. The directory loses up to 1200 characters to the notes when the prompt is tight; the mission and the objective never.
- The control plane publishes about itself only when someone listens, so the history grows by what the operators asked for.
- **Upgrade order**: control plane, then nodes (an old node still offers `severity`, ignored), then the panel (an old panel shows the new events as `Unknown` and knows no subscriptions: it edits `groups` and nothing else).
- **What stops working as before**: a teammate's addressed `warning` no longer interrupts (the live-tested recall of 26 October relied on it): the deployed pilots emit `recall`, which the control plane reads as `stop.recall` for one release, so they keep recalling; their definitions and prompts move to `stop.recall` in PR 9, and the alias leaves in the release after. Tests that assert an addressed `warning` or `info` makes a task (`application/urgent_test.go`, `events_test.go`) change on purpose.
- The default depth 2 lets a reaction react once; a chain of three needs `events.maxDepth` on the bots along it.

## Left out

- Topics with their own settings (description, retention, a required `data` schema): a topic is still only a name until a team aggregate exists.
- Subscriptions across teams and global topics (decision 1).
- Replay of missed events to a bot that was Paused: it gets its notes, not the tasks.
- A message delivery into a Pending task (only Running tasks take messages; a Pending task gets the event as a note).
- `warn.budget` and other control plane facts beyond the three named; add a kind entry and a publisher call each.
- Deleting a bot's subscriptions when it leaves a team: they stay, inert, until the operator removes them (the topics view shows them as listeners of no team).
