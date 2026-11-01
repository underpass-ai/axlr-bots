# AXLR Bots documentation

AXLR Bots runs persistent, governed bots on your own hosts: one control plane, N hosts, N bots per host, every execution run by [AXLR](https://github.com/underpass-ai/AXLR). Start with the repository [README](../README.md); these pages describe what exists today, and the [vision note](architecture/vision.md) (Spanish, 21 October 2026) is the design they implement.

## Start and use

| Task | Guide |
|:--|:--|
| Write a bot: mission, model, tools, autonomy level, triggers, budgets, placement | [Bot definition](reference/bot-definition.md) |
| Look up a command, its defaults and its environment | [Command reference](reference/cli.md) |
| Run a bot with no language model: a fixed program over one MCP server whose every choice Jev makes | [Runtime](reference/bot-definition.md#runtime) |
| Watch the fleet, send tasks and approve calls from a desktop window | [Control panel](../panel/README.md) |
| Call the control plane over HTTP: listeners, routes, idempotency key, errors | [Operator API](reference/operator-api.md) |
| Know what a node and the control plane say to each other, and how a node is identified | [Node protocol](reference/node-protocol.md) |

## Operational runbooks

| Outcome | Runbook |
|:--|:--|
| Install on one host, or a control plane with TLS for a fleet; keep it running; uninstall | [Install](runbooks/install.md) |
| Join a second host with a one-time token; rotate or revoke its certificate | [Add a host](runbooks/add-a-host.md) |
| Run the node agent as a sidecar in a Kubernetes pod with Helm: the image, the tokens, the demo, your own chart, every value | [Sidecar](runbooks/sidecar.md) |
| Run Jev bots, or read the pushes from a NATS broker, on a host or in a pod | [Install](runbooks/install.md#jev-bots-and-a-broker-on-a-host), [Sidecar](runbooks/sidecar.md#jev-bots-and-a-broker-in-a-pod) |
| Start tasks from a webhook or on a timer and verify nothing fires twice; turn alert groups into incidents a bot triages | [Triggers](runbooks/triggers.md), [Incidents](runbooks/triggers.md#incidents) |
| Approve or deny the write a bot wants to make, be notified of it, let it expire | [Approvals](runbooks/approvals.md) |
| Let a bot ask another bot to do a task, within depth and budget limits that stop loops | [Delegation](reference/bot-definition.md#delegation), [ADR](architecture/delegation.md) |
| Put bots in a team so they know each other and ask each other without naming targets | [Teams](reference/bot-definition.md#teams), [ADR](architecture/groups-and-events.md) |
| Let a bot warn or stop its teammates with an event, and follow the team's timeline | [Events](reference/bot-definition.md#events), [ADR](architecture/groups-and-events.md) |
| Let bots publish on topics and choose who listens, in what mode, from the panel | [ADR](architecture/pubsub-events.md) |
| Set up teams and events from the panel, step by step: the urgent order, a message to a running task, the shortcuts | [Teams and events from the panel](runbooks/teams-and-events.md) |
| Recover after a crash, a silent node or a lost control plane | [Recovery](runbooks/recovery.md) |

## Architecture

| Question | Page |
|:--|:--|
| What the control plane, the node and AXLR each keep, decide and trust; what is next and what is left out | [Control plane, node and AXLR](architecture/control-plane-node-axlr.md) |
| The packages, the state directory, how to build and test | [Layout](architecture/layout.md) |
| How a node runs AXLR, the policy it passes, how it reads the result; the Jev runtime | [Runtime contract](architecture/runtime-contract.md) |
| How a push reaches a node: its own stream, or a NATS broker, and what a broker needs | [Bus port](architecture/bus-port.md) |
| How a world (a simulation, a camera, a sensor, an operator's hand) reaches the bots as text, through the tools, the bus and the notes that exist | [Input adapters](architecture/input-adapters.md) |
| The product and architecture vision this implements | [Vision](architecture/vision.md) |

## Documentation status and evidence

The guides were reconciled with `main` at `91dc705` on 24 October 2026 and checked again at `8a8a38f` on 1 November 2026; the [documentation catalog](documentation-catalog.md) lists every page with its purpose and the revision it was checked at. The [live run](live-run.md) records the demo script's output on a real host, the sidecar on a real cluster and the three live tests. [Brand](brand.md) documents the marks.
