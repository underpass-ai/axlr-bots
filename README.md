<p align="center"><img src="docs/assets/brand/axlr-bots-emblem.svg" width="420" alt="AXLR Bots"></p>

<p align="center"><strong>Deploy bots like services: give each one a job, a policy and a host, and it keeps working.</strong></p>

[![CI](https://github.com/underpass-ai/axlr-bots/actions/workflows/ci.yml/badge.svg)](https://github.com/underpass-ai/axlr-bots/actions/workflows/ci.yml) [![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

AXLR Bots is Underpass's control plane for bots that live as infrastructure: one control plane, N hosts, N bots per host. A bot is a declared resource with a mission, a model, tools, an autonomy level, triggers, budgets and a placement; it exists without running anything, and when a task arrives (by hand, from a webhook, on a timer) the control plane starts an execution on the host the bot is placed on. [AXLR](https://github.com/underpass-ai/AXLR) runs each execution: the LLM proposes, the policy decides, outside the model. The [vision note](docs/architecture/vision.md) is the design; this repository is what exists today.

## Install

User-level, no root, two systemd user services. You need Go 1.26, an `axlr-tui` built from AXLR's `main` (it has `--exec`) and a model key.

```bash
go build -o bin/ ./cmd/...
OPENROUTER_API_KEY=... bin/axlr-bots install --axlr-bin /path/to/axlr-tui
systemctl --user enable --now axlr-bots.service axlr-node.service
```

`systemctl --user status axlr-bots axlr-node` shows both active; `axlr-bots node list` shows this host Online. The [install runbook](docs/runbooks/install.md) has the TLS install for a fleet, the system-wide variant, the environment file and the uninstall.

## Your first bot

```bash
axlr-bots bot register --file examples/bot.json                                  # reads its workspace, manual trigger, any node
axlr-bots bot enable bot
axlr-bots task submit bot --objective "List the files in your workspace" --key first-1
axlr-bots task get task-…                                                        # Pending → Running on this host → Completed
axlr-bots task report task-…                                                     # the answer, kept as the task's artifact
axlr-bots bot events bot                                                         # bot.task.submitted … bot.task.completed, with the trace id
```

## How it works

```text
operator · webhooks · timers ──▶ axlr-bots  (control plane: bots · tasks · executions · nodes · timers · events · audit)
                                     ▲  register · heartbeat · poll · lease · report      every call outbound, mutual TLS
                                     │
                     axlr-node on host-1 … host-N  ──▶  axlr-tui --exec --policy  (AXLR)  ──▶  your model provider
```

- **A bot is not an execution.** The bot is the durable resource; each task runs as an execution that ends, and the bot, its tasks and its events survive every restart.
- **Nodes connect outward, with mutual TLS.** The control plane runs the fleet CA; a host joins with a one-time token and is its certificate from then on. The control plane never reaches into a host.
- **Everything is idempotent.** Submissions carry a key, webhook fires derive theirs from the delivery, timers from the slot, reports are accepted once: a retry, a redelivery or a restart creates nothing twice.
- **The policy lives outside the model.** The bot's level and tool rules go to AXLR as a policy file; a forbidden call is refused before any approval card, and `act-with-approval` stops for a person.

## Choose a path

| You want to… | Read |
|:--|:--|
| Install for one host or a fleet, keep it running after logout, uninstall | [Install](docs/runbooks/install.md) |
| Write a bot: mission, model, tools, autonomy level, triggers, budgets, placement | [Bot definition](docs/reference/bot-definition.md) |
| Start tasks from a webhook or on a timer, without duplicates | [Triggers](docs/runbooks/triggers.md) |
| Approve or deny the write a bot wants to make | [Approvals](docs/runbooks/approvals.md) |
| Add a host: enrollment token, mutual TLS, revocation | [Add a host](docs/runbooks/add-a-host.md) |
| Call the control plane over HTTP | [Operator API](docs/reference/operator-api.md) |
| See what a node and the control plane say to each other | [Node protocol](docs/reference/node-protocol.md) |
| See how a node runs AXLR and reads the result | [Runtime contract](docs/architecture/runtime-contract.md) |
| Look up a command, read a report | [Command reference](docs/reference/cli.md) |
| Recover after a crash, a silent node or a lost control plane | [Recovery](docs/runbooks/recovery.md) |
| Understand who keeps, decides and trusts what; what is next and left out | [Control plane, node and AXLR](docs/architecture/control-plane-node-axlr.md) |
| Read the live run on a real host and the demo script | [Live run](docs/live-run.md) |

[Documentation home](docs/index.md) lists every guide.

## What runs where

| Component | Runs | Does |
|---|---|---|
| `axlr-bots` (control plane) | one host, `systemctl --user` | keeps the bots, tasks, executions, nodes and timers; serves the CLI on a Unix socket, the nodes on a TLS address, operators and webhooks on another; fires the timers; reconciles what a crash left running |
| `axlr-node` (node agent) | every host that runs bots | enrolls with a token, registers, heartbeats, polls for the executions of the bots placed on its host, runs them, reports |
| AXLR (`axlr-tui --exec`) | on the node, one process per execution | the model loop, the tools, the policy and the approvals; writes the result object the node reports |
| your model provider | wherever it is | answers the model calls with the key in that host's environment file |

## Project status

Pre-1.0, one Go module, Linux with systemd. What works today, exercised live on one host with the real AXLR: user-level install, bots with the four autonomy levels and tool rules, manual, webhook and timer triggers with deduplication, the approval path, node enrollment with one-time tokens and mutual TLS, revocation, placement by node or label, the operator API behind a bearer token, correlation ids on every task, execution and event, recovery of the tasks a crash left running, and budgets passed to a runtime that takes them.

Next: the node agent as a sidecar beside a service in a pod, budgets enforced by the runtime recorded on every execution, and a durable store in place of one JSON file per aggregate. [Control plane, node and AXLR](docs/architecture/control-plane-node-axlr.md#what-is-left-out) says what is left out.

## License

Apache License 2.0; see [LICENSE](LICENSE) and [NOTICE](NOTICE). Part of [Underpass AI](https://underpassai.com).
