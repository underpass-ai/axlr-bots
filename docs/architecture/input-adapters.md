# ADR: input adapters

Status: proposed, 29 October 2026; amended 8 November 2026 with the [CloudWatch decision](#decision-8-november-2026-cloudwatch-through-sns) (the product owner: «SI» to a page and a template, in the design conversation of 29 October on the next worlds: an orchestra of 36 chairs conducted by hand, an anthill of 300 bots, a pyramid being built). Principle «no se tiene que trasladar la complejidad al usuario»; builds on [publish/subscribe events](pubsub-events.md), the [sidecar](../runbooks/sidecar.md) and the port fleet (private repo `axlr-bots-examples`, where the files named below live). This page names a pattern over primitives that exist; it adds nothing to the control plane.

## Context

A bot reads text and calls tools: its prompt carries a mission, an objective, its team's directory and its notes; a tool answers with text; a tool's refusal is text it reads ([runtime contract](runtime-contract.md)). A world is none of that: a simulation's positions and physics, a camera's pixels, a microphone's audio, a person's hand in front of a webcam. The port fleet already bridges the two in three places, each written for its own need: the companion computer's `telemetry.json` and the fleet base's `fleet.json`, read with `local_read`; the vision model's findings in that same file; the fleet base's webhooks when the sea changes. The next worlds reuse the same machine with other signals, and each would write its bridge again unless the shape has a name.

An **input adapter** is a small process beside a world service that turns the world's signal into one of the three things a bot already understands: a tool result, an event on its team's bus, or a note in its next prompt. The bot, its definition and the operator's panel do not change; the complexity stays in the adapter.

```text
world (a simulation · a camera · audio · telemetry · physics · an operator's hand)
                                    │
                           ┌────────▼────────┐
                           │  input adapter  │   text out · paced · labelled
                           └────────┬────────┘
           ┌────────────────────────┼────────────────────────┐
           ▼                        ▼                        ▼
    tool result               event on the bus           note in the prompt
    the bot asks              the world tells            the bot knows at its next task
    an MCP tool on loopback,  POST /v1/groups/{name}/events   a subscription of mode note
    or a file in the bot's    warn.* · danger.* · stop.*     (info.* is a note by default)
    read-only workspace       info.* to whoever listens
```

## The three outputs

| Output | When | The primitive it maps to | In the fleet today |
|---|---|---|---|
| **Tool result** | the bot decides when to look, and the answer is for this bot, now | an MCP tool the world service serves on loopback, named in the sidecar's `mcp.json` (`mcp_http.py` refuses any other `Host`, so the node runs in the world's pod); or a file the service writes into the bot's read-only workspace, read with AXLR's `local_read` under a tool rule that pins the path ([sidecar](../runbooks/sidecar.md)) | `run_mission` and `return_home` from `vehicle_pilot.py` on `127.0.0.1:8765`; `telemetry.json` per vehicle; `fleet.json` from `fleet_base.py`, rewritten every second, with the vision's findings in it |
| **Event on the bus** | the world changes and the team should hear it now | `POST /v1/groups/{name}/events` `{name, message, data?, to?}` with the operator token and an `Idempotency-Key` ([operator API](../reference/operator-api.md#routes)): the topic's kind fixes the severity, `to` addresses up to 16 teammates, whoever subscribes hears the rest. For one bot's task, a webhook trigger `POST /v1/triggers/{bot}/{trigger}` with the trigger's secret ([triggers](../runbooks/triggers.md)) | the fleet base fires the dispatcher's `sightings` and the logistics bot's `restock` webhooks (`control_plane.py`); nothing publishes on the team's bus yet |
| **Note** | a slow fact a bot should know when its next task starts; no task now | an `info.*` event heard through a subscription of mode `note` (the default for `info`): the bot's next prompt carries its last 8 notes of 24 hours, at most 1200 characters, under "Recent events in your team" ([bot definition](../reference/bot-definition.md#subscriptions)) | the pilots subscribe `info.*` as notes and `warn.*` as messages (`pilot.json`) |

Which door "the world tells" goes through follows the topic's kind, as the bus fixes it ([pub/sub](pubsub-events.md#topics-and-kinds)); an adapter chooses the kind, nothing else:

| The adapter publishes | Severity | Who hears it | What it makes |
|---|---|---|---|
| `info.<what>` | info | the bots subscribed to it, or the ones in `to`; nobody else | a note (a message or a task when the subscription says so) |
| `warn.<what>` | warning | the same | a line in a running task; a task when the bot is idle |
| `danger.<what>` | critical | every Enabled member, whatever it subscribes to | an urgent task (a normal one for a bot with `events.interruptible: false`) |
| `stop.<what>` | critical | every Enabled member | an urgent task, which drops the bot's queued tasks too; `stop.all` stops the whole team |
| `error.*`, `done.*` | | refused, `not_allowed`: the control plane's only | |

## Rules

| # | Rule | Why, in the code |
|---|---|---|
| 1 | **Text out, always.** The bot never gets pixels or audio: it gets what the adapter saw, as short text, with positions as fractions of the frame or as coordinates. The screen that shows the world carries the honesty label («each creature is one LLM bot; it sees what the adapter reports») | the fleet's snapshot says `"simulation": true` and every page shows it; the vision model is asked for fractions of the frame, and the base maps them to the sea |
| 2 | **The adapter paces, not the model.** A look that costs a frame or a model call returns when the frame is read; a move returns when the move is done; past its rate the adapter answers from its last reading and says how old it is. Rate and budget are the adapter's to enforce, so a swarm of 300 cannot drown a GPU by asking faster | the control plane bounds a bot's calls per task (`maxToolCallsPerTask`), not how often 300 bots call a world service; the publish route has no rate on the operator's side; a bot's intake degrades to notes only from the 11th `task` or `message` a minute, and a critical is never degraded nor coalesced: an adapter that loops `danger.*` makes an urgent task per bot per event |
| 3 | **A deterministic adapter says so; a model adapter names its model.** A reading of a position table is exact and says `exact`. A reading by a vision model names the model it was configured with, and the adapter keeps what each call cost | the fleet's vision keeps its model in `VISION_MODEL`, the imagery's source and credit on each result, and, during a search, the cost of each call on the incident; it scores no confidence per sighting, and this page claims none |
| 4 | **An adapter never decides.** It reports. A refusal (a fence, the physics) is a tool error the bot reads; the policy, the approval and the stop stay in the control plane | `mcp_http.ToolError` is answered as the tool's result with `isError: true`, as `run_mission` refuses a route outside the geofence; `act-with-approval` still asks a person before every call |
| 5 | **One adapter per signal, beside the world service**, in the world's pod. It is part of the world, not of the fleet: no node, no certificate. What it needs is the operator's side of the trust: the bearer token and the fleet CA to publish, or a trigger's secret to fire a webhook, from a Secret mounted read-only | the fleet base reads `CP_TOKEN_FILE` on each call and never logs it; an adapter that holds the token can publish `stop.all`, so it is the operator's process, with the operator's care |

## The adapters today

| Adapter | Signal in | Text out | Where |
|---|---|---|---|
| State | each vehicle's `/state`: position, battery, phase; the stations' stock; the ships AIS hears | `fleet.json` (flat: vehicles, stations, zones, findings, ships, the incident); `telemetry.json` (metres from home, limits, targets) | `fleet_base.py`, `vehicle_pilot.py` |
| Vision | PDOK's aerial photo of the frame under the vehicle (Esri out at sea), with the simulation's containers drawn in | `findings`: a one-line summary, short observations, an alert flag; during a search the containers' places in the frame as fractions, mapped to sightings | `vision.py`; unmerged work adds the simulator's own camera as a source |
| Operator's hand | a webcam and a hand tracker | `info.tempo`, `info.cue.<section>`, `stop.all` on the orchestra's bus | planned for the orchestra; not in the repository |
| GitHub | the webhooks of the repositories `--repos` names (`check_suite`, `pull_request`), signed with the hook's secret | a CI event and a merged event, their names (`--ci-event`, `--merged-event`) and their team (`--team`) the operator's, as the operator ([topics between teams](team-topics.md#where-githubs-facts-come-from)) | `adapters/githubhooks`, `axlr-bots github-hooks serve` ([runbook](../runbooks/github-hooks.md)) |
| Activity audit | the fleet's own activity: each task's end on the teams' timelines, its executions' calls and its progress items, through the socket | one event per failure signature, its name (`--event`) and its team (`--team`) the operator's, as the operator; deterministic, no model | `adapters/activityaudit`, `axlr-bots activity-audit serve` ([runbook](../runbooks/activity-audit.md)) |
| Template | a fake world in memory | `observe(bot_id)` as text, `watch()` as events | `world-adapter/` (private repo `axlr-bots-examples`) |

## The template

`world-adapter/` in the private repo `axlr-bots-examples` is the shape a new adapter starts from: Python, the standard library only, runnable as it is.

```text
world-adapter/
  adapter.py        observe(bot_id) -> str          one reading per look, paced, labelled
                    watch() -> Iterator[Event]      the world's changes as events (name, message, data, to)
                    Publisher                       POST /v1/groups/{team}/events as the operator; prints without a url
  config.json       the team, the world, rate.perMinute, the control plane's address, the label
  README.md         what the signal is, what the text looks like, which model if any, what it costs, the label
  test_adapter.py   a frame → the expected text; a change → the expected event; a flood of calls → the rate holds
```

Acceptance for a new adapter, before a world is announced:

| Check | Expected |
|---|---|
| `python3 -m unittest -v test_adapter` | the tests pass, with no network and no model |
| one bot calls the look tool | text, within the time the README states for the signal (at once from a table, a model's latency from a frame); a refusal is a tool error, not an exception |
| 300 bots call it at once | the world or the model is read at most `rate.perMinute` times a minute; every call is answered, from the last reading, with its age |
| the world changes | the event lands with the kind that fits: `warn.*` to the bot it concerns, `danger.*` or `stop.*` when every bot must stop |
| the screen | the honesty label of the README is on it |

## Where it goes in the code

Nowhere in `domain/` or `application/`, and not in the control plane's `adapters/`: the control plane has every primitive. A world's adapter that the operator runs on a host of the fleet may live in this repository as its own package and subcommand, beside the control plane and not in it, as `adapters/githubhooks` does (`axlr-bots github-hooks serve`, a separate process that publishes through the socket). The other exception is a source that already speaks a standard protocol and that opens incidents: it is a trigger type, a door of the control plane, as `alertmanager` is and `cloudwatch` is (below), not a process beside a world. A new world adds `<world>/` to the private repo `axlr-bots-examples` with its service, its adapter, a bot definition per creature and a Helm release of its own; the port fleet's files stay as they are.

## Decision, 8 November 2026: CloudWatch through SNS

[axlr-sre](https://github.com/underpass-ai/axlr-sre)'s design (§5, phase 2) takes CloudWatch alarms as an input, keyed by the alarm and `StateChangeTime`, `ALARM` opening and `OK` closing. CloudWatch publishes an alarm's state change to an SNS topic; SNS posts it to an HTTPS subscription. The control plane takes it as a trigger type of its own, `cloudwatch`, on the trigger route `POST /v1/triggers/{bot}/{trigger}`, with no adapter process in between.

| Question | Decision | Why |
|---|---|---|
| Transport | SNS HTTP/S delivery: `Notification`, `SubscriptionConfirmation`, `UnsubscribeConfirmation`, told apart by `x-amz-sns-message-type` and the body's `Type`, which must agree | what SNS does without a Lambda or a queue of ours |
| Credential | SNS's own signature of the message, mandatory, on every route (the socket included); the trigger takes no `secret` or `signingKey` | SNS sets no header of ours, so `X-Axlr-Secret` cannot be sent; a secret in the URL's path or query lands in every proxy's and load balancer's log, so it is not accepted |
| The signing certificate | fetched only from `https://sns.<region>.amazonaws.com/SimpleNotificationService-<id>.pem`, the URL rebuilt from the parts checked and never the text the body carried, refused (`403`, audited) before any fetch otherwise, no redirect, at most 64 KiB, ten seconds, cached an hour and never past its expiry; `SignatureVersion` 1 (SHA1withRSA) and 2 (SHA256withRSA), the string to sign as AWS documents it | a body names its own certificate's URL: anything but SNS's host would let a forger sign with a key of its own, or make the control plane fetch an address of its choosing |
| Subscription | audited with its `SubscribeURL`; visited only after the signature verifies, only in the shape `https://sns.<region>.amazonaws.com/?Action=ConfirmSubscription&TopicArn=<arn>&Token=<token>`, rebuilt from those parts; confirmed by the control plane only when the trigger says `"autoConfirm": true`, which needs `topics`; by default a person confirms it | the signature proves the message came from SNS, not from whose topic: any AWS account can subscribe the URL to its own topic, and confirming it would let that topic open incidents |
| Topics | optional `topics`, at most 16 SNS topic ARNs: a message whose `TopicArn` is not listed is refused, `403`, audited; absent, any topic SNS signed for is taken; `autoConfirm: true` is refused at register and update without it | the signed `TopicArn` is what names whose topic sent a message; the list makes the subscription safe to confirm by itself |
| Replays | a message whose `Timestamp` is more than 15 minutes from the control plane's clock, either way, is refused, `403`, audited; checked with the topic before the certificate is fetched | a signed message verifies for good, and SNS signs its `Timestamp`: the window bounds what a captured message is worth, and a refusal costs no fetch |
| Mapping | `ALARM` opens or repeats an incident with group key `cloudwatch:<AlarmArn>` (the alarm's name without one), fingerprint `StateChangeTime`, title `<AlarmName>: <NewStateReason>`, the trigger's `severity` (sev3), source `cloudwatch`, the triage task with the alarm's JSON as `input`; `OK` resolves it; `INSUFFICIENT_DATA` is audited; repeat, reopen within 30 minutes and a new incident later follow the incident's rules | the incident aggregate is the same for every source; only the parse is CloudWatch's |
| Severity and namespace | the trigger's `severity`; no namespace | an alarm's tags are not in its notification: reading them is a `cloudwatch:ListTagsForResource` call with an AWS credential the control plane does not hold |

## Consequences

- A world reaches the bots through three doors and no other, so the panel shows all of it: a tool call in the execution's trace, an event in the team's timeline with its mode, a note under **Knew**.
- The bot's definition keeps its fields: mission, tools, level, triggers, subscriptions, budgets, placement. A new world changes which tools a bot names and what it subscribes to, never the control plane.
- An adapter that publishes holds the operator token: it is trusted as the operator and recorded as one (`group.event.published`, `by: operator`).
- Sizing follows from the limits that exist: 300 bots that call `observe` cost the adapter at most `rate.perMinute` readings a minute, and each bot its own tool calls against `maxToolCallsPerTask`; one `warn.*` that 300 bots subscribe to costs the control plane 300 deliveries, each on the receiver's own budget.

## Not this

- Not a new field in the bot definition.
- Not a broker, not a sensor framework: a small process beside a world service.
- Not the node agent: it never runs a bot, and a bot never runs in it.

## Left out

- For `cloudwatch`: the alarm's tags (severity, namespace) through the CloudWatch API, the China partition (`amazonaws.com.cn`), and SNS's raw message delivery (which carries no signature, so it is refused).
- A credential of the adapter's own: today it holds the operator token or a trigger's secret.
- The way out: a bot's `move` or `play` is a tool the world serves, as `run_mission` is; this page is about the way in.
- A rate on the publish route per caller: the adapter paces itself.
