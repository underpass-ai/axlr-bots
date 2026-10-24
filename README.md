# axlr-bots

**AXLR Bots**: persistent, governed bots that run as infrastructure. It
implements the Underpass Bots vision (`underpass-bots-vision-arquitectura.md`)
on top of AXLR. The control plane decides *when, where, as whom and under which
permissions* a unit of work runs; **AXLR** decides *how* an agent reasons
and uses tools. A bot has no role baked in here: its definition
(mission, model, tools, autonomy level, triggers, budgets, placement) is
what an operator writes, and the same install serves every definition.

Two commands come out of this module:

| Command | Role | Where it runs |
|---|---|---|
| `axlr-bots` | the control plane: the registry of bots, tasks, executions, policies and nodes, its API, and the operator's CLI | one host (the daemon `serve`); the CLI anywhere the socket is |
| `axlr-node` | the node agent: registers with the control plane, polls for the executions of the bots placed on its host and runs them with AXLR there | every host that runs bots (N hosts, N bots each) |

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
                 │ axlr-node     │       │ axlr-node     │
                 │ axlr-tui --exec    │       │ axlr-tui --exec    │
                 │ bots placed here   │       │ bots placed here   │
                 └────────────────────┘       └────────────────────┘
```

Every arrow between a node and the control plane goes **out from the
node**: the control plane never reaches into a host. On the control
plane's host both run under `systemctl --user` and may talk over a Unix
socket; a node on any host (this one included, as installed here) talks
to the control plane's TLS address with **mutual TLS**: the control plane
runs the fleet's CA, each node holds a client certificate whose common
name is its node id, and a new host joins with a one-time token (see
[Adding a host](#adding-a-host)). The messages are the same on both
paths.

## Layout (hexagonal)

| Package | Role |
|---|---|
| `domain` | Bot, Task, Execution, Node, Enrollment and Timer aggregates; value objects validated in constructors (autonomy level, tool rules, triggers with their schedules and credentials, placement, leases); a node's certificate serial and its `Revoked` status; events (vision §11.2) under a schema version with the correlation ids; audit entries. One file per type. |
| `application` | Ports (`BotRepository`, `TaskRepository`, `ExecutionRepository`, `TimerRepository`, `NodeRepository`, `EnrollmentRepository`, `Certifier`, `CertificateDenylist`, `EventPublisher`, `EventReader`, `AuditSink`, `RuntimeExecutor`, `DecisionExecutor`, `TraceLocator`, `ArtifactStore`, `NodeTransport`) and use cases (`RegisterBot`, `EnableBot`, `UpdateBot`, `SubmitTask`, `FireTrigger`, `TickTimers`, `RunTask`, `ApproveTask`, `CancelTask`, `ListExecutions`, `ListEvents`, `RegisterNode`, `HeartbeatNode`, `NextAssignment`, `RenewLease`, `ReportExecution`, `CreateEnrollmentToken`, `EnrollNode`, `AuthenticateNode`, `RevokeNode`, `MarkOfflineNodes`, `ReconcileTasks`, `RunNextPending`, `GetReport`, …); `SerialRunner` serialises runs, assignments, reports and revocations and interrupts the run a cancel names. |
| `nodeproto` | The messages of the node protocol, shared by the server and the node's client; the enrollment token's encoding. |
| `adapters/pki` | The fleet CA (ECDSA P-256; the key under `<state>/ca`, mode 0600), the node certificates it signs from a node's request (common name = node id, a year), the control plane's own certificate (issued at start for the host's names), the node's key and request, its stored credentials, and the TLS 1.3 configurations of both ends. |
| `adapters/filestore` | One JSON file per aggregate under the state directory, written atomically (temp file, fsync, rename); the timers (the next fire of every timer trigger); the enrollments (the hash of each token, never the token); the denylist of revoked certificate serials; the report artifacts. |
| `adapters/jsonl` | Append-only `events.jsonl` (read back per bot for `GET /v1/bots/{id}/events`) and `audit.jsonl`. |
| `adapters/axlr` | Runs `axlr-tui --exec --json [--policy <file>] [--correlation <file> --budget <file>]` as a subprocess and reads the execution result object; keeps the raw lines as the trace and the policy and correlation files as evidence. Used by the node, and by the control plane in single-process mode. |
| `adapters/unixapi` | The API: a `Service` interface with `Local` (in process) and `Client` (HTTP) implementations; the `Server` serves everything on the Unix socket, the node protocol alone on the node TLS address (the node's identity is its client certificate) and the operator routes alone on the operator TLS address (a bearer token; webhooks their own secret); the `Client` is also the node's `NodeTransport`. |
| `adapters/daemon` | The control plane as a service: the API on the socket and the two TLS addresses, in-process runs for bots no node may run, the timer triggers on every tick, reconciliation and offline nodes on a timer. |
| `adapters/nodeagent` | The node agent's loop: register, heartbeat, poll, run, renew, report. |
| `adapters/systemd` | Renders and installs the two units and the environment file (or the node's alone, for a host that joins a control plane elsewhere); removes them. |
| `adapters/system` | Wall clock, random ids. |
| `compose` | The composition root the commands share. |
| `cmd/axlr-bots`, `cmd/axlr-node` | The two commands. |
| `internal/fakeaxlr` | A shell stand-in for `axlr-tui` the tests run. |

## Install (user-level, the default)

The platform installs without root under the user's own directories and
runs under `systemctl --user`. The control plane command installs itself
and the node agent found beside it.

```bash
GOFLAGS=-p=2 go build -o bin/axlr-bots ./cmd/axlr-bots
GOFLAGS=-p=2 go build -o bin/axlr-node ./cmd/axlr-node
export OPENROUTER_API_KEY=...                           # copied into the env file, mode 0600, never printed
bin/axlr-bots install --axlr-bin /path/to/axlr-tui \
    --listen 127.0.0.1:7878 --control https://127.0.0.1:7878 \  # the node TLS address; 0.0.0.0:7878 for other hosts
    --api-listen 127.0.0.1:7879                              # the operator API over TLS (optional)
axlr-bots ca init                                        # the fleet CA, once; <state>/ca/ca.key is 0600
systemctl --user enable --now axlr-bots.service
token=$(axlr-bots node enroll-token --node-id "$(hostname -s | tr 'A-Z' 'a-z')" | sed -n 's/.*"token": "\([^"]*\)".*/\1/p')
axlr-node enroll --control https://127.0.0.1:7878 --token "$token" --state ~/.local/state/axlr-node   # this host's certificate
systemctl --user enable --now axlr-node.service
loginctl enable-linger "$USER"                           # keeps the services running after you log out
axlr-bots node list                                      # the host, Online, with its certificate's serial
axlr-bots bot register --file examples/bot.json
axlr-bots bot enable bot
```

Without `--listen` and `--control` the control plane serves the socket
only and the node unit talks over it (`AXLR_NODE_CONTROL=unix://…`): no
CA, no enrollment, the host name as the node id. That is the one-host
install; the TLS install above is what this machine runs, and the only
one a second host can join.

