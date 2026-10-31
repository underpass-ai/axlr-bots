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
| Keeps | bots, tasks, executions, nodes, timers, enrollments, events, audit, reports | its credentials, the traces, policy files and workspaces of what it ran (on the host, or on its pod's volume) | the session: transcript, tool calls, decisions, usage |
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

## Delegation between bots

A bot whose definition names other bots in `delegation.to` may ask one of them to do a task while it runs ([ADR](delegation.md)). The node serves the execution the tool `axlr_bots_delegate` over a local socket, AXLR reaches it through `axlr-node mcp-relay`, and the ask goes to the control plane over the node's mutual-TLS channel. The control plane alone decides: it takes the asking bot, its task, its depth, its chain and its budget from the execution the node holds, refuses a target the bot may not ask or that takes no delegated task, a chain deeper than its limit, a bot already on the chain and a share too small to run, and otherwise creates the child under the parent's trace id with its grant. A waiting parent lends its node's slot, so the child can run on the same node.

```text
 bot-a's execution ──axlr_bots_delegate──▶ node ──mTLS──▶ control plane: checks, creates bot-b's task (depth 1, grant)
        ▲                                                         │
        └────────── the child's answer ◀── node polls ◀───────────┘   bot-b's execution runs on a node, reports
```

## The node as a sidecar

The same `axlr-node`, from the node image ([`distribution/node.Dockerfile`](../../distribution/node.Dockerfile): `axlr-node` and AXLR's `axlr-tui`, distroless, user 65532), runs as a container in a service's pod, beside the service, packaged by the Helm chart [`charts/axlr-bots-node`](../../charts/axlr-bots-node) ([sidecar runbook](../runbooks/sidecar.md)). Systemd disappears; nothing else changes on the control plane, which stays one per fleet, on a host or wherever it is reachable, and sees the pod as one more node.

| | On a host (systemd) | In a pod (sidecar) |
|---|---|---|
| Identity | a token minted for the host's node id, `axlr-node enroll` once | a token per replica in a Secret; the first start enrolls (`--token-file`), refusing a token for another node id before sending it; later starts keep the certificate |
| Node id | the token's | `<idPrefix><pod name>`: a StatefulSet's ordinal, stable across restarts |
| State | `~/.local/state/axlr-node` | a PVC per replica (`volumeClaimTemplates`); AXLR's own state on a tmpfs |
| Labels | the token's and `AXLR_NODE_LABELS` | the token's, plus `namespace`, `app` and `pod` from the Downward API |
| Which bots | those placed on it, and those placed on any node | those placed on it by name or selector only (`dedicated`) |
| What the bots read | the host's files under the bot's workspace | the service's volume, mounted read-only as the bot's workspace (`sidecar.workspaces`) |
| Network | outbound to the control plane | the same, under a NetworkPolicy: egress to DNS, the control plane and the model provider only |
| Model key | the host's env file | a Secret only the sidecar reads |

The chart renders a demo (a StatefulSet whose service writes a log the sidecar reads) or, as a dependency, the templates another chart includes in its own pod. A sidecar waits rather than exits when it cannot enroll yet, so a missing or wrong token never takes the service's pod out of its endpoints. The webhook trigger is the one that fits there: the service, or whatever watches it, calls the control plane.

## What is next

- **One execution, one sandbox** (vision §9.2): an execution runs as an `axlr-tui` process inside the node it is placed on, sharing that node's user, filesystem and limits (on a host, the node's user; in a pod, the sidecar container). A Job per execution, with its own identity, filesystem and limits, is still to come.
- **Budgets enforced by the runtime**: the control plane passes `--budget` (on a node too, since delegation: the requests and tool calls, not only the seconds) and records `budget_exceeded`; the budget record on the execution (what was passed) is still to be stored.
- **A durable store**: a real database behind the repository ports in place of one JSON file per aggregate.

## What is left out

A systemd unit event as a trigger type; an incident's own lock (the open path holds the submissions lock and the task's end runs under the serial runner, and the file store has no optimistic locking, vision §8.4), the Alertmanager adapter over `POST /v1/incidents` and the states between a diagnosis and a resolution (the [AXLR Bots SRE design](https://github.com/underpass-ai/axlr-sre/blob/main/docs/design.md)); Kubernetes resources for bots (`Bot`, `BotExecution`: no CRDs, vision §9.3); an approval queue beyond `task approve` (who may decide, and when; an expired approval); operator client certificates on the API address (the bearer token is the one credential); a `GET /v1/tasks` listing; `axlr-serve` as a second `RuntimeExecutor`; delegation from the control plane's in-process runs and a cancel that reaches a task's children ([delegation](delegation.md#left-out)); automatic certificate renewal, CRL/OCSP and a CA key outside the state directory ([node protocol](../reference/node-protocol.md#limits-today)). The vision's §13 and §14 name the rest for Phase 2.
