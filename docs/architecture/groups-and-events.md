# ADR: teams of bots, events and urgent tasks

Status: accepted, 26 October 2026 (the product owner: «Todo ok»). Vision principle 13 ([#30](https://github.com/underpass-ai/axlr-bots/pull/30): the operator creates bots and groups and recalls a bot at once from the panel); builds on [delegation](delegation.md).

## Context

A bot asks another only by explicit delegation (`delegation.to`): no directory, no events, no groups. The product owner wants bots that "know each other and belong to groups" and work together "by events", with two rules:

- "Desde el control pane puedo sentir que puedo controlar mis bots": everything from the panel, one click, immediate feedback.
- "No se tiene que trasladar la complejidad al usuario": a group is a **team** with a name, picked when creating a bot; everything else has a default; patterns and limits only under *Advanced*.

Guide case, with nothing configured but the team: the operator tells the plane's bot "dile al quad-1 que se retire, hay un barco"; quad-1 cuts its mission, flies home, and the operator sees why.

```text
operator ──"dile al quad-1 que se retire"──▶ pilot-plane-0 (its directory lists pilot-quad-1 and its tools)
   └─ axlr_bots_emit_event(name "recall", to [pilot-quad-1], severity warning, message "ship 244… entering the zone")
        ▼ control plane: same team, rate, depth, cycle
        ├─▶ pilot-quad-1: urgent task (a teammate's warning addressed to it interrupts by default)
        │     ├─ its task in progress, or a pending approval, ends Cancelled (preempted)
        │     └─ return_home (runs without a person) ──▶ vehicle RTL ──▶ "returning home: ship 244… entering the zone"
        ├─▶ history: bot.event.emitted
        └─▶ panel timeline and banner, phone page (warning); notify targets (critical)
```

## Decision

### What the operator sees and does

| In the panel | How | Feedback |
|---|---|---|
| put bots in a team | New bot and Edit bot have one field, **Team**: pick one or type a new name. API: `"groups": ["port"]` | the team appears in the Teams view at once; a team of one member is flagged (a typo) |
| see the teams and who is in each | **Teams** view: each member with its line of the directory (purpose, tools, where it runs) and its status | live, as bots change |
| watch events flow | the team's timeline: emitted → task received → preempted → done, coloured by severity; `GET /v1/groups/{name}/events?after=<cursor>&wait=20s` long-polls, so an event shows within a second | `warning` and `critical` also raise a banner |
| give an order in plain words | the task box on a bot, as today, with an **Urgent** switch | the task shows in the timeline with its status |
| stop or recall a bot at once | **Stop**: pauses the bot and cancels its open tasks; **Recall**: an urgent task "Return home now" for a bot that has `return_home` | the cancelled and new tasks change on screen within the node's renewal (10 s) |
| see why each bot decided | every event carries its `message` (required: what and why); a task shows what started it (the event and who emitted it, a delegation, a person), its tool calls with arguments, who approved (a person or `policy`), its answer | one line per step in the timeline; the task detail has the rest |
| change the defaults | *Advanced*, closed by default: events it may emit, events it listens to, what interrupts it, rate, calls that run without asking | — |

### Defaults of a team (nothing written)

| Behaviour | Default | *Advanced* (field) |
|---|---|---|
| knowing the others | each assignment's prompt gets a **directory** of its teammates, one line each: id, purpose (first sentence of its mission, ≤ 120 chars), tools, where it runs. At most 32. When mission + objective + directory pass the prompt's 8000 characters, the directory is cut from the end with "… and N more", never the mission or the objective | — |
| delegating | a member may delegate to any teammate, and takes delegations from them: the team is the consent. Outside the team, denied as today. Depth, cycle and budget as in [delegation](delegation.md) | `delegation.to` adds bots outside; `delegation.fromTeam: false` refuses teammates |
| emitting | a member may emit any event name to its team, severity up to `warning`, 6 per minute | `events.emit`: names or patterns, each with `maxSeverity` (`critical` only here); `events.perMinute` (≤ 60) |
| hearing | a member hears the events **addressed to it** (`to`) and the team's **broadcasts of `warning` or `critical`**; `info` broadcasts go to the history only. Each event heard is a new task (trigger `event`) | `event` triggers with names or patterns replace the default |
| being interrupted | a teammate's event of `warning` or `critical` **addressed to it** makes an urgent task. Broadcasts never interrupt by default | `events.interruptible: false`, or `interrupt` on each `event` trigger |
| running without asking | none; the panel pre-ticks `return_home` when the bot's tools include it | `approvals.autoApprove` |

### The rules underneath

| # | Question | Decision |
|---|---|---|
| 1 | What a team is | A **tag on the bot**, up to 8, identifier syntax; the panel calls it Team, the API `groups`. No new aggregate or store: a team exists while a bot names it; `GET /v1/groups` and `GET /v1/groups/{name}` are derived from the tenant's bots |
| 2 | Who receives | Enabled teammates of the emitter that hear it (defaults above), narrowed to `to` when given; never the emitter. `to` naming a bot outside its teams: `not_member` |
| 3 | How it arrives | A task of the receiver, trigger `event`, objective "Event {{.name}} from {{.from}} ({{.severity}}): {{.message}}" plus the data, or the trigger's own template; key `event:<event id>:<receiver>`, so a retried call creates nothing twice |
| 4 | Loops and depth | The receiver's task gets the emitter's **lineage** (depth + 1, the chain), cut with `depth_exceeded` / `cycle` exactly like a delegation; the receiver is skipped, the event still goes to the history and to people |
| 5 | Budget | The receiver runs on **its own budgets and timeout**. A grant from the emitter's pool, as in delegation, would starve a broadcast (a pool split among 8 teammates leaves less than one call each) and kill a recall when the emitter is about to end. The cost stays bounded: per emitter by `perMinute` × team size, per chain by its depth, and every hop is a task the panel shows |
| 6 | Who may make a task urgent | The **operator**: at submission (Urgent), or by the receiver's definition, whose default lets a teammate's addressed `warning` interrupt. A bot never makes a broadcast urgent and never raises its severity past its `maxSeverity`; delegations, webhooks and timers are never urgent |
| 7 | What urgent does | It goes first in `NextAssignment`. The bot's **Running and WaitingApproval** tasks that are not urgent end `Cancelled`, reason `preempted by <task>` (`bot.task.preempted`); its Pending tasks wait behind; an urgent task never cancels another urgent one |
| 8 | A pending approval of a preempted task | The wait is closed; a late approve or deny gets `409` and the call never runs; the card leaves the panel and the phone page |
| 9 | How fast | Cancelling refuses the execution's next lease renewal and the node stops `axlr-tui`; the node's `DefaultRenew` goes from 30 s to **10 s**. Cancelling stops the model, not the vehicle: the quad flies on until the urgent task calls `return_home` |
| 10 | A call without a person | `approvals.autoApprove`: rules like `toolRules` (`{"tool": "return_home"}`). When a task stops on a matching call, the control plane approves it at once as actor `policy` (audit `task.decide`, no notification): one more execution, a few seconds. AXLR's `act-with-approval` asks for every call even under `--approve all`, so this lives in the control plane until AXLR has a per-tool rule. `run_mission` (take off, go somewhere new) stays a person's decision |
| 11 | Where it shows | `bot.event.emitted` on the emitter's stream (name, severity, message, data, `to`, who got a task, who was skipped and why), `bot.event.refused`, `bot.task.preempted`; `info`: history; `warning`: also the panel's banner and the fleet's phone page; `critical`: also the emitter's `approvals.notify` targets |
| 12 | Stop | `POST /v1/bots/{id}/stop` does on the server, in one call, what the panel's Stop does today on the client ([#32](https://github.com/underpass-ai/axlr-bots/pull/32)): pause the bot if Enabled, then cancel its Pending, Running and WaitingApproval tasks (reason `stopped by <actor>`); it answers how many were cancelled and which could not be. Enable undoes the pause; the panel switches to the route when it lands |

### The tool

Served by the node agent on the execution's socket, next to `axlr_bots_delegate` ([delegation §1](delegation.md#1-how-the-model-asks)), to every bot in a team. The description tells the model when to use which: delegate to ask a teammate for a task and its answer; emit to tell teammates something happened, warn or recall them, without waiting.

```json
{
  "name": "axlr_bots_emit_event",
  "inputSchema": {
    "type": "object",
    "properties": {
      "name":     {"type": "string", "description": "a short dotted name, e.g. recall, ship.sighted"},
      "message":  {"type": "string", "minLength": 1, "maxLength": 280, "description": "one line for people and bots: what happened and why"},
      "severity": {"type": "string", "enum": ["info", "warning"], "default": "info", "description": "warning: people are told, and a teammate it is addressed to stops what it does"},
      "to":       {"type": "array", "items": {"type": "string", "enum": ["pilot-quad-1", "…"]}, "maxItems": 16, "description": "only these teammates; absent: the whole team"},
      "data":     {"type": "object", "description": "facts for the receivers, at most 8 KiB as JSON"}
    },
    "required": ["name", "message"],
    "additionalProperties": false
  }
}
```

It answers at once with the event id, who got a task and who was skipped and why. Refusals of the whole call, each a `bot.event.refused` and an audit entry: `not_allowed`, `severity_not_allowed`, `no_team`, `not_member`, `rate_limited`. Event names: dot-separated segments `[a-z0-9-]`, ≤ 64 chars; a pattern takes `*` (one segment) or `**` (any).

### The definition

What the panel writes for the guide case (only `groups`; the rest is default):

```json
"groups": ["port"]
```

The same bot with every *Advanced* field:

```json
"groups": ["port"],
"delegation": {"to": ["logistics"], "fromTeam": true},
"events": {"emit": [{"name": "recall", "maxSeverity": "warning"}, {"name": "ship.*", "maxSeverity": "critical"}], "perMinute": 6, "interruptible": true},
"triggers": [{"type": "manual"}, {"type": "event", "name": "ships", "events": ["ship.*"], "interrupt": false, "objective": "…"}],
"approvals": {"autoApprove": [{"tool": "return_home"}]},
"priority": "on a task: normal (default) or urgent"
```

### The fleet

`examples/drone-fleet/app/vehicle_pilot.py` gets a second tool, `return_home {reason}`: RTL to the vehicle's station (quad and rover RTL, plane RTL then its autoland), "already home" when it is there. The pilot definitions get `groups: ["port"]` and `autoApprove` for `return_home`. After [#29](https://github.com/underpass-ai/axlr-bots/pull/29) is on main.

## Order of PRs (each from `origin/main`, base `main`)

| PR | Contents |
|---|---|
| 1 | this ADR |
| 2 | teams: `groups`, derived `/v1/groups`, the directory, delegation inside the team; CLI, OpenAPI, bot reference |
| 3 | events: the defaults, `events`, trigger `event`, the node's tool, delivery and limits, `bot.event.*`, group events with `after` and `wait`, notify on `critical` |
| 4 | urgent tasks, Stop, `autoApprove`; `DefaultRenew` 10 s |
| 5 | fleet: `return_home`, pilot definitions, warnings on the phone page |
| 6 | panel: Team in New bot and Edit bot, *Advanced*, the Teams view, the live timeline, Urgent, Stop, Recall, the task's why; contract test |
| 7 | guide: set it up from the panel (`docs/runbooks/teams-and-events.md`) |

## Consequences

- New fields wherever the definition travels; an old panel reads the `event` trigger and the new events as `Unknown` (#23) and still works. **Upgrade the control plane first**, then the nodes (an old node never offers the emit tool), then the panel.
- A bot in a team gets two more tools (delegate, emit), still under its level and its policy; a bot in no team sees no change.
- Putting two bots in a team lets them give each other tasks and interrupt each other with an addressed `warning`: the team is a trust boundary, and the panel says so when a bot joins one.

## Left out

- Events from the operator (`POST /v1/groups/{name}/events`) and from webhooks; urgent webhooks and timers.
- Cancelling a preempted task's Pending queue and its delegated children.
- Teams with their own settings (description, notify targets): a team aggregate can come later without changing the bots' `groups`.
- Approval without the extra execution: needs a per-tool rule in AXLR.
