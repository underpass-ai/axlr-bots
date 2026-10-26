# Runbook: recover

What to do after a crash, a dead host or a lost control plane. Every task is written as `Running` with its execution before anything runs, so a crash leaves a visible `Running` task, never a lost one.

## A task stays Running

Three things end a `Running` task: the node's report, the daemon's reconciliation, or `task reconcile` by hand.

1. Read it: `axlr-bots task get task-…` shows the execution, its `nodeId` (or none for an in-process run) and its lease.
2. Wait for the daemon: on start and every `--reconcile-every` (a minute) it fails the executions whose node stopped renewing its lease (two minutes, renewed every 30 s) and the in-process runs older than the grace (the bot's `taskTimeoutSeconds` plus a minute).
3. Or reconcile now:

```bash
axlr-bots task reconcile                # {"failed": ["task-…"], "skipped": []}
axlr-bots task reconcile --grace 30s
```

Check: the task and its execution are `Failed` (the task is `Expired` when the lost run was the denial of an approval nobody decided in time; [approvals](approvals.md#expiry)) with the diagnosis (`reconciled: … node web-1 stopped renewing its lease at …`, or `…; the process that ran it is gone`); `bot events <bot>` has a `bot.task.failed` event with `"reason": "reconciled"`; `<state>/audit.jsonl` has a `task.reconcile` entry. Running it again is harmless. Submit the task again with a new key if it should run.

## A node went silent

The daemon marks `Offline` a node silent for over a minute (`node.offline` event); its leased executions are reconciled as above. When the host comes back, the agent registers again and is `Online`; nothing to do. If the host is gone for good, revoke it ([add a host](add-a-host.md#retire-or-revoke-a-host)).

## The control plane restarted

The bots, tasks, executions, nodes and timers are files under `<state>`; everything is there after a restart. A timer whose slot passed during the stop fires once for that slot and arms the next ([triggers](triggers.md#check-the-catch-up)); a node that heartbeats a control plane with no record of it gets `404` and registers again. Check: `node list`, `bot list`, `task get` on the last tasks; `journalctl --user -u axlr-bots -n 5` shows `daemon: serving on …`.

## The control plane lost its state

A node whose certificate the fleet CA signed may register even when the control plane does not know it, so the fleet comes back as the nodes heartbeat. The CA key is under `<state>/ca`: without it, no node certificate verifies and no new server certificate can be issued. Keep `<state>/ca` with the backups; a lost CA means `ca init` again and a new token for every host.

## The daemon refuses to start

`journalctl --user -u axlr-bots -n 20`. The usual causes: `--listen` without `ca init` (the daemon refuses to listen), a port in use, an unreadable environment file, or an `AXLR_TUI` that does not exist (the in-process runs fail; the nodes run their own). A stale socket file is removed at start.

## A run never ends on the node

The node stops a run at the bot's timeout and reports `Failed`. `task cancel task-…` ends it sooner: the node's run stops at its next lease renewal and its late report is dropped. The trace is on the node under `<node-state>/traces/<execution>.jsonl` for the diagnosis.
