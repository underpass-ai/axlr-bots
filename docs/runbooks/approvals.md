# Runbook: approve or deny a write

At the `act-with-approval` level every allowed write or command is a person's decision: AXLR stops at the first one with `needs_decision`, the task is `WaitingApproval`, and nothing runs until an operator decides. The level mapping is in the [runtime contract](../architecture/runtime-contract.md#the-policy); the bot fields are in the [bot definition](../reference/bot-definition.md#autonomy-levels).

## Run the example

[`examples/bot-act-with-approval.json`](../../examples/bot-act-with-approval.json) is the example bot one level up: `local_write` allowed, level `act-with-approval`.

```bash
axlr-bots bot register --file examples/bot-act-with-approval.json
axlr-bots bot enable bot-act-with-approval
axlr-bots task submit bot-act-with-approval --objective "Create hello.txt in your workspace containing hello" --key w-1
```

Check, a few seconds later: `axlr-bots task get task-…` shows `"status": "WaitingApproval"`, the last execution `"outcome": "DecisionRequired"`, and `result.pendingDecision` names the tool (`local_write`), the call id, the session and the call's arguments (the path and the content that would be written). The file does not exist in the bot's workspace (`<node-state>/workspaces/bot-act-with-approval/`).

## Decide

```bash
axlr-bots task approve task-…    # a new execution carries the decision
axlr-bots task deny task-…       # or: the session goes on without the call
```

Both are `POST /v1/tasks/{id}/decide`; `task decide <id> approve|deny` is the same. The decision is a new execution of the task. When the stopped execution ran on a node, the decision is queued for that node (the AXLR session lives there); the node runs `axlr-tui --session <id> --decide approve --policy <file>` on its next poll and reports. In single-process mode the control plane runs it itself.

Check after `approve`: `task get` shows a second execution of `"kind": "decide"`, then `"status": "Completed"`; the file exists with its content; `task report` prints the answer. After `deny`: the task ends with the outcome of that execution and the file never appears. Either way the write never ran before the decision: `bot events bot-act-with-approval` shows `bot.task.decided` between the two executions.

## Cancel instead

```bash
axlr-bots task cancel task-…
```

A `WaitingApproval` task ends `Cancelled`; the decision is never asked and the session stays where it is.

## Limits today

Approval is `task approve` by whoever reaches the socket or holds the bearer token: there is no queue of pending approvals, no record of who may decide, and an approval does not expire. The `observe`, `propose` and `act-within-policy` levels never stop for a decision: the policy either allows the call or refuses it before any card.
