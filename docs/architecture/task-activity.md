# ADR: the complete activity of a task

Status: accepted, 14 November 2026. The owner asked for the whole activity of a task in one place: what it did, what the tasks it delegated did, what each cost, and nothing lost to the bounds of the live progress timeline. Served by `ActivityService` (`api/proto/axlrbots/operator/v1/activity.proto`): `GetTaskActivity` and `WatchTaskActivity`.

## Where each part comes from

| Part of `TaskActivity` | Source | Port |
|---|---|---|
| `task` | the task as stored | `TaskRepository` |
| `children` | the task's own children, followed recursively (the delegation lineage) | `TaskRepository` |
| `executions` (usage, cost) | the task's execution ids, in order | `ExecutionRepository` |
| `events` | the task's whole timeline, `bot.task.*` events including every `bot.task.progress` the control plane kept | `EventReader` |
| `progress_cut` | counted from the execution's `bot.task.progress` events with the same rules `RecordProgress` applies | none, derived from `events` |
| `completed_items` | the execution's transcript, read into items | `TranscriptStore` + `SessionItemReader` |
| `subtree_usage` | the sum of the usage of the task's executions and of every included descendant | derived |

`GetTaskActivity` (`application/get_task_activity.go`) walks the tree once. A task met twice (a lineage that loops) is skipped the second time; a child or an execution that no longer exists is left out; any other store failure fails the call.

### Options

| Option | Effect |
|---|---|
| `exclude_children` | the task alone; `subtree_usage` is its own |
| `max_depth` | levels of children followed; 0 is the delegation limit, 8. A level not followed is not summed either |
| `skip_completed_progress` | a cut is still marked, its transcript is not read (`completion: NONE`) |
| `max_completed_items` | items completed per execution, 0 is 2000; past it `completion_bounded` is true |

### Subtree usage

Requests, tokens and tool calls add up. `cost_usd` adds the known costs and `cost_unknown` is true when any execution below is unknown, so the figure is a lower bound then, never a claim of "free".

### The timeline, whole

`EventReader.ListTaskEvents` answers the newest N events. The use case asks for 1000, and for four times more while the answer fills what was asked, up to 50 000, so a task with several long executions is not cut at one page.

## When a run is completed from its transcript

The live timeline stops growing past 400 progress events or 256 KiB of items per execution ([live-progress](live-progress.md)). The use case recounts both from the events and marks the cut:

| Reason | Condition (checked in this order) |
|---|---|
| `BYTE_LIMIT` | the items kept reach 256 KiB |
| `EVENT_LIMIT` | the execution has 400 progress events |
| `BATCH_TRUNCATED` | the last batch says `truncated` (it lost items to a bound) |

`events_kept`, `bytes_kept`, `dropped` (what the nodes said they dropped) and `cut_at` (the time of the last item kept) come with it.

Only an execution with a cut is completed. For each session in the execution's result (`sessions` and `session_id`, once each) the transcript is read from the store and turned into items by the runtime's adapter (`adapters/axlr/session_item_reader.go`: the assistant's words, each call, and each tool's answer with status, error and duration). The use case never sees the session format. Then:

1. the last call item (kind and call id) the timeline kept is found in the transcript's items, and everything after it is the completion;
2. with no such call (the timeline kept words only) the items after `cut_at` are;
3. words the timeline already kept before `cut_at` are not repeated.

`completion` is `TRANSCRIPT` and `transcript_sessions` lists what was read; it is `NONE` when no transcript is kept, when the session cannot be read, or when `skip_completed_progress` was asked. A session file keeps no clock: a tool's answer carries the time it ran and the rest takes the time of the last item that had one. The model's reasoning is not in the items (the provider's reasoning details are opaque), and a run executed in process has no transcript yet.

A transcript is uploaded when the run ends, so a cut of a running execution is marked at once and its completion arrives with the end.

## Watching

`WatchTaskActivity` (`application/watch_task_activity.go`) keeps nothing per stream. The first message is the snapshot (the same tree as `GetTaskActivity`). After it, every change the control plane records wakes the watch through `application.ChangeSignal`, which the daemon wires to the same hub the long polls of the timelines wait on (`unixapi.Local.ActivityChanges`). On a wake the tree is read again and what is newer than the last cursor is sent, oldest first:

| Update | When |
|---|---|
| `task_changed` | a task's `updated_at` is newer |
| `execution_changed` | an execution started or ended |
| `event_appended` | an event of any task of the tree is newer |
| `child_attached` | a task created after the cursor, with everything known of it |
| `progress_completed` | a cut found, or its completion read at the execution's end |
| `heartbeat` | nothing changed in 15 s |

The **cursor** is the instant of the newest change told (`a1.<unix nanoseconds>`). A client resumes with the last one it saw: the server reads the tree and sends what is newer, so a resume is always honoured and the server keeps no history. A cursor that is not ours starts over with a snapshot. The stream ends with OK when every task of the tree has ended and no execution runs, and with CANCELLED when the client cancels.

Limits: updates are matched by time, so two changes with the very same nanosecond may be told once, and an event recorded late with an older time than the cursor is not told until the next snapshot.

## Wiring

- `Dependencies.SessionItems` is optional (nil: cuts are marked, never completed); `compose.Dependencies` sets the AXLR reader.
- `grpcapi.NewActivityService(deps, changes, heartbeat).Register(server)` adds the service. The server must raise its send limit above 4 MiB for the snapshot (`grpc.MaxSendMsgSize`).
- `adapters/grpcapi/activity_errors.go` maps errors for this service only; it is meant to be replaced by the package's own mapping.
