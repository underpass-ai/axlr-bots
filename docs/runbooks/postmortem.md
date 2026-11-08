# Runbook: an incident's postmortem

When an incident of a qualifying severity is resolved, the control plane asks a bot to write its postmortem. A person approves the text or sends it back. The incident stays `Resolved` throughout: a postmortem that fails never reopens or escalates it. The decisions are in the [ADR](../architecture/incident-postmortem.md).

```text
Resolved ──▶ Writing ──answer──▶ Draft ──approve──▶ Approved ──▶ the outputs announce it
                │                  └──send back──▶ Writing (once) ──▶ Draft ──send back──▶ LeftToPerson
                └──fails──▶ Failed ──send back──▶ Writing
```

## Give a team a scribe

One teammate of the triage bot declares the phase:

```json
{"type": "incident", "name": "postmortem", "phase": "postmortem", "severities": ["sev1", "sev2"]}
```

`severities` is optional and defaults to `sev1` and `sev2`. It takes `sev1` to `sev4`, without repeats, and only on this phase. No teammate declaring the phase, or two, leaves the incident `Resolved` and is audited `incident.postmortem` (denied).

How an alert gets its severity:

| Source | Severity |
|:--|:--|
| Alertmanager `severity` label | `critical`, `page` → sev1; `error`, `major` → sev2; `warning` and anything else or none → sev3; `info`, `none` → sev4 |
| `cloudwatch` trigger | its own `severity`, sev3 when absent |
| `incident open --severity` | as given, sev3 when absent |

So a rule labelled `warning` is sev3 and gets no postmortem by default. A lab whose alerts are `warning` lists `sev3` on the trigger.

The trigger's optional `objective` may name `{{.timeline}}`, `{{.diagnosis}}`, `{{.proposal}}`, `{{.decisions}}`, `{{.verdict}}`, `{{.cost}}`, `{{.note}}` and `{{.draft}}`, each cut to fit, and the incident's own fields. A runtime reads the objective; the task's `input` has the same facts whole (`axlr-bots.incident-postmortem-input/v1`).

## Read and answer it

```console
$ axlr-bots incident postmortem show inc-18              # the Markdown on stdout, its state on stderr
$ axlr-bots --actor tirso incident postmortem approve inc-18
$ axlr-bots --actor tirso incident postmortem send-back inc-18 --note "name an owner for each action"
```

On the panel: **Incidents**, the incident, section **Postmortem**, with **Approve** and **Send back**; the list marks one that waits with `postmortem`. The panel answers as its **Decide as** name.

Check: `incident get` answers `postmortem` with `state`, `markdown`, `authorTaskId`, `approvedBy` and `approvedAt`; the audit log has `incident.postmortem.approve` with the actor; a target that hears the default events got `incident.postmortem_approved`.

## Who may answer

A name is required. `approvals.approvers` of the bot that took the phase apply when it lists them, compared as on a task's decision. A `policy:` name is refused, and so is the daemon's own name: a unit started without `--actor` is `cli:$USER`, so start it with `--actor <name>` or that user's answers are refused. A refusal changes nothing and is audited `incident.postmortem.refused`.

## When it does not work

| What you see | Why | What to do |
|:--|:--|:--|
| no `postmortem` on a resolved incident, audit says `skipped` | the severity is not on the trigger | add it to `severities` |
| audit `incident.postmortem` denied | no teammate takes the phase, two do, or the scribe is paused | fix the team; a later resolution launches it |
| `Failed` | the task failed, expired, was cancelled or answered nothing; `reason` says which | send it back with a note |
| `SentBack` and no task | the rewrite could not start (see the audit) | fix it and send it back again; it does not count twice |
| `LeftToPerson` | sent back twice | approve the last draft, or write it outside the control plane |
| `403 forbidden` | no name, a `policy:` name, the daemon's own, or not an approver | decide as yourself |
| `409` on an old postmortem | the incident reopened: it belongs to an earlier cycle | wait for the next resolution |
