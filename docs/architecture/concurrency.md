# ADR: concurrency in the control plane

Status: accepted, 2 November 2026.

## Context

`SerialRunner` (`application/serial_runner.go`) is the one door through which the daemon's worker, the API's `task run` and `task decide`, the approval expiry and the node protocol (`NextAssignment`, `Report`, `Delegate`, `Emit`, `Subscribe`, `RevokeNode`, `Publish`) change tasks and executions. Until now it had one mutex, held for the whole of each call. An in-process run is `axlr-tui`, up to the bot's task timeout (15 minutes by default), and its settle could start a second one (a call the bot's `autoApprove` names). For that long every node's poll and report waited: a report tried five times, waiting 30 s for an answer each time (about two and a half minutes), and then gave up, the two-minute lease lapsed, and reconcile failed an execution whose result was on its way. The daemon also ran the worker on its main goroutine, so the timers, the approval expiry and the reconciler waited behind a chain of in-process runs.

## Decision

Two mutexes, always taken in this order: `slot`, then `mu`. `flightMu` is taken alone, last.

```text
 Run · RunNext · Decide · ExpireApprovals          NextAssignment · Report · Delegate · Emit
 ───────────────────────────────────────          Subscribe · Publish · RevokeNode · Reconcile
 slot ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━   ──────────────────────────────────────────
 mu   ━━━ start ━━┓            ┏━━ settle ━━━      mu ━━ the whole call (short) ━━
                  ┗ runtime ━━━┛
                   (mu let go)
```

| Lock | Held by | Keeps out |
| --- | --- | --- |
| `slot` | the whole of `Run`, `RunNext`, `Decide`, `ExpireApprovals` | a second in-process `axlr-tui` on the host; a call decided by a person and denied by its deadline at once |
| `mu` | every runner method, but let go of while the runtime runs | two state changes of the same task or execution at once: a task both assigned to a node and run in process, a report and a reconcile ending one execution twice, a delegation and a report changing one parent |
| `flightMu` | `track`, `Interrupt`, `running` | nothing but the map of in-process runs, so a cancel interrupts a run without waiting for either lock |

The four slot methods use a copy of the dependencies whose `Runtime` and `Decisions` are wrapped (`application/serial_runner_slot.go`): the wrapper unlocks `mu`, calls the runtime and locks `mu` again before it returns. It is only ever called with `mu` held by the calling goroutine; the settle, and the second run an `autoApprove` starts from it, go through the same copy and so keep the invariant. The node methods use the raw dependencies: they never reach the runtime.

While `mu` is let go of, a cancel may end the execution. The settle reads it again first (`dropIfEnded`) and keeps that end. Reconcile now runs through the runner (`SerialRunner.Reconcile`, under `mu`) and is told which tasks are in flight, so it never fails a task the daemon is running in process, however small `--grace`.

The bot writers go through the runner too (`application/serial_runner_bots.go`, added 5 November 2026): register, `PATCH`, enable, pause, stop and delete each read the bot, change it and save it under `mu`, as a run's own subscription ask (`Subscribe`) does, so two changes of one bot at once never lose one (before, a `PATCH` and a pause could). The timers' pass (`TickTimers`) is under `mu` as well. A delete also holds the service's submissions lock first (`Local.submits`, then `mu`; nothing under `mu` takes `submits`), and so does the timers' pass: no submission, fire or timer creates a task for a bot while it is deleted, and none of its tasks is assigned, settled or delegated to meanwhile.

The daemon runs `drain` on a worker goroutine of its own, one at a time (`TryLock`: a tick that finds it busy skips), and waits for it on the way out. The worker carries out the approval deadlines that passed before each run, since an expiry takes the slot as a run does. The main loop only fires the timers and ticks the reconciler, so neither waits for a run.

The two mutexes live in one process. Between processes the state directory has its own lock, `<state>/.lock` (`internal/statelock`, `flock(LOCK_EX|LOCK_NB)`, added 2 November 2026): the daemon holds it for its whole life, from before it binds its socket; `task reconcile`, `ca init` and any command that falls back to the files because no daemon answered take it for their run. While the daemon holds it they are refused (*the state at <dir> is in use by another process*) instead of failing a task the daemon is running or settling, and a second `serve` is refused instead of taking the first one's socket. Two such commands with no daemon exclude each other the same way. The kernel drops the lock when its holder ends, however it ends, so a crash leaves nothing to clean, and the file stays. Commands that go through the socket never take it: the daemon is the one writer.

## The windows that remain

- **Between the probe and the lock.** `serve` checks the lock before it starts its bus and lets it go; `daemon.Run` takes it again for good. Two `serve`s started in that instant may both write the API token file and both sweep the NATS outbox (the broker drops the duplicate publishes within its window); the second is then refused at `Run`.
- **Registration and heartbeats** do not go through the runner, on purpose: behind a run they would make a node flap Offline. A node record written by a heartbeat and by an assignment at once can lose one of the two writes; that belongs to the node repository.
- **A decision waits for the slot.** An operator's `task decide`, and an approval expiry, wait for the in-process run going on, even when the decision is carried by a node: the runner cannot know that before it reads the task, and it reads it under `mu`, after the slot.
- **Shutdown waits for the run.** The worker ends when its run ends; the runtime must honour its context (the `axlr-tui` adapter sends the process SIGTERM), or the daemon's stop waits for the run's timeout.
- **A run started between two saves.** A crash between saving an execution and saving its task still leaves an orphan; reconcile fails it after the grace, as before.

## Consequences

- A node's poll, report, delegation, event and subscription wait for a state change, never for an in-process run. A report no longer outlives its lease because of one.
- The host still runs one in-process `axlr-tui` at a time, and `TestSerialRunnerNeverOverlaps` still holds.
- The state format does not change: switching back to the previous binary is a swap and a restart.
