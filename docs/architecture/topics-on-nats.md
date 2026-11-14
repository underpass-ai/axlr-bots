# ADR: a team's topics on NATS, beside the delivery

Status: accepted, 14 November 2026. The product owner settled each question below in turn, the same day.

## Context

The control plane is the bus of a team's topics ([publish/subscribe](pubsub-events.md)): a bot emits, the control plane records the event and makes the tasks, messages and notes of the teammates that hear it. Nothing leaves the control plane but its API.

An application that links its teams, or tells the world what a team said, reads that API today: it polls a team's timeline every second, keeps a cursor file per route, and publishes back as the operator. It works, but it is a poll, and it is a second way in that no test of the broker covers.

A NATS broker is already beside the control plane for the [pushes to the nodes](bus-port.md), with an outbox, mutual TLS and a contract suite.

## Decisions

| # | Question | Answer |
|---|---|---|
| 1 | What goes over NATS | **A team's topics** (not, for now, the pushes to the nodes) |
| 2 | Where NATS enters | **Beside the delivery.** The control plane delivers inside the team as today, and then publishes a copy of the event on NATS. It also reads a subject where an application publishes on a team as the operator. Not in the delivery path (emit stays synchronous and its answer still lists who heard it), and no crossing between teams in the platform |
| 3 | Which events | **All of a team's events, each with its author**: what a bot emits, what the operator publishes, and the control plane's own (`done.task`, `error.bot`, `warn.approval`). The application filters |
| 4 | The application's bridge | The application that asked links its teams through NATS only. The API stays for the panel, the CLI and any other application |

## The two subjects

```text
bot ─emit─▶ control plane ─record─▶ deliveries in the team (as today)
                 │
               outbox                       ┌──────────── an application ────────────┐
                 ▼                          │                                         │
   axlr.topic.<tenant>.<team>.<name>   ──▶  │ durable consumer of its own             │
   stream AXLR_TOPIC                        │                                         │
                                            │ publishes                               │
   axlr.publish.<tenant>.<team>.<name> ◀──  │                                         │
   stream AXLR_PUBLISH                      └─────────────────────────────────────────┘
                 │
   control plane's durable consumer ─▶ publish on the team as the operator (same rules as POST /v1/groups/{name}/events)
```

| | Out: a team's topic | In: a publication on a team |
|---|---|---|
| Subject | `axlr.topic.<tenant>.<team>.<name>` | `axlr.publish.<tenant>.<team>.<name>` |
| Stream | `AXLR_TOPIC`, defined by the control plane | `AXLR_PUBLISH`, defined by the control plane |
| Who publishes | only the control plane | an application |
| Who reads | an application, with a durable consumer of its own | only the control plane (consumer `control-plane`) |
| `Nats-Msg-Id` | `<event id>@<team>`: JetStream drops a repeated id across the whole stream, so an event told to two teams needs one id per team | the publication's idempotency key (1 to 128 characters), required, unique across all the application's teams (the window covers the whole stream) |
| Retention | 24 hours, 1 GB at most | 24 hours |

An inbound publication carries no team in its envelope: the control plane matches the subject's tokens against the existing teams, and no match or two matches is a refusal, as is a `name` other than the subject's.

The tenant and the team are one token each: `.`, `*`, `>` and spaces are written `_`. Two names that differ only there share a subject, so a reader checks the envelope's `team` and `tenant`, which are exact. The event's name keeps its dots as tokens, so a NATS filter `axlr.topic.t.team-a.order.>` is the platform's `order.**`.

These names are new on purpose: a broker that kept the streams of the earlier cut (`AXLR_DOMAIN`) must not replay them.

## The envelope

The schemas will be in the spec `docs/reference/topics.asyncapi.json` (PR 2 below); the DTOs are generated from it and mapped in the adapter, and the domain sees no JSON.

**Out**, one message per team the event was told to:

| Field | Value |
|---|---|
| `id` | the event's id, as the team's timeline shows it |
| `tenant`, `team` | exact |
| `name`, `severity`, `message`, `data` | as recorded |
| `publisher` | `bot`, `operator` or `control-plane` |
| `bot` | the bot that emitted it, or the one the control plane spoke of; absent for the operator |
| `to` | the addressed teammates, if any |
| `traceId` | the emitter's trace, when there is one |
| `at` | when it was recorded |

**In**:

| Field | Value |
|---|---|
| `name`, `message`, `data`, `to` | as `POST /v1/groups/{name}/events` takes them |
| the idempotency key | the `Nats-Msg-Id` header |

## Rules

| | Rule |
|---|---|
| Source of truth | the control plane. The event is recorded first, then written to the outbox, then published; a restart republishes what the outbox kept |
| At least once | a repeat inside the broker's window (2 minutes) is dropped by `Nats-Msg-Id`; past it, a reader dedupes by `id` (out) and the control plane by the key (in) |
| Order | per subject, the stream's order |
| An inbound publication refused | (`done.*` and `error.*` are the control plane's, an unknown team, a bad envelope) is taken off (`Term`), logged and audited as `event.publish` `denied`. It is never redelivered |
| An inbound publication that failed | (the store, a timeout) is redelivered after a growing delay |
| Loops | a publication by the operator starts a new chain, so nothing in the platform cuts a loop that goes out and comes back through an application. The application must make none, and test it |
| Off | without `--topics nats`, nothing is published or read, and teams behave as before |

## Configuration

| Side | Flags |
|---|---|
| control plane | `axlr-bots serve --topics nats --nats-url … [--nats-creds file] [--nats-cert file --nats-key file] [--nats-ca file]` (`AXLR_BOTS_TOPICS`). The broker flags are the ones of `--bus nats`; the two are independent |
| broker, control plane's user | publish `axlr.topic.>` and `$JS.API.>`, ack `$JS.ACK.AXLR_PUBLISH.>` |
| broker, an application's user | publish `axlr.publish.>`; its own consumer on `AXLR_TOPIC`: `$JS.API.CONSUMER.*.AXLR_TOPIC.<its name>`, `$JS.API.CONSUMER.MSG.NEXT.AXLR_TOPIC.<its name>` (two tokens, not covered by `*`), `$JS.ACK.AXLR_TOPIC.<its name>.>`, and with a single filter `$JS.API.CONSUMER.CREATE.AXLR_TOPIC.<its name>.<filter>`. The full list is in [bus port](bus-port.md) |

## Order of work

| # | PR | Acceptance |
|---|---|---|
| 1 | this ADR | links resolve; `pubsub-events.md` and `bus-port.md` point here |
| 2 | the spec, its generator and the DTOs | `go generate` leaves no diff; a drift test |
| 3 | out: the control plane's tap and the NATS publisher | with an embedded broker: a bot's event, the operator's and a `done.task` each reach the subject once, with their author; a restart republishes the outbox; nothing without `--topics nats` |
| 4 | in: the control plane's consumer | a publication makes the deliveries `POST` makes; a repeat makes none; a refused one is taken off and audited; a failing store redelivers |
| 5 | the application, in its own repository | its bridge reads and writes NATS only; its integration test runs on a broker |

## Left out

- The pushes to the nodes over this broker stay as the [bus port](bus-port.md) has them.
- Topics across tenants.
- A replay beyond the stream's 24 hours.
