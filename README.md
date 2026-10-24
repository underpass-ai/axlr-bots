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
node**: the control plane never reaches into a host. On one host (the MVP,
and this machine) both run under `systemctl --user` and talk over a Unix
socket; nodes elsewhere use a loopback or network address, and TLS with
client certificates goes in front of it without changing a message.

## Layout (hexagonal)

| Package | Role |
|---|---|
| `domain` | Bot, Task, Execution and Node aggregates; value objects validated in constructors (autonomy level, tool rules, triggers, placement, leases); events (vision §11.2) under a schema version; audit entries. One file per type. |
| `application` | Ports (`BotRepository`, `TaskRepository`, `ExecutionRepository`, `NodeRepository`, `EventPublisher`, `AuditSink`, `RuntimeExecutor`, `DecisionExecutor`, `TraceLocator`, `ArtifactStore`, `NodeTransport`) and use cases (`RegisterBot`, `EnableBot`, `SubmitTask`, `RunTask`, `ApproveTask`, `RegisterNode`, `HeartbeatNode`, `NextAssignment`, `RenewLease`, `ReportExecution`, `MarkOfflineNodes`, `ReconcileTasks`, `RunNextPending`, `GetReport`, …); `SerialRunner` serialises runs, assignments and reports. |
| `nodeproto` | The messages of the node protocol, shared by the server and the node's client. |
| `adapters/filestore` | One JSON file per aggregate under the state directory, written atomically (temp file, fsync, rename); the report artifacts. |
| `adapters/jsonl` | Append-only `events.jsonl` and `audit.jsonl`. |
| `adapters/axlr` | Runs `axlr-tui --exec --json [--policy <file>]` as a subprocess and reads the execution result object; keeps the raw lines as the trace and the policy file as evidence. Used by the node, and by the control plane in single-process mode. |
| `adapters/unixapi` | The API: a `Service` interface with `Local` (in process) and `Client` (HTTP) implementations, the `Server` (Unix socket and TCP), and the node protocol's routes; the `Client` is also the node's `NodeTransport`. |
| `adapters/daemon` | The control plane as a service: the API, in-process runs for bots no node may run, reconciliation and offline nodes on a timer. |
| `adapters/nodeagent` | The node agent's loop: register, heartbeat, poll, run, renew, report. |
| `adapters/systemd` | Renders and installs the two units and the environment file; removes them. |
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
bin/axlr-bots install --axlr-bin /path/to/axlr-tui # a build with --exec; with --policy the bots' rules are enforced
systemctl --user enable --now axlr-bots.service axlr-node.service
loginctl enable-linger "$USER"                           # keeps the services running after you log out
axlr-bots node list                                 # the host, Online
axlr-bots bot register --file examples/bot.json
axlr-bots bot enable bot
```

| File | What |
|---|---|
| `~/.local/bin/axlr-bots`, `~/.local/bin/axlr-node` | the binaries (`install` copies the ones that run) |
| `~/.config/systemd/user/axlr-bots.service` | `Type=simple`, `ExecStart=… serve`, `Restart=on-failure`, `NoNewPrivileges=yes`, `EnvironmentFile=-~/.config/axlr-bots/env`, `WantedBy=default.target` |
| `~/.config/systemd/user/axlr-node.service` | `After=`/`Wants=axlr-bots.service`, `ExecStart=axlr-node --control unix://…/axlr-bots.sock --state ~/.local/state/axlr-node`, the host name as node id and `host=` label |
| `~/.config/axlr-bots/env` | `OPENROUTER_API_KEY=` (or a commented placeholder when the installer had none) and `AXLR_TUI=`; an existing file is never overwritten |
| `~/.local/state/axlr-bots/` | the control plane's state: bots, tasks, executions, nodes, policies, artifacts, logs, the socket |
| `~/.local/state/axlr-node/` | the node's state: the traces, policy files and workspaces of what it ran |

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
own files there. `uninstall --system` reverses it.

## The control plane

`axlr-bots serve` is the daemon (`Type=simple`, logs to the journal).

