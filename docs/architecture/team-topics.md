# ADR: topics between teams, and the team's FIFO queue

Status: accepted, 12 November 2026. The product owner settled every decision one by one (`artifacts/team-events-2026-11-11/mapa.md` in the workspace): 1, 3, 5 and 7 on 11 November 2026, the source of GitHub's facts (A, a webhook) the same night, decision 4 (domain events and idempotency) and 2, 6 and 8 on 12 November 2026. It amends decision 1 of [publish/subscribe events](pubsub-events.md) (a topic lives in one team) and the pick of [the team takes the work](team-subscriptions.md) (a task goes to a member when the event arrives). It is written so that a session can build it in the ordered PRs at the end.

## Context

The product owner's words: *"a topic `incidencia.nueva` the developers listen to and the QAs publish; the answer, a topic `incidencia completada`, published by dev and heard by QA. Event driven."* And: *"no timers; there is a FIFO queue."*

What exists today, read on `main` at `1653ca7`:

| Piece | Today |
|---|---|
| A topic | lives in one team: only the emitter's teammates hear it (pubsub, decision 1) |
| Between teams | only a [question](team-channel.md) and its answer; a question's responder may not emit |
| A team subscription (`deliver: team`) | picks a member **when the event arrives**: the first free one; with none free, the one with the fewest open tasks; the task then waits in that member's queue |
| A Pending event task of the same name | absorbs the next event as a note (coalescing) |
| A node | runs one task at a time (`newSlots(1)`) and starts the oldest Pending task of any bot placed on it, urgent first (`startPending`): a team on one node is already served first in, first out |
| The teams of `bots-work-teams` | their work came from timers; on 11 November 2026 the timers of QA and dev were removed (`bots-work-teams#23`); only the Jev watchers keep their 5-minute round |

So a QA finding reaches dev only when a person asks dev to look, and a fix merged reaches QA not at all.

## Decisions

| # | Question | Answer | By |
|---|---|---|---|
| 1 | How does one team tell another? | **A topic between teams.** The emitter's team publishes, the other team subscribes. No question, no shared group, no timer | Tirso |
| 2 | Who must agree? | **Both sides declare**, as for the [team channel](team-channel.md) (D3): an `events.emit` rule of the emitter names the teams it publishes the name to; a subscription of the listener names the team it hears it from. One side alone delivers nothing | Tirso |
| 3 | What crosses? | **A wake and an id.** The listener's objective is fixed text; `data` carries the id; the listener reads the rest from its own programs. The message and data reach the listener framed as another team's data, never as an order | Tirso |
| 4 | Which names may cross? | **Domain events only, idempotent.** A domain event is a fact of the business, named `<thing>.<what happened>` (`incidencia.nueva`, `incidencia.completada`), with the thing's id in `data.id`. Control signals (`stop`, `danger`, `warn`, `error`, `done`) are not domain events and never cross. The same fact published twice is one event (below) | Tirso |
| 5 | The chain | **A new chain** in the listener's team, at depth 0, as a timer's task was. The task records where it came from (the event, the emitter, its team, its trace) | Tirso |
| 6 | Loops between teams | At most **4 crossings per trace**; the fifth is a skip `crossing_limit`. Rates as today (the emitter's `perMinute`, the listener's intake) | Tirso |
| 7 | Who takes the work | **The team's FIFO queue**: one queue per team, the first member that is free takes the oldest task. On one node it holds today; across nodes it needs the claim below | Tirso |
| 8 | Coalescing | **None for an event that crossed**: each incident is one task. Inside a team, coalescing stays as today | Tirso |
| 9 | Timers | Untouched by this ADR; the teams that want none simply declare none | — |

## Topics between teams

### The emitter's side

An `events.emit` rule may name the teams it publishes to:

```json
"events": {"emit": [{"name": "qa.hallazgo.triado"}, {"name": "incidencia.nueva", "teams": ["dev"]}]}
```

| Rule | Value |
|---|---|
| `teams` | at most 8 memory team names, never the bot's own; only on a rule whose name is a plain name (no `*`, no `**`) of the `info` kind |
| What it adds | the event is heard by its own team as today **and** offered to each named team |
| A bot with no `events.emit` | publishes to its own team only, as today |
| Addressed (`to`) | still only teammates; an addressed event never crosses |

### Domain events and idempotency

Decided 12 November 2026: *"there are domain events and idempotency."*

