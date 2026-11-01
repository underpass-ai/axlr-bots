# Bots

A bot has no role baked in: its definition is what an operator writes, and the same install serves every definition. A bot exists without running anything; a task makes the control plane start an execution for it, and when the execution ends the bot is still there (bot ≠ execution).

## The definition

[`cmd/axlr-bots/testdata/bot.json`](../../cmd/axlr-bots/testdata/bot.json) is the example bot: it reads the files of its workspace and does what each task asks.

```json
{
  "id": "bot", "tenant": "default", "name": "bot", "owner": "operator",
  "mission": "Do what each task asks, within the policy.",
  "level": "observe",
  "allowedTools": ["local_read", "local_search", "local_list"],
  "toolRules": {"allow": [], "deny": []},
  "triggers": [{"type": "manual"}],
  "placement": {},
  "model": {"model": "anthropic/claude-haiku-5.5", "routingPolicy": "cost-optimized", "allowRemoteProviders": true, "maxContextTokens": 32000},
  "budgets": {"maxModelCallsPerTask": 10, "maxToolCallsPerTask": 20, "maxDelegationDepth": 0, "taskTimeoutSeconds": 300}
}
```

| Field | What it says |
|---|---|
| `id`, `tenant`, `name`, `owner` | the identity; `owner` is who answers for the bot. The identity never changes after registration |
| `mission` | the standing responsibility; every task's objective is read against it |
| `level` | the autonomy level ([below](#autonomy-levels)); default `observe` |
| `allowedTools` | AXLR's tool names (`local_read`, `local_search`, `local_list`, `local_write`, `local_exec`, …); nothing else may be called |
| `toolRules` | narrows the allowed tools by their arguments ([below](#tool-rules)) |
| `triggers` | how tasks reach the bot: `manual` (the default), `webhook`, `timer`, `delegation`, `incident`, `alertmanager`; a bot that declares triggers takes only those ([below](#triggers)). A trigger of type `event` is read as a [subscription](#subscriptions), for one release |
| `placement` | where the bot's tasks run: `{"nodes": ["web-1"]}`, `{"selector": {"zone": "eu"}}`, or nothing for any node but a dedicated one, such as a sidecar ([nodes](node-protocol.md)) |
| `approvals` | how a pending call waits for a person: `ttl` (default `1h`), `onExpire` (`deny`, the default, or `cancel`) and the `notify` targets told when a task starts to wait ([below](#approvals)) |
| `delegation` | the bots outside its teams this bot may ask to do a task, how deep a chain through it may go and the share of its task a child may take; absent: it asks only its teammates ([below](#delegation)) |
| `groups` | the teams the bot belongs to, up to 8; the panel calls each a Team. Teammates know each other and may ask each other to do a task ([below](#teams)) |
| `events` | what the bot may tell its teams: the names it may emit and how many a minute; absent: any name but `error.*` and `done.*`, 6 a minute ([below](#events)) |
| `subscriptions` | the topics the bot listens to in its teams, at most 16; with or without them it hears what is addressed to it and the team's `danger.*` and `stop.*` ([below](#subscriptions)) |
| `model` | the model policy: `model` is passed to AXLR as `--model`; `routingPolicy`, `allowRemoteProviders` and `maxContextTokens` are validated and stored with the bot |
| `budgets` | `taskTimeoutSeconds` bounds every execution (15 minutes when absent); `maxModelCallsPerTask` and `maxToolCallsPerTask` go to AXLR as the execution's budget when the build takes `--budget` ([runtime contract](../architecture/runtime-contract.md)); `maxDelegationDepth` is the depth limit of a [delegation](#delegation) that names none. For a Jev bot the model calls are Jev's and the tool calls the program's |
| `runtime` | what runs the bot's tasks on its node: absent (or `kind: axlr`) is an AXLR session, a language model planning the run; `kind: jev` with its `jev` program is a fixed sequence of tool calls whose every decision is one question to Jev, with no language model ([below](#runtime)) |

## Autonomy levels

The levels are the vision's (§10.4). The policy is applied outside the model: AXLR refuses a call the policy forbids before any approval card, and the model reads the refusal as the tool's result.

| Level | What the bot may do |
|---|---|
| `observe` | read only; with `local_exec` allowed, only the commands the rules name |
| `propose` | write a plan or a diff, nothing else |
| `act-with-approval` | every allowed write or command waits for a person ([approvals](../runbooks/approvals.md)) |
| `act-within-policy` | every allowed call runs; the tool rules are the limit |

How each level is handed to AXLR (`--approve`, the policy file) is in the [runtime contract](../architecture/runtime-contract.md#the-policy).

## Approvals

A call the policy leaves to a person stops the task in `WaitingApproval` ([runbook](../runbooks/approvals.md)). `approvals` says how long it waits, what happens when nobody decides, and who is told.

```json
"approvals": {
  "ttl": "15m",
  "onExpire": "deny",
  "notify": [
    {"type": "webhook", "name": "ops", "url": "https://hooks.example.com/axlr", "secret": "<16 to 256 characters>"},
    {"type": "command", "name": "desktop", "command": ["/usr/local/bin/axlr-notify", "--urgency", "critical"]}
  ]
}
```

| Field | What it says |
|---|---|
| `ttl` | a duration from `1m` to `720h`; the wait's deadline is when the task stopped plus the `ttl`, kept on the task (`approval.deadline`) so a restart keeps it |
| `onExpire` | `deny`: at the deadline the call is denied as a person's deny would (a new execution hands the denial to the session, which goes on without the call); `cancel`: the task ends `Expired` and the session stays where it is. A denial whose run fails ends the task `Expired` too, and a paused bot's expired task ends `Expired` either way ([expiry](../runbooks/approvals.md#expiry)) |
| `notify` | up to 8 targets, names unique (the type when not given). A `webhook` takes an absolute `http`/`https` `url` without credentials and the `secret` its deliveries are signed with; a `command` takes the program by its absolute path and its arguments |
| `autoApprove` | up to 16 rules, as in `toolRules` (`{"tool": "return_home"}`, argument patterns allowed): a call that fits one is approved by the control plane at once, as actor `policy:autoApprove`, without a notification. An MCP call AXLR asks for as `axlr_call_tool` is judged by the tool it names. It costs one more execution (a few seconds): AXLR's `act-with-approval` asks for every call, so the decision is the control plane's. A bot paused meanwhile, or any other refusal, leaves the call to a person as usual |

A decision that arrives after the deadline authorises nothing (vision §14): it is refused with `409 approval_expired`, whether the daemon has carried out the expiry yet or not, and the call never runs. The secret is stored with the bot and redacted in every answer, as a trigger's is; a `PATCH` of `approvals` replaces the whole value, secrets included. A bot defined before approvals waits an hour and then denies.

## Delegation

A bot may ask another bot to do a task while it runs: the model calls the tool `axlr_bots_delegate`, the control plane checks the ask and creates a task of the other bot, and the call returns that bot's answer ([ADR](../architecture/delegation.md)). It is denied by default on both sides: the asking bot names its targets, and the target declares the `delegation` trigger.

```json
"delegation": {"to": ["bot-b"], "maxDepth": 1, "budgetShare": 0.5}
```

| Field | What it says |
|---|---|
| `to` | the bots it may ask, by id: at most 16, not itself. A bot with targets is given the tool: the control plane adds `axlr_bots_delegate` to its allowed tools, so its policy names it |
| `maxDepth` | how deep a chain through this bot may go, 1 to 8; absent: `budgets.maxDelegationDepth`, else 1. A chain is cut at the least `maxDepth` of the bots that asked along it |
| `budgetShare` | the share of the asking task a child may take, more than 0 and at most 1; absent: 0.5 |
| `split` | how that share is taken: `geometric`, of what the asking task has not granted yet (later children get less), or `even`, of the whole pool divided by `maxChildren` (every child the same); absent: `geometric` |
| `maxChildren` | the most children one task may ask for, 1 to 32; needed by `even`, a cap only with `geometric` |
| `fromTeam` | `false`: the bot takes no task its teammates delegate; absent: it takes them ([teams](#teams)) |

The target takes delegated tasks when its `triggers` include `{"type": "delegation"}` (no options, like `manual`) or when the asking bot is its teammate ([teams](#teams)); the asking bot's tenant must be its tenant, and it must be Enabled. An ask is refused, with an event `bot.delegation.refused` and its reason, when the target is neither in `to` nor a teammate (`not_allowed`), cannot take it (`target_unavailable`), the chain would be deeper than allowed (`depth_exceeded`), the target already asked along the chain (`cycle`: A→B→A), or there is too little left to share (`budget_exhausted`: the task already asked `maxChildren` bots, "maxChildren reached", or the grant is under one model or tool call, or under 30 seconds). The child is granted, of each limit the asking task has, the share of what it has not granted yet (`geometric`) or the share of the whole limit over `maxChildren` (`even`), never more than the target's own budgets; its deadline is the share of the asking task's time left, under either split. To give each of *N* children its whole budget *c*, size the pool at `N × c / budgetShare` with `even` ([ADR](../architecture/delegation.md#3-how-depth-and-budget-propagate)). It runs with that grant, under its own mission, level and tools, with the asking task's trace id; a child that cannot end by its deadline (it waited for a node or a person past it) fails with the reason `deadline`.

[`cmd/axlr-bots/testdata/bot-delegating.json`](../../cmd/axlr-bots/testdata/bot-delegating.json) asks [`cmd/axlr-bots/testdata/bot-delegate-target.json`](../../cmd/axlr-bots/testdata/bot-delegate-target.json); the tool's schema and the wiring are in the [ADR](../architecture/delegation.md#1-how-the-model-asks).

## Urgent tasks

Only the operator makes a task urgent: with `priority: urgent` at submission (`task submit --urgent`, the panel's Urgent switch), or in the definition of the bot that hears a teammate's event (below). An urgent task is assigned before the bot's other tasks, and it cancels the bot's tasks that are Running or WaitingApproval and not urgent: each ends `Cancelled` with the reason `preempted by the urgent task <id> (<why>)` and a `bot.task.preempted` event, a running execution stops as soon as the control plane pushes the cancel on its node's stream (at the node's next lease renewal, 10 s, without the stream), and a waiting call never gets its decision (a late approve or deny is `409`). Pending tasks wait behind it; an urgent task never cancels another urgent one. Cancelling stops the model, not what it started: a vehicle on a mission flies on until the urgent task calls, say, `return_home`.

**Stop** (`bot stop`, `POST /v1/bots/{id}/stop`) is the operator's brake: it pauses the bot and cancels every task it has not finished, Pending included.

## Teams

A team is a name bots share: `"groups": ["port"]`. Nothing else needs to be written ([ADR](../architecture/groups-and-events.md)). A team exists while a bot of the tenant names it; `axlr-bots group list` and `GET /v1/groups` show each team with its members.

| Inside a team, by default | Change it with |
|---|---|
| each task's prompt lists the bot's Enabled teammates between its mission and the task, one line each: id, the first sentence of its mission, its tools, where it runs, whether it takes delegated tasks. At most 32; when the prompt's 8000 characters would not hold them all, the list is cut from the end ("… and N more"), never the mission or the objective | — |
| a bot may ask any Enabled teammate to do a task with `axlr_bots_delegate`, without naming it in `delegation.to`; the target needs no `delegation` trigger: the team is its consent. Depth, cycle and budget are checked as for any [delegation](#delegation) | `delegation.to` adds bots outside the team; `"delegation": {"fromTeam": false}` refuses the teammates' tasks (the `delegation` trigger still takes anyone's that may ask) |

```json
{"id": "plane", "tenant": "default", "name": "plane", "owner": "operator", "mission": "…", "groups": ["port"], "…": "…"}
```

[`cmd/axlr-bots/testdata/bot-team-a.json`](../../cmd/axlr-bots/testdata/bot-team-a.json) asks [`cmd/axlr-bots/testdata/bot-team-b.json`](../../cmd/axlr-bots/testdata/bot-team-b.json), its teammate in `releases`, with no `delegation` block and no `delegation` trigger. Teams belong to a tenant: two tenants may use one name, and their bots are not teammates.

## Events

A bot in a team may tell it that something happened with the tool `axlr_bots_emit_event` (`name`, `message`, `data`, `to`), which the node serves next to `axlr_bots_delegate`: a warning, a recall, a sighting. It does not wait.

The first segment of the name is its **kind**, and the kind fixes the severity ([publish/subscribe ADR](../architecture/pubsub-events.md)): `stop.*` and `danger.*` are `critical`, `warn.*` is `warning`, `info.*` is `info`; `error.*` (`critical`) and `done.*` (`info`) are the control plane's own and a bot may not emit them. Any other name is `info`, except `recall`, which is read as `stop.*` (`critical`) for one release. The tool takes no severity; an older node that still sends one has it ignored. The control plane gives a task to each teammate that hears the event, and records it as `bot.event.emitted` on the bot's stream, with who got a task and who did not and why; the team's timeline (`GET /v1/groups/{name}/events`, `group events`) shows it.

| By default | Change it with |
|---|---|
| a bot may emit any name but `error.*` and `done.*`, 6 a minute | `"events": {"emit": [{"name": "stop.recall"}, {"name": "warn.*"}], "perMinute": 12}`: only those names (`*` one dot-separated segment, `**` any), never `error.*` or `done.*`; at most 60 a minute. A rule's `maxSeverity` is accepted and ignored: the kind decides |
| a teammate hears the events addressed to it (`to`) and the team's `danger.*` and `stop.*`; anything else reaches only who [subscribed](#subscriptions) | `subscriptions` on the receiving bot |
| a bot takes at most 10 events a minute as tasks or messages; from the 11th a delivery is a note (`degraded: intake_rate`), and a critical always lands. Notes do not count | `"events": {"intakePerMinute": 20}`, 1 to 60 |
| an event's chain goes 2 deep: a task an event made may tell the team once more | `"events": {"maxDepth": 3}` on the emitter, 1 to 8 |
| a critical (`danger.*`, `stop.*`) a bot hears makes an [urgent](#urgent-tasks) task: it stops what the bot was doing. A warning does not, unless the subscription that heard it says `"urgent": true` | `"events": {"interruptible": false}`: nothing a teammate emits interrupts the bot |

The teammate's task runs under its own mission, policy and budgets, until its own timeout, with the emitter's trace id; its objective says who said what (`Your teammate plane says (stop.recall): …`, or the subscription's template over `name`, `from`, `severity`, `message`, `data`, `dataJSON`, `to`, `teams`, `eventId`), and its `delegation` lineage names the event (`eventId`). A chain is cut as a delegation's is: a teammate already on the chain is skipped (`cycle`), and so is one that would be deeper than the chain allows (`depth_exceeded`: an event's chain goes as deep as the emitter's `events.maxDepth`, 2 by default, so a reaction may react once; along a chain of events and delegations the least limit of its hops holds). The same call made twice (a retry) is the same event and makes no task twice. A teammate's task that cannot start before its own timeout (its node was offline) fails with the reason `deadline`: a stale recall never runs an hour late. Telling the team needs no person even for an `act-with-approval` bot: the control plane checks every event itself, so it approves the emit call at once (`decidedBy: policy:team-event`); a delegation still asks, unless `approvals.autoApprove` names `axlr_bots_delegate`. A whole event is refused, as `bot.event.refused`, when the bot is in no team (`no_team`), may not emit the name (`not_allowed`: no rule fits it, or it is `error.*` or `done.*`), addresses a bot outside its teams (`not_member`) or passed its rate (`rate_limited`).

## Subscriptions

A bot listens to the topics of its teams it subscribes to ([ADR](../architecture/pubsub-events.md)). Each entry of `subscriptions` is one name or pattern; one per pattern, at most 16:

```json
"subscriptions": [
  {"topic": "pedido.*", "mode": "task", "from": ["recepcion"]},
  {"topic": "warn.stock", "urgent": true},
  {"topic": "done.*", "mode": "note", "objective": "Note what {{.from}} finished: {{.message}}"}
]
```

| Field | Meaning |
|---|---|
| `topic` | a name or pattern (`*` one segment, `**` any) |
| `mode` | what a heard event does: `task` (a new task), `message` (a line in the bot's running task, through [mid-run chat](node-protocol.md); a task when it runs nothing on a node) or `note` (kept for its next task's prompt; no task); absent: by the severity (`info` note, `warning` message, `critical` task). An urgent event is always a task |
| `from` | only the events of these teammates, `operator` or `control-plane`; absent: anyone in the team. A `PATCH` that names a bot outside the teams is `400` |
| `urgent` | `true`: a `warn.*` it hears makes an urgent task. A critical is urgent anyway |
| `objective` | the template of the task it makes, over the event's fields; absent: `Your teammate <from> says (<name>): <message>` |
| `status` | `active` (absent) or `pending`: a bot's ask a person has not approved; it hears nothing. A bot asks with the tool `axlr_bots_subscribe {topic, why}`, served next to the emit tool; a second ask for a pattern it has is `already_subscribed`, a 17th `too_many`; the ask records `bot.subscription.asked` and carries `askedBy`, `why` and `askedAt`. A `PATCH` may keep a pending entry as it is, make it `active` or drop it, never add or edit one |

Who hears an event: every Enabled teammate of the emitter but the emitter. An event addressed to it (`to`) is always heard, by its matching subscription or else by the implicit one (`team`); an event not addressed to it is heard only by an active subscription whose topic and `from` fit; every bot also hears the team's `danger.*` and `stop.*`, whatever it subscribes to, so a Stop always lands. The receiver records `bot.event.heard` (`eventId`, `name`, `from`, `by`, `mode`, `taskId` or `messageId`); a `PATCH` of the list records `bot.subscription.changed`.

A message reads `Your teammate <from> says (<name>): <message>`, with the facts as compact JSON on a second line, and comes `from: event:<event id>`. A note waits for the bot's next task: when a task starts, its prompt carries the bot's last 8 notes of the last 24 hours, newest last, at most 1200 characters, under "Recent events in your team", between the directory and the task; the notes are cut before the directory, and the mission and the objective never. The task keeps them as `input.notes` (`from`, `name`, `message`, `at`), so its detail shows what it knew and a later execution of it is given the same. A Paused bot's notes are recorded too; it reads them when it is enabled. A repeat of the same event gives nothing twice.

An event that would make a second task while the bot's task from an event of the **same name** is still Pending (not started) makes none: it is recorded as a note with `coalescedInto: <task id>`, and that task's prompt, built when it starts, carries it. A critical is never coalesced: a second `stop.*` is a second urgent task.

The control plane publishes its own facts about a bot's task on the bot's teams, as that bot and `by: control-plane`, only when a teammate subscribes to the name; the bot itself never hears its own:

| Name | When | `data` |
|---|---|---|
| `error.bot` (critical) | the task failed or expired | `taskId`, `reason` |
| `warn.approval` (warning) | the task waits for a person to approve a call | `taskId`, `tool` |
| `done.task` (info) | the task completed | `taskId`, `answer` (the task's result, cut at 280 characters) |

A supervisor that subscribes to `error.*` gets an urgent task for each teammate's failure; a task an `error.bot` made is on the failed task's chain, so a chain of failures is cut at `events.maxDepth`. With nobody subscribed, the history keeps only `bot.task.failed`. A trigger `{"type": "event", "events": ["a.*"], "interrupt": true}` in a definition is read as `{"topic": "a.*", "mode": "task", "urgent": true}`, and `bot get` shows it so; the next release refuses it.

```bash
axlr-bots bot subscribe almacen 'pedido.*' --from recepcion [--mode task]
axlr-bots bot subscriptions almacen
axlr-bots bot unsubscribe almacen 'pedido.*'
```

## Tool rules

`toolRules` narrow the allowed tools by their arguments in the runtime contract's words: a glob over each argument's value as text (`*` within a path segment, `**` across segments, `?` one character; an array such as `args` is matched as its compact JSON). A tool with allow rules may only be called as a rule says; a tool without rules is allowed as a whole; a deny rule wins. A rule that names a tool outside `allowedTools` is refused, so a narrower tool list cannot strand a rule.

```json
"toolRules": {
  "allow": [{"tool": "local_exec", "arguments": {"program": "systemctl", "args": "[\"status\",*"}}],
  "deny":  [{"tool": "local_write", "arguments": {"path": "**/.env"}}]
}
```

## Triggers

A trigger is how tasks reach a bot. It has a type, a name unique within the bot (the type when not given) and, for a webhook or a timer, an objective template the fire renders over the event (`text/template`; a field the template names and the event lacks refuses the fire, so an objective never says `<no value>`). A bot that declares triggers takes only those; the default is `manual` alone.

Every fire creates a task through the same path as a manual submission, under an **idempotency key derived from the trigger and the event**, so a redelivered webhook or a scheduler that restarts and fires the same slot finds the task it already created and starts nothing (vision §14.7). Every fire is a `bot.trigger.fired` event with the task, the key and `deduplicated` true or false; the task keeps the trigger and the event as its `input`.

```json
"triggers": [
  {"type": "manual"},
  {"type": "webhook", "name": "deploy", "secret": "<16 to 256 characters>",
   "objective": "Handle event {{.type}} for {{.service}}"},
  {"type": "webhook", "name": "deploys", "signingKey": "<16 to 256 characters>",
   "objective": "Look at the deploy of {{.service}}"},
  {"type": "alertmanager", "name": "alerts", "secret": "<16 to 256 characters>"},
  {"type": "timer", "name": "clock", "every": "10m",
   "objective": "Say the current time of this host (the trigger fired at {{.firedAt}})."},
  {"type": "timer", "name": "nightly", "cron": "0 3 * * 1-5", "objective": "Write the nightly report"}
]
```

| Type | Fired by | Key | Authentication |
|---|---|---|---|
| `manual` | `task submit --key`, `POST /v1/bots/{id}/tasks` with `Idempotency-Key` | the caller's key | the socket, or the operator bearer token |
| `webhook` | `POST /v1/triggers/{bot}/{trigger}` with a JSON object as the body | `webhook:<name>:<hash>` of the `Idempotency-Key` header (the delivery id) when the sender set one, else of the body | the trigger's `secret` in `X-Axlr-Secret`, or the HMAC-SHA256 of the body under its `signingKey` in `X-Axlr-Signature: sha256=<hex>` (`X-Hub-Signature-256` is read too); never the bearer token. Over the socket the caller is trusted, as for every route |
| `timer` | the daemon, on every tick | `timer:<name>:<unix seconds of the slot>` | none: it is the control plane's own |
| `delegation` | another bot's execution, through its node ([delegation](#delegation)) | `delegation:<asking task>:<hash of the bot and the objective>` | the asking node's certificate, and the asking bot's `delegation.to` or a shared team |
| `event` | a teammate's event the bot heard ([subscriptions](#subscriptions)); the task names the subscription's topic, or `team` | `event:<event id>:<bot>` | the emitting node's certificate, a shared team and the emitter's `events` |
| `alertmanager` | `POST /v1/triggers/{bot}/{trigger}` with Alertmanager's webhook payload (version 4) as the body: a webhook whose body is read. A **firing** group opens an incident of the bot and its triage task (the payload is the task's `input`; the title is `<alertname> in <namespace>`, the severity the `severity` label: critical sev1, error sev2, warning sev3, info sev4), is a repeat of the incident the group already has (Alertmanager resends a group every `repeat_interval`: alerts added, no task), or reopens one resolved less than 30 minutes ago; the alerts marked resolved inside it are recorded. A **resolved** group (`send_resolved: true` on the receiver) resolves the incident and starts nothing; a group nobody had is ignored. The trigger's optional `objective` renders over the incident and, under `alert`, the payload. A body that is not Alertmanager's is `400` | `incident:<incident id>:<cycle>`; none for a repeat or a resolution | as a `webhook`: the trigger's `secret` or `signingKey` |
| `incident` | the control plane, when `POST /v1/incidents` opens or reopens an incident for the bot ([operator API](operator-api.md#routes)); the trigger's optional `objective` is rendered over the incident (`id`, `title`, `severity`, `groupKey`, `fingerprints`, `cycle`, …), the default being "Incident … Find the cause with evidence and report it; change nothing"; the task's `input` is the incident. One task per cycle: a repeated alert starts nothing. When the task ends, the incident is Diagnosed (Completed) or Escalated (anything else) | `incident:<incident id>:<cycle>` | the socket, or the operator bearer token |

**Webhook.** A webhook is fired from outside with the trigger's own credential: the shared `secret` compared in constant time, or a signature over the body under the `signingKey` (what GitHub and most senders produce). An unknown bot or trigger, a missing or wrong credential and a tampered body are all `403`, so the route tells a stranger nothing; a body past 256 KiB is `413`. The secret and the signing key are stored with the bot (file mode 0600) and redacted in every answer of the API and the CLI. The body is stored on the task as its `input`; only the rendered objective reaches AXLR.

**Timer.** A timer runs `every` so often (at least a second; the first fire is when it is armed) or on a five-field `cron` expression evaluated in the host's time zone (numbers only; Sunday is 0 or 7; a day of month and a day of week both restricted match either, as in cron). Its state is one file, `<state>/timers/<bot>~<trigger>.json`, with the next fire.

Catch-up: a daemon that comes back after a long stop finds the slot in the past, **fires once for it** with that slot's key (so a crash between the fire and the save fires the same slot again and finds the same task) and arms the next slot after now; the slots in between are not replayed. A paused bot's timers are advanced, not fired; a changed schedule re-arms the timer; a removed trigger drops it. The timer's event carries `trigger`, `scheduledAt` and `firedAt`, which the template may use.

The procedure, with the checks, is the [triggers runbook](../runbooks/triggers.md).

## Runtime

A bot's tasks run as an AXLR session unless its definition says otherwise: a model reads the mission and the objective and calls the tools. A **Jev bot** has no model. Its node runs a fixed program over one MCP server, and every decision in it is one question to [Jev](https://typesafe.ai), TypeSafe's judgement model, which answers a choice among options in about 0.2 s. The world being acted on (the MCP server) writes the questions; the program only carries them to Jev and carries the answer back. It is for a bot whose steps are known and whose only open point is a choice: the orchestra's chairs, which play the phrase Jev picks.

```json
"runtime": {
  "kind": "jev",
  "jev": {
    "server": "score",
    "start": "phrases",
    "act": {"tool": "play", "choice": "phrase", "verdict": "judge", "pass": ["dynamics"]},
    "wait": "listen",
    "stop": "stop",
    "model": "jev-1.13.0"
  }
}
```

| Field | Meaning |
|---|---|
| `server` | the MCP server that serves the tools, by its name in the node's `~/.config/axlr/mcp.json` (the file the node's AXLR reads): one server per bot |
| `start` | the tool a task begins with (no arguments) |
| `act` | how the program acts on a choice: the `tool`, the argument that takes the `choice` (the first whole number in Jev's choice, else its text: «phrase 3: bars 17–24» is 3), the argument that takes Jev's `verdict` as `{choice, probability, confidence}` (optional), and the fields of the event's data to `pass` on as they are (at most 8; a cue's `dynamics`) |
| `wait` | the tool that waits for the next question |
| `stop` | the tool a `stop.*` task calls once (optional) |
| `tell` | what the bot tells its teams after each act (optional): the event's `name` (one the bot may emit) and the `fields` of the decision that ride as its data: `choice`, `probability`, `confidence`, the act's choice argument, a field passed on; all of them when none is named. The message is Jev's choice. Teammates hear it as any event ([subscriptions](#subscriptions)) |
| `model` | the pinned Jev version; `jev-1.13.0` when absent, never a `-latest` alias |

The program, for a task that an event made (a cue): `start`; then, while a result carries `ask: {state, question, options}`, put it to Jev, call `act` with Jev's choice, call `wait`, which answers with the next `ask`; a result with `done: true` ends the task. A result with neither (a paused world, or between questions) is waited on again, a little later each time (up to 5 s between waits): a pause never ends a task; the budgets and `taskTimeoutSeconds` end a task the world never ends. For a `stop.*` task: `stop` once. An event heard while the task runs (a cue in mode `message`) changes what the next `act` passes on. Jev not answering is retried on the next question, three in a row fail the task; the world refusing `act` three times in a row fails it too; the budgets bound Jev's calls (`maxModelCallsPerTask`) and the tools' (`maxToolCallsPerTask`) and end a task with `budget_exceeded`, as AXLR's do.

The program's tools must be in `allowedTools`, and the level must be `act-within-policy`: the program calls its tools without asking a person. A definition or an update that narrows them past the program is refused. The execution's report carries the tool calls, the usage (Jev's calls and input tokens, the cost at TypeSafe's price) and a receipt, `axlr-bots.jev-receipt/v1`, with each decision: a hash of the question, the choice, its probability, Jev's confidence, the latency and whether the act was taken. The trace stays on the node, as AXLR's does.

A node runs Jev bots only when it has `TYPESAFE_API_KEY` in its environment: it then registers the runtime `jev` in its capabilities, and the control plane assigns a Jev bot to no other node. `axlr-node --mcp-config` names another `mcp.json`; `--jev-model` another pinned version.

## Register, enable, update

```bash
axlr-bots bot register --file cmd/axlr-bots/testdata/bot.json      # POST /v1/bots; a repeat of the id is 409
axlr-bots bot enable bot                             # the bot accepts tasks from now on
axlr-bots bot update bot --file patch.json           # PATCH /v1/bots/{id}
axlr-bots bot get bot                                # credentials redacted
axlr-bots bot events bot --limit 100                 # the bot's newest events, oldest first
```

A task's `priority` is `urgent` when it went first ([urgent tasks](#urgent-tasks)), absent otherwise.

A patch replaces the fields it names (`mission`, `level`, `allowedTools`, `toolRules`, `triggers`, `placement`, `approvals`, `delegation`, `groups`, `events`, `subscriptions`, `model`, `budgets`) with their whole new value; the identity and the status do not change that way, and a patch that strands a tool rule or names a bad trigger changes nothing. [`cmd/axlr-bots/testdata/bot-triggers-patch.json`](../../cmd/axlr-bots/testdata/bot-triggers-patch.json) adds a timer and a webhook to the example bot; [`cmd/axlr-bots/testdata/bot-act-with-approval.json`](../../cmd/axlr-bots/testdata/bot-act-with-approval.json) is the example bot one level up, with `local_write` allowed.

## Tasks

A task is one objective for one bot. It is created by a manual submission or a trigger, under an idempotency key, and runs as one or more executions (a new one after each approval). Its states are `Pending`, `Running`, `WaitingApproval`, `Completed`, `Failed`, `Expired` (nobody decided its call in time) and `Cancelled`; every task carries a trace id (`correlationId`) that every execution and event repeats.

```bash
axlr-bots task submit bot --objective "List the files in your workspace" --key list-1
axlr-bots task get task-…
axlr-bots task report task-…
```
