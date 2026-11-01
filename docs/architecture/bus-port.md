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
| `domain/` | events, topics, subscriptions, teams | brokers, subjects, any use case |
| `application/` | `NodePusher` (push to a node), `NodePush` (an execution to renew now, or all; an optional `Ack`), the rules | brokers |
| `adapters/unixapi` | the **direct** adapter: a per-node ring the use cases push into and the stream route the node holds open | |
| `adapters/bus/nats` | the **nats** adapter: publisher with an outbox, subscriber with a durable consumer; subjects, streams, credentials | the domain |
| `adapters/bus/bustest` | the port's contract, run against every adapter | |
| `adapters/nodeagent` | `Streamer`: the node's end; `Agent.Streamer` chooses it | |
| a use case (the examples in the private repo `axlr-bots-examples`, or yours) | which bots tell what, the broker's deployment and its permissions | |

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
| 6 | Only the control plane publishes; a node reads its own subject | the broker's permissions: `nodeUser` in `adapters/bus/nats/tls_test.go` lists them; the node also needs the JetStream API for its own consumer (`$JS.API.CONSUMER.*.AXLR_PUSH.node-<id>.>`) |
| 7 | One contract suite for every adapter | `adapters/bus/bustest.Run`: opens with a resync, in order, another node hears nothing, a repeat tolerated, a reopened stream resyncs; run on the direct adapter (`adapters/unixapi/bus_contract_test.go`) and on NATS with an embedded server (`adapters/bus/nats/nats_test.go`) |

## Configuration

| Side | Flags | Effect |
|---|---|---|
| control plane | `axlr-bots serve --bus nats --nats-url nats://host:4222 [--nats-creds file] [--nats-cert file --nats-key file] [--nats-ca file]` (`AXLR_BOTS_BUS`, `AXLR_BOTS_NATS_URL`, `AXLR_BOTS_NATS_CREDS`, `AXLR_BOTS_NATS_CERT`, `AXLR_BOTS_NATS_KEY`, `AXLR_BOTS_NATS_CA`) | every push also goes to the broker; the direct stream stays on, so a node may read either. The control plane defines the stream as it connects, at start |
| node | `axlr-node --bus nats --nats-url … [--nats-creds file] [--nats-cert file --nats-key file] [--nats-ca file]` (`AXLR_NODE_BUS`, `AXLR_NODE_NATS_URL`, `AXLR_NODE_NATS_CREDS`, `AXLR_NODE_NATS_CERT`, `AXLR_NODE_NATS_KEY`, `AXLR_NODE_NATS_CA`) | the node reads its pushes from the broker instead of holding the stream open; polls, leases, reports and messages still go to the control plane. It finds the stream, never defines it |

**Mutual TLS.** With a `tls://` broker and an `https://` control plane a node shows the broker its own certificate from enrollment (`CN=<node id>,O=axlr-bots nodes`) unless `--nats-cert` names another; the control plane shows one `axlr-bots ca issue-client --name control-plane --out <dir>` wrote (`CN=control-plane,O=axlr-bots clients`). A broker with `verify_and_map` checks both against the fleet CA and takes the user from the subject, so rule 6 holds per certificate: `adapters/bus/nats/tls_test.go` runs the contract that way and checks that a node cannot open another's consumer and that a certificate the broker does not list is refused. A chart in the private repo `axlr-bots-examples` is one deployment; [deploy a broker](#deploy-a-broker) lists what any deployment needs.

`--bus direct` (the default on both) is the stream over the node's own connection to the control plane, which needs no broker. The nats publisher never blocks the use cases: `Push` writes the outbox and returns; one goroutine publishes in order and waits for the broker when it is away.

## Deploy a broker

What the broker must provide, as the adapter's code sets it and as `adapters/bus/nats/tls_test.go` configures the broker it runs the contract against. Nothing here changes the control plane's record: with the broker away, the nodes renew on their own clock (10 s) and lose nothing.

| Item | Value |
|---|---|
| Broker | NATS with JetStream; the stream is file-backed, so give JetStream a volume |
| Stream | `AXLR_PUSH`, subjects `axlr.push.>`: limits retention, file storage, a push kept an hour, a republish dropped within 2 minutes (`Nats-Msg-Id`), the oldest dropped first. The control plane creates or updates it when it connects; a node only looks it up |
| A node's subject | `axlr.push.<node id>`, with `.`, `*`, `>` and spaces in the id written `_` |
| A node's consumer | `node-<node id>`, the same way: durable, filtered on the node's subject, explicit acks, the pushes from its creation on and then from where it stopped, 30 s to ack, at most 256 unacknowledged, dropped by the broker after 24 hours without the node |
| TLS | the broker's own server certificate (the clients check it against `--nats-ca`, else the system's CAs) and `verify_and_map: true` with the fleet CA (`<state>/ca/ca.crt`) as its client CA, so each client is the user its certificate's subject names |
| Users, in one account with JetStream | `CN=control-plane,O=axlr-bots clients`: publish `axlr.push.>` and `$JS.API.>`, subscribe `_INBOX.>`. Each node, `CN=<node id>,O=axlr-bots nodes`: publish `$JS.API.STREAM.INFO.AXLR_PUSH`, `$JS.API.CONSUMER.CREATE.AXLR_PUSH.node-<id>.axlr.push.<id>`, `$JS.API.CONSUMER.INFO.AXLR_PUSH.node-<id>`, `$JS.API.CONSUMER.MSG.NEXT.AXLR_PUSH.node-<id>` and `$JS.ACK.AXLR_PUSH.node-<id>.>`; subscribe `_INBOX.>`. `nodeUser` in the test writes this entry; a node the broker does not list is refused |

```bash
axlr-bots ca issue-client --name control-plane --out ~/.config/axlr-bots/nats       # client.key (0600), client.crt, ca.crt
axlr-bots serve --listen 0.0.0.0:7878 … --bus nats --nats-url tls://nats.example.internal:4222 \
  --nats-cert ~/.config/axlr-bots/nats/client.crt --nats-key ~/.config/axlr-bots/nats/client.key --nats-ca <the broker's CA>
axlr-node --control https://cp.example.internal:7878 --bus nats --nats-url tls://nats.example.internal:4222 \
  --nats-ca <the broker's CA>                                                       # shows the broker its enrollment certificate
```

`ca issue-client` refuses to overwrite a `client.key` it finds. Start the control plane first: it defines the stream, and a node that comes first waits a few seconds, logs `node: stream: … (retrying)` and renews on its clock until the stream exists. Check: the control plane's journal says `daemon: bus nats at <url> (outbox <state>/outbox/nats)` and `bus: nats: publishing to AXLR_PUSH on <url>`; each node's says `bus: nats: node <id> reads axlr.push.<id> on <url>`. Files left under `<state>/outbox/nats/` are pushes the broker has not taken yet; they go out in order when it comes back. On systemd the same flags go in the env file as `AXLR_BOTS_BUS`, `AXLR_BOTS_NATS_*` and `AXLR_NODE_BUS`, `AXLR_NODE_NATS_*` ([install](../runbooks/install.md#jev-bots-and-a-broker-on-a-host)); in a pod, the chart's `bus.*` values ([sidecar](../runbooks/sidecar.md#jev-bots-and-a-broker-in-a-pod)).
