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
                       │ nodes · events.jsonl · audit.jsonl      │
                       │ artifacts/<task>/report.md              │
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
| `domain` | Bot, Task, Execution, Node and Enrollment aggregates; value objects validated in constructors (autonomy level, tool rules, triggers, placement, leases); a node's certificate serial and its `Revoked` status; events (vision §11.2) under a schema version; audit entries. One file per type. |
| `application` | Ports (`BotRepository`, `TaskRepository`, `ExecutionRepository`, `NodeRepository`, `EnrollmentRepository`, `Certifier`, `CertificateDenylist`, `EventPublisher`, `AuditSink`, `RuntimeExecutor`, `DecisionExecutor`, `TraceLocator`, `ArtifactStore`, `NodeTransport`) and use cases (`RegisterBot`, `EnableBot`, `SubmitTask`, `RunTask`, `ApproveTask`, `RegisterNode`, `HeartbeatNode`, `NextAssignment`, `RenewLease`, `ReportExecution`, `CreateEnrollmentToken`, `EnrollNode`, `AuthenticateNode`, `RevokeNode`, `MarkOfflineNodes`, `ReconcileTasks`, `RunNextPending`, `GetReport`, …); `SerialRunner` serialises runs, assignments, reports and revocations. |
| `nodeproto` | The messages of the node protocol, shared by the server and the node's client; the enrollment token's encoding. |
| `adapters/pki` | The fleet CA (ECDSA P-256; the key under `<state>/ca`, mode 0600), the node certificates it signs from a node's request (common name = node id, a year), the control plane's own certificate (issued at start for the host's names), the node's key and request, its stored credentials, and the TLS 1.3 configurations of both ends. |
| `adapters/filestore` | One JSON file per aggregate under the state directory, written atomically (temp file, fsync, rename); the enrollments (the hash of each token, never the token); the denylist of revoked certificate serials; the report artifacts. |
| `adapters/jsonl` | Append-only `events.jsonl` and `audit.jsonl`. |
| `adapters/axlr` | Runs `axlr-tui --exec --json [--policy <file>]` as a subprocess and reads the execution result object; keeps the raw lines as the trace and the policy file as evidence. Used by the node, and by the control plane in single-process mode. |
| `adapters/unixapi` | The API: a `Service` interface with `Local` (in process) and `Client` (HTTP) implementations; the `Server` serves everything on the Unix socket and the node protocol alone on the TLS address, where the node's identity is its client certificate; the `Client` is also the node's `NodeTransport`, over the socket or over TLS with the node's certificate. |
| `adapters/daemon` | The control plane as a service: the API on the socket and the TLS address, in-process runs for bots no node may run, reconciliation and offline nodes on a timer. |
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
    --listen 127.0.0.1:7878 --control https://127.0.0.1:7878   # the TLS address; 0.0.0.0:7878 for other hosts
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
| `~/.config/systemd/user/axlr-bots.service` | `Type=simple`, `ExecStart=… serve [--listen <addr>] [--server-names …]`, `Restart=on-failure`, `NoNewPrivileges=yes`, `EnvironmentFile=-~/.config/axlr-bots/env`, `WantedBy=default.target` |
| `~/.config/systemd/user/axlr-node.service` | `After=`/`Wants=axlr-bots.service` (or `network-online.target` with `--node-only`), `Environment=AXLR_NODE_CONTROL=<--control, or unix://…/axlr-bots.sock>`, `ExecStart=axlr-node --state ~/.local/state/axlr-node`, the host name as the `host=` label; over `https://` the node id is the certificate's |
| `~/.config/axlr-bots/env` | `OPENROUTER_API_KEY=` (or a commented placeholder when the installer had none) and `AXLR_TUI=`; an existing file is never overwritten |
| `~/.local/state/axlr-bots/` | the control plane's state: bots, tasks, executions, nodes, enrollments, policies, artifacts, logs, the socket, and `ca/` (the fleet CA: `ca.key` 0600, `ca.crt`) and `pki/revoked.json` (the denylist) |
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
  0660, and the node protocol on a TLS address when `--listen` names one
  (`127.0.0.1:7878` for this host, `0.0.0.0:7878` for nodes on other
  hosts). HTTP+JSON, because `net/http` is in the standard library and
  the JSON is the same the CLI prints. The TLS address serves only what a
  node needs: health, enrollment and the five node routes; a node
  certificate opens no operator route, and the operator's commands go
  through the socket.
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
rules and timeout) or a `decide` (the session, the call and the decision)
and a lease of two minutes the node renews every 30 s. The control plane
stores the node id and the lease on the execution; `node.registered`,
`node.heartbeat`, `node.offline`, `execution.assigned` and
`execution.reported` are the events. The trace stays on the node; the
execution keeps its reference. Capabilities say which `axlr-tui` the node
has and whether it takes `--policy`.

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
- `triggers` is how tasks reach the bot; `manual` (the API and the CLI) is
  the one type today and the default. A systemd unit event, a timer and a
  webhook are the next types, each with its router; a bot that declares
  triggers takes only those.