- It serves the API on a Unix socket, `<state>/axlr-bots.sock`, mode
  0660, and optionally on a TCP address (`--listen 127.0.0.1:7878`, or
  `0.0.0.0:7878` for nodes on other hosts). HTTP+JSON, because `net/http`
  is in the standard library and the JSON is the same the CLI prints.
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
| register `{id, hostName, labels, capabilities}` | `POST /v1/nodes` | node → control plane | per node id: a repeat describes the node anew |
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

`examples/demo.sh` runs the whole thing user-level: build, install, start
both services, show the node Online, register the example bot, submit a
generic task and watch the node run it, read the report, the approval
path with a harmless write in the bot's workspace, then restart both
services and show the node, the bots and the tasks are still there. The
installation stays running (`DEMO_UNINSTALL=1` removes it).

```bash
export OPENROUTER_API_KEY=...
AXLR_TUI=/path/to/axlr-tui sh examples/demo.sh
```

Run on 24 Oct 2026 on this machine (user `gx10a`, host `gx10-ff95`), with
`claude-haiku-5.5` through OpenRouter and an `axlr-tui` built from AXLR's
`claude/execution-policy` branch (`/home/gx10a/Documents/ai/tmp/axlr-tui-policy`):

```bash
set -a; . ~/.hermes/.env; set +a            # exports OPENROUTER_API_KEY
AXLR_TUI=/home/gx10a/Documents/ai/tmp/axlr-tui-policy sh examples/demo.sh
```

```text
## 0. build and install (user-level: ~/.local, ~/.config, systemctl --user)
  "mode": "user",
  "binary": "/home/gx10a/.local/bin/axlr-bots",
  "nodeBinary": "/home/gx10a/.local/bin/axlr-node",
    "/home/gx10a/.config/systemd/user/axlr-bots.service",
    "/home/gx10a/.config/systemd/user/axlr-node.service"
  "envFile": "/home/gx10a/.config/axlr-bots/env",
  "socket": "/home/gx10a/.local/state/axlr-bots/axlr-bots.sock",
    "systemctl --user enable --now axlr-bots.service axlr-node.service",
    "loginctl enable-linger gx10a  # keep the services running after logout"

## 1. start both services; the node registers itself
Created symlink …/default.target.wants/axlr-bots.service → …/axlr-bots.service.
Created symlink …/default.target.wants/axlr-node.service → …/axlr-node.service.
    "id": "gx10-ff95",
    "hostName": "gx10-ff95",
      "host": "gx10-ff95"
      "policySupported": true
    "status": "Online",

## 2. register the example bot (observe: AXLR's read tools, manual trigger, any node)
  "id": "bot",
  "level": "observe",
      "type": "manual"
  "status": "Enabled",

## 3. a generic task: the control plane assigns it to the node, the node runs it with AXLR
task task-72e656baa6bdbdb7ef4902e8 is Completed after about 8s
    "costUsd": 0.0013252499999999998
    "enforced": true,
  "nodeId": "gx10-ff95",
  "outcome": "Completed",
    "requests": 3,
    "runtimeLevel": "observe",
    "toolCalls": 2,
report:
    The workspace has no files. I listed it recursively, up to 8 levels deep, both with an explicit `.` path
    and with the default path, and both calls returned an empty list. The count is 0.

## 4. the approval path: a write runs only after task approve
task task-ed866c91d729455d701abf96 is WaitingApproval after about 4s
  "status": "WaitingApproval",
  "outcome": "DecisionRequired",
      "tool": "local_write",
      "callId": "toolu_01MKirykmsETWGzfZzsMAkmB",
      "arguments": {
  "nodeId": "gx10-ff95",
the file does not exist yet: nothing ran without approval
approving...
    "status": "Running",
    "kind": "decide",
    "nodeId": "gx10-ff95"
task task-ed866c91d729455d701abf96 is Completed after about 6s
    Nothing was missing: I re-read `hello-20261024T020356Z.txt` and it contains exactly `hello`, so the request is complete.
-rw------- 1 gx10a gx10a 5 Oct 24 04:04 /home/gx10a/.local/state/axlr-node/workspaces/bot-act-with-approval/hello-20261024T020356Z.txt
hello

## 5. restart both services: node, bots and tasks are still there
    "id": "gx10-ff95",
    "status": "Online",
    "id": "bot-act-with-approval",
    "status": "Enabled",
    "id": "bot",
    "status": "Enabled",
  "id": "task-72e656baa6bdbdb7ef4902e8",
  "status": "Completed",
  "id": "task-ed866c91d729455d701abf96",
  "status": "Completed",
service logs (last lines):
Stopped axlr-bots.service - AXLR Bots control plane.
Started axlr-bots.service - AXLR Bots control plane.
2026/10/24 04:04:16 daemon: serving on /home/gx10a/.local/state/axlr-bots/axlr-bots.sock, tick 2s
Started axlr-node.service - AXLR Bots node agent.
2026/10/24 04:04:16 node: registered as gx10-ff95 (gx10-ff95), axlr "" policy true

## installed and running
check:     systemctl --user status axlr-bots.service axlr-node.service
uninstall: /home/gx10a/.local/bin/axlr-bots uninstall [--purge]
```

