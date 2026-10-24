# The runtime contract

How a node (or the control plane in single-process mode) runs AXLR and reads what came of it. The adapter speaks the contract in AXLR's [`docs/runtime-contract.md`](https://github.com/underpass-ai/AXLR/blob/main/docs/runtime-contract.md): the control plane decides *when, where, as whom and under which permissions* a unit of work runs; AXLR decides *how* the agent reasons and uses tools. One execution is one AXLR session (plus the successors its context cap opens); the session stays on the host that ran it, so a later execution can continue it with a decision.

## The request

The adapter runs `axlr-tui` as a subprocess:

| Input | Flag | From |
|---|---|---|
| Prompt | `--exec "<objective>"` | the task's objective |
| Events | `--json` | always: the turn's events as JSON lines, kept as the trace |
| Model | `--model <id>` | the bot's model policy |
| Workspace | `--root <state>/workspaces/<bot>` | one directory per bot on the host |
| Approval level | `--approve none\|local` | the policy table below |
| Policy | `--policy <state>/policies/<execution>.json` | the bot's level and tools, when the build takes the flag |
| Correlation | `--correlation <state>/correlation/<execution>.json` | the four ids, when the build takes the flag |
| Budget | `--budget <file>` | the bot's budgets, when the build takes the flag |
| Timeout | the process is stopped | the bot's `taskTimeoutSeconds` (15 minutes when absent) |
| Decision | `--session <id> --decide approve\|deny [--call <id>] --policy <file> --json` | an approval or denial of the pending call |

The adapter reads `axlr-tui --help` once to learn which flags the build takes (`-policy`, `-correlation`, `-budget`, `-execution`) and `--version` for the node's capabilities. With `-correlation` it passes `--correlation` (and `--budget`); with `-execution` alone it passes one `--execution <bundle>` holding the policy, the budget and the correlation in place of `--policy`; without any of them the files are still written as evidence and the execution says `"carried": false`. AXLR v0.14.0 takes `--policy`, `--correlation` and `--budget`.

## The policy

Before each execution the adapter writes `<state>/policies/<execution>.json` with the bot's level and its allowed tools and rules, in the contract's words, and passes it with `--policy`:

| Bot level | AXLR level | `--approve` | Why |
|---|---|---|---|
| `observe` without `local_exec` | `observe` | `none` | reads only |
| `observe` with `local_exec` | `act-within-policy` | `local` | AXLR's `observe` forbids every command whatever its arguments; the allow list still holds only reads and the commands the rules name |
| `propose` | `propose` | `local` | writes of a plan or a diff only |
| `act-with-approval` | `act-with-approval` | `none` | every allowed write or command is a person's decision (`needs_decision`) |
| `act-within-policy` | `act-within-policy` | `local` | the policy decides |

Without `-policy` in the binary the approval level stays `none`: nothing runs without a person (fail closed), and the policy file is kept as a record. The execution shows `"policy": {"level", "runtimeLevel", "enforced", "path"}`. A forbidden call is refused by AXLR before any card: the model reads `forbidden by the execution's policy: …` as the tool result, the call is `forbidden` in `result.toolCalls` and the run goes on (vision §14.5).

## Correlation

The control plane generates every id: `bot_id` at registration, `task_id` and a `trace_id` (`trace-…`, the task's `correlationId`; one from upstream is kept instead) at submission, `execution_id` at each attempt. The task stores the trace id, each execution a `correlation` record (`botId`, `taskId`, `executionId`, `traceId`, `carried`, `path`), and every event names the bot, the task, the execution and the trace, so an execution is reconstructed from any of them (vision §14.8). The adapter writes `<state>/correlation/<execution>.json` with the contract's object, `{"correlation": {"bot_id", "task_id", "execution_id", "trace_id"}}`, for every run. When the build takes `--correlation`, every event line and the result carry the four ids unchanged; the node reports `correlationSupported` in its capabilities.

## The budget

`{"budget": {"seconds", "requests", "tool_calls"}}` from the bot's budgets (the timeout, `maxModelCallsPerTask`, `maxToolCallsPerTask`), passed with `--budget` when the build takes it. A run that reaches a limit ends `budget_exceeded`; the control plane records it as `Failed` with the limit as the reason (`budget exceeded: budget: tool_calls 60 reached`) and keeps the refused call in the result.

## The result

The last JSON line, `{"type":"result","result":{"schema":"axlr.execution-result/v1",…}}`, gives the status (`completed`, `needs_decision`, `interrupted`, `failed`, `budget_exceeded`), the answer, the sessions, the pending call with its arguments, the tool calls, the usage (requests, tokens, cost) and the artifacts. The adapter keeps the raw lines as the trace (`<state>/traces/<execution>.jsonl`), bounded per line. Fallback for an older `axlr-tui` without the result line: the exit code (0 completed, 3 decision required, 1 failed) plus the `completed`, `failed` and `decision.required` lines.

| Result status | Execution outcome | Task |
|---|---|---|
| `completed` | `Completed` | `Completed`; the answer is written to `<state>/artifacts/<task>/report.md` on the control plane |
| `needs_decision` | `DecisionRequired` | `WaitingApproval` ([approvals](../runbooks/approvals.md)) |
| `failed`, `interrupted`, a timeout, a cancel | `Failed` (or `Cancelled` when the task was cancelled) | the same, with the reason |
| `budget_exceeded` | `Failed`, `budget exceeded: …` | `Failed` |

The execution keeps the node, the policy record, the correlation record, the usage with its cost, the trace reference and the result (vision §12.3).
