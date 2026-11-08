# ADR: an incident's postmortem

Status: accepted, 8 November 2026. axlr-sre design §4 (the state diagram: `Resolved → Postmortem` for sev1 or sev2), §10 (the scribe: timeline from the events, cause, actions with an owner and a date), §12 (phase 3, whose exit criterion is a postmortem a person approved). Builds on the incident phases ([ADR](incident-phases.md)), the outputs ([ADR](incident-outputs.md)) and the approvers ([ADR](approvers.md)).

## Context

Each phase of an incident is a task of its own, launched by the control plane. A resolved incident ended there. The design asks for one more phase: a bot writes down what happened and a person approves it. Two things differ from the phases before. It comes after the incident is over, so it must not be able to change that: a scribe that fails is not an incident that needs a person again. And a person reviews text, not a tool call, so the approval is not the task approval of the apply phase.

## Decisions (8 November 2026)

| # | Decision |
|:--|:--|
| D1 | **A field, not a status.** The incident stays `Resolved` and carries `postmortem`, a value with states of its own: `Writing`, `Draft`, `SentBack`, `Failed`, `LeftToPerson`, `Approved`. See the alternatives below |
| D2 | The phase is `postmortem`. A resolved incident launches it on the one teammate of its triage bot that declares `{"type": "incident", "phase": "postmortem"}`, under the key `incident:<id>:<cycle>:postmortem`. None or two declaring it: the incident stays `Resolved`, audited `incident.postmortem` (denied). Every resolution launches it (the operator's `incident resolve`, Alertmanager's, CloudWatch's) except the one `bot delete` makes: the team is going away |
| D3 | Which severities get one is on that trigger: `severities: ["sev1", "sev2"]`, the default. A severity it does not list is skipped, audited with outcome `ok` and `skipped` |
| D4 | The task's input is a named document, `axlr-bots.incident-postmortem-input/v1` (`application.PostmortemInput`): timeline, diagnosis, proposal, who decided what and when, verify's verdicts, cost, and on a rewrite the person's note and the draft. **The objective carries the same facts**, cut to fit 8000 bytes: only a Jev bot reads a task's input; a runtime reads its objective. Its template fields are `timeline`, `diagnosis`, `proposal`, `decisions`, `verdict`, `cost`, `note`, `draft` and the incident's own |
| D5 | The task's answer is the postmortem, stored as Markdown (at most 32000 bytes, cut as a diagnosis is) with its author task. A task that failed, expired, was cancelled or answered nothing leaves `Failed` with the reason. Nothing else moves |
| D6 | A person answers with `POST /v1/incidents/{id}/postmortem/approve` or `/send-back` `{note}`, `incident postmortem approve\|send-back` or the panel. The actor is asserted as on a task's decision (`X-Axlr-Actor` or `decidedBy`) and **must be named**: the answer is recorded under it. A `policy:` name is refused; on a daemon so is its own actor, whatever the bot lists. `approvers` of the bot that took the phase apply when it lists them. Refusals are audited `incident.postmortem.refused` with the actor |
| D7 | A send-back relaunches the phase with the note (key `…:postmortem:<attempt>`). `IncidentMaxPostmortemSendBacks` is 2, the same rule as a proposal's `sendBacks`: the second send-back launches nothing and leaves it `LeftToPerson`, whose last draft may still be approved. The count is the postmortem's own: a reopening resets the incident's |
| D8 | Approval publishes `incident.postmortem_approved`, in the outputs' default list; its body carries the text cut to 2 KiB and who approved it. A target stored with the old default list hears it too; one that chose its events does not until it names it. The drafted, failed and sent-back events exist and are nameable, not default |

## Why a field

| | A field on a Resolved incident (chosen) | A status `Postmortem` |
|:--|:--|:--|
| the design's diagram | an arrow drawn differently: the same lifecycle, inside `Resolved` | literal |
| "Resolved stays reachable" | by construction | every incident of a qualifying severity would leave it, or need two statuses that both mean closed |
| the readers of `Resolved` | unchanged: `IsResolved`, `CanReopen`, `Repeat`, `AttachTask`, `--status Resolved`, the outputs, the panel's filters | each must learn the new status; one forgotten reopens or repeats wrongly |
| a failure of the phase | cannot reach the incident: the moves are on the value | needs a rule at every transition |
| stored incidents | absent field, loaded untouched | same |

## What was settled beyond the design

- **A cycle may hold several postmortem tasks**, one per send-back. The rule of one task per phase per cycle stays for the other phases. Whether one is due is the postmortem's to say (`PostmortemDue`), not `HasTask`.
- **A reopening leaves the postmortem as it was.** It belongs to its cycle: nothing can approve or send it back once the incident is `Open`, even after a later resolution, and the next resolution writes another, replacing it. An approved text replaced that way stays in its author task's answer. A scribe still running when the incident reopens only adds its cost.
- **Whose approvers.** Those of the bot that took the phase, the only bot the postmortem is tied to. If that bot was deleted nobody can read them, and the answer is refused (fail closed).
- **A `Failed` postmortem is never retried by itself.** A person may send it back. A send-back whose task cannot start (a paused scribe) stays `SentBack` and audited; a second send-back tries again and does not count twice.
- **What the input leaves out.** Each field is named, and the timeline takes a short list of payload keys, so a tool call's arguments, an alert's payload, the trigger, output and notification credentials never enter it. The diagnosis and the proposal are the bots' own words: the control plane does not scan them. The timeline leaves out the postmortem's own events and tasks, and `incident.repeated`, which comes every repeat interval.

## How it is carried out

```text
any status ──resolved──▶ Resolved ──severity on the trigger──▶ task of phase postmortem
                                                                 Writing
Writing  ──task Completed with an answer──▶ Draft                incident.postmortem_drafted
Writing  ──task Failed, Expired, Cancelled, no answer──▶ Failed  incident.postmortem_failed
Draft, Failed ──send-back {note}──▶ SentBack ──launch──▶ Writing incident.postmortem_sent_back
Draft, Failed ──second send-back──▶ LeftToPerson
Draft, LeftToPerson with a draft ──approve──▶ Approved           incident.postmortem_approved → outputs
```

| Layer | What changed |
|:--|:--|
| domain | `IncidentPhasePostmortem`, `IncidentPostmortem`, its state and snapshot, `Incident`'s moves (`incident_review.go`), `Trigger.WritesPostmortemFor`, the four events, the outputs' default |
| application | `postmortemLauncher` (never returns an error: a resolution is not undone by what comes after it), `postmortemBrief`, `PostmortemInput`, `incidentAdvancer.postmortemEnded`, `ReviewPostmortem`, the outputs' body and Slack message |
| adapters/unixapi | the two routes, `Service`, `Local`, `Client`, the contract |
| cmd/axlr-bots | `incident postmortem show\|approve\|send-back` |
| panel | the Postmortem section of an incident, its text in a reader, its state, Approve and Send back; a `postmortem` mark in the list; a trigger's `severities` kept when a definition is saved |

## Left out

- **Writing the Markdown into a cluster repository** (design §10). The approved text is what the exit criterion needs; a repository needs a credential and a place to write, which are the scribe's bot's concern.
- **The scribe itself.** It is registered later in the axlr-sre repo.
- **Editing the text.** A person approves it or sends it back; `LeftToPerson` keeps the last draft.
- **Two persons on a postmortem.** `approvals.twoPersons` is about writes in production.
- **A crash between a resolution and the launch.** The incident stays `Resolved` with no postmortem and nothing retries it, since no later alert reaches a closed incident's phases. An operator sees the audit's absence, not a state.
- **Rendering.** The panel shows the Markdown as text, selectable; it does not lay out headings.
- **Lab severities.** The lab's alerts are `warning` (sev3) but for `LabNodeNotReady` (`critical`, sev1). The default is not changed for it: the lab's scribe trigger lists `sev3`.