Against the vision's criteria: the bot, the node and both tasks survived
the restart of both services (§14.1); the task was started by the API
(§14.2); AXLR received the policy and produced the result object, enforced
(§14.3, §14.5); the write ran only after `task approve` (§10.4); the
executions keep the node, the policy, the cost and the trace reference
(§12.3); nothing needed root (§9.1). The installation is left running on
this machine; `loginctl show-user gx10a -p Linger` says `yes`, so it
survives a logout. The `axlr ""` in the node's log is the capability the
agent reads from `axlr-tui --version`, which this build does not print.

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
  events.jsonl             audit.jsonl               axlr-bots.sock (while the daemon runs)
  policies/<exec-id>.json  artifacts/<task-id>/report.md
  traces/, workspaces/     (single-process runs; a node keeps its own under its state)
```

## Command reference

```text
axlr-bots [--state <dir>] [--axlr-bin <path>] [--socket <path>] [--actor <name>] <command>

  serve [--listen <addr>] [--tick 2s] [--reconcile-every 1m] [--grace <duration>]
  node list | node get <node-id>
  bot register --file <bot.json> | bot enable <bot-id> | bot list | bot get <bot-id>
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>]
  task run <task-id>                           in process (exit 0 completed, 3 decision required, 1 failed)
  task approve <task-id> | task deny <task-id> | task decide <task-id> approve|deny
  task get <task-id> | task report <task-id> [--path] | task reconcile [--grace <duration>]
  execution get <execution-id>
  install [--system] [--prefix <dir>] [--state <dir>] [--node-state <dir>] [--unit-dir <dir>] [--env-file <path>]
          [--axlr-bin <path>] [--node-binary <path>] [--destdir <dir>] [--no-systemctl]
  uninstall [--system] [--prefix <dir>] … [--purge]

axlr-node --control unix:///path/to/axlr-bots.sock | http://host:port
               [--node-id <id>] [--labels k=v,k=v] [--state <dir>] [--axlr-bin <path>] [--heartbeat 15s] [--poll 20s]
```

`--state` defaults to `$AXLR_BOTS_STATE`, then `$XDG_STATE_HOME/axlr-bots`,
then `~/.local/state/axlr-bots`; `--axlr-bin` to `$AXLR_TUI`,
then `axlr-tui` on `PATH`; `--socket` to `$AXLR_BOTS_SOCKET`, then
`<state>/axlr-bots.sock` (`--socket ""` never uses a daemon). The node
reads `AXLR_NODE_CONTROL`, `AXLR_NODE_ID`, `AXLR_NODE_LABELS`,
`AXLR_NODE_STATE` and `AXLR_TUI`; its id defaults to the host
name. Output is JSON on stdout; `task report` prints the report as text.

## Next: nodes on other hosts, with mTLS

The messages do not change. The control plane serves `--listen
0.0.0.0:7878` behind a TLS terminator (or `net/http` with a server
certificate and `ClientAuth: RequireAndVerifyClientCert`), each node gets a
client certificate whose common name is its node id, and the server takes
the node id from the certificate instead of the URL; `axlr-node
--control https://control.example:7878 --cert … --key …`. The leases,
the idempotent report and the outbound-only direction already assume a
network between the two.

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
expired approval), budgets and correlation enforced by the runtime, mTLS
and node authentication, `axlr-serve` as a second `RuntimeExecutor`, and a
real store (one JSON file per aggregate scans the directory for the
idempotency key, the Pending tasks and the Running executions). The
vision's §13 and §14 name them for Phase 2.
