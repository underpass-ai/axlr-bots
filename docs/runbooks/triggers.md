# Runbook: start tasks from a webhook or on a timer

Give a bot automatic triggers, fire them, and verify that a redelivery or a restart creates nothing twice; point Alertmanager at a bot and verify that a group resent thirty times is one incident; open, follow and close an incident by hand. The trigger types, their fields and their keys are in the [bot definition](../reference/bot-definition.md#triggers).

## Add the triggers

[`cmd/axlr-bots/testdata/bot-triggers-patch.json`](../../cmd/axlr-bots/testdata/bot-triggers-patch.json) adds a timer every ten minutes and a webhook to the example bot. Replace the secret (16 to 256 characters) first.

```bash
axlr-bots bot update bot --file cmd/axlr-bots/testdata/bot-triggers-patch.json
```

Check: `bot get bot` lists the three triggers with `"secret": "<redacted>"`; a `bot.updated` event; within a tick, `<state>/timers/bot~clock.json` exists with `nextFireAt`, and an interval timer fires at once, so `bot events bot` shows a `bot.trigger.fired` and a new task.

## Fire a webhook

From outside, over the operator address, with the trigger's own credential and a delivery id:

```bash
state=~/.local/state/axlr-bots
curl --cacert $state/ca/ca.crt -H "X-Axlr-Secret: $secret" -H "Idempotency-Key: gh-delivery-1" \
     -d '{"type": "deploy", "service": "web"}' https://cp.example.internal:7879/v1/triggers/bot/deploy
```

Or over the socket: `axlr-bots trigger fire bot deploy --body '{"type": "deploy", "service": "web"}' --delivery gh-delivery-1`.

Check: the first delivery answers `201` with `"created": true` and the task; the same `Idempotency-Key` again answers `200`, `"created": false` and the same task id; without the secret `403`; a wrong signature `403`; a body past 256 KiB `413`. `task get` shows the rendered objective (`Handle event deploy for web …`), `"trigger": {"type": "webhook", "name": "deploy"}` and the body as `input`; `bot events bot` has two `bot.trigger.fired` events for the delivery, the second with `"deduplicated": true`.

## Point Alertmanager at a bot

Give the bot an `alertmanager` trigger (a secret or a signing key, like a webhook) and a receiver in Alertmanager's configuration that posts to it; `send_resolved: true` is what closes the incident:

```yaml
receivers:
  - name: axlr-bots
    webhook_configs:
      - url: https://cp.example.internal:7879/v1/triggers/triage/alerts
        send_resolved: true
        http_config:
          tls_config: {ca_file: /etc/alertmanager/axlr-ca.crt}
          http_headers:                      # Alertmanager 0.27 or later; the route takes the trigger's own secret
            X-Axlr-Secret: {secrets: ["<the trigger's secret>"]}
route:
  group_by: [alertname, namespace]
  group_wait: 30s
  group_interval: 2m
  repeat_interval: 2m
  receiver: axlr-bots
```

Check: the first firing group answers `201`, `"action": "opened"`, the incident and its triage task; every resend of the same group (`repeat_interval`) answers `200`, `"action": "repeated"`, no task, and `incident get` shows `repeats` growing and the new alerts under `fingerprints`; when the group goes green, `"action": "resolved"`, the incident is `Resolved` and no task was started; a group that fires again within 30 minutes answers `"action": "reopened"` with a new task and `"cycle": 2`. After an hour of resends, `incident list` holds one incident for the group. A body that is not Alertmanager's answers `400`.

## Incidents

An incident is one alert group of a tenant and the triage tasks one bot ran for it ([operator API](../reference/operator-api.md#routes)). Alertmanager opens them through an `alertmanager` trigger (above); anything else, or a person, through `POST /v1/incidents` or `axlr-bots incident open`, for a bot that declares the `incident` trigger. One task per cycle: a repeated alert starts nothing.

```text
new group key ──▶ Open, cycle 1, a triage task ──task Completed──▶ Diagnosed (the answer is the diagnosis)
                      └──task Failed, Expired or Cancelled──▶ Escalated (a person must look)
same key, not Resolved ──▶ a repeat: its alerts added, no task
resolved group, or incident resolve ──▶ Resolved ──same key within 30 minutes──▶ Open, cycle + 1, a new task
                                                  └─same key later──▶ a new incident
```

| Status | Means |
|---|---|
| `Open` | nobody has said the cause yet: its triage task is pending or running |
| `Diagnosed` | the task completed; its answer, the cause with its evidence, is the incident's `diagnosis` |
| `Escalated` | the task failed, expired or was cancelled; `reason` says which, and a person takes it from there |
| `Resolved` | its alerts went green, or a person closed it; a running task still settles and its cost is still added, but it moves the incident no further |

```bash
axlr-bots bot update triage --file patch.json     # {"triggers": [{"type": "manual"}, {"type": "incident"}]}; the bot is Enabled
axlr-bots incident open --tenant default --bot triage --group-key "HighLatency/web" \
    --title "High latency in web" --severity sev2 --fingerprints a1
axlr-bots incident list --status Open
axlr-bots incident get inc-…                      # alerts, cycle, taskIds, diagnosis, reason, repeats, costUsd
axlr-bots incident resolve inc-… --reason "rolled back"
```

Check: the first `open` answers `created: true`, the incident `Open` at cycle 1 and its triage task, key `incident:<id>:1`, whose objective is the trigger's template or the default ("Incident … Find the cause with evidence and report it; change nothing") and whose `input` is the incident. The same group key again answers `created: false`, no task, and the incident with the new fingerprints and `repeats` one higher. After `resolve`, the same key within 30 minutes answers `reopened: true`, cycle 2 and a task keyed `…:2`. The bot's events show `incident.opened`, `incident.repeated`, `incident.resolved`, `incident.reopened`, and `incident.diagnosed` or `incident.escalated` when its task ends. Severity is `sev1` to `sev4` (`sev3` when absent); a bot without the `incident` trigger, or not Enabled, refuses the incident.

## Check the catch-up

Restart the control plane while a timer slot is in the past (stop it for longer than the interval, or restart it right after a fire):

```bash
systemctl --user restart axlr-bots.service
```

Check: the timer fires once for the slot it found in the past (one task for that slot, with the key `timer:clock:<slot>`), the slots in between are not replayed, and `<state>/timers/bot~clock.json` holds the next fire. A slot already fired before the restart creates no second task: the same key finds the same task.

## Remove or pause

- `bot update bot --file` with `{"triggers": [{"type": "manual"}]}` removes the timer and the webhook; the timer file is dropped at the next tick.
- A changed `every` or `cron` re-arms the timer; a paused bot's timers advance without firing.

## What the live run showed

Three demo runs within ten minutes, each re-applying the triggers and restarting both services: one timer task for the slot, one task for the redelivered webhook, two fires in the events ([live run](../live-run.md)).
