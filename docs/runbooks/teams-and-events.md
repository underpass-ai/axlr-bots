# Teams and events from the panel

Bots that share a **Team** know each other, ask each other to do a task and warn each other with events; the operator sets it all up in the desktop panel and writes nothing but the team's name ([ADR](../architecture/groups-and-events.md), [reference](../reference/bot-definition.md#teams)). An event's name says how much it matters by its first word (`stop`, `danger`, `warn`, `info`), and each bot listens to the topics it subscribes to, in the mode it chooses ([publish/subscribe](../architecture/pubsub-events.md), [subscriptions](../reference/bot-definition.md#subscriptions)). The example is the [port fleet](drone-fleet.md): the plane's bot warns quad-1's bot that a ship is coming, quad-1 flies home, and the operator sees why.

| Step | In the panel | What happens |
|---|---|---|
| 1. Put bots in a team | **Bots → New bot → Team**: `port`; or a bot's page → **Team → Change** | the bot is in `port`; its teammates read it in their next task's directory |
| 2. Check the team | **Teams → port** | each member with what it is for, its tools and where it runs; "only one bot: a typo?" when a name was typed differently |
| 3. Give the order | **Tasks → New task**, bot `pilot-plane-0`: «Dile al quad-1 que se retire: hay un barco entrando en su zona» | the plane's bot calls `axlr_bots_emit_event` with `stop.recall` for `pilot-quad-1` (an older pilot's `recall` is read the same); no person approves telling the team |
| 4. Watch it | the banner **Alarm to the team · team port** wherever you are; **Teams → port → What happens** | quad-1's bot gets an **urgent** task: what it was doing ends **Stopped for urgent**, `return_home` is **Approved by its policy**, the vehicle flies home, the task is **Done** with the reason |
| 5. See why | open quad-1's task | **Told by** `pilot-plane-0: …`, the urgent pill, the tool call and who approved it (`policy:autoApprove`) |
| 6. Recall it yourself | a bot's page or the team's page → **Recall** | an urgent task «Return home now»: one click, no approval |
| 7. Brake | a bot's page → **Stop** | the bot is paused and every task it had not finished is cancelled |
| 8. Listen to a topic | a bot's page → **Listens to → Listen to a topic**: topic `warn.*`, mode (by its kind unless you pick: `task` a new task, `message` into its running task, `note` for its next task), from (anyone) | the line `warn.* · message · from anyone`; `bot.subscription.changed` in the timeline; **Remove** undoes it |
| 9. Answer a bot's ask | the card «`almacen` asks to listen to `error.*`: "…"» on its page; it counts in **need you** | **Approve**: it hears the topic from now on; **Deny**: the ask is dropped. Until then it hears nothing of it |
| 10. See the team's topics | **Teams → port → Topics** | each topic: who publishes, who listens and how, the last event; **nobody listens** in red on one nobody hears |
| 11. Publish an event | **Teams → port → Publish an event**: name `stop.all`, message, optional To | `stop.all` is the generalised Recall: every member gets an urgent task; `warn.*` reaches who listens while it works; `info.*` is a note. The timeline shows it and what it did to each bot within a poll |
| 12. One press | `~/.config/axlr-bots-panel/shortcuts.toml`: `[[shortcut]]` `team`, `label`, `name`, `message`, `data`; restart the panel → **Teams → orchestra → Shortcuts** | each press publishes its event at once, as the operator, with a new key; a stop is red ([the orchestra's](../../examples/orchestra/panel/shortcuts.toml)) |

A task can be urgent from the panel too: **New task → Urgent** sends it first and stops what the bot is doing.

## What the bots need

| Bot | Needs |
|---|---|
| any bot in a team | nothing more: the team gives it the delegation and the emit tools, still under its level |
| a vehicle's pilot that comes home | `return_home` in its **Tools**; the panel then lets `return_home` run without a person (`approvals.autoApprove`); taking off (`run_mission`) still asks |

## Under Advanced (a definition file)

Everything else has a default; a bot's definition file (**Bots → From file**) can change it:

| Field | Default | Example |
|---|---|---|
| `events.emit` | any name but `error.*` and `done.*`; the first segment (`stop`, `danger`, `warn`, `info`) fixes the severity | `[{"name": "stop.recall"}, {"name": "warn.*"}]` |
| `events.perMinute` | 6 | `12` |
| `events.intakePerMinute` | 10 events a minute taken as tasks or messages; past it, notes | `20` |
| `events.maxDepth` | an event's chain goes 2 deep | `3` |
| `events.interruptible` | a critical a teammate emits (`stop.*`, `danger.*`) interrupts | `false` |
| `subscriptions` | hears what is addressed to it and the team's `danger.*` and `stop.*` | `[{"topic": "ship.*", "urgent": true}]` (`bot subscribe <bot> 'ship.*'`); a trigger of type `event` is read as one |
| `delegation.fromTeam` | takes its teammates' tasks | `false` |
| `approvals.autoApprove` | none (the panel adds `return_home`) | `[{"tool": "return_home"}]` |

## When it does not work

| You see | Why | Do |
|---|---|---|
| the plane says it cannot tell quad-1 | they are not in the same team, or quad-1 is paused | **Teams → port**: both there and on |
| quad-1 got the event but kept flying | its pilot has no `return_home`, or the vehicle's service is older | add the tool; update the fleet's `fleet-app` |
| quad-1 waits for your OK to come home | its definition has no `autoApprove` for `return_home` (it was added before, or from a file without it) | approve it this time; then add `"approvals": {"autoApprove": [{"tool": "return_home"}]}` to its definition, or re-add it from the form with `return_home` in Tools |
| `rate_limited` in the timeline | the plane emitted more than its rate in a minute | wait a minute, or raise `events.perMinute` |
| `depth_exceeded` | a chain of events went past its depth | by design (2: a reaction may react once); raise `events.maxDepth` on the emitter if a longer chain is wanted |
| a bot never reacts to a teammate's `info.*` or `warn.*` | nothing addressed it and it does not listen to that topic | its page → **Listen to a topic**; **Teams → port → Topics** shows **nobody listens** |
| a warning arrived as a line in its chat, not a task | it was working: `warn.*` is a `message` by default | pick mode `task` on its subscription, or `"urgent": true` to stop what it does |
| a bot "noted" an event and did nothing | `info.*` is a `note` by default: it is in its next task's prompt (**Knew** in that task) | pick mode `task`, or send it a task |
| «too many this minute» on a line | the bot took 10 events in a minute as tasks or messages; the rest are notes | raise `events.intakePerMinute` on that bot |
| «joined its waiting task» | an event of the same name found its task still Pending | by design: the task's prompt carries both; a `stop.*` is never joined |
| a bot's ask stays pending | asks wait for a person | its page → **Approve** or **Deny** |
| the Topics part says the control plane is older | the control plane predates topics | update the control plane; the rest of the panel works |