| File | What |
|---|---|
| `~/.local/bin/axlr-bots`, `~/.local/bin/axlr-node` | the binaries (`install` copies the ones that run) |
| `~/.config/systemd/user/axlr-bots.service` | `Type=simple`, `ExecStart=… serve [--listen <addr>] [--server-names …] [--api-listen <addr>]`, `Restart=on-failure`, `NoNewPrivileges=yes`, `EnvironmentFile=-~/.config/axlr-bots/env`, `WantedBy=default.target` |
| `~/.config/systemd/user/axlr-node.service` | `After=`/`Wants=axlr-bots.service` (or `network-online.target` with `--node-only`), `Environment=AXLR_NODE_CONTROL=<--control, or unix://…/axlr-bots.sock>`, `ExecStart=axlr-node --state ~/.local/state/axlr-node`, the host name as the `host=` label; over `https://` the node id is the certificate's |
| `~/.config/axlr-bots/env` | `OPENROUTER_API_KEY=` (or a commented placeholder when the installer had none) and `AXLR_TUI=`; an existing file is never overwritten |
| `~/.local/state/axlr-bots/` | the control plane's state: bots, tasks, executions, timers, nodes, enrollments, policies, correlation files, artifacts, logs, the socket, `api.token` (the operator bearer token, 0600, written at the first start with `--api-listen`), and `ca/` (the fleet CA: `ca.key` 0600, `ca.crt`) and `pki/revoked.json` (the denylist) |
| `~/.local/state/axlr-node/` | the node's state: the traces, policy files and workspaces of what it ran, and `pki/` (`node.key` 0600, `node.crt`, `ca.crt`, `enrollment.json`) once enrolled |

A user service cannot sandbox the filesystem (`ProtectSystem=` and
`ProtectHome=` need privileges a user manager lacks); the unit says so.
`--destdir <dir>` renders everything under a directory (packaging, tests),
`--no-systemctl` writes the files and runs nothing.

```bash
axlr-bots uninstall           # disable --now both units, remove them and the binaries
axlr-bots uninstall --purge   # also both state directories and the env file (the key)
```

### The system-wide variant

`install --system` (as root) puts the binaries under `/usr/local/bin`, the
units under `/etc/systemd/system`, the env file at `/etc/axlr-bots/env`
and the state under `/var/lib/axlr-bots` and `/var/lib/axlr-node`,
all run by a dedicated user `axlr-bots` (`useradd --system`, in
`systemd-journal`) with `ProtectSystem=strict`, `ProtectHome=yes`,
`StateDirectory=` and `HOME=` the state directory so `axlr-tui` writes its
own files there. `uninstall --system` reverses it. With `--listen`, run
`ca init` (and `node enroll-token`) as the service user, or `chown -R
axlr-bots: /var/lib/axlr-bots/ca` afterwards: the key is mode 0600 and
the daemon must read it. The system-wide variant was not exercised with
TLS on this machine; the user-level one was.

## The control plane

`axlr-bots serve` is the daemon (`Type=simple`, logs to the journal).

