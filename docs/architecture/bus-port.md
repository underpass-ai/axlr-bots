# The bus port

How the control plane's pushes reach the nodes, and so how one bot's event reaches another bot's running task fast. A port in the application layer, two adapters, and nothing of either in the domain or the use cases.

```
bot A ──emit──▶ control plane ──record──▶ events.jsonl
                 (rules: permissions,              │
                  intake, coalescing,        tappedEvents
                  maxDepth, lineage)               │
                                             NodePusher.Push(node, push)
                                     ┌─────────────┴─────────────┐
                              direct (always)               nats (optional)
                              per-node ring in the hub      outbox file → JetStream subject
                              GET /v1/nodes/{id}/stream     durable consumer per node
                                     └─────────────┬─────────────┘
                                             nodeagent.Streamer
                                                   │ poke → renew lease now → relay messages
                                                node ──▶ bot B's running task
```

## What goes where

| Layer | Holds | Knows nothing of |
|---|---|---|
| `domain/` | events, topics, subscriptions, teams | brokers, subjects, the orchestra |
| `application/` | `NodePusher` (push to a node), `NodePush` (an execution to renew now, or all; an optional `Ack`), the rules | brokers |
| `adapters/unixapi` | the **direct** adapter: a per-node ring the use cases push into and the stream route the node holds open | |
| `adapters/bus/nats` | the **nats** adapter: publisher with an outbox, subscriber with a durable consumer; subjects, streams, credentials | the domain |
| `adapters/bus/bustest` | the port's contract, run against every adapter | |
| `adapters/nodeagent` | `Streamer`: the node's end; `Agent.Streamer` chooses it | |
| `examples/orchestra` | the use case: which bots tell what, the broker's deployment and its permissions | |

Another broker (Kafka, Redis Streams, MQTT) is another package under `adapters/bus/` that passes `bustest.Run`; the core does not change.

## A push

A push is a hint, not the message: *execution X of yours needs you now* (it was cancelled or preempted, it expired, a message reached it), or *resync* (renew and read everything you run). The node then renews the lease and reads the execution's messages from the control plane, which is the source of truth. A lost push costs nothing but the time to the next renewal (10 s); a repeated push costs one renewal more. That is what makes at-least-once enough.

## The seven points

| | Point | How |
|---|---|---|
| 1 | The control plane keeps the record; the broker carries deliveries; an outbox between them | the event is recorded first (`events.jsonl`), then pushed; the nats publisher writes `<state>/outbox/nats/<node>/<time>-<seq>.json` before it tries the broker and removes it on the broker's acknowledgement; a restart republishes what is left |
| 2 | At-least-once, deduplicated by the ids we have | the outbox file's name is the publish id (`Nats-Msg-Id`, a 2-minute window); the node tolerates a repeat (one more renewal); events, tasks and messages keep their own ids |
| 3 | Order per bot | one subject and one durable consumer per node; a node runs one execution, so per node is per bot |
| 4 | Bot → bot goes through the control plane | unchanged: `emit` → `EmitEvent` (permissions, intake per minute, coalescing, `events.maxDepth`, lineage) → a task or a message → the push |
| 5 | A node that was away gets what it missed | the durable consumer resumes where it stopped (the broker keeps an hour); every stream opens with a resync, so even without the broker the node renews and reads everything it runs |
| 6 | Only the control plane publishes; a node reads its own subject | the broker's permissions: see `examples/orchestra/nats.conf`; the node also needs the JetStream API for its own consumer (`$JS.API.CONSUMER.*.AXLR_PUSH.node-<id>.>`) |
| 7 | One contract suite for every adapter | `adapters/bus/bustest.Run`: opens with a resync, in order, another node hears nothing, a repeat tolerated, a reopened stream resyncs; run on the direct adapter (`adapters/unixapi/bus_contract_test.go`) and on NATS with an embedded server (`adapters/bus/nats/nats_test.go`) |

## Configuration

| Side | Flags | Effect |
|---|---|---|
| control plane | `axlr-bots serve --bus nats --nats-url nats://host:4222 [--nats-creds file]` (`AXLR_BOTS_BUS`, `AXLR_BOTS_NATS_URL`, `AXLR_BOTS_NATS_CREDS`) | every push also goes to the broker; the direct stream stays on, so a node may read either |
| node | `axlr-node --bus nats --nats-url … [--nats-creds file]` (`AXLR_NODE_BUS`, `AXLR_NODE_NATS_URL`, `AXLR_NODE_NATS_CREDS`) | the node reads its pushes from the broker instead of holding the stream open; polls, leases, reports and messages still go to the control plane |

`--bus direct` (the default on both) is the stream over the node's own connection to the control plane, which needs no broker. The nats publisher never blocks the use cases: `Push` writes the outbox and returns; one goroutine publishes in order and waits for the broker when it is away.
