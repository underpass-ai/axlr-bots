# Autonomy ladder

Let one runbook, in one namespace, earn the right to apply without a card ([ADR](../architecture/autonomy-ladder.md)). Until it has earned it, nothing about the apply changes.

## Opt a bot in

The bot that applies declares the ladder on its apply trigger, and runs at `act-within-policy`; the trigger itself stays at `act-with-approval`.

```json
{
  "level": "act-within-policy",
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
| `name` | the alert name: Alertmanager's `alertname`, CloudWatch's alarm name |
| `tool` | the one write tool this runbook may run alone; it must take a `namespace` argument, which is what binds it |
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
act-within-policy   no card, inside the bounds     a deny -1, a red verify or failed apply -2
```

Only people move a runbook up: a verify's green counts for a cycle a person approved, and a policy's approval counts nothing.

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

`step` is where it stands; `earned` is what counts since it was last lowered; `totals` never falls; `history` lists every signal. A lowering's entry has `from`, `to` and `earnedBefore`: what it took back.

## Why did this apply keep its card?

The audit says, on the `incident.apply` line of the incident:

| `ladder` | What to do |
|:--|:--|
| `runbook … stands on …` | it has not earned the top step yet |
| `namespace … is outside the bounds` | add the namespace to the runbook |
| `a sev1 in production: the card stays` | by design: production is `approvals.twoPersons.namespaces` |
| `the bot's approvals send this incident to two persons` | by design: two people, never a ladder |
| `the incident has N namespaces` | a runbook counts in one: narrow the alert group |
| `the runbook's record could not be read` | look at the state directory; the card is the safe side |

An apply that acted alone shows `level: act-within-policy` and `ladder: act-within-policy` on that line, and `policy.ladder` on `task get`.

## When a runbook is lowered

Nothing is reset behind your back. The history keeps every step; the earning restarts, and the panel shows why. A lowered runbook keeps its card until it earns the top step again.
