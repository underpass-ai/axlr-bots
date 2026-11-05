# Runbook: recover

What to do after a crash, a dead host or a lost control plane. Every task is written as `Running` with its execution before anything runs, so a crash leaves a visible `Running` task, never a lost one.

## A task stays Running

Three things end a `Running` task: the node's report, the daemon's reconciliation, or `task reconcile` by hand.

1. Read it: `axlr-bots task get task-…` shows the execution, its `nodeId` (or none for an in-process run) and its lease.
2. Wait for the daemon: on start and every `--reconcile-every` (a minute) it fails the executions whose node stopped renewing its lease (two minutes, renewed every 10 s) and the in-process runs older than the grace (the bot's `taskTimeoutSeconds` plus a minute; for a delegated task, never later than a minute past its grant's deadline). It also fails, past the same grace, a `Running` execution whose task does not run it (a crash between the two saves left it), with `reconciled: orphan: …`, an `execution.orphaned` audit entry, and its id under `orphaned`; the task is left as it is.
3. Or reconcile now:

```bash
axlr-bots task reconcile                # {"failed": ["task-…"], "skipped": [], "orphaned": []}
axlr-bots task reconcile --grace 30s
```

A `Running` or late task whose bot's file is gone (lost by hand: `bot delete` ends a bot's tasks first) is listed under `skipped` and the pass goes on with the others; delete or restore the bot file to end it. The daemon's own pass never fails a task it is running in process, whatever `--grace`. `task reconcile` works on the files and takes the lock on the state directory (`<state>/.lock`): while the daemon serves it is refused (`the state at … is in use by another process …`), since the daemon reconciles by itself; stop the daemon first to reconcile by hand ([concurrency](../architecture/concurrency.md#decision)).

Check: the task and its execution are `Failed` (the task is `Expired` when the lost run was the denial of an approval nobody decided in time; [approvals](approvals.md#expiry)) with the diagnosis (`reconciled: … node web-1 stopped renewing its lease at …`, or `…; the process that ran it is gone`); `bot events <bot>` has a `bot.task.failed` event with `"reason": "reconciled"`; `<state>/audit.jsonl` has a `task.reconcile` entry. Running it again is harmless. Submit the task again with a new key if it should run.

## A node went silent

The daemon marks `Offline` a node silent for over a minute (`node.offline` event); its leased executions are reconciled as above. When the host comes back, the agent registers again and is `Online`; nothing to do. If the host is gone for good, revoke it ([add a host](add-a-host.md#retire-or-revoke-a-host)).

## The control plane restarted

The bots, tasks, executions, nodes and timers are files under `<state>`; everything is there after a restart. A timer whose slot passed during the stop fires once for that slot and arms the next ([triggers](triggers.md#check-the-catch-up)); a node that heartbeats a control plane with no record of it gets `404` and registers again. Check: `node list`, `bot list`, `task get` on the last tasks; `journalctl --user -u axlr-bots -n 5` shows `daemon: serving on …`.

## The control plane lost its state

A node whose certificate the fleet CA signed may register even when the control plane does not know it, so the fleet comes back as the nodes heartbeat. The CA key is under `<state>/ca`: without it, no node certificate verifies and no new server certificate can be issued. Keep `<state>/ca` with the backups; a lost CA means `ca init` again and a new token for every host.

## The daemon refuses to start

`journalctl --user -u axlr-bots -n 20`. The usual causes: `--listen` without `ca init` (the daemon refuses to listen), a port in use, an unreadable environment file, or an `AXLR_TUI` that does not exist (the in-process runs fail; the nodes run their own). A stale socket file is removed at start. `the state at … is in use by another process` means another daemon (or a command such as `task reconcile`) holds `<state>/.lock`: find it with `fuser -v <state>/.lock` and stop it. A lock file left by a crash is no obstacle: the kernel drops the lock with its process, and the file itself stays on purpose.

## A record was set aside

A record file under `<state>` that is not valid JSON (cut by a full disk, edited by hand) no longer hides the rest of its kind: the first listing that meets it moves it to `<state>/<kind>/.quarantine/<id>.json.<unixnano>` (for example `<state>/tasks/.quarantine/task-….json.1793…`), logs `… is not valid JSON: …; set aside as …` once, and goes on with the other records. From then on `task get` (or `bot get`, …) of that id answers not found. To recover it, repair the JSON and move it back to `<state>/<kind>/<id>.json` with the daemon stopped; or keep it for the diagnosis and delete it later. A file that is valid JSON but not a valid record (an unknown status, written by hand or by a newer binary before a downgrade) is set aside the same way, with the log line `… is refused by the domain: …; set aside as …`: one such task file used to stop every run, every node's poll and every submission. After an upgrade back, move it back with the daemon stopped. Check: `find <state> -path '*/.quarantine/*'`.

## A run never ends on the node

The node stops a run at the bot's timeout and reports `Failed`. `task cancel task-…` ends it sooner: the node's run stops at its next lease renewal and its late report is dropped. The trace is on the node under `<node-state>/traces/<execution>.jsonl` for the diagnosis.
