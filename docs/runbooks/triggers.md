# Runbook: start tasks from a webhook or on a timer

Give a bot automatic triggers, fire them, and verify that a redelivery or a restart creates nothing twice; point Alertmanager at a bot and verify that a group resent thirty times is one incident; point a CloudWatch alarm at a bot through Amazon SNS; open, follow and close an incident by hand. The trigger types, their fields and their keys are in the [bot definition](../reference/bot-definition.md#triggers).

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
            X-Axlr-Secret: {files: [/etc/alertmanager/secrets/axlr-trigger/secret]}
route:
  group_by: [alertname, namespace]
  group_wait: 30s
  group_interval: 2m
  repeat_interval: 2m
  receiver: axlr-bots
```

The secret is a mounted file (`files:`), not inline (`secrets:`): prometheus-operator re-renders Alertmanager's configuration and writes an inline `secrets:` value as the literal `<secret>`, which the trigger refuses with `403`. With kube-prometheus-stack, put the secret in a Secret, list it under `alertmanager.alertmanagerSpec.secrets` (it is mounted at `/etc/alertmanager/secrets/<name>/`) and name the file.

Check: the first firing group answers `201`, `"action": "opened"`, the incident and its triage task; every resend of the same group (`repeat_interval`) answers `200`, `"action": "repeated"`, no task, and `incident get` shows `repeats` growing and the new alerts under `fingerprints`; when the group goes green, `"action": "resolved"`, the incident is `Resolved` and no task was started; a group that fires again within 30 minutes answers `"action": "reopened"` with a new task and `"cycle": 2`. After an hour of resends, `incident list` holds one incident for the group. A body that is not Alertmanager's answers `400`.

## CloudWatch through SNS

A CloudWatch alarm reaches a bot through an Amazon SNS topic with an HTTPS subscription to the trigger's route. Give the bot a `cloudwatch` trigger: no secret (SNS cannot send one), a severity for its incidents, the topic it takes messages from in `topics`, and `autoConfirm` left `false`.

```json
{"triggers": [{"type": "cloudwatch", "name": "cw", "severity": "sev3", "autoConfirm": false,
               "topics": ["arn:aws:sns:eu-west-1:123456789012:axlr-sre-alarms"],
               "objective": "CloudWatch alarm {{.alarmName}} fired: {{.reason}}. Find the cause with evidence and report it; change nothing."}]}
```

```bash
axlr-bots bot update triage --file patch.json
topic=$(aws sns create-topic --name axlr-sre-alarms --query TopicArn --output text)
aws cloudwatch put-metric-alarm --alarm-name shop-web-5xx ... --alarm-actions "$topic" --ok-actions "$topic"
aws sns subscribe --topic-arn "$topic" --protocol https \
    --notification-endpoint https://cp.example.internal:7879/v1/triggers/triage/cw
```

`--ok-actions` is what closes the incident, as `send_resolved` does for Alertmanager. SNS must reach the operator address and trust its certificate: SNS only delivers to HTTPS endpoints whose certificate a public CA signed, so put the route behind a proxy or load balancer with a public certificate when the fleet CA signs the operator address.

**Confirm the subscription by hand.** SNS posts a `SubscriptionConfirmation` first. The trigger verifies it, answers `200` with `"action": "ignored"`, and records the `SubscribeURL` in the audit log (`<state>/audit.jsonl`, action `trigger.subscription`, with the `topicArn`). Read it there and open it, or confirm with the token:

```bash
grep '"trigger.subscription"' ~/.local/state/axlr-bots/audit.jsonl | tail -n 1   # check topicArn is yours
curl -s "$subscribe_url"                                                       # or: aws sns confirm-subscription --topic-arn "$topic" --token "$token"
aws sns list-subscriptions-by-topic --topic-arn "$topic"                      # SubscriptionArn is no longer PendingConfirmation
```

Why by hand: the signature proves that a message came from SNS, not from whose topic. Any AWS account can subscribe this URL to a topic of its own; confirming every request would let that topic open incidents here. With `"autoConfirm": true` the trigger visits the URL itself (`"action": "confirmed"`, audited as `trigger.subscription.confirm`); the trigger refuses it without `topics`, so only the topics listed can be confirmed. `topics` lists up to 16 SNS topic ARNs; a message of another topic is `403` and audited (`trigger.fire`, outcome `denied`). Without `topics` any topic SNS signed for is taken.

**Test it** with a message shaped like an alarm's (SNS signs it like any other), the ALARM first and the OK after it:

```bash
aws sns publish --topic-arn "$topic" --message '{"AlarmName":"shop-web-5xx","NewStateValue":"ALARM","NewStateReason":"test from the runbook","StateChangeTime":"2026-11-08T10:00:00.000+0000","AlarmArn":"arn:aws:cloudwatch:eu-west-1:123456789012:alarm:shop-web-5xx"}'
aws sns publish --topic-arn "$topic" --message '{"AlarmName":"shop-web-5xx","NewStateValue":"OK","NewStateReason":"test from the runbook","StateChangeTime":"2026-11-08T10:05:00.000+0000","AlarmArn":"arn:aws:cloudwatch:eu-west-1:123456789012:alarm:shop-web-5xx"}'
# or the alarm itself: aws cloudwatch set-alarm-state --alarm-name shop-web-5xx --state-value ALARM --state-reason test
```

Check: the ALARM answers `201`, `"action": "opened"`, an incident with source `cloudwatch`, group key `cloudwatch:<AlarmArn>`, title `shop-web-5xx: test from the runbook`, and its triage task with the alarm's JSON as `input`; the same message again (SNS retries) and an ALARM with a new `StateChangeTime` while it is open answer `200`, `"repeated"`; the OK answers `"resolved"` and starts nothing; an ALARM within 30 minutes of it `"reopened"`, cycle 2; a later one opens a new incident. `INSUFFICIENT_DATA` answers `"ignored"` and is audited. A body whose signature does not verify, or whose `SigningCertURL` is not `https://sns.<region>.amazonaws.com/SimpleNotificationService-<id>.pem` (or whose `SubscribeURL` is not SNS's `?Action=ConfirmSubscription&TopicArn=…&Token=…` on that host), is `403`, audited and with nothing fetched; so is a message of a topic not in `topics`, and one whose `Timestamp` is more than 15 minutes from the control plane's clock, either way (a replay, or a clock out of step: keep the host on NTP); each refusal is audited as `trigger.fire`, outcome `denied`, with the reason. A body that is not SNS's is `400`.

| SNS message | Alarm state | Action | Incident | Task |
|---|---|---|---|---|
| `SubscriptionConfirmation` | | `ignored` (`confirmed` with `autoConfirm`) | none | none |
| `UnsubscribeConfirmation` | | `ignored`, audited | none | none |
| `Notification` | `ALARM`, a new group key | `opened` | new, Open, cycle 1 | triage |
| `Notification` | `ALARM`, the group open | `repeated` | the `StateChangeTime` added | none |
| `Notification` | `ALARM`, resolved less than 30 minutes ago | `reopened` | cycle + 1 | triage |
| `Notification` | `OK` | `resolved` (`ignored` when nothing is open) | Resolved, its alarms marked resolved | none |
| `Notification` | `INSUFFICIENT_DATA` | `ignored`, audited | unchanged | none |

The signature is checked as AWS documents it: the certificate at `SigningCertURL` is fetched over HTTPS from `sns.<region>.amazonaws.com` only, the URL rebuilt from the host and path checked, no redirect followed, at most 64 KiB, kept for an hour (never past its expiry), and the body's fields are verified against its RSA key, SHA1 for `SignatureVersion` 1 and SHA256 for 2. The check runs on every route, the socket included. The tests use bodies composed from AWS's documented formats under `domain/testdata/sns-*.json` (not captured from a real topic), re-signed with a key each test makes (`internal/snstest`).

## Incidents

An incident is one alert group of a tenant and the tasks its phases ran for it ([operator API](../reference/operator-api.md#routes)): the triage task of the bot it belongs to, then the fix, apply and verify tasks of its teammates ([ADR](../architecture/incident-phases.md)), and once resolved its [postmortem](postmortem.md). Alertmanager opens them through an `alertmanager` trigger and CloudWatch through a `cloudwatch` trigger (above); anything else, or a person, through `POST /v1/incidents` or `axlr-bots incident open`, for a bot that declares the `incident` trigger. One task per phase in each cycle: a repeated alert starts nothing. The incident keeps every `namespace` label its alerts carry (`incident open --namespace` by hand; a label that is no namespace is left out and audited), which a bot's `approvals.twoPersons` reads ([two persons](approvals.md#two-persons)).

Each phase is a task of its own, launched by the control plane when the incident reaches it, never by a bot that would wait through an approval: fix once it is `Diagnosed`, apply once it is `Proposed`, verify once it is `Verifying`. A phase belongs to the one teammate of the triage bot (same tenant, a shared team) that declares an `incident` trigger with that `phase`. No such teammate: the incident stays where it is (`Diagnosed`, `Proposed` or `Verifying`), as it did before the phase existed. Two, or one that is not Enabled: it stays there too, the refusal audited (`incident.<phase>`), and the next alert of the group tries again.

The apply phase's approval is the task's own ([approvals](approvals.md)): its bot runs at `act-with-approval`, its write call stops the task in `WaitingApproval` (with `warn.approval` and the notifications as for any task) and the incident stays `Proposed` while it waits, marked `needsYou`. **Approve** moves it to `Applying` at once; the task's completion to `Verifying` and the verify task. **Deny**, or an expiry, sends it back to `Open` in a new cycle with the reason, as a failed or cancelled apply and a red verify do; the second send-back since it was last reopened escalates it (`sendBacks`), and its alerts are then only repeats. Only the task the incident stands on now can be approved. The verify task's answer says `VERDICT: red` when the fault is still there (the incident goes back to `Open`) or `VERDICT: green` when it took; green, or no verdict, leaves it `Verifying` until the group resolves. Back in `Open`, the control plane starts the new cycle's triage at once, without waiting for Alertmanager to resend the group (every `repeat_interval`, 4 hours by its default); the send-back cap bounds the loop. When that triage cannot start (the triage bot paused or gone), the next alert of the group starts it.

An incident trigger may narrow the bot's policy for its phase: `level` at most the bot's, `tools` some of its `allowedTools` ([bot definition](../reference/bot-definition.md#triggers)). Under `observe` and `propose` AXLR treats the allow-listed MCP tools as reads, so a fix phase whose list held `apply` would apply without asking: give the fix phase `propose` and its read tools, the apply phase `act-with-approval` and `apply`.

```text
new group key ──▶ Open, cycle 1, a triage task ──task Completed──▶ Diagnosed (the answer is the diagnosis)
                      └──task Failed, Expired or Cancelled──▶ Escalated (a person must look)
Diagnosed, a teammate takes the fix phase ──▶ its fix task ──task Completed──▶ Proposed (the answer is the proposal; nothing applied)
                      └──task Failed, Expired or Cancelled──▶ Escalated
Proposed, a teammate takes the apply phase ──▶ its apply task ──write call waits──▶ Proposed, needsYou
                      ├──a person approves──▶ Applying ──task Completed──▶ Verifying, the verify task
                      ├──a person denies, or the approval expires──▶ Open, cycle + 1
                      └──task Failed or Cancelled──▶ Open, cycle + 1
(every send-back to Open counts: the second since the last reopening is Escalated)
Verifying ──verify says VERDICT: red──▶ Open, cycle + 1 · green or nothing: Verifying until the group resolves
          └──verify task Failed, Expired or Cancelled──▶ Escalated
Open after Proposed, Applying or Verifying ──at once──▶ the triage of the new cycle (the next alert, when it cannot start)
same key while Escalated ──▶ Open, cycle + 1, a new task (twice; then a repeat, left to a person)
same key, not Resolved ──▶ a repeat: its alerts added, no task
resolved group, or incident resolve ──▶ Resolved ──same key within 30 minutes──▶ Open, cycle + 1, a new task
                                                  └─same key later──▶ a new incident
```

| Status | Means |
|---|---|
| `Open` | nobody has said the cause yet: its triage task is pending or running |
| `Diagnosed` | the triage task completed; its answer, the cause with its evidence, is the incident's `diagnosis`. Its fix task, when a teammate takes the fix phase, is pending or running |
| `Proposed` | the fix task completed; its answer, the change and its rollback, is the incident's `proposal`. Nothing is applied. With a teammate taking the apply phase, its apply task is pending, running or waiting for your approval (`needsYou`); without one, a person reads it, applies it or not, and resolves the incident |
| `Applying` | a person approved the apply task's call: the change is being carried out |
| `Verifying` | the apply task completed; the verify task, when a teammate takes the phase, checks it, and the group's resolution closes the incident |
| `Escalated` | the triage, fix or verify task failed, expired or was cancelled, or it went back to Open twice from a proposal (`sendBacks`); `reason` says which. The next alert of the group starts another triage (`"action": "reopened"`, the event's `retry` true), at most twice since the last proposal or reopening (`retries`), never after two send-backs; after that a person takes it from there |
| `Resolved` | its alerts went green, or a person closed it; a running task still settles and its cost is still added, but it moves the incident no further |

```bash
axlr-bots bot update triage --file patch.json     # {"triggers": [{"type": "manual"}, {"type": "incident"}], "groups": ["sre"]}; the bot is Enabled
axlr-bots bot update fix --file patch.json        # {"triggers": [{"type": "incident", "phase": "fix"}], "groups": ["sre"]}: the fix phase
# one bot, two phases: {"level": "act-with-approval", "allowedTools": ["get", "diff", "apply"], "triggers": [
#   {"type": "incident", "name": "fix", "phase": "fix", "level": "propose", "tools": ["get", "diff"]},
#   {"type": "incident", "name": "apply", "phase": "apply", "tools": ["get", "diff", "apply"]}]}
axlr-bots bot update verify --file patch.json     # {"triggers": [{"type": "incident", "phase": "verify"}], "groups": ["sre"]}: the verify phase
axlr-bots incident open --tenant default --bot triage --group-key "HighLatency/web" \
    --title "High latency in web" --severity sev2 --fingerprints a1
axlr-bots incident list --status Open
axlr-bots incident get inc-…                      # alerts, cycle, tasks (bot, phase, cycle), diagnosis, proposal, reason, repeats, retries, sendBacks, costUsd (every phase's, delegated children included), needsYou, currentTask, approvals
axlr-bots task approve task-…                     # the apply task's call: the incident is Applying; task deny sends it back to Open
axlr-bots incident resolve inc-… --reason "rolled back"
```

Check: the first `open` answers `created: true`, the incident `Open` at cycle 1 and its triage task, key `incident:<id>:1`, whose objective is the trigger's template or the default ("Incident … Find the cause with evidence and report it; change nothing") and whose `input` is the incident. The same group key again answers `created: false`, no task, and the incident with the new fingerprints and `repeats` one higher. After `resolve`, the same key within 30 minutes answers `reopened: true`, cycle 2 and a task keyed `…:2`. The bot's events show `incident.opened`, `incident.repeated`, `incident.resolved`, `incident.reopened`, and `incident.diagnosed` or `incident.escalated` when its task ends. With a fix teammate, `Diagnosed` brings its task (key `incident:<id>:<cycle>:fix`, the incident as its `input`, the objective with the diagnosis, the triage task's trace), and its end `incident.proposed` (`proposal`, `proposedBy`) or `incident.escalated`; `incident get` lists both tasks and the cost of both. With an apply teammate, `Proposed` brings its task (key `…:<cycle>:apply`, the objective with the proposal and the diagnosis, its trigger's narrowed level and tools in the execution's `policy`); its wait marks the incident `needsYou`; `task approve` publishes `incident.applying`, its completion `incident.verifying` and the verify task (key `…:<cycle>:verify`, the default objective asking for the `VERDICT:` line); `task deny` publishes `incident.reopened` with `from: Proposed`, the `reason`, `sendBacks`, `endedTaskId` and the `taskId` of the new cycle's triage, started at once (key `incident:<id>:<cycle>`). `incident get` counts the `approvals` of its tasks: the product's rule is one per incident, shown and not enforced. Severity is `sev1` to `sev4` (`sev3` when absent); a bot without the `incident` trigger, or not Enabled, refuses the incident.

## Check the catch-up

Restart the control plane while a timer slot is in the past (stop it for longer than the interval, or restart it right after a fire):

```bash
systemctl --user restart axlr-bots.service
```

Check: the timer fires once for the slot it found in the past (one task for that slot, with the key `timer:clock:<slot>`), the slots in between are not replayed, and `<state>/timers/bot~clock.json` holds the next fire. A slot already fired before the restart creates no second task: the same key finds the same task.

## Remove or pause

- `bot update bot --file` with `{"triggers": [{"type": "manual"}]}` removes the timer and the webhook; the timer file is dropped at the next tick.
- A changed `every` or `cron` re-arms the timer; a paused bot's timers advance without firing. A timer a bot never had (a new bot, or a trigger added while it was paused) is armed only once the bot takes tasks, so an interval fires at once on enable.

## What the live run showed

Three demo runs within ten minutes, each re-applying the triggers and restarting both services: one timer task for the slot, one task for the redelivered webhook, two fires in the events ([live run](../live-run.md)).
