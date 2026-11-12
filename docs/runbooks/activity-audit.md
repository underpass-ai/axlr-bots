# Runbook: the activity audit

How the operator runs the input adapter that audits the fleet's finished tasks and turns each **new kind of failure** into one event for a team the operator names (`--team`), under an event name the operator names (`--event`), so that whoever listens can fix it. Neither has a default: the platform names no team and no event, and an application that runs the adapter (a separate repository, say) chooses both and declares the event in its own AsyncAPI. It is deterministic (no model), event driven (no timer: it wakes when a task ends) and it reads and publishes through the control plane's socket only, as the operator. The pattern is an [input adapter](../architecture/input-adapters.md); the wire is [`activity-audit.asyncapi.json`](../reference/activity-audit.asyncapi.json). Nothing here runs by itself: the unit below is a file to write.

```text
axlr-bots serve (the control plane)
   │ GET /v1/groups/{team}/events?after=&wait=30s     one long poll per team: only the wake-up
   │ GET /v1/tasks?since=<cursor>                     the reconciliation: a gap, a restart, a bot in no team
   │ GET /v1/tasks/{id} · /executions · /events       what is audited: the task, its calls, its progress items
   ▼
axlr-bots activity-audit serve   (axlr-activity-audit.service)
   failures → signatures → <state>/activity-audit/seen.json   (atomic; one event per signature, ever)
   │ POST /v1/groups/<team>/events  <event>, Idempotency-Key activity:<signature>
   ▼
the team: the members that subscribe to <event> hear it
```

## What it reads