- `placement` says where the bot's tasks run (above).

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
the control plane listening on `127.0.0.1:7878` with TLS beside its
socket, create the fleet CA, start the control plane, enroll this host
as a node through the TLS path with a one-time token (so the installed
node uses mTLS, as a node on another host would), start it and show it
Online with its certificate, register the example bot, submit a generic
task and watch the node run it, read the report, the approval path with
a harmless write in the bot's workspace, then restart both services and
show the node, the bots and the tasks are still there. The installation
stays running (`DEMO_UNINSTALL=1` removes it); a second run keeps the CA
and the node's credentials (`DEMO_REENROLL=1` enrolls the node again,
which rotates its certificate).

```bash
export OPENROUTER_API_KEY=...
AXLR_TUI=/path/to/axlr-tui sh examples/demo.sh
```

Run on 24 Oct 2026 on this machine (user `gx10a`, host `gx10-ff95`), with
`claude-haiku-5.5` through OpenRouter and an `axlr-tui` built from AXLR's
`claude/execution-policy` branch (`/home/gx10a/Documents/ai/tmp/axlr-tui-policy`).
The CA and the bots were there from the first run of the day;
`DEMO_REENROLL=1` makes the node enroll again, which is why its
certificate's serial changes and the previous one stops being accepted:

```bash
set -a; . ~/.hermes/.env; set +a            # exports OPENROUTER_API_KEY
DEMO_REENROLL=1 AXLR_TUI=/home/gx10a/Documents/ai/tmp/axlr-tui-policy sh examples/demo.sh
```

