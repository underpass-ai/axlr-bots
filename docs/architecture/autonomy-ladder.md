# ADR: progressive autonomy, one runbook in one namespace

Status: accepted, 8 November 2026. axlr-sre design §4 (the Incident), §8.2 (runbooks), §8.3 (progressive autonomy), §11 and §12 (phase 3). Decisions D8, D13 and D14 of the phase-3 plan. Builds on the incident's phases ([ADR](incident-phases.md)), the policy pinned on a task (#133) and who may approve ([ADR](approvers.md), #139).

## Context

An apply phase has always stopped on a card. §8.3 wants a runbook to earn the right to act alone, from the record of what people did with its diagnoses, proposals and applies. The control plane already knows those acts, but scattered: the decision is on the task, the verdict on the verify task's answer, and the unit §8.3 names, a runbook in a namespace, is nowhere.

## Decisions

| # | Decision |
|:--|:--|
| 1 | **Unit: one runbook in one namespace.** A new aggregate, `RunbookAutonomy`, with its own repository (`<state>/autonomy/<id>.json`, 0600), independent of `Incident`. Its id is minted from the tenant, the apply bot, the runbook and the namespace, so a lookup is a read, not a scan |
| 2 | **The runbook is named by the alert name the incident already carries, matched exactly.** The title must be `<runbook> in <namespace>` for a runbook the trigger declares and the incident's one namespace (`LadderRunbook.Titles`), never a cut at the first ` in `. A CloudWatch incident names no namespace and has no unit. A group whose alerts differ in name, whose worst alert is worse than its first, or that was truncated is `mixed`: no unit and no counts. A severity label the mapping does not name (`P1`, `high`, none) leaves the group unknown. Only the opening delivery can say uniform and no later one raises unknown, so a hand-opened incident counts but always keeps its card |
| 3 | **Opt-in on the apply trigger**: `ladder: {runbooks: [{name, tool, namespaces}], thresholds?}`. A bot without it behaves exactly as before; the launcher audits nothing new and the task is pinned as before |
| 4 | **D13: count by cycle.** Each signal is idempotent per incident, cycle and signal, read from the history. Twenty reopened cycles of one incident count twenty; one cycle never counts twice |
| 5 | **D14: only `act-within-policy` removes the card.** Every lower step keeps the apply phase with its card. A runbook starts at the level the apply trigger declares; reaching the top takes all three counters: 5 diagnoses, 10 proposals, 20 applies, configurable in `thresholds`, and never lowered for the lab (D8) |
| 6 | **Signals are a person's.** Counted: a named person marking a diagnosis correct, a named person approving **the runbook's own tool**, and a green verify of a cycle a person approved (recorded under the approver's name). A ladder needs `approvals.approvers`; `policy:`, `bot:`, `node:`, `trigger:` names (through `ActorKey`) and registered bot ids are no people. The lab's judge goes on that list by name (D6). A policy, a decision with no name, the control plane's own name on a daemon, an expiry and a verify with no verdict count nothing. Nothing outside the control plane promotes: Jev has no path to a counter |
| 7 | **Lowering fails safe.** Any denial lowers one step, named or not; only an expiry does not. A call denied while `Applying` also fails the phase and sends the incident back. A red verify lowers two; an apply that failed once its write was under way lowers two (§4's "apply fails, rollback": no rollback phase exists), and so does a run acting alone that made more than its one write. A failure the runtime reports with no tool call wrote nothing and counts nothing; one that reports nothing at all may have written, and lowers. All of this falls on the unit pinned on the apply task, and still applies after the incident was resolved or changed |
| 8 | **Lowering clears nothing silently.** The history keeps every entry, a lowering entry says the step it moved and what had been earned (`earnedBefore`), and the panel shows it |
| 9 | **Effective level computed at launch and pinned** on the apply task, as #133 pins the phase's policy: the grant is the level, the tools and the namespace. Every apply on a ladder pins its unit, whose signals fall on it whatever the incident says later |
| 10 | **A sev1 never acts alone**, in any namespace and whatever the operator listed as production. The card stays, with the reason audited on `incident.apply` |
| 11 | **Bounds**: per runbook, the one write tool it may run alone and its namespaces. Outside them the apply takes the card, audited. A run acting alone is pinned a budget of `LadderMaxToolCalls` (4) calls: the bridge lookup, the reads of its runbook and manifest and its one write (§8.1); only completed writes that are no `confirm: false` preview count toward the one |
| 12 | **The grant is asked again before the run** (review of #143). Immediately before an unattended run starts, in process, when a node takes it and when it is re-delivered after a crash, `ladderKeeper.recheck` asks whether the incident still stands on the task and whether the ladder still grants it. Not standing: the task is cancelled, nothing runs. Standing but no longer granted: the grant is taken back and the run takes the card. Both are audited on `incident.apply`, and a re-attach through the idempotency key audits the grant the task really carries |
| 13 | **A person can stop it**: `autonomy lower <id>` (`POST /v1/autonomy/{id}/lower`), a named approver, one step, audited, and it cancels the unit's runs already acting alone. `checkLadder` refuses a lower bot level while a ladder exists, so this is the brake |
| 14 | **A lost lowering costs the card, not a count.** An apply is recorded **outstanding** durably when it is granted and when a person approves it, and while one is outstanding the runbook gets no other grant. A record that cannot be written is no grant. The entry ends on a recorded green or a recorded lowering, or when the task wrote nothing (refused before it ran, taken back, failed with no call). A verify with no verdict ends nothing: the runbook keeps its card until a person lowers it or runs `autonomy clear`, which ends the entries and costs nothing earned. A tool change resets the entries with the record |
| 15 | **The tool is part of what was earned.** A record is bound to its write tool; changing the runbook's tool starts it over, history kept. Deleting a bot removes its records, and registering a bot removes what a bot of that id left: it earns from zero |
| 16 | **A read never sets a record aside.** An unreadable or unknown record is an error, the card, and the file stays for a person: a fresh record in its place would forget a lowering |

## Decided in the building (8 November 2026)

These go beyond the brief. Each is the conservative side of something the brief left open.

| Point | Decision | Why |
|:--|:--|:--|
| What a promotion reads | The counters **earned since the last lowering**, beside totals that never fall | With totals only, a runbook at the thresholds that is lowered would be promoted again by its next signal. A lowering therefore restarts the earning; it says so in its history entry, and the totals stay |
| The lower rungs | `observe` to `propose` takes the diagnoses, `propose` to `act-with-approval` the proposals, the top all three | §8.3's table, and how a lowered runbook climbs back |
| The trigger's level | A ladder needs `level: act-with-approval` on the trigger, and the **bot** at `act-within-policy` | The trigger is where the card comes from while the runbook earns; a pinned level is held to the bot's (#133), so a bot below the top could never use it. See the consequence below |
| Two persons | Beyond any sev1, **any** incident the bot's `twoPersons` covers keeps the card | #139 D5: a call that waits for two is never decided by a rule, and a ladder removing the card would be such a rule |
| Several namespaces | An incident naming none or several is outside the unit: it keeps the card and counts nothing | The unit is one namespace, and the bound below is one namespace |
| Outside the bounds | Signals of a namespace the ladder does not list are not counted | The record exists only for what could one day act alone |
| "Unedited" | Every approval counts as unedited: the control plane has no way to edit a call | There is nothing to compare it with; when an edit path exists this is where it would be read |
| Wrong diagnosis | Kept in the history; it counts nothing and lowers nothing | §8.3 lists it as a signal but gives it no consequence |
| A second mark | The same verdict again changes nothing; the other is refused (409) | A mark is one act per cycle |
| Who may mark | A named person among the apply bot's `approvers` | The same people who may approve its calls |
| Bound as an argument rule | The grant's allow rule is `{tool, arguments: {namespace}}`, replacing the bot's allow rules for that tool | A launch check alone would trust the model to stay in the namespace; AXLR holds it. A call without the `namespace` argument is refused too |
| Tools at the top | The grant keeps `local_read`, `local_list`, `local_search` and the one write | MCP does not say which tools read, and at this level the allow list is the only limit |
| Cancelled applies | An apply task that is cancelled does not lower; one cancelled while acting alone stays outstanding | A cancel is a person's stop, not a failure, and what it left is unknown |
| A denial while `Applying` | Only on an apply with a ladder unit | A bot with no ladder behaves as before |
| Hand-opened incidents | Counted, never granted | Nobody can say its alerts are one alert |

## AXLR's behaviour, verified

The ladder depends on three things AXLR does, so they were checked before building on them, at AXLR `2bbf46c` (v0.16.0), reading `tui/domain/execution_policy.go`, `tui/application/automatic_tool_policy.go` and `tui/cmd/axlr-tui/exec.go`, and running a scripted model against a real MCP server through the real `--exec` path, with the file `adapters/axlr/ladder_policy_test.go` renders:

| Policy file at `act-within-policy` with an allow rule on the write and its namespace | `--approve` | Result |
|:--|:--|:--|
| the write, in the namespace | `all` | `completed`, `auto_approve`, the write ran, no card |
| the write, in the namespace | `local` | `needs_decision`: a card; nothing ran |
| the write, in another namespace | `all` | `forbidden`, nothing ran |
| a tool not on the allow list | `all` | `forbidden`, nothing ran |

So the policy file alone does not remove the card: `--approve local`, which the adapter passes for this level, auto-approves only local tools, and an MCP write still stops. A task the ladder grants runs with `--approve all`, carried as `Unattended` on the run (the node message's `unattended`; an older node ignores it and the run stops at the card). This is the runtime contract's own chain ("a call it allows still passes the run's approval level"), and `--approve all` cannot widen a run: the allow list refuses everything it does not name before the approval level is consulted. A bot at `act-within-policy` with no ladder keeps `--approve local`.

## How the decisions are carried out

```text
incident reaches Proposed ─▶ launcher asks the ladder of the apply bot
   no ladder ──────────────────────▶ the phase's policy, as before
   not earned, outside the bounds,
   any sev1, two persons, a mixed group,
   an apply with no recorded end ───▶ the card, reason on incident.apply
   the runbook has earned the top ─▶ outstanding, then pinned: act-within-policy,
                                     reads + one write, held to the namespace, --approve all
task about to run ─▶ recheck: not standing ─▶ cancelled, no run
                              no longer granted ─▶ the card
```

| Signal | Comes from | Effect |
|:--|:--|:--|
| `diagnosis-correct` | `POST /v1/incidents/{id}/diagnosis`, CLI, panel | diagnoses + 1 |
| `diagnosis-wrong` | the same | history only |
| `proposal-approved` | a named person's approval of the runbook's own tool, `incidentAdvancer.decided` | proposals + 1 |
| `apply-verified` | a verify `VERDICT: green`, of a cycle a person approved | applies + 1 |
| `apply-denied` | any denial while `Proposed` or `Applying` | one step down |
| `verify-red` | a verify `VERDICT: red` | two steps down |
| `apply-failed` | an apply task `Failed` after its approval, or acting alone | two steps down |
| `apply-overrun` | a run acting alone with more than one write | two steps down |
| `lowered-by-person` | `autonomy lower` | one step down, ends every outstanding entry |
| `tool-changed` | the runbook's write tool changed on the ladder | starts over, history kept |
| `outstanding-cleared` | `autonomy clear` | ends every outstanding entry, nothing else |

A store that fails is audited (`autonomy.signal`) and the incident goes on. A lost count is a lost count; a lost lowering leaves the apply's outstanding entry, so it costs the runbook's next apply its card (decision 14). A record that cannot be read keeps the card. Signals run under the `SerialRunner`'s `mu`, like the settle that carries them, so two of one runbook never lose a count.

### What an operator reads

`axlr-bots autonomy list|get`, `GET /v1/autonomy` and `/v1/autonomy/{id}` show the step, `totals`, `earned`, the `thresholds` and the `history`. `GET /v1/incidents/{id}` adds `autonomy` with the mark of this cycle (`diagnosisMark`); the panel's incident page shows the step, the progress, the history newest first and **Diagnosis correct** / **Diagnosis wrong**. The audit lines are `autonomy.signal`, `autonomy.changed` (with `from` and `to`), `autonomy.mark` and `autonomy.lower` (refused), `autonomy.deleted` and `incident.apply` with `ladder` (why it has its card, or has none). `task get` shows the grant under `policy.ladder`.

### Layer map

| Layer | What changed |
|:--|:--|
| domain | `AlertGroup`, `AutonomyOutstanding`, `RunbookAutonomy`, `AutonomySignal`, `AutonomyCounters`, `AutonomyThresholds`, `AutonomyEntry`, `AutonomyID`, `RunbookName`, `Ladder`, `LadderRunbook`, `LadderUnit`, `LadderGrant`, `LadderDecision`; `Trigger.Ladder`, `Bot.DecideLadder`, `ExecutionPolicy.Granted` and `Unattended`, the pin |
| application | `AutonomyRepository`, `ladderKeeper` and its `recheck`, `MarkDiagnosis`, `LowerAutonomy`, `ClearAutonomy`, `ListAutonomy`, `GetAutonomy`; hooks in `incidentAdvancer` and `phaseLauncher`; `ExecutionRequest.Unattended` |
| adapters | `filestore.AutonomyRepository`; `axlr.preparedPolicy.actAlone`; the routes and the contract; `nodeproto.RunMessage.unattended` |
| cmd, panel | `autonomy list|get|lower|clear`, `incident diagnosis`; the incident page, the mark, and a bot edit that keeps a trigger's `ladder` |

## Alternatives

- **Promote from `Incident` fields.** The history must outlive any incident and a runbook is seen across many: another aggregate. Rejected.
- **Count per incident.** §4's reopening turns twenty runs into one incident and §8.3 counts applies (D13). Rejected.
- **A new `alertName` field on the incident.** Precise, but a stored field for what the title already says. Matching the title exactly against the declared runbooks fails closed. Taken instead.
- **Approve the call from the control plane under a policy (`policy:ladder`).** It would work with AXLR's `--approve local`, and it would make an act of the control plane look like a decision on every apply, under a name the signals must exclude. `--approve all` with a tight allow list is what the runtime contract offers. Rejected.
- **`--approve all` for every bot at `act-within-policy`.** Simpler, and it changes what existing bots do. Rejected: a bot with no ladder behaves as today.

## Trust boundary

**A rule-authored twin of a real alert cannot be told from it, and is uniform: this is a limit of the design, held by deployment and not by code.** The alert rules are part of what this trusts. A runbook is named by an alert's `alertname` and its namespace by the alert's `namespace` label, so whoever can write an alert rule can name a runbook and a namespace, and reach an unattended write inside the runbook's bounds. **Before opting a bot in, deploy with Prometheus Operator's `enforcedNamespaceLabel` or an equivalent**, so a tenant's rule cannot claim another namespace, and treat the rules of those namespaces as you treat the bot's definition.

## Consequences

- A bot opts in only with: `approvals.approvers` naming people (and no bot may later be registered under one of those names), the bot at `act-within-policy`, the apply trigger at `act-with-approval` with its own `tools`, and rules deployed under the trust boundary above.
- An apply with no green leaves the runbook on the card until a person runs `autonomy clear` or `autonomy lower`. A pending grant cancelled before it ran ends its own entry.
- A bot on a ladder must be at `act-within-policy`, and its other triggers are held to that level. Its incident triggers name their own; its **manual** tasks run at the bot's level with `--approve local` (local writes run, MCP writes still ask). Keep the local write tools off such a bot, or drop its manual trigger.
- An exit run of phase 3 needs a verify phase: without a green verdict nothing counts toward the applies.
- The history keeps the last 4096 lines, dropping first those that moved no step. Counters stay exact; a redelivery of a cycle older than that would count again.
- Thresholds in `thresholds` are read when a signal arrives and at launch. Raising them does not lower a runbook already at the top, but it may no longer act alone until it earns the new number.

## Left out

Autonomy per tool arguments other than the namespace (a runbook whose write has no `namespace` argument cannot be bound and always takes the card); Jev as a judge of the lab's signals (§11, the judge reads the incident, not the ladder); a bus event for a step change (audit only); rollback as a phase of its own; operators resetting or raising a runbook by hand; the panel's bot editor showing the ladder (it keeps it, it does not edit it).
