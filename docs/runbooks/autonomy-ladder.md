# Autonomy ladder

Let one runbook, in one namespace, earn the right to apply without a card ([ADR](../architecture/autonomy-ladder.md)). Until it has earned it, nothing about the apply changes.

## Opt a bot in

The bot that applies declares the ladder on its apply trigger, runs at `act-within-policy` and lists `approvals.approvers`; the trigger itself stays at `act-with-approval`. A bot without approvers is refused.

**Alert rules are part of the trust boundary.** A runbook and its namespace are read from the alert: whoever can write a rule can name them. Deploy with Prometheus Operator's `enforcedNamespaceLabel` or an equivalent before opting a bot in.

```json
{
  "level": "act-within-policy",
  "approvals": {"approvers": ["tirso", "sre-judge"]},
  "allowedTools": ["local_read", "local_list", "get", "apply", "patch"],
  "triggers": [
    {
      "type": "incident",
      "name": "apply",
      "phase": "apply",
      "level": "act-with-approval",
      "tools": ["local_read", "local_list", "apply", "patch"],
      "ladder": {
        "runbooks": [
          {"name": "LabDeploymentDrift", "tool": "apply", "namespaces": ["drift-lab"]}
        ]
      }
    }
  ]
}
```

| Field | Meaning |
|:--|:--|
| `name` | the alert name, Alertmanager's `alertname`: the incident's title must be exactly `<name> in <namespace>` |
| `tool` | the one write tool this runbook may run alone; it must take a `namespace` argument, which is what binds it. Changing it starts the runbook over |
| `namespaces` | where. An incident in another namespace keeps its card and counts nothing |
| `thresholds` | optional: `diagnoses`, `proposals`, `applies`. Defaults 5, 10, 20 |

The default thresholds are not lowered for a lab: the point is that a runbook gets there by its history.

Raising a bot's level to `act-within-policy` also raises what its manual tasks may do: keep local writes off such a bot, or drop its manual trigger.

## What happens

```text
step                what the apply does            how it moves
observe             card                           5 diagnoses marked correct
propose             card                           10 proposals approved
act-with-approval   card                           5 + 10 + 20 applies approved and verified green
act-within-policy   no card, inside the bounds     a deny -1; a red verify, a failed apply or a second write -2
```

Only people move a runbook up: a verify's green counts for a cycle a person approved, and only an approval of the runbook's own tool counts. Any denial moves it down.

Before an apply runs alone the ladder is asked again. If the incident no longer stands on the task, it is cancelled; if the runbook is no longer granted, the run takes the card. A sev1 never runs alone.

## Mark a diagnosis

On the incident's page in the panel, **Diagnosis correct** or **Diagnosis wrong** (with the panel's actor set in Settings). Or:

```bash
axlr-bots --actor tirso incident diagnosis inc-… correct
```

Each cycle has one mark. Marking it the other way is refused; the same one again changes nothing.

## Read a runbook

```bash
axlr-bots autonomy list --runbook LabDeploymentDrift
axlr-bots autonomy get ladder-…
```

## Stop a runbook

```bash
axlr-bots --actor tirso autonomy lower ladder-… --reason "the last two applies looked wrong"
```

One step down, for a named approver, audited. It ends every outstanding apply; a task already granted takes the card when it comes to run.

## A runbook stuck on the card

`outstanding` lists the applies whose end is not recorded: a verify with no verdict, a run cancelled halfway, a lowering the store lost. While one is there the runbook gets no grant. It ends on a recorded green or lowering; otherwise look at the apply and `autonomy lower` it.

`step` is where it stands; `earned` is what counts since it was last lowered; `totals` falls only when the runbook's tool changes; `history` lists the signals. A lowering's entry has `from`, `to` and `earnedBefore`: what it took back.

## Why did this apply keep its card?

The audit says, on the `incident.apply` line of the incident:

| `ladder` | What to do |
|:--|:--|
| `runbook … stands on …` | it has not earned the top step yet |
| `namespace … is outside the bounds` | add the namespace to the runbook |
| `a sev1 never acts alone` | by design |
| `an earlier apply … has no recorded end` | see above |
| `the alert group is mixed` or `not known to be one alert` | by design: one alert must speak for the group; a hand-opened incident never acts alone |
| `the grant was taken back: …` | it was granted at launch and no longer was when it ran |
| `no run: …` | its incident moved on; the task was cancelled |
| `the bot's approvals send this incident to two persons` | by design: two people, never a ladder |
| `the incident has N namespaces` | a runbook counts in one: narrow the alert group |
| `the runbook's record could not be read` | look at the state directory; the card is the safe side |

An apply that acted alone shows `level: act-within-policy` and `ladder: act-within-policy` on that line, and `policy.ladder` on `task get`.

## When a runbook is lowered

Nothing is reset behind your back. The history keeps every step; the earning restarts, and the panel shows why. A lowered runbook keeps its card until it earns the top step again.
