# ADR: an incident's apply and verify phases, and a policy per phase

Status: accepted, 8 November 2026. axlr-sre design §4 (the Incident aggregate and its state machine), §6 (the brigade), §8.1 (blast radius), §8.3 (progressive autonomy), §9 (the Incidents screen), §12 (phase 2, "act with approval"). Builds on the fix phase of [axlr-bots#128](https://github.com/underpass-ai/axlr-bots/pull/128).

## Context

Until now an incident stopped at `Proposed`: the fix task's answer was the change and its rollback, and a person applied it by hand, outside the control plane. Phase 2 of axlr-sre wants the brigade to apply the proposal once a person says yes, check that it took, and go back when it did not. Three rules of the design hold: each phase is a task of its own (a sidecar loses an approval in wait if its pod dies, so nothing long crosses an approval); a task never changes the incident's status by itself (the control plane knows which phase launched which task); the delegation never crosses phases (a parent that waited through an approval is what §4 forbids).

Approvals already exist for any task: a task at `act-with-approval` stops in `WaitingApproval` on its first gated call, with a deadline and an expiry action, notifications, `task approve|deny` and the panel's Approve and Deny ([approvals](../runbooks/approvals.md)).

## Decisions (8 November 2026)

| # | Decision |
|:--|:--|
| D1 | The phase-2 approval **is** the task approval. On `Proposed` the control plane launches the task of phase `apply` on the teammate that declares `{"type": "incident", "phase": "apply"}`; its gated write call puts the task in `WaitingApproval`; the incident stays `Proposed` while it waits (it needs a person). The approval moves the incident to `Applying` the moment it is decided; the task's completion moves it to `Verifying` and launches the task of phase `verify`; the group's resolution from Alertmanager moves it to `Resolved`, as before. A denial or an expiry sends it back to `Open` with the reason; the second denial escalates it. A failed apply sends it back to `Open` ("apply fails, rollback"). A verify task that says the alert is still red sends it back to `Open`; otherwise the incident stays `Verifying` until the group resolves |
| D2 | An `incident` trigger may carry `tools` (a subset of the bot's `allowedTools`) and `level` (at most the bot's level); a definition that widens either is refused. The tasks of that phase run under the narrowed policy, rendered in the execution's policy file, and the execution's record keeps the level and the tools it ran with. Absent, the phase keeps the bot's |
| D3 | One approval per incident is the product's rule. The control plane does not enforce it; `incident get` and the panel show the count of the incident's approvals, from its tasks' approvals |

Why D2: under `observe` and `propose` AXLR vouches the allow-listed MCP tools as reads, so a `propose` phase whose allow-list held an `apply` tool would apply without a card. One bot may take both the fix phase (at `propose`, with its read tools) and the apply phase (at `act-with-approval`, with `apply`), and each phase names only what it needs. Phase 3's autonomy ladder will set the apply phase's level per runbook on the same field.

## How the decisions are carried out

### The state machine

```text
alert firing ─────────────────────────────▶ Open (cycle 1, triage task)
Open        ──triage Completed──────────────▶ Diagnosed (diagnosis), fix task launched
Diagnosed   ──fix Completed─────────────────▶ Proposed (proposal), apply task launched
Proposed    ──apply call approved───────────▶ Applying                                   incident.applying
Proposed    ──apply Completed, no wait──────▶ Applying ─▶ Verifying                      (a level that asks nobody)
Applying    ──apply Completed───────────────▶ Verifying, verify task launched            incident.verifying
Verifying   ──verify says VERDICT: red──────▶ Open (cycle + 1, reason)                   incident.reopened {from}
Verifying   ──verify green or no verdict────▶ Verifying (waits for the source)
Proposed    ──apply denied or expired───────▶ Open (cycle + 1, reason, denials + 1)      incident.reopened {from, denials, taskId}
Proposed    ──second denial─────────────────▶ Escalated (left to a person; no retry)     incident.escalated
Proposed or Applying ──apply Failed or Cancelled──▶ Open (cycle + 1, reason)             incident.reopened {from}
Verifying   ──verify Failed, Expired, Cancelled──▶ Escalated
any but Resolved ──group resolved, or resolve──▶ Resolved
Open (sent back) ──at once──▶ triage task of the new cycle (the next alert, when it cannot start)
```

`Applying` and `Verifying` only appear on an incident whose team has a bot taking the apply phase: an incident stored before them, and one with no such bot, load and behave unchanged. Every move is the control plane's, from the phase that launched the task (`incidentAdvancer`): the triage task diagnoses, the fix task proposes, the apply task's decision and end apply, the verify task's answer verifies. The phase a status launches is `fix` for `Diagnosed`, `apply` for `Proposed`, `verify` for `Verifying`; the launcher (`application/incident_phase_launcher.go`) gives it to the one teammate of the triage bot that declares it, under the key `incident:<id>:<cycle>:<phase>`, and leaves the incident where it is when none does, or two do (audited `incident.<phase>`). A repeat alert launches the phase that is due and missing, as it did for fix.

### Three choices the decisions left open, settled on 8 November 2026

1. **"Two denials of the same cycle" is counted since the last reopening** (confirmed 8 November 2026). A cycle holds one task per phase (the store refuses another, and the idempotency key `incident:<id>:<cycle>:apply` would find the first again). So every return to `Open` (a denial, an expiry, a failed apply, a red verify) starts a new cycle, keeping the diagnosis and the proposal until the new triage replaces them. The incident counts its `denials` (an expiry is one) and the `IncidentMaxDenials`-th (2) escalates it; a reopening after a resolution resets the count, a proposal does not. An incident escalated that way is not retried by the next alert (`CanRetry` is false), unlike one whose triage failed.
2. **After going back to `Open`, the control plane starts the new cycle's triage at once** (decided 8 November 2026, replacing the first version of this PR, which waited for the next alert). The alert is still firing, and a production Alertmanager resends a group only every `repeat_interval`, 4 hours by its default; the denial cap already bounds the loop (a person must approve each apply, and the second denial escalates). The `incident.reopened` event then carries `from`, `reason`, `denials`, `endedTaskId` and the new triage task's `taskId`. When that triage cannot start (the triage bot gone, paused or without its incident trigger, a store that fails) the move is audited (`incident.open`, denied), the event is published without a task, and the next alert of the group starts the triage, as after a crash between an incident's save and its task. The task that ended stays the incident's: a denied apply whose session goes on adds its cost when it ends, and moves nothing.
3. **The verify contract is one line** (confirmed 8 November 2026). The verify task's answer says `VERDICT: red` when the fault is still there, `VERDICT: green` when the change took (the first such line, any case; `domain.ParseVerifyVerdict`). Red sends the incident back to `Open`. Green, or no verdict, leaves it `Verifying` until the group resolves; there is no timeout. A verify task that fails, expires or is cancelled escalates the incident. With no bot taking the verify phase the incident waits in `Verifying` for the source. A timeout of `Verifying` is deliberately left for the SLA and budget work of phase 3 (decided 8 November 2026).

### The decision of the apply call

`ApproveTask.resume` is where a person's approve or deny and an expiry that denies (`onExpire: deny`) all meet; there, after the task and the new execution are saved and before a node's decision is queued, the advancer moves the incident whose current task is this apply task and which is still `Proposed`: approved is `Applying`, denied or expired is back to `Open`. A call decided while the incident is `Applying` (the apply's second gated call) moves nothing: the task's end does. An expiry that cancels the task (`onExpire: cancel`) arrives as the task's end, `Expired` while `Proposed`, and counts as a denial. After a denial the session goes on without the call and the task ends later: the incident has moved on, so that end only adds its cost, audited. A store that fails while the decision is carried is audited and the decision goes on; the task's end is the safety net (`Proposed` with an approved wait goes through `Applying` to `Verifying`, with a denied one is a denial).

### A policy per phase

`domain.Bot.PolicyFor(task.Trigger())` gives a task's level, tools and rules: the incident trigger's when it names them, the bot's otherwise. The allow rules of the tools a narrower list leaves out are dropped (`ToolRules.Within`), so no rule widens it back; the deny rules stay. Every place an execution's policy is built uses it: the in-process run and decision, and a node's run and decision assignment. `Bot.CheckTriggers` refuses, at register and update, a trigger level above the bot's and a trigger tool outside its `allowedTools`; an update that lowers the bot's level or narrows its tools under a trigger's is refused too. The execution's `policy` record now keeps `allowedTools` beside `level`.

### What an operator reads

`GET /v1/incidents` marks each incident `needsYou` when its current task waits for an approval where the incident stands (the phase its status launches, or the apply while `Applying`), from one read of the waiting tasks. A denied apply whose session asks again does not need a person: the incident is in a later cycle and deciding it would act on a proposal it no longer stands on. `GET /v1/incidents/{id}` adds `currentTask` (bot, phase, status, the approval it waits for) and `approvals` (approved, denied, expired, waiting), each task counted by its last wait. The panel puts the incidents that need you first under any sort, and the incident's page shows the call with **Approve** and **Deny**, the Tasks screen's own decision.

## Left out

The autonomy ladder (phase 3 sets the apply phase's `level` per runbook), who may approve and two persons in production, Slack, CloudWatch, the scribe and the postmortem. The axlr-sre bots (`bots/fix.json`) do not declare the apply phase yet, and no verify bot exists there. The panel's bot editor keeps a trigger's `phase`, `level` and `tools` when it reads a definition but does not edit them.