```text

## 0. build and install (user-level: ~/.local, ~/.config, systemctl --user; the control plane listens on 127.0.0.1:7878 with TLS)
  "mode": "user",
  "binary": "/home/gx10a/.local/bin/axlr-bots",
  "nodeBinary": "/home/gx10a/.local/bin/axlr-node",
    "/home/gx10a/.config/systemd/user/axlr-bots.service",
    "/home/gx10a/.config/systemd/user/axlr-node.service"
  "envFile": "/home/gx10a/.config/axlr-bots/env",
  "socket": "/home/gx10a/.local/state/axlr-bots/axlr-bots.sock",
  "control": "https://127.0.0.1:7878",
  "listen": "127.0.0.1:7878",
    "/home/gx10a/.local/bin/axlr-bots --state /home/gx10a/.local/state/axlr-bots ca init  # once per fleet, before the service starts",
    "systemctl --user enable --now axlr-bots.service axlr-node.service",
    "loginctl enable-linger gx10a  # keep the services running after logout"

## 1. the fleet CA (once per fleet; kept across runs)
(the CA exists: /home/gx10a/.local/state/axlr-bots/ca)
  "fingerprint": "dcb7e7d6b66e604bc768f9932f621c1803972be2cd3e63e0d6c5ba7fe1cfd2cc",

## 2. start the control plane; enroll this host as a node over TLS with a one-time token; start the node
{
  "token": "axlr1.<one line, shown once; consumed below>",
  "id": "enr-30c8df33b7736347176ba570",
  "nodeId": "gx10-ff95",
  "expiresAt": "2026-10-24T09:45:43.293843263Z"
}
  "nodeId": "gx10-ff95",
  "control": "https://127.0.0.1:7878",
  "serial": "32a865fa9c3309f8c98ce2f3032ae8f0",
  "notAfter": "2027-10-24T09:35:43Z",
  "state": "/home/gx10a/.local/state/axlr-node"
    "id": "gx10-ff95",
    "hostName": "gx10-ff95",
      "host": "gx10-ff95"
      "policySupported": true
    "status": "Online",
    "certificateSerial": "32a865fa9c3309f8c98ce2f3032ae8f0"
2026/10/24 11:35:43 node: control plane https://127.0.0.1:7878, client certificate /home/gx10a/.local/state/axlr-node/pki/node.crt for node gx10-ff95 (serial 32a865fa9c3309f8c98ce2f3032ae8f0, until 2027-10-24)

## 3. register the example bot (observe: AXLR's read tools, manual trigger, any node)
(bot already registered: the state survived an earlier run)
  "id": "bot",
  "level": "observe",
      "type": "manual"
  "status": "Enabled",

## 4. a generic task: the control plane assigns it to the node over TLS, the node runs it with AXLR
task task-b052948e28e28fde50ccbdc1 is Completed after about 8s
    "costUsd": 0.0012014449999999998
    "enforced": true,
  "nodeId": "gx10-ff95",
  "outcome": "Completed",
    "requests": 3,
    "runtimeLevel": "observe",
    "toolCalls": [
    "toolCalls": 2,
report:
    The workspace has no files. Both listings, the default and an explicit root path, came back empty and untruncated, so the count is 0.

## 5. the approval path: a write runs only after task approve
task task-b94395d7beac5a2a8f641664 is WaitingApproval after about 4s
  "status": "WaitingApproval",
  "outcome": "DecisionRequired",
        "callId": "toolu_01RTxmDSSBugpexXDgF45Cxs",
      "tool": "local_write",
      "callId": "toolu_01RTxmDSSBugpexXDgF45Cxs",
      "arguments": {
  "nodeId": "gx10-ff95",
the file does not exist yet: nothing ran without approval
approving...
    "status": "Running",
    "kind": "decide",
    "nodeId": "gx10-ff95"
task task-b94395d7beac5a2a8f641664 is Completed after about 8s
    Nothing is missing: `hello-20261024T093542Z.txt` exists in the workspace and contains exactly `hello`, which I confirmed by reading it back.
-rw------- 1 gx10a gx10a 5 Oct 24 11:35 /home/gx10a/.local/state/axlr-node/workspaces/bot-act-with-approval/hello-20261024T093542Z.txt
hello
## 6. restart both services: node, bots and tasks are still there
    "id": "gx10-ff95",
    "status": "Online",
    "certificateSerial": "32a865fa9c3309f8c98ce2f3032ae8f0"
    "id": "bot-act-with-approval",
    "status": "Enabled",
    "id": "bot",
    "status": "Enabled",
  "id": "task-b052948e28e28fde50ccbdc1",
  "status": "Completed",
  "id": "task-b94395d7beac5a2a8f641664",
  "status": "Completed",
service logs (last lines):
Stopped axlr-bots.service - AXLR Bots control plane.
Started axlr-bots.service - AXLR Bots control plane.
2026/10/24 11:36:03 daemon: serving on /home/gx10a/.local/state/axlr-bots/axlr-bots.sock and https://127.0.0.1:7878, tick 2s
axlr-node.service: Consumed 3.589s CPU time.
Started axlr-node.service - AXLR Bots node agent.
2026/10/24 11:36:03 node: control plane https://127.0.0.1:7878, client certificate /home/gx10a/.local/state/axlr-node/pki/node.crt for node gx10-ff95 (serial 32a865fa9c3309f8c98ce2f3032ae8f0, until 2027-10-24)
2026/10/24 11:36:03 node: registered as gx10-ff95 (gx10-ff95), axlr "" policy true

## installed and running
check:     systemctl --user status axlr-bots.service axlr-node.service
uninstall: /home/gx10a/.local/bin/axlr-bots uninstall [--purge]
```

Against the vision's criteria: the bot, the node and both tasks survived
the restart of both services (§14.1); the task was started by the API
(§14.2); AXLR received the policy and produced the result object, enforced
(§14.3, §14.5); the write ran only after `task approve` (§10.4); the
executions keep the node, the policy, the cost and the trace reference
(§12.3); nothing needed root (§9.1); the node authenticated with a client
certificate the fleet CA issued against a one-time token, the control
plane with a certificate from the same CA, over TLS 1.3, and the
operator's commands stayed on the socket (§9.4: mutual authentication,
verifiable delegation through the token, revocation with `node revoke`,
encryption in transit). The installation is left running on this
machine with the node on the TLS path; `loginctl show-user gx10a -p
Linger` says `yes`, so it survives a logout. The `axlr ""` in the node's
log is the capability the agent reads from `axlr-tui --version`, which
this build does not print.

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
- Not yet taken by AXLR: budgets and correlation (the contract's reserved
  fields; see the TODO in `adapters/axlr/executor.go`).