- It serves the API on a Unix socket, `<state>/axlr-bots.sock`, mode
  0660, the node protocol on a TLS address when `--listen` names one
  (`127.0.0.1:7878` for this host, `0.0.0.0:7878` for nodes on other
  hosts), and the operator API on another TLS address when
  `--api-listen` names one (`127.0.0.1:7879`; see [The operator
  API](#the-operator-api)). HTTP+JSON, because `net/http` is in the
  standard library and the JSON is the same the CLI prints. The node
  address serves only what a node needs: health, enrollment and the
  five node routes; a node certificate opens no operator route, and the
  operator address serves no node route.
- The TLS address is TLS 1.3 with a server certificate the fleet CA
  issues at start for the host's names (the listen address, the host
  name, `localhost`, the loopback, every interface address when it
  listens on all of them, plus `--server-names`), reissued before it
  expires, and a client certificate verified against the CA. Without
  `ca init` the daemon refuses to listen.
- Every CLI command tries the socket first (`--socket`, `AXLR_BOTS_SOCKET`,
  default `<state>/axlr-bots.sock`): when the daemon answers, the
  command is carried by it, so a `task submit` reaches the running service
  and never touches the state files. When nothing answers, the command
  works on the state directory as in Phase 0: the task is spooled.
- A Pending task of a bot an **Online node** may run waits for that node's
  next poll. A Pending task no Online node may run is run **in process**
  by the daemon on its next tick (`--tick 2s`), with the control plane's
  own `axlr-tui`: the **single-process mode**, also what `task run` does
  from the CLI. Both go through one `SerialRunner`, so a task is never run
  twice or in two places.
- On every tick it also runs the scheduler: each timer trigger whose
  slot has come fires once (see [Triggers](#triggers)).
- On start and every minute (`--reconcile-every`) it marks Offline the
  nodes silent for over a minute, fails the executions whose node stopped
  renewing its lease, and fails the in-process runs a dead daemon left
  Running past the grace (the bot's timeout plus a minute).

## Nodes and the protocol

A node is a resource of the control plane, like a bot. The agent
registers at start, heartbeats every 15 s, long-polls for work, renews a
lease while it works and reports when it is done. A restarted agent
registers again and comes Online; a control plane that lost its state
answers a heartbeat with 404 and the agent registers again.

| Message | Route | Direction | Idempotency |
|---|---|---|---|
| enroll `{tokenId, secret, hostName, csr}` | `POST /v1/enroll` | node → control plane, **without a certificate** | once per token: the answer is the node's certificate, the CA and the node |
| register `{id, hostName, labels, capabilities}` | `POST /v1/nodes` | node → control plane | per node id: a repeat describes the node anew; a registration without labels keeps the node's |
| heartbeat | `POST /v1/nodes/{id}/heartbeat` | node → control plane | yes |
| next assignment | `GET /v1/nodes/{id}/assignments/next?wait=20s` | node → control plane (long poll; `204` when none) | each execution is handed out once; an unleased one assigned to the node (a queued decision) goes first |
| renew lease | `POST /v1/nodes/{id}/executions/{exec}/lease` | node → control plane | yes; refused for another node's or an ended execution |
| report `{outcome, result, usage, failureReason, traceReference, policy}` | `POST /v1/nodes/{id}/executions/{exec}/report` | node → control plane | **accepted once**: a repeat answers `accepted: false` with the stored outcome |

An assignment carries a `run` (the task's objective, model, level, tools,
rules, timeout and correlation ids) or a `decide` (the session, the call
and the decision) and a lease of two minutes the node renews every 30 s.
The control plane stores the node id and the lease on the execution;
`node.registered`, `node.heartbeat`, `node.offline`, `execution.assigned`
and `execution.reported` are the events. The trace stays on the node; the
execution keeps its reference. Capabilities say which `axlr-tui` the node
has and whether it takes `--policy` and the correlation ids. A lease the
control plane refuses for good (the task was cancelled) stops the run on
the node, whose late report is then dropped.

Placement is part of the bot's definition: `"placement": {"nodes":
["web-1"], "selector": {"zone": "eu"}}`, or nothing for any node.

### Node identity

Over the TLS address a node **is** its client certificate: the server
takes the node id from the certificate's common name, refuses a URL
node id (or a registration body) that names another node, and refuses
the certificate when its serial is on the denylist or is not the one
recorded at the node's last enrollment. Over the socket, where there is
no certificate, the node id in the URL is trusted as before (the same
host, the same user). A node the control plane does not know but whose
certificate the CA signed may register: that is how a node comes back
after the control plane lost its state.

- `axlr-bots node enroll-token [--node-id <id>] [--labels k=v] [--ttl 1h]`
  mints a **one-time token** bound to a future node id (generated when
  not given) and to the labels the node starts with. The token is one
  line, `axlr1.…`: the token id, a 256-bit secret, the node id and the CA
  certificate. The control plane stores the hash of the secret, never the
  secret; the token is printed once and is in no log. `node.enroll-token`
  is audited with the token id.
- `axlr-node enroll --control https://host:7878 --token …` generates the
  node's key on the host (it never leaves it), verifies the control plane
  against the CA the token carries (or `--ca-file`), sends a signing
  request with the token and stores `node.key` (0600), `node.crt` and
  `ca.crt` under `<node-state>/pki`. The token is consumed before the
  certificate is signed, so a failure burns the token rather than issue
  twice; an expired, used or unknown token is refused (409, 409, 403).
  The enrollment is a `node.enrolled` event and a `node.enroll` audit
  entry with the certificate's serial. A node that enrols again (a fresh
  token for the same id) rotates its certificate: the previous one goes
  on the denylist and stops being accepted at the next request.
- `axlr-bots node revoke <id>` marks the node `Revoked`, puts its
  certificate's serial on the denylist (`<state>/pki/revoked.json`,
  checked on every request a certificate authenticates, so an open
  connection is cut off too), and fails the executions it still held with
  their tasks (`revoked: node web-2 was revoked at …`, a `bot.task.failed`
  event with `"reason": "revoked"`). A revoked node cannot register,
  heartbeat or take work; a fresh token readmits it with a new
  certificate, and the old serial stays denied.

## Adding a host

One control plane, N hosts, N bots per host; every host connects
outward. On the control plane's host the daemon must listen on an
address the new host reaches (`install --listen 0.0.0.0:7878
--server-names cp.example`, port 7878 open inbound), and the operator
mints a token there:

```bash
axlr-bots node enroll-token --node-id web-2 --labels zone=eu --ttl 1h
# {"token": "axlr1.…", "id": "enr-…", "nodeId": "web-2", "labels": {"zone": "eu"}, "expiresAt": "…"}
```

On the new host, as the user the node will run as, with the repository
checked out (the token and the control plane's URL are the only things
copied from the first host; the model key is that host's own):

```bash
GOFLAGS=-p=2 go build -o bin/axlr-bots ./cmd/axlr-bots
GOFLAGS=-p=2 go build -o bin/axlr-node ./cmd/axlr-node
export OPENROUTER_API_KEY=...                                  # this host's key, into its env file
bin/axlr-bots install --node-only --control https://cp.example:7878 --axlr-bin /path/to/axlr-tui
~/.local/bin/axlr-node enroll --control https://cp.example:7878 --token 'axlr1.…' --state ~/.local/state/axlr-node
systemctl --user enable --now axlr-node.service
loginctl enable-linger "$USER"
```

`--state` names the directory the unit runs with (`enroll` would
otherwise follow `XDG_STATE_HOME` when it is set, and the unit would not
find the credentials). `install --node-only` puts `axlr-node`, its unit (with
`AXLR_NODE_CONTROL=https://cp.example:7878` in its environment and no
dependency on a local control plane) and the env file in place, and no
control plane: the second host runs none. `enroll` prints the node id,
the certificate's serial and expiry; the unit then runs with that
certificate, and the agent's first log line says so (`client certificate
…/pki/node.crt for node web-2 (serial …)`). Back on the control plane:

```bash
axlr-bots node list                      # web-2 Online, certificateSerial set, labels {"zone": "eu", "host": "web-2"}
axlr-bots bot register --file bot-eu.json   # "placement": {"selector": {"zone": "eu"}} runs on web-2 only
axlr-bots node revoke web-2              # when the host is retired or compromised
```

`TestTwoNodesOverTLSRunTheBotWhereItsSelectorSays` (`adapters/daemon`)
is this flow in one process: a control plane over TLS on `127.0.0.1:0`,
two agents enrolled with tokens in zones `eu` and `us`, a bot whose
selector names `us`, and the task's execution on `node-us`. The same
test runs against the real `axlr-tui` and a model key with
`AXLR_BOTS_LIVE=1` (below).

## A bot's definition

```json
{
  "id": "bot", "tenant": "default", "name": "bot", "owner": "operator",
  "mission": "Do what each task asks, within the policy.",
  "level": "observe",
  "allowedTools": ["local_read", "local_search", "local_list"],
  "toolRules": {"allow": [], "deny": []},
  "triggers": [{"type": "manual"}],
  "placement": {},
  "model": {"model": "anthropic/claude-haiku-5.5", "routingPolicy": "cost-optimized", "allowRemoteProviders": true, "maxContextTokens": 32000},
  "budgets": {"maxModelCallsPerTask": 10, "maxToolCallsPerTask": 20, "maxDelegationDepth": 0, "taskTimeoutSeconds": 300}
}
```

- `level` is the autonomy level of vision §10.4: `observe`, `propose`,
  `act-with-approval` or `act-within-policy` (default `observe`).
- `allowedTools` are AXLR's tool names; `toolRules` narrow them by their
  arguments in the runtime contract's words: a glob over each argument's
  value as text (`*` within a path segment, `**` across segments, `?` one
  character; an array such as `args` is matched as its compact JSON). A
  tool with allow rules may only be called as a rule says; a tool without
  rules is allowed as a whole; a deny rule wins.
- `triggers` is how tasks reach the bot: `manual` (the API and the CLI,
  the default), `webhook` and `timer`; a bot that declares triggers takes
  only those (see [Triggers](#triggers)).
- `placement` says where the bot's tasks run (above).

`PATCH /v1/bots/{id}` and `bot update --file <patch.json>` replace the
fields a patch names (`mission`, `level`, `allowedTools`, `toolRules`,
`triggers`, `placement`, `model`, `budgets`); the identity and the status
do not change that way, and a patch that strands a tool rule or names a
bad trigger changes nothing.

## Triggers

A trigger has a type, a name unique within the bot (the type when not
given) and, for a webhook or a timer, an objective template the fire
renders over the event (`text/template`; a field the template names and
the event lacks refuses the fire, so an objective never says `<no
value>`). Every fire creates a task through the same path as a manual
submission, under an **idempotency key derived from the trigger and the
event**, so a redelivered webhook or a scheduler that restarts and fires
the same slot finds the task it already created and starts nothing
(vision §14.7). Every fire is a `bot.trigger.fired` event with the task,
the key and `deduplicated` true or false; the task keeps the trigger and
the event as its `input`.

```json
"triggers": [
  {"type": "manual"},
  {"type": "webhook", "name": "deploy", "secret": "<16 to 256 characters>",
   "objective": "Handle event {{.type}} for {{.service}}"},
  {"type": "webhook", "name": "alerts", "signingKey": "<16 to 256 characters>",
   "objective": "Look at the alert {{.alertname}} on {{.instance}}"},
  {"type": "timer", "name": "clock", "every": "10m",
   "objective": "Say the current time of this host (the trigger fired at {{.firedAt}})."},
  {"type": "timer", "name": "nightly", "cron": "0 3 * * 1-5", "objective": "Write the nightly report"}
]
```

| Type | Fired by | Key | Authentication |
|---|---|---|---|
| `manual` | `task submit --key`, `POST /v1/bots/{id}/tasks` with `Idempotency-Key` | the caller's key | the socket, or the operator bearer token |
| `webhook` | `POST /v1/triggers/{bot}/{trigger}` with a JSON object as the body | `webhook:<name>:<hash>` of the `Idempotency-Key` header (the delivery id) when the sender set one, else of the body | the trigger's `secret` in `X-Axlr-Secret`, or the HMAC-SHA256 of the body under its `signingKey` in `X-Axlr-Signature: sha256=<hex>` (`X-Hub-Signature-256` is read too); never the bearer token. Over the socket the caller is trusted, as for every route |
| `timer` | the daemon, on every tick | `timer:<name>:<unix seconds of the slot>` | none: it is the control plane's own |

`examples/bot-triggers-patch.json` is this patch for the example bot
(`bot update bot --file examples/bot-triggers-patch.json`, with the
secret replaced; the demo generates one per run).

A timer runs `every` so often (at least a second; the first fire is when
it is armed) or on a five-field `cron` expression evaluated in the
host's time zone (numbers only; Sunday is 0 or 7; a day of month and a
day of week both restricted match either, as in cron). Its state is one
file, `<state>/timers/<bot>~<trigger>.json`, with the next fire: a
daemon that comes back after a long stop finds the slot in the past,
**fires once for it** with that slot's key (so a crash between the fire
and the save fires the same slot again and finds the same task) and
arms the next slot after now; the slots in between are not replayed. A
paused bot's timers are advanced, not fired; a changed schedule re-arms
the timer; a removed trigger drops it. The timer's event carries
`trigger`, `scheduledAt` and `firedAt`, which the template may use. The
webhook's secret and signing key are stored with the bot and redacted in
every answer of the API and the CLI; the webhook body is bounded by 256
KiB and stored on the task.

## Correlation

The control plane generates every id: `bot_id` at registration,
`task_id` and a `trace_id` (`trace-…`, the task's `correlationId`; one
from upstream is kept instead) at submission, `execution_id` at each
attempt. The task stores the trace id, each execution a `correlation`
record (`botId`, `taskId`, `executionId`, `traceId`, `carried`, `path`),
and every event names the bot, the task, the execution and the trace,
so an execution is reconstructed from any of them (vision §14.8). The
runtime adapter writes `<state>/correlation/<execution>.json` with the
contract's object (`{"correlation": {"bot_id", "task_id",
"execution_id", "trace_id"}}`) for every run and reads `axlr-tui --help`
once: with `-correlation` it passes `--correlation <file>` (and
`--budget <file>` with `-budget`: `{"budget": {"seconds", "requests",
"tool_calls"}}` from the bot's budgets); with `-execution` alone it
passes one `--execution <bundle>` holding the policy, the budget and
the correlation in place of `--policy`; otherwise it passes nothing and
the execution says `"carried": false`. The installed `axlr-tui` on this
machine (the policy build) takes none of them, so every execution below
says `carried: false` while the ids are already generated, stored and
in every event; the node reports `correlationSupported` in its
capabilities. A binary built from AXLR's `claude/execution-budget`
branch (PR underpass-ai/AXLR#178, `--correlation` and `--budget` with
`--exec` or `--decide`) was run on 24 Oct 2026 with the files this
adapter writes: the run completed, every event line and the result
carried the four ids unchanged, and `TestLiveExec` passed through the
adapter with both flags on the command line. A run that reaches a
budget limit ends `budget_exceeded`; the control plane records it as
`Failed` with the limit as the reason (`budget exceeded: budget:
tool_calls 60 reached`) and keeps the refused call in the result.

## The operator API

The vision's minimal API (§11.1) plus the routes the CLI uses, served
on the socket (the CLI's path, trusted) and, with `serve --api-listen
<addr>` (or `install --api-listen`), on a TLS address for operators and
webhook senders. The TLS certificate is the fleet CA's (`ca init`,
`--cacert <state>/ca/ca.crt` on the client); the operator routes take
`Authorization: Bearer <token>` with the token of `<state>/api.token`
(generated at the first start, mode 0600, never logged), the trigger
route takes the trigger's own credential, and `/healthz` is open. A
node certificate is no credential here: the operator address serves
no node route, and the bearer token is the only way in, which keeps
node certificates and operator access apart (a client-certificate
scheme for operators would need its own CA profile; it is left out).
Bodies are bounded (1 MiB; a webhook 256 KiB) and unknown fields are
refused.

| Route | Does | Idempotency |
|---|---|---|
| `POST /v1/bots` | register a bot from its definition | per id: a repeat is `409 conflict` |
| `GET /v1/bots`, `GET /v1/bots/{id}` | list, show (credentials redacted) | |
| `PATCH /v1/bots/{id}` | replace the fields the patch names | the patch is the whole value of each field |
| `POST /v1/bots/{id}/enable` | let the bot accept tasks | |
| `POST /v1/bots/{id}/tasks` `{objective, correlationId?}` | submit a task; `Idempotency-Key` header (or `idempotencyKey` in the body; the header wins) | same bot and key: same task, `200` instead of `201` |
| `GET /v1/bots/{id}/events?limit=100` | the bot's newest events, oldest first (at most 1000) | |
| `POST /v1/triggers/{bot}/{trigger}` | fire a webhook with the body as the event; `Idempotency-Key` is the delivery id | same delivery (or same body): same task, `200` |
| `GET /v1/tasks/{id}` | show a task: status, trigger, input, trace id, executions | |
| `GET /v1/tasks/{id}/executions` | the task's executions with their correlation, policy, usage and result | |
| `POST /v1/tasks/{id}/cancel` | stop a task: Pending never runs, Running ends `Cancelled` (the in-process run is interrupted, a node's run stops at its next lease renewal, the late report is dropped), WaitingApproval never gets its decision | a Cancelled task answers `200` with `changed: false`; Completed or Failed is `409` |
| `POST /v1/tasks` `{botId, objective, idempotencyKey?}` | the older form of submit, with the same header | as above |
| `POST /v1/tasks/{id}/run`, `/decide`, `GET /v1/tasks/{id}/report`, `GET /v1/executions/{id}` | the CLI's run, approve/deny, report and execution routes | |
| `GET /v1/nodes`, `GET /v1/nodes/{id}`, `POST /v1/enrollments`, `POST /v1/nodes/{id}/revoke` | the nodes: list, show, mint a token, revoke | |

Errors are `{"kind": "validation|not_found|state|conflict|forbidden|unauthorized|too_large|internal", "error": "…"}`
with the matching status (`400`, `404`, `409`, `409`, `403`, `401`,
`413`, `500`).

```bash
state=~/.local/state/axlr-bots
curl --cacert $state/ca/ca.crt -H "Authorization: Bearer $(cat $state/api.token)" https://127.0.0.1:7879/v1/bots
curl --cacert $state/ca/ca.crt -H "Authorization: Bearer $(cat $state/api.token)" -H "Idempotency-Key: deploy-42" \
     -d '{"objective": "List the files in your workspace"}' https://127.0.0.1:7879/v1/bots/bot/tasks
curl --cacert $state/ca/ca.crt -H "X-Axlr-Secret: $secret" -H "Idempotency-Key: gh-delivery-1" \
     -d '{"type": "deploy", "service": "web"}' https://127.0.0.1:7879/v1/triggers/bot/deploy
```

Before each execution the adapter writes `<state>/policies/<execution>.json`
and passes it with `--policy` when the binary takes it (the adapter reads
`axlr-tui --help` once):

| Bot level | AXLR level | `--approve` | Why |
|---|---|---|---|
| `observe` without `local_exec` | `observe` | `none` | reads only |
| `observe` with `local_exec` | `act-within-policy` | `local` | AXLR's `observe` forbids every command whatever its arguments; the allow list still holds only reads and the commands the rules name |
| `propose` | `propose` | `local` | writes of a plan or a diff only |
| `act-with-approval` | `act-with-approval` | `none` | every allowed write or command is a person's decision (`needs_decision`) |
| `act-within-policy` | `act-within-policy` | `local` | the policy decides |

Without `-policy` in the binary the approval level stays `none`: nothing
runs without a person (fail closed), and the policy file is kept as a
record. The execution shows `"policy": {"level", "runtimeLevel",
"enforced", "path"}`. A forbidden call is refused by AXLR before any card:
the model reads `forbidden by the execution's policy: …` as the tool result,
the call is `forbidden` in `result.toolCalls` and the run goes on (vision
§14.5).

## The approval path

`examples/bot-act-with-approval.json` is the example bot one level up:
`local_write` is allowed and AXLR runs it under `act-with-approval` with
`--approve none`, so the first write is a card: the run stops with
`needs_decision`, the task is `WaitingApproval` and the execution's
`result.pendingDecision` names the tool, the call, the session and the
call's arguments (what would be written, and where).

```bash
axlr-bots task submit bot-act-with-approval --objective "Create hello.txt in your workspace containing hello" --key w-1
axlr-bots task get task-…        # WaitingApproval; pendingDecision.tool = local_write
axlr-bots task approve task-…    # a new execution carries the decision
axlr-bots task deny task-…       # or: the session ends without the write
```

When the stopped execution ran on a node, the decision is queued for that
node (the AXLR session lives there) and the node runs `axlr-tui --session
<id> --decide approve --policy <file>` on its next poll and reports; in
single-process mode the control plane runs it itself. Either way the write
never runs before `task approve`: `TestTheWriteNeverRunsWithoutTheApproval`
(`cmd/axlr-bots`) and `TestAgentRunsAssignedTasksAndDecisions`
(`adapters/nodeagent`) prove it against the stand-in, whose marker file
appears only after the approval and never after a denial.

## Reports

When an execution completes, its answer is written to
`<state>/artifacts/<task-id>/report.md` on the control plane, listed in
the execution's `result.artifacts` as `report.md`, and named in the
`bot.task.completed` event and the daemon's log line.

```bash
axlr-bots task report <task-id>          # the text
axlr-bots task report --path <task-id>   # only where it is
```

## Recovery

A task is written as `Running` with its execution *before* anything runs,
so a crash leaves a visible `Running` task, never a lost one. Three things
end it: the node's report, the daemon's reconciliation (an expired lease,
or an in-process run older than the grace), or `task reconcile` by hand:

```bash
axlr-bots task reconcile                # {"failed": ["task-…"], "skipped": []}
axlr-bots task reconcile --grace 30s
```

The failed execution and task carry the diagnosis (`reconciled: … node
web-1 stopped renewing its lease at …`, or `…; the process that ran it is
gone`), a `bot.task.failed` event with `"reason": "reconciled"` and a
`task.reconcile` audit entry. Running it again is harmless.

## Live run on this machine

`examples/demo.sh` runs the whole thing user-level: build, install with
the control plane listening for nodes on `127.0.0.1:7878` and for
operators on `127.0.0.1:7879`, both TLS, beside its socket, create the
fleet CA, start the control plane, enroll this host as a node through
the TLS path with a one-time token (so the installed node uses mTLS, as
a node on another host would), start it and show it Online with its
certificate, register the example bot, submit a generic task and watch
the node run it, read the report, the approval path with a harmless
write in the bot's workspace, restart both services and show the node,
the bots and the tasks are still there; then the automatic triggers: a
timer every 10 minutes and a webhook on the example bot, the webhook
fired with `curl` on the operator address with the trigger's secret,
delivered twice (one task), refused without the secret, the operator
route refused without the bearer token, both tasks and their executions
with the trace ids, and a last restart that fires nothing twice. The
installation stays running (`DEMO_UNINSTALL=1` removes it); a second run
keeps the CA and the node's credentials (`DEMO_REENROLL=1` enrolls the
node again, which rotates its certificate).

```bash
export OPENROUTER_API_KEY=...
AXLR_TUI=/path/to/axlr-tui sh examples/demo.sh
```

Run on 24 Oct 2026 on this machine (user `gx10a`, host `gx10-ff95`), with
`claude-haiku-5.5` through OpenRouter and an `axlr-tui` built from AXLR's
`claude/execution-policy` branch (`/home/gx10a/Documents/ai/tmp/axlr-tui-policy`).
The CA, the node's credentials and the bots were there from earlier runs
of the day. This was the third run of the demo within ten minutes: the
timer `clock` was armed and fired by the first (its slot `15:59:22Z`,
task `task-462b…`), and the two later runs, each re-applying the triggers
and restarting both services, found that slot fired and created no
second timer task, which is what step 8 counts. The webhook's delivery
id carries the run's time stamp, so each run is one new webhook task:

```bash
set -a; . ~/.hermes/.env; set +a            # exports OPENROUTER_API_KEY
AXLR_TUI=/home/gx10a/Documents/ai/tmp/axlr-tui-policy sh examples/demo.sh
```

```text

## 0. build and install (user-level: ~/.local, ~/.config, systemctl --user; nodes on 127.0.0.1:7878 and operators on 127.0.0.1:7879, both TLS)
  "mode": "user",
  "binary": "/home/gx10a/.local/bin/axlr-bots",
  "nodeBinary": "/home/gx10a/.local/bin/axlr-node",
    "/home/gx10a/.config/systemd/user/axlr-bots.service",
    "/home/gx10a/.config/systemd/user/axlr-node.service"
  "envFile": "/home/gx10a/.config/axlr-bots/env",
  "socket": "/home/gx10a/.local/state/axlr-bots/axlr-bots.sock",
  "control": "https://127.0.0.1:7878",
  "listen": "127.0.0.1:7878",
  "apiListen": "127.0.0.1:7879",
    "/home/gx10a/.local/bin/axlr-bots --state /home/gx10a/.local/state/axlr-bots ca init  # once per fleet, before the service starts",
    "the operator bearer token is written to /home/gx10a/.local/state/axlr-bots/api.token at the first start",
    "systemctl --user enable --now axlr-bots.service axlr-node.service",
    "loginctl enable-linger gx10a  # keep the services running after logout"

## 1. the fleet CA (once per fleet; kept across runs)
(the CA exists: /home/gx10a/.local/state/axlr-bots/ca)
  "fingerprint": "dcb7e7d6b66e604bc768f9932f621c1803972be2cd3e63e0d6c5ba7fe1cfd2cc",

## 2. start the control plane; enroll this host as a node over TLS with a one-time token; start the node
(the node is enrolled: /home/gx10a/.local/state/axlr-node/pki; DEMO_REENROLL=1 enrolls it again and rotates its certificate)
    "id": "gx10-ff95",
    "hostName": "gx10-ff95",
      "host": "gx10-ff95"
      "policySupported": true,
    "status": "Online",
    "certificateSerial": "32a865fa9c3309f8c98ce2f3032ae8f0"
2026/10/24 18:04:28 node: control plane https://127.0.0.1:7878, client certificate /home/gx10a/.local/state/axlr-node/pki/node.crt for node gx10-ff95 (serial 32a865fa9c3309f8c98ce2f3032ae8f0, until 2027-10-24)

## 3. register the example bot (observe: AXLR's read tools, manual trigger, any node)
(bot already registered: the state survived an earlier run)
  "id": "bot",
  "level": "observe",
      "type": "manual",
      "type": "timer",
      "type": "webhook",
  "status": "Enabled",

## 4. a generic task: the control plane assigns it to the node over TLS, the node runs it with AXLR
task task-708a26189b5bf886b9c785f3 is Completed after about 8s
    "costUsd": 0.00122603
    "enforced": true,
  "nodeId": "gx10-ff95",
  "outcome": "Completed",
    "requests": 2,
    "runtimeLevel": "observe",
    "toolCalls": [
    "toolCalls": 1,
report:
    The workspace has **0 files**. The recursive listing of the workspace root (`.`, up to 8 levels deep) came back empty.
    
    The listing skips `.git` and the console's `.axlr` and `.kernel` stores by design, so any files there wouldn't appear.

## 5. the approval path: a write runs only after task approve
task task-c31765eca100f22cb1ed470d is WaitingApproval after about 4s
  "status": "WaitingApproval",
  "outcome": "DecisionRequired",
        "callId": "toolu_01LwYwj9yNxWLCxYWjmEcNfu",
      "tool": "local_write",
      "callId": "toolu_01LwYwj9yNxWLCxYWjmEcNfu",
      "arguments": {
  "nodeId": "gx10-ff95",
the file does not exist yet: nothing ran without approval
approving...
    "status": "Running",
    "kind": "decide",
    "nodeId": "gx10-ff95"
task task-c31765eca100f22cb1ed470d is Completed after about 10s
    Nothing is missing: I read the file back and it contains exactly `hello`, so the request is complete.
-rw------- 1 gx10a gx10a 5 Oct 24 18:04 /home/gx10a/.local/state/axlr-node/workspaces/bot-act-with-approval/hello-20261024T160422Z.txt
hello
## 6. restart both services: node, bots and tasks are still there
    "id": "gx10-ff95",
    "status": "Online",
    "certificateSerial": "32a865fa9c3309f8c98ce2f3032ae8f0"
    "id": "bot-act-with-approval",
    "status": "Enabled",
    "id": "bot",
    "status": "Enabled",
  "id": "task-708a26189b5bf886b9c785f3",
  "status": "Completed",
  "id": "task-c31765eca100f22cb1ed470d",
  "status": "Completed",
service logs (last lines):
Started axlr-bots.service - AXLR Bots control plane.
2026/10/24 18:04:50 daemon: operator token read from /home/gx10a/.local/state/axlr-bots/api.token
2026/10/24 18:04:50 daemon: serving on /home/gx10a/.local/state/axlr-bots/axlr-bots.sock and https://127.0.0.1:7878 (nodes) and https://127.0.0.1:7879 (operators, bearer token), tick 2s
axlr-node.service: Consumed 3.685s CPU time.
Started axlr-node.service - AXLR Bots node agent.
2026/10/24 18:04:50 node: control plane https://127.0.0.1:7878, client certificate /home/gx10a/.local/state/axlr-node/pki/node.crt for node gx10-ff95 (serial 32a865fa9c3309f8c98ce2f3032ae8f0, until 2027-10-24)
2026/10/24 18:04:50 node: registered as gx10-ff95 (gx10-ff95), axlr "" policy true correlation false

## 7. automatic triggers: a timer every 10 minutes and a webhook on the example bot
  "name": "bot",
      "type": "manual",
      "name": "manual"
      "type": "timer",
      "name": "clock",
      "objective": "Say the current time of this host (the trigger fired at {{.firedAt}}).",
      "every": "10m0s"
      "type": "webhook",
      "name": "deploy",
      "objective": "Handle event {{.type}} for {{.service}}: say in one sentence what you would check first, without running anything.",
      "secret": "<redacted>"
(the secret is generated per run and shown redacted; the timer is armed and fires at once)
curl --cacert /home/gx10a/.local/state/axlr-bots/ca/ca.crt -H 'X-Axlr-Secret: <secret>' -H 'Idempotency-Key: deploy-20261024T160422Z' -d '{"type": "deploy", "service": "web"}' https://127.0.0.1:7879/v1/triggers/bot/deploy
{"created":true
"task":{"id":"task-dc002e74d9f2a08d0f42aa6e"
"idempotencyKey":"webhook:deploy:1476c6218f02cdcd11c12fc3acc42bd1"
"trigger":{"type":"webhook"
the same delivery again: "created":false, task task-dc002e74d9f2a08d0f42aa6e (the same)
without the secret: HTTP 403
operator route without the token: HTTP 401
operator route with the token: HTTP 200
timer task: task-462b9058850cd655a0b0e0a5
task task-dc002e74d9f2a08d0f42aa6e is Completed after about 6s
task task-462b9058850cd655a0b0e0a5 is Completed after about 0s
  "id": "task-dc002e74d9f2a08d0f42aa6e",
  "objective": "Handle event deploy for web: say in one sentence what you would check first, without running anything.",
  "status": "Completed",
  "idempotencyKey": "webhook:deploy:1476c6218f02cdcd11c12fc3acc42bd1",
  "correlationId": "trace-8f1cf2d09f22bf4a441c59e3",
    "type": "webhook",
    "name": "deploy"
    "type": "deploy",
    "id": "exec-840bc8f94fb89071e514f9e8",
    "outcome": "Completed",
      "costUsd": 0.000907775
      "traceId": "trace-8f1cf2d09f22bf4a441c59e3",
      "carried": false,
    "nodeId": "gx10-ff95",
report:
    I would first check the web deploy's target environment and the build artifact it will ship: confirm the intended branch or commit, that the production build passes locally, and that required env vars and config for that environment are set.
  "id": "task-462b9058850cd655a0b0e0a5",
  "objective": "Say the current time of this host (the trigger fired at 2026-10-24T15:59:22Z).",
  "status": "Completed",
  "idempotencyKey": "timer:clock:1792857562",
  "correlationId": "trace-b3291e374a270360e9a61f6f",
    "type": "timer",
    "name": "clock"
    "id": "exec-c5b43e95c519018452e2276d",
    "outcome": "Completed",
      "costUsd": 0.0014355799999999999
      "traceId": "trace-b3291e374a270360e9a61f6f",
      "carried": false,
    "nodeId": "gx10-ff95",
report:
    Nothing is missing: the only time I have is the trigger timestamp, 2026-10-24T15:59:22Z (UTC). None of my tools can read the host's live clock, so the host's current time may be somewhat later.
timer state (the next fire is persisted):
  "schedule": "every 10m0s",
  "nextFireAt": "2026-10-24T16:09:22.594343227Z",
  "lastScheduledAt": "2026-10-24T15:59:22.594343227Z",
  "lastTaskId": "task-462b9058850cd655a0b0e0a5"

## 8. restart both services: the timer fires nothing twice, the webhook delivery stays one task
timer tasks for the slot 2026-10-24T15:59:22.594343227Z: 1; timer tasks in all: 1 (one per slot)
fires of the delivery deploy-20261024T160422Z in the events: 2 (two: the delivery and its redelivery); tasks for it: 1
2026/10/24 18:04:57 daemon: operator token read from /home/gx10a/.local/state/axlr-bots/api.token
2026/10/24 18:04:57 daemon: serving on /home/gx10a/.local/state/axlr-bots/axlr-bots.sock and https://127.0.0.1:7878 (nodes) and https://127.0.0.1:7879 (operators, bearer token), tick 2s

## installed and running
check:     systemctl --user status axlr-bots.service axlr-node.service
uninstall: /home/gx10a/.local/bin/axlr-bots uninstall [--purge]
```

Against the vision's criteria: the bot, the node, the tasks and the
timer's next fire survived the restarts of both services (§14.1); tasks
were started by the API and by two automatic triggers, a timer and a
webhook (§14.2); AXLR received the policy and produced the result
object, enforced (§14.3, §14.5); the write ran only after `task approve`
(§10.4); the redelivered webhook and the restarted scheduler created no
second task, and both fires are in the events with `deduplicated`
(§14.7); every task has a trace id and every execution and event names
the bot, the task, the execution and the trace (§14.8; the runtime did
not carry them yet, `carried: false`); the executions keep the node, the
policy, the cost and the trace reference (§12.3); nothing needed root
(§9.1); the node authenticated with a client certificate the fleet CA
issued against a one-time token, the control plane with a certificate
from the same CA, over TLS 1.3; the operator's commands stayed on the
socket and the operator address refused a request without the bearer
token and a webhook without its secret (§9.4). The installation is left
running on this machine with the node on the TLS path and the timer
firing every ten minutes; `loginctl show-user gx10a -p Linger` says
`yes`, so it survives a logout. The `axlr ""` in the node's log is the
capability the agent reads from `axlr-tui --version`, which this build
does not print. The live webhook test ran against this installation
after the demo and passed in 6 s (`task-7142729d…` Completed on
`gx10-ff95`, $0.00094).

## Phase 0 demo (offline)

`examples/phase0-demo.sh` runs the single-process flow with no daemon and
no node: register, enable, submit, `task run`, read the result, resubmit
the same key. Without `AXLR_TUI` it uses `examples/fake-axlr-tui`,
a shell stand-in that prints a finished turn and its result object.

```bash
sh examples/phase0-demo.sh
```

## Build and test

```bash
GOFLAGS=-p=2 go build ./... && GOFLAGS=-p=2 go test -cover ./...
```

Every package has at least 80% coverage. The AXLR adapter is tested against
a helper process that imitates `axlr-tui`; the commands, the API, the
daemon and the node agent against `internal/fakeaxlr` (a shell stand-in
that answers `--help` and `--version`, records its arguments and plays a
scenario, including the approval flow with a marker file); the installer
renders into a `--destdir`. One test runs the real binary only when
`AXLR_LIVE=1` is set:

```bash
AXLR_LIVE=1 AXLR_TUI=/path/to/axlr-tui GOFLAGS=-p=2 go test ./adapters/axlr -run TestLiveExec -v
```

The triggers are tested in `domain` (definitions, the HMAC and secret
checks, the cron parser and its next fire, the fire keys within the
key's length), `application` (a redelivered webhook finds its task, a
timer catches up once after a restart and fires the same slot again
without a second task, a paused bot's timers advance, a cancel of a
running task through the runner drops the late report),
`adapters/unixapi` (the operator routes, the `Idempotency-Key` header,
a webhook refused without its secret or with a tampered signature,
accepted with the secret or the signature, `413` past the bound, the
bearer token on the operator address with no node route on it, a
cancel of a running task through the API) and `adapters/daemon` (a
timer fires at once when armed and once more after a restart with the
slot in the past). One test fires a webhook against the daemon running
on this machine and waits for the node to complete the task:

```bash
AXLR_BOTS_LIVE=1 GOFLAGS=-p=2 go test ./cmd/axlr-bots -run TestLiveWebhookRunsOnTheNode -v
```

The node protocol over TLS is tested in `adapters/pki` (the handshakes:
an enrolled node, a node that trusts another CA, a certificate another
CA issued, no certificate), `adapters/unixapi` (enrollment end to end,
a reused and an expired token, a wrong secret, no certificate on a node
route, a certificate for `web-1` on `web-2`'s routes and in a
registration body, a revoked serial on an open and on a new connection,
a superseded certificate after rotation), `application` (the use cases
with a fixed clock) and `adapters/daemon` (two enrolled nodes and a bot
placed by selector). One of them also runs against the real binary and a
model key:

```bash
AXLR_BOTS_LIVE=1 AXLR_TUI=/path/to/axlr-tui OPENROUTER_API_KEY=... GOFLAGS=-p=2 go test ./adapters/daemon -run TestLiveTwoNodesOverTLS -v
```

Run on 24 Oct 2026 on this machine, it passed in 7 s: the task of the bot
whose selector names `us` ran on `node-us` (`claude-haiku-5.5`, $0.00095),
with both agents enrolled and speaking over TLS to a control plane on
`127.0.0.1:0`.

## The runtime contract

The adapter speaks the contract in AXLR's `docs/runtime-contract.md`:

- Request: `--exec "<objective>"`, `--model` from the bot's model policy,
  `--root <state>/workspaces/<bot>`, `--approve` as the policy table says,
  `--policy <file>` when the binary takes it, a timeout from the bot's
  budgets (default 15 min).
- Result: the last JSON line, `{"type":"result","result":{"schema":"axlr.execution-result/v1",…}}`,
  gives status (`completed`, `needs_decision`, `interrupted`, `failed`),
  answer, sessions, pending call with its arguments, tool calls, usage and
  artifacts. Fallback for an older `axlr-tui`: exit code (0 completed, 3
  decision required, 1 failed) plus the `completed`, `failed` and
  `decision.required` lines.
- Decision: `--session <id> --decide approve|deny [--call <id>] --policy <file> --json`.
- Reserved fields: the correlation and the budget are written per
  execution and passed when the binary's `--help` shows the flags (see
  [Correlation](#correlation)); the installed build does not take them,
  the budget branch's build does.

Outcome mapping: `completed` → `Completed`; `needs_decision` →
`DecisionRequired`, task `WaitingApproval`; `failed`, `interrupted` and
timeouts → `Failed`.

State directory layout:

```text
<state>/
  bots/<bot-id>.json       tasks/<task-id>.json      executions/<exec-id>.json   nodes/<node-id>.json
  timers/<bot-id>~<trigger>.json (the next fire)      api.token (0600, with --api-listen)
  enrollments/<enr-id>.json (the hash of each token)  ca/ca.key (0600), ca/ca.crt   pki/revoked.json
  events.jsonl             audit.jsonl               axlr-bots.sock (while the daemon runs)
  policies/<exec-id>.json  correlation/<exec-id>.json  artifacts/<task-id>/report.md
  traces/, workspaces/     (single-process runs; a node keeps its own under its state)

<node-state>/
  pki/node.key (0600), pki/node.crt, pki/ca.crt, pki/enrollment.json   (once enrolled)
  traces/, policies/, correlation/, workspaces/<bot-id>/
```

## Command reference

```text
axlr-bots [--state <dir>] [--axlr-bin <path>] [--socket <path>] [--actor <name>] <command>

  serve [--listen <addr>] [--server-names a,b] [--api-listen <addr>] [--api-token-file <path>]
        [--tick 2s] [--reconcile-every 1m] [--grace <duration>]
  ca init | ca show
  node list | node get <node-id>
  node enroll-token [--node-id <id>] [--labels k=v,k=v] [--ttl 1h] | node revoke <node-id>
  bot register --file <bot.json> | bot enable <bot-id> | bot list | bot get <bot-id>
  bot update <bot-id> --file <patch.json> | bot events <bot-id> [--limit 100]
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>]
  trigger fire <bot-id> <trigger> --body '<json>' | --file <event.json> [--delivery <id>]
  task run <task-id>                           in process (exit 0 completed, 3 decision required, 1 failed)
  task approve <task-id> | task deny <task-id> | task decide <task-id> approve|deny
  task get <task-id> | task executions <task-id> | task cancel <task-id>
  task report <task-id> [--path] | task reconcile [--grace <duration>]
  execution get <execution-id>
  install [--system] [--prefix <dir>] [--state <dir>] [--node-state <dir>] [--unit-dir <dir>] [--env-file <path>]
          [--axlr-bin <path>] [--node-binary <path>] [--listen <addr>] [--server-names a,b] [--api-listen <addr>]
          [--control <url>] [--node-only] [--destdir <dir>] [--no-systemctl]
  uninstall [--system] [--prefix <dir>] … [--purge]

axlr-node --control unix:///path/to/axlr-bots.sock | https://host:7878
               [--node-id <id>] [--labels k=v,k=v] [--state <dir>] [--axlr-bin <path>] [--heartbeat 15s] [--poll 20s]
axlr-node enroll --control https://host:7878 --token <token> [--ca-file <pem>] [--state <dir>] [--host-name <name>]
```

`--state` defaults to `$AXLR_BOTS_STATE`, then `$XDG_STATE_HOME/axlr-bots`,
then `~/.local/state/axlr-bots`; `--axlr-bin` to `$AXLR_TUI`,
then `axlr-tui` on `PATH`; `--socket` to `$AXLR_BOTS_SOCKET`, then
`<state>/axlr-bots.sock` (`--socket ""` never uses a daemon); `serve`
reads `AXLR_BOTS_LISTEN`, `AXLR_BOTS_SERVER_NAMES` and
`AXLR_BOTS_API_LISTEN`, and `--api-token-file` defaults to
`<state>/api.token`. The node reads
`AXLR_NODE_CONTROL`, `AXLR_NODE_ID`, `AXLR_NODE_LABELS`, `AXLR_NODE_STATE`,
`AXLR_NODE_TOKEN` (for `enroll`) and `AXLR_TUI`; over the socket its id
defaults to the host name, over `https://` it is the certificate's and a
`--node-id` that disagrees is refused. Output is JSON on stdout; `task
report` prints the report as text.

## Next: the same node agent as a sidecar

Vision §9.2: the node agent runs inside the pod, beside the service, as a
container from the same image. The state directory becomes a volume, the
environment (`AXLR_NODE_CONTROL`, `AXLR_TUI`, the model key) comes
from the pod spec, and systemd disappears; the service's files are mounted
read-only and the bots placed on that node (by a `selector` on its labels)
read them with AXLR's own tools. The control plane stays where it is, one
per cluster or per fleet, and the webhook trigger
(`POST /v1/triggers/{bot}/{trigger}` from the orchestrator's events or an
alerting source, with the trigger's secret) is the trigger type that fits
there.

## What is left out

A systemd unit event as a trigger type, an approval queue beyond `task
approve` (who may decide, and when; an expired approval), budgets and
correlation enforced by the runtime (the control plane passes them once
`axlr-tui` takes the flags), operator client certificates on the API
address (the bearer token is the one credential), a `GET /v1/tasks`
listing, `axlr-serve` as a second `RuntimeExecutor`, and a real store
(one JSON file per aggregate scans the directory for the idempotency
key, the Pending tasks and the Running executions). On the PKI: a node certificate
lasts a year and is renewed by enrolling again with a fresh token (no
automatic renewal yet), revocation is the control plane's own denylist
(no CRL or OCSP for other verifiers), and the CA key lives in the
control plane's state directory (no HSM, no intermediate). The vision's
§13 and §14 name the rest for Phase 2.
