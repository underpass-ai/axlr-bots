# ADR: groups of bots, events and urgent tasks

Status: proposed, 26 October 2026. Vision principle 13 ([#30](https://github.com/underpass-ai/axlr-bots/pull/30): the operator creates bots and groups and recalls a bot at once from the panel); builds on [delegation](delegation.md).

## Context

A bot asks another only by explicit delegation (`delegation.to`): no directory, no events, no groups. The product owner wants bots that "know each other and belong to groups" and work together "by events". Guide case: the operator tells the plane's bot to warn quad-1's bot to pull back because a ship is coming; quad-1 cuts its mission, flies home, and the operator is told why.

```text
operator ──task──▶ pilot-plane-0 ──axlr_bots_emit_event("vehicle.recall", to quad-1, "ship 244… entering the zone")──▶ control plane
                                                                                                                 │ checks: emitter may emit it, same group, rate, depth, cycle, budget
                        ┌───────────────────────────────────────────────┬──────────────────────────────────────┤
                        ▼                                               ▼                                      ▼
   pilot-quad-1: urgent task (its trigger `event` with interrupt)   history: bot.event.emitted        panel timeline, phone page (warning),
   ├─ cancels its own task in progress (preempted)                                                      notify targets (critical)
   └─ calls return_home ── autoApprove ──▶ vehicle RTL ──▶ task Completed "returning home: ship 244… entering the zone"
```

## Decision

| # | Question | Decision |
|---|---|---|
| 1 | What is a group | A **tag on the bot**: `"groups": ["port"]`, up to 8, identifier syntax. No new aggregate or store: a group exists while a bot names it; `GET /v1/groups` and `GET /v1/groups/{name}` are derived from the bots of the tenant |
| 2 | What a member knows | Each assignment's prompt gets a **directory** of the other members of its groups, one line each: id, purpose (first sentence of its mission, ≤ 120 chars), tools, where it runs (nodes or selector), what it takes (`delegation`, the events it listens to). At most 32 members. The prompt keeps its 8000-character limit: when mission + objective + directory do not fit, **the directory is cut** from the end with "… and N more", never the mission or the objective |
| 3 | Delegating inside a group | A member may delegate to **any member** that declares the `delegation` trigger, without listing it; `delegation.to` still adds bots outside the group. The tool's `enum` is that union. Outside both: `not_allowed`, as today. Depth, cycle and budget are unchanged |
| 4 | Who may emit what | The bot's definition: `events.emit` lists names or patterns, each with a `maxSeverity` (default `warning`). The model chooses within it; a bot in no group emits nothing |
| 5 | Who receives | Every Enabled bot that **shares a group** with the emitter and has an `event` trigger matching the name; narrowed to `to` when the call names members. Never the emitter itself |
| 6 | How it arrives | As a new task of the subscriber (trigger `event`), objective rendered from the trigger's template over the event; idempotency key `event:<event id>:<trigger>` |
| 7 | Limits | Rate: `events.perMinute` (default 6, at most 60), counted from the emitter's own `bot.event.emitted` in the last minute, so a restart keeps it. Depth and cycle: the subscriber's task gets the emitter's **lineage** (depth + 1, chain), cut with `depth_exceeded` / `cycle` like a delegation. Budget: each delivery is a child of the emitting task: model and tool calls are `share × (pool − granted) / subscribers`, never more than the subscriber's own; **the deadline is the subscriber's own timeout** (nobody waits for an event, and a recall must not die because the emitter is about to end) |
| 8 | Where it shows | `bot.event.emitted` on the emitter's stream (name, severity, message, data, deliveries, skipped), `GET /v1/groups/{name}/events`; `info`: history only; `warning`: also the panel's banner and the fleet's phone page; `critical`: also the emitter's `approvals.notify` targets (webhook, command) |
| 9 | Who may make a task urgent | **Only the operator**: at submission (`priority: urgent`, panel/API/CLI) or in the subscriber's definition (`event` trigger with `interrupt: true`). A bot never chooses urgency; it can only emit what the operator let it emit, and the receiver says what interrupts it. Delegations, webhooks and timers are never urgent |
| 10 | What urgent does | The task goes first in `NextAssignment`. The bot's **Running and WaitingApproval** tasks that are not urgent end `Cancelled` with reason `preempted by <task>` (`bot.task.preempted`); its Pending tasks wait behind. An urgent task never cancels another urgent one |
| 11 | A pending approval of a preempted task | The wait is closed; a late approve or deny gets `409` (task cancelled) and the call never runs; the card leaves the panel and the phone page |
| 12 | How fast the cut is | Cancelling refuses the execution's next lease renewal and the node stops `axlr-tui`. This ADR lowers the node's `DefaultRenew` from 30 s to **10 s**. Cancelling stops the model, not the vehicle: the quad keeps flying until the urgent task calls `return_home` |
| 13 | A call without a person | `approvals.autoApprove`: rules like `toolRules` (`{"tool": "return_home"}`, argument patterns allowed). When a task stops on a call that matches, the control plane decides `approve` at once as actor `policy` (audit `task.decide`, no notification). Costs one more execution (a few seconds). AXLR's `act-with-approval` adds a card to every call even under `--approve all`, so this lives in the control plane; an AXLR per-tool rule would remove the extra execution later. `run_mission` (take off, go somewhere new) stays a person's decision |

### The tool

Served by the node agent on the execution's socket, next to `axlr_bots_delegate` ([delegation §1](delegation.md#1-how-the-model-asks)), and added to the allowed tools of every bot with `events.emit`:

```json
{
  "name": "axlr_bots_emit_event",
  "inputSchema": {
    "type": "object",
    "properties": {
      "name":     {"type": "string", "description": "the event, one of (or matching): vehicle.recall, ship.*"},
      "message":  {"type": "string", "minLength": 1, "maxLength": 280, "description": "one line for people and bots: what happened and why"},
      "severity": {"type": "string", "enum": ["info", "warning"], "default": "info"},
      "data":     {"type": "object", "description": "facts for the receivers, at most 8 KiB as JSON"},
      "to":       {"type": "array", "items": {"type": "string", "enum": ["pilot-quad-1", "…"]}, "maxItems": 16, "description": "only these members; absent: every member that listens"}
    },
    "required": ["name", "message"],
    "additionalProperties": false
  }
}
```

It answers at once with the event id, who got a task (bot, task id) and who was skipped and why. Refusals of the whole call: `not_allowed` (name not in `events.emit`), `severity_not_allowed`, `no_group`, `not_member` (`to` names a bot outside the emitter's groups), `rate_limited`; each is `bot.event.refused` plus an audit entry. Per receiver: `depth_exceeded`, `cycle`, `budget_exhausted`, `target_unavailable`.

### The definition

```json
"groups": ["port"],
"events": {"emit": [{"name": "vehicle.recall", "maxSeverity": "warning"}, {"name": "ship.*"}], "perMinute": 6},
"triggers": [
  {"type": "manual"}, {"type": "delegation"},
  {"type": "event", "name": "recall", "events": ["vehicle.recall"], "interrupt": true,
   "objective": "Recall from {{.from}}: {{.message}}. Return home now with return_home and say why."}
],
"approvals": {"ttl": "20m", "onExpire": "deny", "autoApprove": [{"tool": "return_home"}]}
```

| Field | Rule |
|---|---|
| event name | dot-separated segments `[a-z0-9-]`, ≤ 64 chars; a pattern takes `*` (one segment) or `**` (any) |
| trigger `event` | `events`: 1 to 16 names or patterns; `objective`: a template over `name`, `from`, `severity`, `message`, `data`, `group`, `emittedAt` (default: "Event {{.name}} from {{.from}} ({{.severity}}): {{.message}}"); `interrupt`: default false |
| task `priority` | `normal` (default) or `urgent`, shown on the task and in its approval notification |

### The fleet

`examples/drone-fleet/app/vehicle_pilot.py` gets a second tool, `return_home {reason}`: RTL to the vehicle's station (quad and rover RTL, plane RTL then its autoland), "already home" when it is at its station; the pilot bots get `groups: ["port"]`, the recall trigger and `autoApprove` for `return_home`. It goes after [#29](https://github.com/underpass-ai/axlr-bots/pull/29) (ships as targets, `ais`) is on main.

## Order of PRs (each from `origin/main`, base `main`)

| PR | Contents |
|---|---|
| 1 | this ADR |
| 2 | groups: `groups`, derived `/v1/groups`, the directory, delegation to members; CLI, OpenAPI, bot reference |
| 3 | events: `events.emit`, trigger `event`, the node's tool, delivery and limits, `bot.event.*`, `/v1/groups/{name}/events`, notify on `critical` |
| 4 | urgent tasks and `autoApprove`; `DefaultRenew` 10 s |
| 5 | fleet: `return_home`, pilot definitions, warnings on the phone page (after #29) |
| 6 | panel: Groups and Listens to in New bot, a Groups view with members, events in the timeline; contract test |
| 7 | guide: set it up from the panel (`docs/runbooks/groups-and-events.md`) |

## Consequences

- New fields everywhere the definition travels; an old panel reads the `event` trigger and the new events as `Unknown` (#23) and still works. **Upgrade the control plane first**, then nodes (an old node never offers the emit tool), then the panel.
- A bot in a group gets the delegation tool when any other member takes delegations: its allowed tools grow by that one tool, still under its level.
- The prompt is longer by the directory; a bot in no group sees no change.

## Left out

- Events from the operator (`POST /v1/groups/{name}/events`) and from webhooks; urgent webhooks and timers.
- Cancelling a preempted task's Pending queue and its delegated children.
- Groups with their own policy (description, notify targets, allowed events): a group aggregate can come later without changing the bots' `groups` field.
- Approval without the extra execution: needs a per-tool rule in AXLR.
