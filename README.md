# underpass-bots

The control plane of **Underpass Bots**: persistent, governed bots that run
as infrastructure. The control plane decides *when, where, as whom and under
which permissions* a unit of work runs; **AXLR** decides *how* an agent
reasons and uses tools. This repository is the recommended experiment of
vision §18: an **SRE bot that lives on a Linux host under systemd**, is woken
by a failed unit, diagnoses the host with read-only commands and writes a
report; later, at a higher autonomy level, it fixes the unit once a person
approves.

```text
systemd unit fails ──OnFailure──▶ underpass-bots-alert@<unit>  ──socket──▶ underpass-bots.service (daemon)
                                   alert --unit <unit>                       bots · tasks · executions · policies
operator ──CLI──────────────────────────────────────────────────socket──▶    events.jsonl · audit.jsonl · artifacts/
                                                                                   │ one axlr-tui per execution
                                                                                   ▼
                                                            axlr-tui --exec --json --policy <file> --approve local|none
                                                            journalctl · systemctl status · df · free · … (policy-allowed)
```

What is here, in the order of the vision:

| Vision | Here |
|---|---|
| §9.1 one Linux server, systemd for survival | `underpass-bots install` writes `underpass-bots.service` (a dedicated user, `ProtectSystem=strict`, the state dir writable), `--user` mode for a test without root |
| §10.4 autonomy levels | a bot has a `level` (`observe`, `propose`, `act-with-approval`, `act-within-policy`) and `toolRules` that narrow its tools by arguments; both become AXLR's per-execution policy (`--policy`) |
| §12 Server Incident Bot | `examples/sre-host-bot.json` (observe: read-only host commands) and `examples/sre-host-fixer.json` (act-with-approval: `systemctl restart` as the only write) |
| §12.3 evidence | the diagnosis objective asks for timeline, services, commands run, hypotheses by evidence and next steps; the answer is the task's report file |
| §14.2 one automatic trigger | the `underpass-bots-alert@.service` template attached with an `OnFailure=` drop-in; `alert --unit` for a script or a person |
| §14.7 idempotent trigger | the alert's key is `alert:<unit>:<window start>` (10 min): a flapping unit makes one task |
| §14.6 no task left Running | the daemon reconciles on start and every minute; `task reconcile` by hand |
| §14.5 a forbidden tool is blocked outside the model | AXLR refuses the call before any card and the execution records `policy.enforced` |

## Layout (hexagonal)

