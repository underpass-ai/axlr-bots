# underpass-bots

The stub control plane of **Underpass Bots**: persistent, governed bots that
run as infrastructure. The control plane decides *when, where, as whom and
under which permissions* a unit of work runs; **AXLR** decides *how* an agent
reasons and uses tools. This repository is Phase 0 of the roadmap: a bot that
survives a restart, a task run once through the runtime contract, and a
result read without reading traces.

```text
operator ──CLI──▶ control plane (this repo) ──RuntimeExecutor──▶ axlr-tui --exec --json
                   bots · tasks · executions                        one process per execution
                   events.jsonl · audit.jsonl                       result object at the end
```

## Layout (hexagonal)

| Package | Role |
|---|---|
| `domain` | Bot, Task, Execution aggregates; value objects validated in constructors; events (vision §11.2) under a schema version; audit entries. One file per type. |
| `application` | Ports (`BotRepository`, `TaskRepository`, `ExecutionRepository`, `EventPublisher`, `AuditSink`, `RuntimeExecutor`, `DecisionExecutor`, `TraceLocator`) and use cases (`RegisterBot`, `EnableBot`, `SubmitTask`, `RunTask`, `ApproveTask`, `ReconcileTasks`, `GetTask`, `GetExecution`, …). |
| `adapters/filestore` | One JSON file per aggregate under the state directory, written atomically (temp file, fsync, rename). |
| `adapters/jsonl` | Append-only `events.jsonl` and `audit.jsonl`. |
| `adapters/axlr` | Runs `axlr-tui --exec --json --approve none` as a subprocess and reads the execution result object; keeps the raw lines as the trace. |
| `adapters/system` | Wall clock, random ids. |
| `cmd/underpass-bots` | The command. |

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
- Not yet enforced by AXLR: the bot's tool allow-list, budgets and
  correlation. They are recorded in the execution request and will be passed
  as the contract's reserved `policy`, `budget` and `correlation` fields
  (see the TODO in `adapters/axlr/executor.go`).

Outcome mapping: `completed` → execution `Completed`, task `Completed`;
`needs_decision` → `DecisionRequired`, task `WaitingApproval`; `failed`,
`interrupted` and timeouts → `Failed`, task `Failed`.

## Build and test

```bash
GOFLAGS=-p=2 go build ./... && GOFLAGS=-p=2 go test -cover ./...
```

Every package has at least 80% coverage. The AXLR adapter is tested against a
helper process that imitates `axlr-tui`; one test runs the real binary only
when `AXLR_LIVE=1` is set:

```bash
AXLR_LIVE=1 UNDERPASS_AXLR_BIN=/path/to/axlr-tui GOFLAGS=-p=2 go test ./adapters/axlr -run TestLiveExec -v
```

It needs an `axlr-tui` with `--exec` (the installed 0.13.0 does not have it;
build one from AXLR `main` or use the contract build) and a model provider
for it (`OPENROUTER_API_KEY` or `~/.config/axlr/credentials.json`).

## Phase 0 demo

`examples/phase0-demo.sh` runs the whole script below. Without
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
  events.jsonl                audit.jsonl
  traces/<exec-id>.jsonl      traces/<exec-id>.stderr.log
  workspaces/<bot-id>/        (the --root AXLR runs in)
```

## Approval

The run's approval level is `--approve none`: AXLR runs nothing the settings
do not already allow, and a held call ends the execution in
`needs_decision`. The control plane then shows a `WaitingApproval` task whose
execution is `DecisionRequired` with `result.pendingDecision`
(`tool`, `callId`, `sessionId`), and the runtime session keeps the call.

```bash
$ub task decide task-… approve    # or deny
```

`task decide` appends a new execution to the task, moves it to `Running`
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
underpass-bots [--state <dir>] [--axlr-bin <path>] [--actor <name>] <command>

  bot register --file <bot.json>
  bot enable <bot-id>
  bot list
  bot get <bot-id>
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>]
  task run <task-id>
  task decide <task-id> approve|deny
  task get <task-id>
  task reconcile [--grace <duration>]
  execution get <execution-id>
```

`--state` defaults to `$UNDERPASS_STATE`, then `$XDG_STATE_HOME/underpass-bots`,
then `~/.local/state/underpass-bots`. `--axlr-bin` defaults to
`$UNDERPASS_AXLR_BIN`, then `axlr-tui` on `PATH`. Output is JSON on stdout.

## What Phase 0 leaves out

Scheduler and triggers (reconciliation is a command, not a loop), an
approval queue beyond `task decide` (who may decide, and when), tool
allow-list enforcement (needs the contract's `policy`), budgets enforced by
the runtime, `axlr-serve` as a second `RuntimeExecutor`, and a real store. The vision's §13 and §14 name
them for Phase 1 and 2.