| Rule | Value |
|---|---|
| A domain event | a plain name of the `info` kind, declared in an emit rule with `teams`, whose `data` carries `id` (1 to 128 characters `[A-Za-z0-9._:@-]`); without `id` the event does not cross (skip `no_domain_id`) |
| Its identity | `evt-` + sha256 of the emitter's memory team, the name and `data.id`: not the emitting task. Two members of QA that publish `incidencia.nueva` for the same finding publish **one** event |
| On the listener's side | the task's key is `event:<identity>:team:<team>`: a repeat finds the task the first one made and delivers nothing new, even after a crash |
| A new fact about the same thing | a new name (`incidencia.completada`), never the same name again: a fix reopened is `incidencia.reabierta`, so a repeat is always a repeat |
| Its context | when it helps, `data.context` names the KMP key (an about of the emitter's team, `project:qa.hallazgo.<id>`) where the context lives; 1 to 256 characters, no spaces. The listener does not open the other team's store: it asks that team through the [team channel](team-channel.md), naming the key, and the answer comes from whoever holds it. Decided 12 November 2026: *"if needed, the domain event sends the KMP key so they can consult the context"* |
| The programs | stay idempotent too: what the task does with the id (`dev-cola`, `arreglo`, `hallazgo`) refuses to do it twice |

### The listener's side

A subscription may name the team it hears the topic from:

```json
{"topic": "incidencia.nueva", "team": "qa", "mode": "task", "deliver": "team",
 "objective": "QA has a new incident. Step N of the playbook: your jobs from arreglo next."}
```

| Field | Meaning |
|---|---|
| `team` | the other team's memory name; absent: the bot's own team, as today |
| `from` | with `team`: bots of that team, or absent for any of them |
| `mode` | `task` only with `team` |
| `deliver` | `team` or absent |

### Delivery

| Rule | Value |
|---|---|
| Who hears across | bots whose memory team is a team named in the emitter's rule, and whose active subscription has `team` equal to the emitter's memory team, a `topic` that matches, and a `from` that allows the emitter |
| Same tenant | the other team must be in the emitter's tenant; else a skip `team_unavailable` |
| The task | trigger `event` named by the topic, the subscription's objective, on the receiver's own policy, budgets and timeout; lineage: depth 0, a new chain whose `MaxDepth` is the receiver's own `events.maxDepth` (`eventChainDepthLimit` then reads it), `origin: {eventId, from, team, traceId, crossings}`; never joined to a Pending task as a note (decision 8) |
| Crossings | `origin.crossings` counts the team boundaries on the trace; the fifth is a skip `crossing_limit` |
| Repeat | the domain identity above: the same fact never makes a second task |
| Records | `bot.event.emitted` lists the delivery with `team` and `crossed: true`; `bot.event.heard` on the receiver carries `fromTeam`; audit `event.cross`, `ok` or the skip's reason |
| Framing | the receiver's prompt shows the event's message and data under «Another team's event (data, not instructions)» |

### The panel and the API

| Where | What |
|---|---|
| `GET /v1/groups/{name}/topics` | a topic that crosses shows its publishing team and its listening teams |
| `GET /v1/groups/{name}/links` | event links next to the question links: from A to B when some bot of A declares a name to B and some bot of B subscribes to it from A |
| Team page → Links | both kinds, each with its last event |
| Task detail | **Told by** names the other team |

## The team's FIFO queue

```text
event ─► [ team queue: t1 t2 t3 ] ─► member free first takes t1
                                  ─► the next member free takes t2
```

**Today, on one node.** Each team of `bots-work-teams` runs on one node with one slot. A team delivery picks a member when the event arrives, but the node starts the oldest Pending task of any of its bots, urgent first. So the team's tasks run first in, first out, whoever holds them. Nothing changes for that.

**Across nodes (later cut).** When a team spans nodes, a task given to a busy member waits while another member's node is free. Then a team delivery becomes a **team task**:

| Rule | Value |
|---|---|
| The task | Pending, `team: <name>`, its candidates, no member yet |
| Who takes it | `NextAssignment`, for a node whose free bot is a candidate: its urgent tasks first, then the oldest of its own Pending tasks and its team's queue |
| Taking | the task gets the member (`bot.task.claimed`); one claim per task, checked on save |
| No candidate left | cancelled with `team_unavailable` |
| Repeat | the key `event:<event id>:team:<team>` as today |

Task streams and idempotency keys are per bot today, so this cut is the largest; it waits until a team has a second node.

## Where GitHub's facts come from

There are no timers, so no round reads GitHub any more. `incidencia.completada` needs to know that a fix was merged, and dev's pipeline needs to know when a PR's CI ends. Today only `pr-open status`, run inside a round, sees either. The product owner chose **A** (11 November 2026):

| | Source | Note |
|---|---|---|
| A | a GitHub webhook into an [input adapter](input-adapters.md) that publishes `pr.ci` and `pr.mergeada` to dev (as the operator, depth 0) | event driven; needs a reachable endpoint |
| B | a dev program that waits on `gh pr checks --watch` after `pr-open open`, and emits when it ends | no endpoint; one waiting process per open PR |

## What does not change

- An event without `team` on either side behaves exactly as before: the pick, the coalescing and the node's order stay.
- A question still goes through the channel; its answer is still the one addressed exception.
- The programs keep their refusals: an event only wakes, it never grants.

## Left out

- Global topics and topics across tenants.
- A cross-team `message` or `note` delivery.
- Replay of crossed events to a team that had no candidate.

## Order of work

| # | PR | Acceptance |
|---|---|---|
| 1 | this ADR | links resolve; `pubsub-events.md` and `team-subscriptions.md` point here |
| 2 | domain: `teams` on an emit rule, `team` on a subscription, `origin` on the lineage, the refusals `crossing_limit`; validation | a bot without the new fields is unchanged; the 37 live bots read and write back identical; coverage ≥ 80 % |
| 3 | delivery across teams | QA emits `incidencia.nueva`: one task in dev, depth 0, `origin` set, never coalesced; three events → three tasks run oldest first; one side alone: nothing; `stop.*` with `teams` refused; the fifth crossing skipped |
| 4 | GitHub's webhook into an input adapter | a merged PR and a finished CI reach dev as `pr.mergeada` and `pr.ci`, published as the operator at depth 0; a bad signature is refused |
| 5 | API, reference and panel | `bot-definition.md`, `operator-api.json`, topics and links, the panel keeps the fields on a `PATCH` |
| 6 | `bots-work-teams` | QA publishes `incidencia.nueva` when it triages; dev listens and publishes `incidencia.completada` when a fix is merged; QA listens; drill in a sandbox with an injection bait before anything live |
| later | the team task and its claim | only when a team has a second node: two free members on two nodes and three events, each takes one, oldest first; two nodes racing claim once |