| On | It reads | For |
|---|---|---|
| a `bot.task.completed`, `bot.task.failed`, `bot.task.expired` or `bot.task.cancelled` on any team's timeline | the task, its executions (`result.toolCalls`: name, call id, status) and its timeline (`bot.task.progress`: `tool.started` with the call's cut summary and `mcpTool`, `tool.ended` with its status and its error's first line), joined by call id | every call that ended `failed`; a call `denied` by the policy or a person is a decision and is not audited |
| a task that ended `Failed` | its `failureReason` (the last execution's when the task has none) | one failure of kind `task_failed` |
| a long poll that answered a full window (1000 events), or came back after the control plane did not answer | the task list from the cursor | the tasks whose end it may have missed, and a bot in no team |

It never reads the state directory, a trace or a workspace. Of the state directory it touches only its own `activity-audit/` (the seen-store and a lock).

## The signature

A failure's signature is `failure-` and 16 hex characters of the SHA-256 of its key. The key is what makes the kind of the failure, with what varies normalised away:

| Failure | Key |
|---|---|
| a `local_exec` | the program's basename, its first argument, the exit code, the error's first line |
| an `axlr_call_tool` | the plugin's tool it reached (`notes/notes_ask`), the error's first line |
| any other tool (`axlr_remember`, `axlr_tools`, `local_read`…) | the tool, the error's first line |
| a task that ended `Failed` | its reason, as `interrupted: model endpoint <addr> provider_failure (HTTP 504)` |

The normalisation, in order: secrets redacted (`ghp_…`, `github_pat_…`, `sk-…`, `AKIA…`, `xox?-…`, a private key's header, `Bearer <token>`); the first line only; quoted text (`"…"`, `«…»`, `` `…` ``, `'…'`), URLs, paths under `/home`, `/Users`, `/tmp`, times, dates, UUIDs, the fleet's ids (`task-…`, `exec-…`, `call_…`, `read_…`, `ws:…`), shas, addresses (`127.0.0.1:8790`, `localhost:8790`) and numbers replaced; an HTTP status kept (`HTTP 504` and `HTTP 429` are different failures); at most 400 characters. The event carries that normalised line, never a call's output.

**What it cannot see yet.** Today the runtime's lines cut what the full rule needs: the node cuts a call's summary at 120 characters and models write their `why` first, so a `local_exec`'s program is usually gone; AXLR cuts a failed call's error at 240 characters and writes a local or bridged call's answer as its response envelope (`{"protocol_version":1,…,"output":{"exit_cod…`), so the exit code and stderr are gone too. The adapter reads what is there and degrades the same way every time: a `local_exec` whose program it cannot see is keyed by its **bot** instead (`partial: true` in the event, `(partial)` in the report), its exit `nonzero` when the envelope says the program ran, its error `the run's line was cut before the error`. When the node writes the program first and AXLR writes the exit code and stderr's first line in the error, the full rule applies with no change here.

## Noise rules

| Rule | Default | Why |
|---|---|---|
| `--ignore-programs` | none | programs whose refusals are expected, such as a team's sandbox probes (`--ignore-programs sandbox-probe,sandbox-term`): a team that tests a product with them reports what it finds itself |
| `--ignore-team-partial` | `true` | a `local_exec` whose program the line hid, by a member of `--team`: the team the events go to may run probes of its own, whose program the cut line hides, and it reports what they find itself. Set it to `false` to audit them as partial signatures |
| a slip the model fixed at once | always | a failed call is *recovered* when a later call of the same tool (the same program for `local_exec`, the same plugin's tool for `axlr_call_tool`) ended `ok` in the same task. A signature whose every occurrence was recovered, all from one bot, waits: one model's typo is noise. The same slip from a **second bot** publishes it: then the contract misleads models (as a tool's error that several models misread does). A partial signature is never recovered |
| its own events | always | it audits tasks only; the event it publishes on the team's timeline is not a task's end |

## Once per signature

The seen-store `<state>/activity-audit/seen.json` (mode 600, written atomically, its schema `SeenFile` in the spec) keeps each signature: its key, its count (every occurrence, recovered ones included), the recovered count, the bots, the first and the last occurrence, when it was published and the event id. A signature publishes **one** event ever: later occurrences grow its count and publish nothing. When a signature falls due, what its event says (the bot, the task, the count) is fixed in the store *before* the publish, so a publish retried after a crash, or after the control plane refused it, is the same body under the same key `activity:<signature>`, and the control plane answers the event it published before. `--rearm <id>[,<id>]` (or `all`) clears a signature once it is fixed: it counts from zero, and its next occurrence publishes it again under `activity:<signature>:r<n>`.

The store also keeps a cursor (the creation time of the oldest task still unfinished at the last reconciliation) and the tasks audited since: a task is counted once, whatever the timelines repeat.

## The event

The event named `--event` (a domain-style name: kind info; `activity.failure` below) to the team `--team` (`team-a` below), as the operator, depth 0:

```json
{"name": "activity.failure",
 "message": "failure-ebbdaebc51f616fa · axlr_remember · bot-b1 (team-b) · - · send the continuation alone to commit a reviewed write",
 "data": {"id": "failure-ebbdaebc51f616fa", "kind": "tool_call", "bot": "bot-b1", "team": "team-b", "task": "task-…", "tool": "axlr_remember",
          "error": "send the continuation alone to commit a reviewed write", "count": 2, "first_at": "2026-11-12T07:07:…Z"},
 "idempotencyKey": "activity:failure-ebbdaebc51f616fa"}
```

The `message` stands on its own, because a coalesced note or a redelivery carries only it and `data.id`: `<id> · <tool> · <bot> (<team>) · <program or -> · <error>`, cut to 280 characters (`task` is the tool of a failed task, the plugin's tool the program of an `axlr_call_tool`, `(-)` a bot in no team). `data.id` is within `[A-Za-z0-9._:-]{1,128}`. `count` is the occurrences seen up to the publish (a backfill can give more than 1 at the first event); the seen-store keeps counting after it, for `report`.

**Pacing.** A listening team settles a bounded number of events per task and the platform redelivers at most twice, so the adapter publishes at most `--per-minute` events a minute (6 by default), oldest first; a flush that reaches the bound waits for the oldest of the minute to leave it. A first `--since` backfill of N new signatures takes about N/6 minutes at the default.

`program` (a `local_exec`'s basename), `mcp_tool` (an `axlr_call_tool`'s) and `partial` appear when they apply; `tool` is absent for `kind: task_failed`. Until a member of the team subscribes to the event's name, the events land on the team's timeline and wake nobody.

## 1. The first run: a dry run, then the replay

The first run has no cursor. Without `--since` it starts **now**: what ended before is marked audited unread, what is running is audited when it ends. To audit today's history once, give `--since`; it applies only while the store has no cursor (a later `--since` is ignored, logged, so a restart never counts a task twice).

See first what it would publish, without publishing or keeping anything (an empty seen-store in memory):

```bash
axlr-bots activity-audit serve --team team-a --event activity.failure --dry-run --once --since 2026-11-12T00:00:00Z
```

It prints `would publish N of M signatures` and the table. Then the replay, which publishes and keeps the store:

```bash
axlr-bots activity-audit serve --team team-a --event activity.failure --once --since 2026-11-12T00:00:00Z
axlr-bots activity-audit report
```

To try it on a copy of the live state, never the live socket: copy `~/.local/state/axlr-bots` without `timers/`, `outbox/`, `outputs.json`, `api.token`, `ca/`, `pki/` and the socket; serve the copy with a stand-in for `axlr-tui` that sleeps (`--axlr-bin`), `--tick 1000h --reconcile-every 1000h --grace 10000h` and no `--listen`, from inside the copy with `--socket cp.sock` (a Unix socket's path is at most 107 bytes); run the dry run against that socket.

## 2. The user unit

Write the file; nothing installs it for you. Like the GitHub hooks adapter it does not read `~/.config/axlr-bots/env`: it needs the socket, not the fleet's keys. On another host than the control plane it reads and publishes through the operator address instead (`axlr-bots --control https://<host>:7879 --token-file <token> --ca-file <fleet CA> activity-audit serve … --store <dir>`); keep one seen-store and move it with the adapter, or every failure it already published goes out again.

`~/.config/systemd/user/axlr-activity-audit.service`:

```ini
[Unit]
Description=AXLR Bots activity audit (each new failure → team-a)
After=axlr-bots.service
Wants=axlr-bots.service

[Service]
Type=simple
ExecStart=%h/.local/bin/axlr-bots --state %h/.local/state/axlr-bots --socket %h/.local/state/axlr-bots/axlr-bots.sock activity-audit serve --team team-a --event activity.failure
Restart=on-failure
RestartSec=5
NoNewPrivileges=yes

[Install]
WantedBy=default.target
```

```bash
systemctl --user daemon-reload
systemctl --user enable --now axlr-activity-audit.service
journalctl --user -u axlr-activity-audit -n 5     # "activity-audit: following N teams"
```

A second `serve` on the same store is refused: the adapter holds `<state>/activity-audit/.lock` for its life.

## 3. Verify

| Do | Expect |
|---|---|
| a task of any bot ends with a failed call never seen | `axlr-bots group events team-a` shows `group.event.published`, `name: activity.failure`, `by: operator`; the log says `published to team team-a` |
| the same failure in a later task | no new event; `axlr-bots activity-audit report` shows its count grown |
| restart the unit | `report` keeps every signature; nothing is published again |
| stop `axlr-bots`, let a task end, start it | the adapter logs the team's poll failing, then reconciles from its cursor and audits the task |
| `serve --rearm <id>` (with the unit stopped) | the signature counts from zero; its next occurrence publishes `activity:<id>:r1` |

## Turn it off

```bash
systemctl --user disable --now axlr-activity-audit.service
rm ~/.config/systemd/user/axlr-activity-audit.service && systemctl --user daemon-reload
```

The seen-store stays in `<state>/activity-audit/`; a later start goes on from its cursor.

## When it does not work

| Symptom | Cause, fix |
|---|---|
| `list the teams: …` at start | the daemon does not answer on the socket: start `axlr-bots` |
| `the state at …/activity-audit is in use by another process` | another `serve` holds the store: stop it |
| `publish failure-…: …` in the log | the control plane refused or did not answer: the signature stays due and the next audit retries it with the same key. A `not found` names a team `--team` that does not exist |
| `1000 tasks since …, the most the control plane lists` | more tasks were created since the cursor than one list answers (the adapter was down long): the older ones are not audited |
| every `local_exec` signature is `(partial)` | the runtime's lines cut the program and the exit code (above): the bot stands in |