| Package | Role |
|---|---|
| `domain` | Bot, Task, Execution aggregates; value objects validated in constructors; events (vision §11.2) under a schema version; audit entries. One file per type. |
| `application` | Ports (`BotRepository`, `TaskRepository`, `ExecutionRepository`, `EventPublisher`, `AuditSink`, `RuntimeExecutor`, `DecisionExecutor`, `TraceLocator`, `ArtifactStore`) and use cases (`RegisterBot`, `EnableBot`, `SubmitTask`, `SubmitAlert`, `RunTask`, `RunNextPending`, `ApproveTask`, `ReconcileTasks`, `GetTask`, `GetReport`, `GetExecution`, …); `SerialRunner` runs one execution at a time. |
| `adapters/filestore` | One JSON file per aggregate under the state directory, written atomically (temp file, fsync, rename); the report artifacts. |
| `adapters/jsonl` | Append-only `events.jsonl` and `audit.jsonl`. |
| `adapters/axlr` | Runs `axlr-tui --exec --json [--policy <file>]` as a subprocess and reads the execution result object; keeps the raw lines as the trace and the policy file as evidence. |
| `adapters/unixapi` | The local API: a `Service` interface with two implementations, `Local` (in process) and `Client` (HTTP over the daemon's Unix socket), and the `Server`. |
| `adapters/daemon` | The service: the socket API, a worker that runs Pending tasks one at a time, reconciliation on a timer. |
| `adapters/systemd` | Renders and installs the units and the environment file; removes them. |
| `adapters/system` | Wall clock, random ids. |
| `compose` | The composition root the command and the daemon share. |
| `cmd/underpass-bots` | The command: `serve`, `install`, `alert`, `bot …`, `task …`, `execution …`. |
| `internal/fakeaxlr` | A shell stand-in for `axlr-tui` the tests run. |

## The runtime contract

The adapter speaks the contract in AXLR's `docs/runtime-contract.md`:

- Request: `--exec "<objective>"`, `--model` from the bot's model policy,
  `--root <state>/workspaces/<bot>`, `--approve none`, a timeout from the
  bot's budgets (default 15 min).
- Result: the last JSON line, `{"type":"result","result":{"schema":"axlr.execution-result/v1",…}}`,
  gives status (`completed`, `needs_decision`, `interrupted`, `failed`),
  answer, sessions, pending call, tool calls, usage and artifacts.
- Fallback for an `axlr-tui` older than the contract: exit code (0 completed,
  3 decision required, 1 failed) plus the `completed`, `failed` and
  `decision.required` lines.
- Decision: `--session <id> --decide approve|deny [--call <id>] --json`, the
  session and call the previous execution's `pendingDecision` names, with the
  same result object at the end. `ApproveTask` (`task decide`) drives it.
- Policy: `--policy <file>` with the bot's level and tool rules (below).
  The adapter reads `axlr-tui --help` once; a binary without `-policy`
  (0.13.0, the contract build) gets no flag, and the execution records
  `policy.enforced: false`.
- Not yet taken by AXLR: budgets and correlation (the contract's reserved
  `budget` and `correlation` fields; see the TODO in `adapters/axlr/executor.go`).

Outcome mapping: `completed` → execution `Completed`, task `Completed`;
`needs_decision` → `DecisionRequired`, task `WaitingApproval`; `failed`,
`interrupted` and timeouts → `Failed`, task `Failed`.

## Autonomy level and tool rules

A bot definition has a `level` (vision §10.4) and `toolRules`:

```json
"level": "observe",
"allowedTools": ["local_read", "local_search", "local_list", "local_exec"],
"toolRules": {
  "allow": [
    {"tool": "local_exec", "arguments": {"program": "journalctl"}},
    {"tool": "local_exec", "arguments": {"program": "systemctl", "args": "[\"status\"**"}}
  ],
  "deny": [
    {"tool": "local_exec", "arguments": {"program": "journalctl", "args": "**--vacuum**"}},
    {"tool": "local_write"}
  ]
}
```

A rule names a tool and patterns its arguments must match, in the words of
AXLR's contract: a glob over each argument's value as text (`*` within a
path segment, `**` across segments, `?` one character; an array such as
`args` is matched as its compact JSON, so `["status"**` means "the first
argument is `status`"). A tool with allow rules may only be called as a rule
says; a tool without rules is allowed as a whole; a deny rule wins. Every
allow rule must name a tool in `allowedTools`.

Before each execution the adapter writes `<state>/policies/<execution>.json`
and passes it with `--policy`:

| Bot level | AXLR level | `--approve` | Why |
|---|---|---|---|
| `observe` without `local_exec` | `observe` | `none` | reads only |
| `observe` with `local_exec` | `act-within-policy` | `local` | AXLR's `observe` forbids every command whatever its arguments; the allow list still holds only reads and the commands the rules name, and the deny list closes their write forms |
| `propose` | `propose` | `local` | writes of a plan or a diff only |
| `act-with-approval` | `act-with-approval` | `none` | every allowed command or write is a person's decision (`needs_decision`), the read-only commands too: AXLR has no notion of a read-only command |
| `act-within-policy` | `act-within-policy` | `local` | the policy decides |

Without `-policy` in the binary the approval level stays `none` whatever the
level: nothing runs without a person (fail closed), and the policy file is
kept as a record. The execution shows all of it:

```json
"policy": {"level": "observe", "runtimeLevel": "act-within-policy", "enforced": true, "path": "…/policies/exec-….json"}
```

A forbidden call is refused by AXLR before any card: the model reads
`forbidden by the execution's policy: …` as the tool result, the call is
`forbidden` in `result.toolCalls` and the run goes on to its answer (vision
§14.5, seen live below).

## Build and test

```bash
GOFLAGS=-p=2 go build ./... && GOFLAGS=-p=2 go test -cover ./...
```

Every package has at least 80% coverage. The AXLR adapter is tested against a
helper process that imitates `axlr-tui`, the command, the API and the daemon
against `internal/fakeaxlr` (a shell stand-in that answers `--help`, records
its arguments and plays a scenario); the installer renders into a `--destdir`
and checks the unit files. One test runs the real binary only when
`AXLR_LIVE=1` is set:

```bash
AXLR_LIVE=1 UNDERPASS_AXLR_BIN=/path/to/axlr-tui GOFLAGS=-p=2 go test ./adapters/axlr -run TestLiveExec -v
```

It needs an `axlr-tui` with `--exec` (the installed 0.13.0 does not have it;
build one from AXLR `main` or use the contract build) and a model provider
for it (`OPENROUTER_API_KEY` or `~/.config/axlr/credentials.json`).

## Install on a Linux host (systemd)

The command installs itself: the binary, the two units and the environment
file for the model key. Nothing goes into a unit that a `systemctl cat`
would show to everyone.

```bash
GOFLAGS=-p=2 go build -o underpass-bots ./cmd/underpass-bots
export OPENROUTER_API_KEY=...                        # copied into the env file, mode 0600, never printed
sudo -E ./underpass-bots install --prefix /usr/local --state /var/lib/underpass-bots --axlr-bin /usr/local/bin/axlr-tui
sudo systemctl enable --now underpass-bots.service
sudo -u underpass-bots underpass-bots --state /var/lib/underpass-bots bot register --file examples/sre-host-bot.json
sudo -u underpass-bots underpass-bots --state /var/lib/underpass-bots bot enable sre-host-bot
```

| File | What |
|---|---|
| `/usr/local/bin/underpass-bots` | the binary (`install` copies the one that runs) |
| `/etc/systemd/system/underpass-bots.service` | `Type=simple`, `User=underpass-bots` (created by `useradd --system`, in `systemd-journal` to read the journal), `EnvironmentFile=-/etc/underpass-bots/env`, `ExecStart=… serve`, `Restart=on-failure`, `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=yes`, `StateDirectory=underpass-bots` and `ReadWritePaths=` the state dir, `HOME=` the state dir so `axlr-tui` writes its own files there |
| `/etc/systemd/system/underpass-bots-alert@.service` | `Type=oneshot`, same user and env file, `ExecStart=… alert --unit %i` |
| `/etc/underpass-bots/env` | `OPENROUTER_API_KEY=` (or a commented placeholder when the installer had none), `UNDERPASS_AXLR_BIN=`, `UNDERPASS_BOT=sre-host-bot`; an existing file is never overwritten |
| `/var/lib/underpass-bots/` | the state: bots, tasks, executions, policies, artifacts, traces, logs and the socket |

`--user` installs the same under `~/.local/bin`, `~/.config/systemd/user`,
`~/.config/underpass-bots/env` and `~/.local/state/underpass-bots`, for
`systemctl --user`: no root, no dedicated user, and no `ProtectSystem=`
(a user manager cannot sandbox the filesystem; the unit says so). The
environment file adds `UNDERPASS_SYSTEMD_USER=1` so `alert` queries the user
manager. `--destdir <dir>` renders everything under a directory
(packaging, tests), `--no-systemctl` writes the files and runs nothing.

```bash
underpass-bots uninstall [--user] [--prefix …]   # disable --now, remove units and binary, daemon-reload
underpass-bots uninstall --user --purge           # also the state directory and the env file (the key)
```

Known needs of the system mode: `dmesg` wants root or
`kernel.dmesg_restrict=0` (the bot reports the refusal and goes on); a
stop of the service sends SIGTERM to a running `axlr-tui`, whose execution
ends `Failed` and is reported as such (`TimeoutStopSec=30s`).

## The service and its socket

`underpass-bots serve` is the daemon (`Type=simple`, logs to the journal):

- It serves the local API on a Unix socket, `<state>/underpass-bots.sock`,
  mode 0660: `POST /v1/tasks`, `POST /v1/alerts`, `POST /v1/tasks/{id}/run`,
  `POST /v1/tasks/{id}/decide`, `GET /v1/tasks/{id}`, `GET /v1/tasks/{id}/report`,
  `GET /v1/executions/{id}`, `GET|POST /v1/bots`, `POST /v1/bots/{id}/enable`,
  `GET /healthz`. HTTP over the socket because `net/http` is in the standard
  library and the JSON is the same the command prints.
- Every command tries the socket first (`--socket`, `UNDERPASS_SOCKET`,
  default `<state>/underpass-bots.sock`). When a daemon answers, the command
  is carried by it: a `task submit` from the alert unit reaches the running
  service, which runs the task, and never touches the state files itself.
  When nothing answers, the command works on the state directory as in
  Phase 0 (and `alert` says so on stderr): the task is spooled.
- The state directory is the spool. Every 2 s (`--tick`) the worker runs
  the oldest Pending task, one at a time (`SerialRunner`: one `axlr-tui`
  per host at a time, and two callers never start the same task). A task
  spooled while the daemon was down runs on its first tick after the
  restart; nothing that was saved is lost.
- On start and every minute (`--reconcile-every`) it reconciles the tasks a
  dead run left Running, with the same grace as `task reconcile` (the bot's
  timeout plus a minute), so a task another process may still be running is
  not failed under it.

## The trigger: a unit fails

`underpass-bots-alert@.service` is a template: `underpass-bots-alert@foo.service`
runs `underpass-bots alert --unit foo`, which submits the diagnosis task for
`foo.service` to the running service. Attach it to any unit with a drop-in
(`systemctl edit foo.service`, or `systemctl --user edit` for a user unit):

```ini
[Unit]
OnFailure=underpass-bots-alert@%N.service
```

`%N` is the unit's name without its type, so `%i` in the alert unit has no
`.service` suffix and `alert` adds it back. `OnFailure=` fires when the unit
enters `failed`: a unit with `Restart=` fires it only once its restarts are
exhausted (`StartLimitBurst`), not on every attempt.

The task's idempotency key is `alert:<unit>:<start of the 10-minute window>`
(`--window`): a flapping unit, a redelivered event or an operator repeating
`alert --unit foo` within the window all name the same task, with
`"created": false`. The objective is the §12.3 request below, with the unit,
the host and the time filled in.

```bash
underpass-bots alert --unit foo [--bot sre-host-bot] [--host $(hostname)] [--at 2026-10-24T10:37:12Z] [--window 10m] [--user]
```

## The diagnosis report

The objective (`application.DiagnosisObjective`) names the unit, the host
and the time, forbids changes and asks for Markdown with exactly these
sections: **1. Timeline**, **2. Services involved**, **3. Commands run**
(each with what it showed), **4. Hypotheses, ordered by evidence**,
**5. Proposed next steps**. When the execution completes, the answer is
written to `<state>/artifacts/<task-id>/report.md`, listed in the
execution's `result.artifacts` as `report.md` and named in the
`bot.task.completed` event and the daemon's log line.

```bash
underpass-bots task report <task-id>          # the text
underpass-bots task report --path <task-id>   # only where it is
```

## Fixing under approval

`examples/sre-host-fixer.json` is the same bot one level up, vision §10.4
**act-with-approval**: `local_exec` is allowed for `systemctl restart`,
`systemctl status`, `systemctl is-active` and `journalctl`, and nothing
else is written. AXLR runs it under `act-with-approval` with
`--approve none`: every allowed command is a card, the run stops at the
first one with `needs_decision`, the task is `WaitingApproval` and the
execution's `result.pendingDecision` names the call.

```bash
$ub task submit sre-host-fixer --objective "Restart demo-broken.service" --key fix-1
$ub task run task-…          # exit 3: WaitingApproval, pendingDecision.tool = local_exec
$ub task approve task-…      # axlr-tui --session <id> --decide approve --policy <file>; the restart runs; exit 0
$ub task deny task-…         # the session ends without the write
```

`TestTheWriteNeverRunsWithoutTheApproval` (`cmd/underpass-bots`) proves it
against the stand-in: the first run passes `--approve none` and a policy at
`act-with-approval`, the marker the fake creates when it runs the restart is
absent after the run and after a refused second run, present only after
`task approve`, and absent after `task deny`. Under this level the
read-only commands are cards too: AXLR has no notion of a read-only
command, so the recommended flow is to diagnose with the observe bot and
fix with the fixer under a narrow objective.

## Host demo (user mode, live)

`examples/host-demo.sh` runs the whole experiment of vision §18 on this
machine without root: install as a user service, start it, register the
bot, break a throwaway unit that has the OnFailure drop-in, watch the task
appear and run, read the report, restart the control plane and show the bot
and the task are still there, then uninstall.

```bash
export OPENROUTER_API_KEY=...
UNDERPASS_AXLR_BIN=/path/to/axlr-tui sh examples/host-demo.sh    # a build with --policy enforces the rules
```

Run on 24 Oct 2026 with `claude-haiku-5.5` through OpenRouter and an
`axlr-tui` built from AXLR's `claude/execution-policy` branch (key lines;
the unit's `ExecStart` exits 1 after logging that `/etc/demo/app.conf` is
missing):

```text
## 0. build and install (user mode)
  "mode": "user",
  "binary": "/home/gx10a/.local/bin/underpass-bots",
    "/home/gx10a/.config/systemd/user/underpass-bots.service",
    "/home/gx10a/.config/systemd/user/underpass-bots-alert@.service"
  "envFile": "/home/gx10a/.config/underpass-bots/env",
  "socket": "/home/gx10a/.local/state/underpass-bots/underpass-bots.sock",

## 1. start the service and register the bot
Created symlink …/default.target.wants/underpass-bots.service → …/underpass-bots.service.
  "id": "sre-host-bot",
  "level": "observe",
  "status": "Enabled",

## 2. a throwaway unit that fails, with the OnFailure drop-in attached
[Unit]
OnFailure=underpass-bots-alert@%N.service
Job for underpass-demo-broken.service failed because the control process exited with error code.
underpass-demo-broken.service failed, as intended (exit 1)

## 3. the task the alert created (the same alert again names the same task)
  "created": false,
    "id": "task-9b6621bbe0d2191460ea8ad5",
    "status": "Running",
    "idempotencyKey": "alert:underpass-demo-broken.service:2026-10-24T01:20:00Z",

## 4. the service runs it through AXLR (policy: observe = read-only host commands)
task task-9b6621bbe0d2191460ea8ad5 is Completed after about 42s
  "outcome": "Completed",
    "requests": 3,
    "toolCalls": 14,
    "costUsd": 0.00808493
    "runtimeLevel": "act-within-policy",
    "enforced": true,
commands the bot ran (from the execution's tool calls):
      7  status: completed   name: local_exec
      3  status: failed      name: local_exec
      3  status: forbidden   name: local_exec
report file: /home/gx10a/.local/state/underpass-bots/artifacts/task-9b6621bbe0d2191460ea8ad5/report.md

## 5. the report
# Diagnosis: underpass-demo-broken.service (host gx10-ff95)

No changes were made. Every command was read-only. Four commands were refused by the execution policy and are noted below.

## 1. Timeline
- **03:25:02**: `systemd[2559]` starts `underpass-demo-broken.service` …
- **03:25:02**: `sh[2874465]: demo-app: cannot open /etc/demo/app.conf: No such file or directory`
- **03:25:02**: `sh[2874465]: demo-app: refusing to start without a configuration`
- **03:25:02**: `systemd[2559]: underpass-demo-broken.service: Main process exited, code=exited, status=1/FAILURE`
- **03:25:02**: `systemd[2559]: underpass-demo-broken.service: Triggering OnFailure= dependencies.`

## 3. Commands run
1. `systemctl --user status underpass-demo-broken.service --no-pager`: exit 1, `Failed to connect to bus: No medium found`. …
2. `journalctl --user -u underpass-demo-broken.service --no-pager -n 200`: 7 lines, the full failure sequence shown in section 1.
4. `uptime`: `03:25:11 up 8 days, 9:42, 1 user, load average: 0.66, 0.89, 1.06`. Load is low.
5. `free -h`: 121Gi total, 32Gi used, 89Gi available. … Memory is not the cause.
6. `df -h`: `/` is 916G, 620G used, 249G available (72%). Disk is not the cause.
10. `dmesg --nopager`: refused by the kernel: `dmesg: read kernel buffer failed: Operation not permitted`.
11. `env XDG_RUNTIME_DIR=/run/user/1000 systemctl --user status …`: **refused by execution policy** …
14. `ls -la /etc/demo`: **refused by execution policy**. I could not check whether the directory exists …

## 4. Hypotheses, ordered by evidence
**H1 (strongest): the configuration file /etc/demo/app.conf is missing, so demo-app exits with status 1 at startup.**
- For: the log names the exact path and error … The failure is in the first second of the run …
**H3: a host-level problem (memory, disk, load or kernel fault) caused the failure.**
- Against: 89Gi memory available, 249G disk free, load under 1. No OOM, I/O or kernel warning …

## 5. Proposed next steps
These are for an operator to do. I did not do them.
2. Confirm the state of `/etc/demo/app.conf` directly …
4. If it is not intentional, restore the configuration file from its source, then restart the unit yourself.

## 6. restart the control plane: the bot and the task are still there
  "id": "sre-host-bot",
  "status": "Enabled",
  "id": "task-9b6621bbe0d2191460ea8ad5",
  "status": "Completed",
Started underpass-bots.service - Underpass Bots control plane (SRE host bot).
2026/10/24 03:25:47 daemon: serving on /home/gx10a/.local/state/underpass-bots/underpass-bots.sock, tick 2s

## cleanup
  "mode": "user",
kept the state (/home/gx10a/.local/state/underpass-bots) and the env file …; remove them with: underpass-bots uninstall --user --purge
```

What the run shows against the vision's criteria: the trigger was the
failed unit (§14.2), the repeated alert named the same task (§14.7), the
three forbidden calls (`env …`, `date`, `ls /etc/demo`) were refused by
AXLR outside the model and the run went on to its answer (§14.5), the bot
changed nothing (§14.12), the bot and the task survived the restart
(§14.1), and the cost and the tool calls are in the execution (§12.3). A
second run of the same script finds the bot already registered and the
state kept.

Two limits seen in the run, both in user mode: `systemctl --user` cannot
reach the user manager from inside an execution because AXLR hands a
command only `PATH` and `HOME` (no `XDG_RUNTIME_DIR`), so the bot has the
unit's journal but not its status; and `ls /etc/demo` was refused because
the observe policy only allows `ls` under `/var/log`. The first is for
AXLR (pass the bus variables through, or let the policy name environment
variables); the second is the policy doing its job, and a rule can widen
it. In system mode `systemctl status` needs no bus variable.

## Next: the same bot as a sidecar

Vision §9.2: the bot moves next to the service it watches, as a container
beside it. What changes, and what does not:

- **No systemd.** The control plane runs as the sidecar's main process
  (`underpass-bots serve`); the container runtime restarts it. `install`,
  the units and the environment file are replaced by the image and its
  environment (`OPENROUTER_API_KEY`, `UNDERPASS_AXLR_BIN`); the state
  directory becomes a volume so the bot and its tasks survive the
  container.
- **Shared volumes instead of the host.** The service's logs and
  configuration are mounted read-only into the sidecar; the bot's policy
  allows `tail`, `head` and `ls` on those mounts instead of `/var/log`, and
  `journalctl` and `systemctl` disappear with the host.
- **The trigger becomes a webhook.** No `OnFailure=`: the alert source
  (the orchestrator's events, a health check, Alertmanager) calls
  `POST /v1/alerts` with `{"unit": "<service>", "at": …}`, so the socket
  grows a TCP listener for the pod network, with authentication. The
  idempotency key stays the same, so a storm of alerts is one task.
- **Fixing under approval stays.** `systemctl restart` becomes the
  orchestrator's restart (an MCP tool or an API call the policy names),
  still under `act-with-approval`, and `task approve` is still the only
  way it runs.
- **Unchanged:** the bot definition's level and rules, the policy file per
  execution, the report artifact, the events and the audit log, the
  reconciliation, and the AXLR contract.

## Phase 0 demo (offline)

`examples/phase0-demo.sh` runs the whole script below with no daemon. Without
`UNDERPASS_AXLR_BIN` it uses `examples/fake-axlr-tui`, a shell stand-in that
prints a finished turn and its result object, so the demo runs offline.

```bash
sh examples/phase0-demo.sh
```

Step by step, with a real runtime:

```bash
export UNDERPASS_STATE=$(mktemp -d)                # or ~/.local/state/underpass-bots
export UNDERPASS_AXLR_BIN=/path/to/axlr-tui        # one with --exec; default axlr-tui
ub="go run ./cmd/underpass-bots"

# 1. register the bot of vision §11
$ub bot register --file examples/server-incident-investigator.json

# 2. restart: a second invocation reads the bot back from disk and enables it
$ub bot enable server-incident-investigator
$ub bot get server-incident-investigator        # "status": "Enabled"

# 3. submit a task with an idempotency key
$ub task submit server-incident-investigator \
    --objective "Investigate an HTTP 500 spike in staging" \
    --key alert-2026-10-23-001                  # "created": true, "id": "task-…"

# 4. run it once through AXLR (exit 0 completed, 3 decision required, 1 failed)
$ub task run task-…

# 4b. exit 3 means the task is WaitingApproval on a tool call: decide it, and
#     the same session runs to its end under a second execution
$ub task get task-…                             # "status": "WaitingApproval"
$ub task decide task-… approve                  # or deny; same exit codes as task run

# 5. read the result: outcome, answer, usage; the trace stays in traces/
$ub execution get exec-…

# 6. resubmit the same key: same task back, "created": false, no new execution
$ub task submit server-incident-investigator \
    --objective "Investigate an HTTP 500 spike in staging" \
    --key alert-2026-10-23-001
$ub task run task-…                             # refused: task is Completed
ls $UNDERPASS_STATE/executions                  # still one file
```

State directory layout:

```text
<state>/
  bots/<bot-id>.json          tasks/<task-id>.json     executions/<exec-id>.json
  events.jsonl                audit.jsonl              underpass-bots.sock (while the daemon runs)
  traces/<exec-id>.jsonl      traces/<exec-id>.stderr.log
  policies/<exec-id>.json     (the --policy file AXLR was given)
  artifacts/<task-id>/report.md
  workspaces/<bot-id>/        (the --root AXLR runs in)
```

## Approval

The run's approval level is `--approve none`: AXLR runs nothing the settings
do not already allow, and a held call ends the execution in
`needs_decision`. The control plane then shows a `WaitingApproval` task whose
execution is `DecisionRequired` with `result.pendingDecision`
(`tool`, `callId`, `sessionId`), and the runtime session keeps the call.

```bash
$ub task approve task-…           # or task deny; task decide task-… approve|deny is the same
```

`task approve` appends a new execution to the task, moves it to `Running`
before anything runs, hands the decision to the session and settles the
outcome exactly as `task run` does: `Completed`, `Failed`, or
`WaitingApproval` again when the next call is held too (then decide again).
Events: `bot.task.decided` `{decision, callId, sessionId, executionId}`,
then `bot.task.completed`, `bot.task.failed` or `bot.task.waiting_approval`.
Audit action: `task.decide`. A wrong word, a task that is not
`WaitingApproval` or a paused bot is refused before anything changes.

## Recovery

`task run` writes the task and its execution as `Running` *before* it calls
the runtime, on purpose: if the process dies (kill, OOM, power loss), the
state directory shows a `Running` task with a `Running` execution and no
`endedAt`, instead of hiding the loss. Nothing else ends them, so the vision's
§14.6 rule, no task left `Running` forever, is kept by an explicit command:

```bash
$ub task reconcile                # {"failed": ["task-…"], "skipped": []}
$ub task reconcile --grace 30s    # a shorter grace for a known-dead process
```

It reads every `Running` task and fails those whose execution started more
than a grace period ago, by default the bot's task timeout plus one minute:
the execution ends `Failed` with the reason `reconciled: the control plane
found it Running after <age>; the process that ran it is gone` (its trace
under `traces/` is kept when the file exists), the task moves to `Failed`
with the same reason, a `bot.task.failed` event with `"reason": "reconciled"`
is published and a `task.reconcile` audit entry is recorded. Younger tasks
may still be running in another process and are reported as `skipped`.
Running it again is harmless. A task whose execution had already ended when
the process died (between the two saves) is failed too, with a reason that
names the execution's outcome.

## Command reference

```text
underpass-bots [--state <dir>] [--axlr-bin <path>] [--socket <path>] [--actor <name>] <command>

  serve [--tick 2s] [--reconcile-every 1m] [--grace <duration>]
  alert --unit <name> [--bot <id>] [--host <name>] [--at <RFC3339>] [--window 10m] [--user] [--correlation <id>]
  bot register --file <bot.json>
  bot enable <bot-id>
  bot list
  bot get <bot-id>
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>]
  task run <task-id>
  task approve <task-id> | task deny <task-id> | task decide <task-id> approve|deny
  task get <task-id>
  task report <task-id> [--path]
  task reconcile [--grace <duration>]
  execution get <execution-id>
  install [--user] [--prefix <dir>] [--state <dir>] [--unit-dir <dir>] [--env-file <path>] [--axlr-bin <path>] [--destdir <dir>] [--no-systemctl]
  uninstall [--user] [--prefix <dir>] … [--purge]
```

`--state` defaults to `$UNDERPASS_STATE`, then `$XDG_STATE_HOME/underpass-bots`,
then `~/.local/state/underpass-bots`. `--axlr-bin` defaults to
`$UNDERPASS_AXLR_BIN`, then `axlr-tui` on `PATH`. `--socket` defaults to
`$UNDERPASS_SOCKET`, then `<state>/underpass-bots.sock`; `--socket ""` never
uses a daemon. `alert` reads `UNDERPASS_BOT` and `UNDERPASS_SYSTEMD_USER=1`
(the env file sets them). Output is JSON on stdout; `task report` prints
the report as text.

## What is left out

An approval queue beyond `task approve` (who may decide, and when; an
expired approval), budgets and correlation enforced by the runtime (the
contract's reserved fields), more triggers than `OnFailure=` (timers,
webhooks: the sidecar's), `axlr-serve` as a second `RuntimeExecutor`, a
TCP listener with authentication, and a real store (one JSON file per
aggregate scans the directory for the idempotency key and for Pending
tasks). The vision's §13 and §14 name them for Phase 2.
