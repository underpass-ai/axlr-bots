# Teams and events from the panel

Bots that share a **Team** know each other, ask each other to do a task and warn each other with events; the operator sets it all up in the desktop panel and writes nothing but the team's name ([ADR](../architecture/groups-and-events.md), [reference](../reference/bot-definition.md#teams)). An event's name says how much it matters by its first word (`stop`, `danger`, `warn`, `info`), and each bot listens to the topics it subscribes to, in the mode it chooses ([publish/subscribe](../architecture/pubsub-events.md), [subscriptions](../reference/bot-definition.md#subscriptions)). The [panel's README](../../panel/README.md) shows each screen.

The example is a team `ops` of three bots: `watcher` reads the deploy log of a service, and `worker-1` and `worker-2` run jobs on it. `run_job` and `rollback` stand for the tools your own service serves to the workers (an MCP server, [sidecar](sidecar.md)); any names work the same. The watcher tells worker-1 that a deploy is starting, worker-1 stops its job and rolls it back, and the operator sees why.

| Step | In the panel | What happens |
|---|---|---|
| 1. Put bots in a team | **Bots → New bot → Team**: `ops`; or a bot's page → **Team → Change** | the bot is in `ops`; its teammates read it in their next task's directory |
| 2. Check the team | **Teams → ops** | each member with what it is for, its tools and where it runs; "only one bot: a typo?" when a name was typed differently |
| 3. Give the order | **Tasks → New task**, bot `watcher`: «Tell worker-1 to stop: a deploy is starting on service-1» | the watcher calls `axlr_bots_emit_event` with `stop.halt` for `worker-1`; no person approves telling the team |
| 4. Watch it | the banner **Alarm to the team · team ops** wherever you are; **Teams → ops → What happens** | worker-1 gets an **urgent** task: what it was doing ends **Stopped for urgent**; its `rollback` is approved **by its policy, no person needed** when its definition has the rule (below); the task is **Done** with the reason |
| 5. See why | open worker-1's task | **Told by** `watcher: …`, the urgent pill, the tool call and who approved it (`policy:autoApprove`) |
| 6. Give an order yourself | a bot's page, or its line on the team's page → **Urgent order**: type the order → **Send the order** | an urgent task with your words: it stops what the bot was doing and does this; the banner «… got an urgent order» and the task's page. Sending twice is the same order: the form keeps one key |
| 7. Talk to it while it works | a bot's page → **Talk to it**: write a message → **Send** | while the bot runs a task, the message goes to that run, which reads it between its steps, and what the run says back shows in the chat (`bot.task.said`); when it runs nothing, the message runs as a new task. A node whose `axlr-tui` takes no `--steer-file` drops it ([node protocol](../reference/node-protocol.md#the-agents-loop)) |
| 8. Brake | a bot's page → **Stop** | the bot is paused and every task it had not finished is cancelled |
| 9. Listen to a topic | a bot's page → **Listens to → Listen to a topic**: topic `warn.*`, mode (by its kind unless you pick: `task` a new task, `message` into its running task, `note` for its next task), from (anyone) | the line `warn.* · message · from anyone`; `bot.subscription.changed` in the timeline; **Remove** undoes it |
| 10. Answer a bot's ask | the card «`worker-2` asks to listen to `error.*`: "…"» on its page; it counts in **need you** | **Approve**: it hears the topic from now on; **Deny**: the ask is dropped. Until then it hears nothing of it |
| 11. See the team's topics | **Teams → ops → Topics** | each topic: who publishes, who listens and how, the last event; **nobody listens** in red on one nobody hears |
| 12. Publish an event | **Teams → ops → Publish an event**: name `stop.all`, message, optional To | `stop.all` stops every member: each gets an urgent task; `warn.*` reaches who listens while it works; `info.*` is a note. The timeline shows it and what it did to each bot within a poll |
| 13. One press | `~/.config/axlr-bots-panel/shortcuts.toml`: `[[shortcut]]` `team`, `label`, `name`, `message`, `data`; restart the panel → **Teams → ops → Shortcuts** | each press publishes its event at once, as the operator, with a new key; a `stop.*` or `danger.*` is red. A label is 1 to 24 characters, a name one name (no pattern, no `error.*` or `done.*`), `data` a table. A bad file is a banner at start; no file, no buttons |

A task can be urgent from the panel too: **New task → Urgent** sends it first and stops what the bot is doing.

```toml
# ~/.config/axlr-bots-panel/shortcuts.toml
[[shortcut]]
team = "ops"
label = "Halt the team"
name = "stop.all"
message = "Stop: the operator halts the team"

[[shortcut]]
team = "ops"
label = "Deploy starts"
name = "warn.deploy.started"
message = "A deploy is starting on service-1"
data = { service = "service-1" }
```

## What the bots need

| Bot | Needs |
|---|---|
| any bot in a team | nothing more: the team gives it the delegation and the emit tools, still under its level |
| a worker whose stop must not wait for a person | `rollback` in its **Tools**, and `"approvals": {"autoApprove": [{"tool": "rollback"}]}` in its definition so it runs without a person (the panel adds no such rule); `run_job` still asks |

## Under Advanced (a definition file)

Everything else has a default; a definition file (**Bots → From file** registers one) or a patch (`axlr-bots bot update <bot> --file <patch.json>`) changes it:

| Field | Default | Example |
|---|---|---|
| `events.emit` | any name but `error.*` and `done.*`; the first segment (`stop`, `danger`, `warn`, `info`) fixes the severity | `[{"name": "stop.halt"}, {"name": "warn.*"}]` |
| `events.perMinute` | 6 | `12` |
| `events.intakePerMinute` | 10 events a minute taken as tasks or messages; past it, notes | `20` |
| `events.maxDepth` | an event's chain goes 2 deep | `3` |
| `events.interruptible` | a critical a teammate emits (`stop.*`, `danger.*`) interrupts | `false` |
| `subscriptions` | hears what is addressed to it and the team's `danger.*` and `stop.*` | `[{"topic": "deploy.*", "urgent": true}]` (`bot subscribe <bot> 'deploy.*'`); a trigger of type `event` is read as one |
| `delegation.fromTeam` | takes its teammates' tasks | `false` |
| `approvals.autoApprove` | none | `[{"tool": "rollback"}]` |

## When it does not work

| You see | Why | Do |
|---|---|---|
| the watcher says it cannot tell worker-1 | they are not in the same team, or worker-1 is paused | **Teams → ops**: both there and on |
| worker-1 got the event but its job went on | its urgent task stopped the model, not the job: the job stops only when a tool says so, and worker-1 has no `rollback`, or the service is older | add the tool; update the service |
| worker-1 waits for your OK to roll back | its definition has no `autoApprove` for `rollback` (the panel's form adds none) | approve it this time; then add `"approvals": {"autoApprove": [{"tool": "rollback"}]}` with `axlr-bots bot update <bot> --file <patch.json>` (a patch replaces the whole `approvals`) |
| `rate_limited` in the timeline | the watcher emitted more than its rate in a minute | wait a minute, or raise `events.perMinute` |
| `depth_exceeded` | a chain of events went past its depth | by design (2: a reaction may react once); raise `events.maxDepth` on the emitter if a longer chain is wanted |
| a bot never reacts to a teammate's `info.*` or `warn.*` | nothing addressed it and it does not listen to that topic | its page → **Listen to a topic**; **Teams → ops → Topics** shows **nobody listens** |
| a warning arrived as a line in its chat, not a task | it was working: `warn.*` is a `message` by default | pick mode `task` on its subscription, or `"urgent": true` to stop what it does |
| a bot "noted" an event and did nothing | `info.*` is a `note` by default: it is in its next task's prompt (**Knew** in that task) | pick mode `task`, or send it a task |
| «too many this minute» on a line | the bot took 10 events in a minute as tasks or messages; the rest are notes | raise `events.intakePerMinute` on that bot |
| «joined its waiting task» | an event of the same name found its task still Pending | by design: the task's prompt carries both; a `stop.*` is never joined |
| a bot's ask stays pending | asks wait for a person | its page → **Approve** or **Deny** |
| a chat message got no answer while the bot worked | the node's `axlr-tui` takes no `--steer-file`, so the node logged and dropped it | update AXLR on that node; meanwhile send it as a new task once the run ends |
| **Urgent order** is greyed out | the bot takes no tasks (Paused or not turned on), or another act on it is on its way | turn it on; wait for the other act |
| the Topics part says the control plane is older | the control plane predates topics | update the control plane; the rest of the panel works |
