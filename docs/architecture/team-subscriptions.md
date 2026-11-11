# ADR: the team takes the work, not a fixed bot

Status: accepted, 11 November 2026. The product owner settled each question below one by one (`artifacts/lead-role-2026-11-11/decisiones.md` in the workspace). It builds on [publish/subscribe events](pubsub-events.md) and reuses the pick of [the team channel](team-channel.md).

Amended 12 November 2026: an event that crossed from another team is never coalesced, and a team on several nodes gets a FIFO queue with a claim: see [the team's FIFO queue](team-topics.md#the-teams-fifo-queue).

## Context

The product owner's vision: *the responsible of a team is a role, not a fixed bot; bots are horizontal, to avoid bottlenecks.* While this design was settled he went further: **everything is horizontal**. There is no fixed lead; any member of the team may take any round, event or question.

What exists today, read on `57c1bf2`:

| Piece | Who gets the work |
|---|---|
| A question of another team ([team channel](team-channel.md)) | already one member: the first free one by id |
| An event a teammate emits | **every** teammate whose subscription fits: three listeners, three tasks |
| A timer | the one bot that declares it |
| The chain cut of an event | a teammate already on the chain is skipped (`cycle`) |

The last row matters in a small team. In the developers' team the normal chain of a fix is lead → worker → reviewer; when the reviewer emits its verdict, every model bot of the team is on the chain and the event reaches nobody: the next step waits for a timer, up to four hours.

## Decisions

| # | Question | Answer |
|---|---|---|
| 1 | What does the team take, rather than a fixed bot? | Questions (as today), **events and timers**: all of it |
| 2 | Where does the role live? | In the **control plane**: one event, one timer slot, one task, given to one member picked by code, never by a model |
| 3 | How does a bot say the work is the team's? | `"deliver": "team"` on its subscription or timer trigger. A team's installer puts it on every bot that runs a model; a Jev bot never carries it |
| 4 | Permissions | Every member that takes the role holds what the role needs. That is in the teams' definitions, not here |
| 5 | Two bots doing the same thing | One delivery per event or slot (its key), and the programs refuse a repeat. No new lock |
| 6 | The chain cut for a team delivery | Being on the chain **does not exclude** a member. The chain's depth limit (`events.maxDepth`) still cuts a loop |
| 7 | Who picks | The pick of the team channel: the first free member by id, free being no task Pending, Running or waiting for a person; with none free, the one with the fewest open tasks, then by id. A pick is final |

## The team

A bot's team, for this ADR, is its **memory team** (`memoryTeamOf`: the first of its teams by name), as for the channel. A marketing squad (`mkt-editorial`) is no team of its own.

## Events

A subscription may say `"deliver": "team"`:

```json
{"topic": "dev.arreglo.aprobado", "mode": "task", "from": ["dev-revisor"], "deliver": "team",
 "objective": "dev-revisor approved a fix: {{.message}}. Open its PR."}
```

| Rule | Value |
|---|---|
| Mode | `task` only; `deliver: team` with `note` or `message` is invalid. A pending subscription a bot asks for never carries it |
| Who hears | as today: teammates of the emitter, never the emitter, whose subscription fits |
| Which deliveries go to the team | those heard through a `deliver: team` subscription, for an event that is **not addressed** (`to`) and **not critical** (`error`, `danger`, `stop`). Addressed and critical events reach every hearer as today |
| Candidates | per team, the hearers above that take tasks. When some Online node may run one of them, only those; else all of them (the task then waits Pending for a node, as a task does today) |
| Coalescing | when a candidate has a Pending event task of the same name not yet started, the event joins it as a note, as it does for one bot today |
| Repeat | before the pick, the task of the key `event:<event id>:team:<team>` is looked for among the candidates: the same event never makes two tasks, even after a crash between the task and the record of the event |
| Chain | depth as today; **no `cycle` skip** (decision 6) |
| The task | trigger `event` named by the topic, the subscription's objective, on the picked bot's own policy, budgets and timeout; its delivery and its `bot.event.heard` carry `team` |
| No candidate | a skip `team_unavailable` naming the team |

## Timers

A timer trigger may say `"deliver": "team"`:

```json
{"type": "timer", "name": "cola", "cron": "30 7,11,15,19 * * *", "deliver": "team",
 "objective": "Queue round: …"}
```

| Rule | Value |
|---|---|
| The timer | one per team and name, `team:<team>~<name>`, armed and fired by the timers' pass as a bot's is |
| Declarers | every bot of the team whose timer trigger of that name says `deliver: team`. They must agree on the schedule; when they do not, the timer is not fired and the pass reports the error |
| Candidates | the declarers that take tasks; preferring those an Online node may run, as for events |
| Repeat | the slot's key is looked for among the declarers before the pick: a slot makes one task |
| The task | the picked bot's own timer trigger: its objective, its policy and budgets |
| Nobody takes tasks | the slot is advanced, not fired, as for a bot that takes none |

## Stuck work

Decided 11 November 2026 (R7): no fixed rule of hours. *Jev checks every 5 minutes the tasks that pass 5 minutes; Jev sees what the bot recorded and gives its verdict: stuck or not.* The team's Jev watcher of [the team channel](team-channel.md#the-jev-watcher) does it, in the same round as the questions.

| Rule | Value |
|---|---|
| Which tasks | Running tasks of the watcher's team (memory team), of bots that run a model: never a Jev bot's, never a question (the asking team's watcher sees those) |
| When | its run began 5 minutes ago or more (`WatchAfter`); a younger one is neither put nor judged |
| What Jev reads | the task, its bot, what it was asked (first 200 characters), its trigger, how long it runs, how many steps its run reported and the last one, the bot's other open tasks |
| Choices | `wait` (nothing changes) or `stuck`; `abandon` stays the questions' and `stuck` is refused for a question |
| Stuck | the task is cancelled with Jev's verdict as the reason (audit `task.stuck`), and the control plane publishes `warn.stuck` (`taskId`, `bot`, `trigger`, `reason`; the message names the trigger too) as that bot: it never hears it; the teammate a `deliver: team` subscription picks takes the work over |
| An older node | its watch call does not ask for tasks (`tasks` in the watch message): it is put questions only, as before |

## What does not change

- An event or timer without `deliver` behaves exactly as before.
- A question still goes through the channel's own checks; only the pick is shared.
- The programs keep their refusals: the role moves who calls them, not what they allow.

## Today's limit

Each team runs on one node with one slot (`newSlots(1)`), so the pick buys resilience (a member paused or broken is covered) rather than speed, until a team has more nodes or slots.

## Order of work

| # | PR | Acceptance |
|---|---|---|
| 1 | The shared pick, out of the channel | the channel's tests unchanged and green |
| 2 | `deliver: team` on subscriptions | one event → one task; first free wins; emitter and non-team listeners excluded; addressed and critical skip the pick; coalescing; repeat after a crash; no cycle skip |
| 3 | `deliver: team` on timers | one slot → one task; schedules that differ are an error; a slot repeated after a crash makes no second task |
| 4 | Reference and panel | `bot-definition.md`, `operator-api.json`, the panel keeps the field on a `PATCH` |
