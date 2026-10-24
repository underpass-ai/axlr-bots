# Runbook: start tasks from a webhook or on a timer

Give a bot automatic triggers, fire them, and verify that a redelivery or a restart creates nothing twice. The trigger types, their fields and their keys are in the [bot definition](../reference/bot-definition.md#triggers).

## Add the triggers

[`examples/bot-triggers-patch.json`](../../examples/bot-triggers-patch.json) adds a timer every ten minutes and a webhook to the example bot. Replace the secret (16 to 256 characters) first.

```bash
axlr-bots bot update bot --file examples/bot-triggers-patch.json
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
