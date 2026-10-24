# Architecture: control plane, node and AXLR

The control plane decides *when, where, as whom and under which permissions* a unit of work runs; AXLR decides *how* the agent reasons and uses tools; the node is the hand that runs AXLR on the host the bot is placed on. A bot is the durable resource; an execution is one run of AXLR for one task, on one host, and it ends.

```text
                       control plane (axlr-bots serve)                 operator / scripts
                       ┌─────────────────────────────────────────┐          axlr-bots task submit …
                       │ bots · tasks · executions · policies    │◀─socket──  bot register · task approve
                       │ nodes · timers · events.jsonl · audit   │◀─TLS+token─  POST /v1/bots/{id}/tasks …
                       │ artifacts/<task>/report.md              │◀─TLS+secret─ POST /v1/triggers/{bot}/{hook}
                       └──────────▲───────────────▲──────────────┘
          register · heartbeat    │               │    register · heartbeat
          next assignment (poll)  │               │    next assignment (poll)
          lease · report          │               │    lease · report
                 ┌────────────────┴───┐       ┌───┴────────────────┐
                 │ host A             │       │ host B             │     … host N
                 │ axlr-node          │       │ axlr-node          │
                 │ axlr-tui --exec    │       │ axlr-tui --exec    │
                 │ bots placed here   │       │ bots placed here   │
                 └────────────────────┘       └────────────────────┘
```

## Responsibilities

| | Control plane (`axlr-bots serve`) | Node (`axlr-node`) | AXLR (`axlr-tui --exec`) |
|---|---|---|---|
| Keeps | bots, tasks, executions, nodes, timers, enrollments, events, audit, reports | its credentials, the traces, policy files and workspaces of what it ran | the session: transcript, tool calls, decisions, usage |
| Decides | which task runs, where, as which bot, with which policy and budget; whether a fire or a report is new | nothing: it runs what it is assigned and reports | each model call and tool call, within the policy; when to stop for a person |
| Talks to | the CLI (socket), operators and webhooks (TLS, bearer token or the trigger's secret), the nodes (TLS, client certificates) | the control plane only, outbound | the model provider, with the key in the host's environment |
| Trusts | the socket's owner; a certificate the fleet CA signed, for that node id only; the bearer token; a trigger's own credential | the CA the token carried | the policy file and the approval level it was given |

## The daemon

`axlr-bots serve` is the control plane as a service (`Type=simple`, logs to the journal).

- It serves the API on the Unix socket `<state>/axlr-bots.sock` (mode 0660), the node protocol on a TLS address when `--listen` names one, and the operator API on another TLS address when `--api-listen` names one ([operator API](../reference/operator-api.md#listeners)). Without `ca init` it refuses to listen.
- A Pending task of a bot an **Online node** may run waits for that node's next poll. A Pending task no Online node may run is run **in process** on the next tick (`--tick 2s`) with the control plane's own `axlr-tui`: the **single-process mode**, also what `task run` does from the CLI. Both go through one `SerialRunner`, so a task is never run twice or in two places.
- On every tick it runs the scheduler: each timer trigger whose slot has come fires once ([triggers](../runbooks/triggers.md)).
- On start and every minute (`--reconcile-every`) it marks Offline the nodes silent for over a minute, fails the executions whose node stopped renewing its lease, and fails the in-process runs a dead daemon left Running past the grace (the bot's timeout plus a minute).
- It binds the TLS listeners before it serves and waits for every server on the way out, so an early failure never removes the socket of the daemon started next.

## The trust model

- Every connection between a node and the control plane goes out from the node, over TLS 1.3 with a client certificate the fleet CA issued against a one-time token; the control plane never opens a connection to a host ([node protocol](../reference/node-protocol.md#identity)).
- A node's private key never leaves its host; the CA key, the token secrets (stored hashed) and the operator bearer token never leave the control plane; the model key is each host's own, in its environment file.
- Operators reach the control plane over the socket (the host's own permissions) or the operator address with a bearer token; a node certificate opens no operator route and the bearer token opens no node route ([operator API](../reference/operator-api.md#listeners)).
- The policy is applied outside the model: the bot's level and tool rules go to AXLR as a file, a forbidden call is refused before any card, and `act-with-approval` stops for a person ([runtime contract](runtime-contract.md#the-policy)).
- Everything that can arrive twice is idempotent: submissions by key, webhook fires by delivery, timer fires by slot, reports once per execution, enrollment once per token.

## What is next

- **The node agent as a sidecar** (vision §9.2): the same `axlr-node` inside the pod, beside the service, as a container from the same image. The state directory becomes a volume, the environment (`AXLR_NODE_CONTROL`, `AXLR_TUI`, the model key) comes from the pod spec, and systemd disappears; the service's files are mounted read-only and the bots placed on that node (by a `selector` on its labels) read them with AXLR's own tools. The control plane stays where it is, one per cluster or per fleet, and the webhook trigger is the trigger type that fits there.
- **Budgets enforced by the runtime**: the control plane already passes `--budget` and records `budget_exceeded`; the budget record on the execution (what was passed) is still to be stored.
- **A durable store**: a real database behind the repository ports in place of one JSON file per aggregate.

## What is left out

A systemd unit event as a trigger type; an approval queue beyond `task approve` (who may decide, and when; an expired approval); operator client certificates on the API address (the bearer token is the one credential); a `GET /v1/tasks` listing; `axlr-serve` as a second `RuntimeExecutor`; automatic certificate renewal, CRL/OCSP and a CA key outside the state directory ([node protocol](../reference/node-protocol.md#limits-today)). The vision's §13 and §14 name the rest for Phase 2.
