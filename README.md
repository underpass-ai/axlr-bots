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
| `application` | Ports (`BotRepository`, `TaskRepository`, `ExecutionRepository`, `EventPublisher`, `AuditSink`, `RuntimeExecutor`) and use cases (`RegisterBot`, `EnableBot`, `SubmitTask`, `RunTask`, `GetTask`, `GetExecution`, …). |
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
- `Decide` runs `--session <id> --decide approve|deny [--call <id>]` for the
  approval path (no use case wires it yet).
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

Every package is above 80% coverage. The AXLR adapter is tested against a
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

## Command reference

```text
underpass-bots [--state <dir>] [--axlr-bin <path>] [--actor <name>] <command>

  bot register --file <bot.json>
  bot enable <bot-id>
  bot list
  bot get <bot-id>
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>]
  task run <task-id>
  task get <task-id>
  execution get <execution-id>
```

`--state` defaults to `$UNDERPASS_STATE`, then `$XDG_STATE_HOME/underpass-bots`,
then `~/.local/state/underpass-bots`. `--axlr-bin` defaults to
`$UNDERPASS_AXLR_BIN`, then `axlr-tui` on `PATH`. Output is JSON on stdout.

## What Phase 0 leaves out

Scheduler and triggers, the approval use case over `Decide`, reconciling a
task left `Running` by a crash, tool allow-list enforcement (needs the
contract's `policy`), budgets enforced by the runtime, `axlr-serve` as a
second `RuntimeExecutor`, and a real store. The vision's §13 and §14 name
them for Phase 1 and 2.