Outcome mapping: `completed` → `Completed`; `needs_decision` →
`DecisionRequired`, task `WaitingApproval`; `failed`, `interrupted` and
timeouts → `Failed`.

State directory layout:

```text
<state>/
  bots/<bot-id>.json       tasks/<task-id>.json      executions/<exec-id>.json   nodes/<node-id>.json
  enrollments/<enr-id>.json (the hash of each token)  ca/ca.key (0600), ca/ca.crt   pki/revoked.json
  events.jsonl             audit.jsonl               axlr-bots.sock (while the daemon runs)
  policies/<exec-id>.json  artifacts/<task-id>/report.md
  traces/, workspaces/     (single-process runs; a node keeps its own under its state)

<node-state>/
  pki/node.key (0600), pki/node.crt, pki/ca.crt, pki/enrollment.json   (once enrolled)
  traces/, policies/, workspaces/<bot-id>/
```

## Command reference

```text
axlr-bots [--state <dir>] [--axlr-bin <path>] [--socket <path>] [--actor <name>] <command>

  serve [--listen <addr>] [--server-names a,b] [--tick 2s] [--reconcile-every 1m] [--grace <duration>]
  ca init | ca show
  node list | node get <node-id>
  node enroll-token [--node-id <id>] [--labels k=v,k=v] [--ttl 1h] | node revoke <node-id>
  bot register --file <bot.json> | bot enable <bot-id> | bot list | bot get <bot-id>
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>]
  task run <task-id>                           in process (exit 0 completed, 3 decision required, 1 failed)
  task approve <task-id> | task deny <task-id> | task decide <task-id> approve|deny
  task get <task-id> | task report <task-id> [--path] | task reconcile [--grace <duration>]
  execution get <execution-id>
  install [--system] [--prefix <dir>] [--state <dir>] [--node-state <dir>] [--unit-dir <dir>] [--env-file <path>]
          [--axlr-bin <path>] [--node-binary <path>] [--listen <addr>] [--server-names a,b] [--control <url>]
          [--node-only] [--destdir <dir>] [--no-systemctl]
  uninstall [--system] [--prefix <dir>] … [--purge]

axlr-node --control unix:///path/to/axlr-bots.sock | https://host:7878
               [--node-id <id>] [--labels k=v,k=v] [--state <dir>] [--axlr-bin <path>] [--heartbeat 15s] [--poll 20s]
axlr-node enroll --control https://host:7878 --token <token> [--ca-file <pem>] [--state <dir>] [--host-name <name>]
```

`--state` defaults to `$AXLR_BOTS_STATE`, then `$XDG_STATE_HOME/axlr-bots`,
then `~/.local/state/axlr-bots`; `--axlr-bin` to `$AXLR_TUI`,
then `axlr-tui` on `PATH`; `--socket` to `$AXLR_BOTS_SOCKET`, then
`<state>/axlr-bots.sock` (`--socket ""` never uses a daemon); `serve`
reads `AXLR_BOTS_LISTEN` and `AXLR_BOTS_SERVER_NAMES`. The node reads
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
per cluster or per fleet, and the webhook trigger (`POST /v1/triggers`
from the orchestrator's events or an alerting source) is the trigger type
that fits there.

## What is left out

Trigger types beyond `manual` (a systemd unit event, a timer, a webhook),
an approval queue beyond `task approve` (who may decide, and when; an
expired approval), budgets and correlation enforced by the runtime,
`axlr-serve` as a second `RuntimeExecutor`, and a real store (one JSON
file per aggregate scans the directory for the idempotency key, the
Pending tasks and the Running executions). On the PKI: a node certificate
lasts a year and is renewed by enrolling again with a fresh token (no
automatic renewal yet), revocation is the control plane's own denylist
(no CRL or OCSP for other verifiers), and the CA key lives in the
control plane's state directory (no HSM, no intermediate). The vision's
§13 and §14 name the rest for Phase 2.
