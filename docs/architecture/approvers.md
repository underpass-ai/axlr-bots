# ADR: who may approve, and two persons in production

Status: accepted, 8 November 2026. axlr-sre design §2 (the row "who may approve"), §4 (the Incident's `approvals`), §8.1 (the never-list: "write to prod on a single decision (sev1/sev2)"), §8.3 (progressive autonomy), §9 (the Incidents screen), §12 (phase 3), §13 Q6 and Q7. Builds on the incident's apply phase ([ADR](incident-phases.md), [axlr-bots#133](https://github.com/underpass-ai/axlr-bots/pull/133)).

## Context

Until now any holder of the operator bearer token could approve or deny any call. The CLI's `--actor` named who was acting, but only in process: through the daemon the decision was recorded as the daemon's own actor, and the panel sent no name at all. The design leaves production at `propose` until two things exist (Q6): a list of who may approve (Q7), and two different people for a write in production (§8.1). There is no user system in the control plane, and this ADR does not add one.

## Decisions (8 November 2026)

| # | Decision |
|:--|:--|
| D1 | **The actor is asserted, not proven.** A decision names who takes it: `X-Axlr-Actor` on `POST /v1/tasks/{id}/decide`, `/approve` and `/deny`, or `decidedBy` in the decide body (both given must agree). The CLI sends its global `--actor` (default `cli:$USER`); the panel sends its `controlPlane.actor` ("Decide as" on the Settings screen) on every request. The token holder says the name; the control plane checks nothing behind it. Per-actor tokens are future work ([axlr-bots#137](https://github.com/underpass-ai/axlr-bots/issues/137)) |
| D2 | A bot's `approvals` block takes `approvers: ["tirso", "oncall"]`: who may approve **and** deny its calls, compared without the CLI's `cli:` prefix and without case (`tirso`, `cli:tirso` and `Tirso` are one person): the CLI's default `cli:$USER` and the panel's plain name never count as two people. A `policy:` name is no approver. Absent or empty is anyone with the token, as before. A decision by another actor, or by nobody named on a bot that lists approvers, is refused with `403 forbidden` and audited `approval.refused` with the actor; nothing changes. No person decides as `policy:…`, and on a daemon, where names count (approvers, two persons), nobody decides under the control plane's own actor: both `403` (review of 8 November 2026) |
| D3 | `approvals.twoPersons` takes `{"namespaces": [...]}`, `{"severities": [...]}` or both (one match is enough): the incidents whose calls wait for two different people. It reads the incident's `namespaces` (new: every namespace its alerts named; any one listed is enough) and `severity`. **`twoPersons` needs `approvers` listing at least two people**: without a list any name typed with the token would count, and one person could be two names. **Two persons is for incidents only**: a task of no incident has neither and waits for one person, whatever the lists say |
| D4 | A call that waits for two: the first approval is recorded on the task's wait (`approvedBy`, `bot.approval.first`, audit `approvals.first` with the actor and the time) and **runs nothing**; the task stays `WaitingApproval` and the incident `Proposed`. A second approval by a different actor runs the call as one approval does today. The same actor again is refused (`409 state`, audited `approval.refused`); so is an approval with no actor named. A denial by anyone who may decide ends the wait as a denial. The deadline is the wait's, set when it began: it covers the whole wait, and an expiry keeps who had approved |
| D5 | A policy is not a person, and never one of two approvals. `autoApprove` and the team's events (`policy:autoApprove`, `policy:team-event`) are not checked against `approvers`. The team's events (emit, subscribe) are approved outright as before, even on a wait that needs two: they are not writes and the control plane checks each. An `autoApprove` rule **never** decides a call that waits for two: two people do (§8.1). An operator who sets `twoPersons` and lists the apply tool in `autoApprove` gets the people, not the rule |
| D6 | **Production stays at `propose` by default (Q6)**: nothing in this change raises a level. The lab runs `act-with-approval` with `approvers` set to the judge's actor (`sre-judge` decides as itself). Production moves to `act-with-approval` only with `approvers` **and** `twoPersons` on the bot that applies (the namespaces of production, or `sev1` and `sev2`) |
| D7 | Compatibility: a bot without the new fields, and a task, an incident or a bot stored before them, behave and read back exactly as before. A wait carries `approvalsNeeded` and `approvedBy` only when it needs two; an incident carries `namespace` only when its alerts named one |

Why asserted and not proven (D1): the smallest change that makes `approvers` mean something is to stop dropping the name the operator already gives. A login, roles or an identity provider would be a product of their own; per-actor tokens (#137) are the next step that turns the name into a fact. Until then the ADR says it plainly: two persons is two names, and anyone holding the token can type both.

Why incident-only (D3): the namespace and the severity are the incident's, and a task has neither. A task could carry a namespace label later; this change keeps two persons on incidents, where §8.1 asks for it.

## How the decisions are carried out

### The `approvals` block

```json
{
  "approvals": {
    "ttl": "30m",
    "onExpire": "deny",
    "approvers": ["tirso", "oncall"],
    "twoPersons": {"namespaces": ["prod", "shop-prod"], "severities": ["sev1", "sev2"]}
  }
}
```

`approvers`: at most 32 names, each without spaces or control characters, at most 128 characters, no repeats (compared without `cli:` and case), no `policy:` name. `twoPersons`: at least one entry, and `approvers` listing at least two people; namespaces are identifiers (lowercase, digits, `.`, `_`, `-`), severities `sev1` to `sev4`; at most 32 of each, no repeats. Anything else is refused at register and update: `twoPersons: true`, an unknown key, an empty `{}`.

### The rules, in order

```text
decision ─▶ actor named? ─valid─▶ task WaitingApproval, wait open, in time … (as before)
            │
            ├─ bot lists approvers, actor absent or not listed ──▶ 403 forbidden, approval.refused
            ├─ deny ───────────────────────────────────────────▶ the wait ends denied (approvedBy kept)
            └─ approve, the wait needs 2:
                 ├─ no actor named ───────────────────────────▶ 403 forbidden, approval.refused
                 ├─ the actor already approved ────────────────▶ 409 state, approval.refused
                 ├─ first of two ──────────────────────────────▶ approvedBy [actor], bot.approval.first, nothing runs
                 └─ second, another actor ─────────────────────▶ the call runs; approvedBy [first, second]
deadline passed, one approval recorded ────────────────────────▶ expired, as before; approvedBy kept
```

How many people a wait needs is set when the task starts to wait (`approvalsNeeded: 2`), from the task's bot's `twoPersons` and the task's incident; at a decision it is raised if the bot asks for two persons since. An incident that cannot be read counts as two when the bot asks for two persons anywhere (fail closed).

### What an operator reads

`task get` and `incident get` (its `currentTask.approval`) show the wait with `approvalsNeeded: 2` and `approvedBy: [{"actor": "tirso", "at": "…"}]`. The first approval answers with the task still `WaitingApproval` and the stopped execution (`task approve` exits 3, as a run that waits). `bot.task.waiting_approval` says `approvalsNeeded`; `bot.task.decided` says `decidedBy` (the named actor now) and, for a call that waited for two, `approvedBy`. The panel's Needs-you card on the incident says **1 of 2 approvals · approved by tirso**, and its banner after the first Approve says **First approval recorded** (nothing ran); its timeline says **First approval**. The audit log's `actor` is the named actor for every line of the decision.

### Where the namespace comes from

The incident keeps every namespace its Alertmanager alerts name: the group's or the common `namespace` label when there is one, else each alert's own, in order and once each (a mixed group names them all), up to 32. A later alert of the group adds the ones it names. `namespace` is the first, for a person to read; `namespaces` lists them all when there are two or more. `POST /v1/incidents` and `incident open --namespace` take one by hand (a bad one is `400`). A label that is no namespace (a space, a control character, too long) is left out and audited `incident.open`: it never refuses the alert, which Alertmanager would only send again. `incident.opened` says `namespace` and `namespaces`. A `twoPersons` that names any of them waits for two.

### Layer map

| Layer | What changed |
|:--|:--|
| domain | `Approvals` (approvers, twoPersons, `MayDecide`, `ApprovalsNeeded`), `TwoPersons`, `ValidateActor`, `ApprovalWait` (`approvalsNeeded`, `approvedBy`, `ApprovalMark`), `Task.AddApproval`, `FinalApproval`, `RaiseApprovalsNeeded`, `Incident.namespace`, `AlertmanagerPayload.Namespace`, event `bot.approval.first` (`domain/event_type.go`) |
| application | `ApproveTask.Execute(…, actor)`: the named actor becomes the use case's actor (audit, events, incident); `mayDecide`, `first`; `approvalsNeeded` when a wait opens (settler) and the rule that `autoApprove` never decides a call that waits for two; `OpenIncidentCommand.namespace` |
| adapters/unixapi | `X-Axlr-Actor` and `decidedBy` on the decide routes; the contract |
| cmd/axlr-bots | `task approve|deny|decide` send `--actor`; `incident open --namespace` |
| panel | `controlPlane.actor` ("Decide as"), sent as `X-Axlr-Actor`; the Needs-you card's progress; the banner after a first approval ("First approval recorded"); the event's words (`panel/src/domain/event.rs`) |

## Left out

- Per-actor tokens and any proof of the actor ([#137](https://github.com/underpass-ai/axlr-bots/issues/137)).
- Approvers per namespace (§8.1 says "per bot and per namespace"): one list per bot today; a bot per production namespace gets its own list.
- Two persons for a task of no incident, or from a task's own namespace label.
- More than two people.
- The panel's Tasks screen and the overview's Needs-you list do not show the progress; the incident's page does. The panel does not hide Approve from the person who already approved: the control plane refuses them.
- Notifications are not sent again after the first approval: the wait is still open, the first delivery's links still decide it.
- A wait never needs fewer people than it began with: a bot whose `twoPersons` is removed mid-wait still takes two for that call (fail closed); the next wait needs one.

## Review of 8 November 2026

The review of #139 found three ways one person could satisfy two persons, and two smaller gaps; each is fixed above and tested: twoPersons without approvers (now refused) and one person under two spellings (`ActorKey`); the team's events recorded as a first approval (a policy never is one); a person naming a `policy:` actor or the daemon's own; a mixed alert group read by its first alert only (`namespaces`); a strange namespace label refusing the alert (left out and audited).
