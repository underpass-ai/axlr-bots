# Runbook: tell Slack, a webhook or a script about incidents

An output tells a place people read what an incident did: opened, the cause, the change proposed and waiting for you, applied, verified, resolved, escalated, and its [postmortem](postmortem.md) approved (`postmortem_approved`, in the default list, with the text cut to 2 KiB; `postmortem_drafted`, `postmortem_failed` and `postmortem_sent_back` may be named). The control plane keeps a list of output targets and posts one message per incident event to each target that wants it. Nothing comes back: to approve, use the panel, `task approve` or the [approval notifications](approvals.md#notifications). The decisions are in the [ADR](../architecture/incident-outputs.md).

```text
incident.<event> ──▶ each target whose events, tenants and severities want it
                     ──▶ one delivery in <state>/deliveries/outputs/ (durable)
                         ──▶ the daemon posts it, oldest first: 10 s, 30 s, 1 min, 5 min, 15 min, then gives up
```

| Type | What it posts | Takes |
|:--|:--|:--|
| `slack` | a Block Kit message to an incoming webhook, unsigned (the URL is the secret) | `url` |
| `webhook` | the incident event as JSON, signed like an approval webhook | `url`, `secret` (16 to 256 characters) |
| `command` | runs a program with the JSON on its stdin: a relay to Teams, a mail, anything | `command`: an absolute program and its arguments |

## Add a Slack target

1. In Slack, create an app with **Incoming Webhooks** on, add a webhook to the channel, copy its URL (`https://hooks.slack.com/services/…`). Treat it as a password: whoever has it posts to the channel.
2. Keep it out of the shell's history and give it to the control plane from a file or stdin:

```bash
axlr-bots output add ops-slack --type slack --url-file - --severities sev1,sev2
# paste the URL, then Enter and Ctrl-D: it is read from stdin and never written anywhere else
```

3. Check it. The list shows the host and a fingerprint, never the URL:

```bash
axlr-bots output list
```

```json
[{"name": "ops-slack", "type": "slack", "fingerprint": "sha256:3f1c0a9b2e7d", "host": "hooks.slack.com",
  "events": ["incident.opened", "incident.reopened", "incident.diagnosed", "incident.proposed", "incident.applying", "incident.verifying", "incident.resolved", "incident.escalated"],
  "severities": ["sev1", "sev2"]}]
```

4. For the **Open in the panel** button, start the control plane with `serve --panel-url https://<where the incident page is>` (or `AXLR_BOTS_PANEL_URL` in its environment file), or give the target its own `--panel-url`. The link is `<base>/incidents/<id>`. The desktop panel has no web address of its own: point it at whatever shows an incident to your people (a page in front of the operator API, a wiki). Without a base the message has no button.

The filters: `--events` takes short names (`opened`) or full ones (`incident.opened`), comma-separated, among `opened`, `repeated`, `reopened`, `diagnosed`, `proposed`, `applying`, `verifying`, `resolved`, `escalated`; `--tenants` and `--severities` narrow further. To change a target, `output remove` it and add it again; its deliveries still pending are skipped (`the output … is gone`).

## Add a webhook or a command

```bash
axlr-bots output add audit-hook --type webhook --url https://receiver.example.internal/axlr --secret-file /etc/axlr/hook.secret
axlr-bots output add teams --type command --events opened,proposed,resolved,escalated -- /usr/local/bin/teams-relay --channel sre
axlr-bots output add --file pager.json      # {"name": "pager", "type": "command", "command": ["/usr/local/bin/page"], "severities": ["sev1"]}
```

Over the operator API: `POST /v1/outputs` with the same fields (`201`, the target as `list` shows it), `GET /v1/outputs`, `DELETE /v1/outputs/{name}` ([operator API](../reference/operator-api.md#routes)).

## Try a target with a local receiver

A receiver on the control plane's host prints what arrives and checks the signature of a webhook:

```python
# receiver.py: python3 receiver.py <secret>   (no secret: a slack target)
import hashlib, hmac, http.server, json, sys
SECRET = sys.argv[1].encode() if len(sys.argv) > 1 else None
class Receiver(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers["Content-Length"]))
        if SECRET:
            expected = "sha256=" + hmac.new(SECRET, body, hashlib.sha256).hexdigest()
            print("signature ok:", hmac.compare_digest(expected, self.headers.get("X-Axlr-Signature", "")),
                  self.headers.get("X-Axlr-Event"), self.headers.get("X-Axlr-Delivery"))
        print(json.dumps(json.loads(body), indent=2))
        self.send_response(200); self.end_headers()
http.server.HTTPServer(("127.0.0.1", 9911), Receiver).serve_forever()
```

```bash
python3 receiver.py a-secret-of-at-least-16 &
printf 'a-secret-of-at-least-16' | axlr-bots output add try-hook --type webhook --url http://127.0.0.1:9911/hook --secret-file -
axlr-bots output add try-slack --type slack --url http://127.0.0.1:9911/services/T/B/X
# a bot that takes incidents (a trigger {"type": "incident"}), then:
axlr-bots incident open --tenant ops --bot triage --group-key try-1 --title "Trying the outputs" --severity sev2
```

With the control plane serving (its delivery pass runs every 2 s), the receiver prints the opened event twice: the signed JSON, then the Block Kit message. Paste the second into Slack's [Block Kit Builder](https://app.slack.com/block-kit-builder) to see it as the channel will. `axlr-bots incident resolve <id>` sends the resolved one. Remove the two targets when done.

## What is posted

### Webhook and command: the incident event

`schema: axlr-bots.incident-event/v1`; the incident as it stands after the event. Headers of a webhook: `Content-Type: application/json`, `X-Axlr-Event: incident.<event>`, `X-Axlr-Delivery` and `Idempotency-Key` (the delivery id, the same on every attempt: drop a repeat), `X-Axlr-Signature: sha256=<hex>` over the raw body.

```json
{
  "schema": "axlr-bots.incident-event/v1",
  "deliveryId": "dlv-86702edacc31de804f2da63d", "event": "incident.proposed", "eventId": "evt-43", "target": "hook",
  "occurredAt": "2026-10-23T15:00:00Z",
  "incidentId": "inc-18", "tenantId": "default", "botId": "fix", "botName": "fix", "taskId": "task-35",
  "title": "KubePodCrashLooping in shop", "severity": "sev2", "status": "Proposed", "cycle": 1,
  "groupKey": "{}/{alertname=\"KubePodCrashLooping\"}", "source": "alertmanager",
  "diagnosis": "CAUSE: deployment web drifted: replicas 1, desired 3",
  "proposal": "PROPOSAL: kubectl apply -f desired/web.yaml\nROLLBACK: replicas 1",
  "needsYou": false, "costUsd": 0.02,
  "openedAt": "2026-10-23T15:00:00Z", "updatedAt": "2026-10-23T15:00:00Z",
  "links": {
    "incident": {"method": "GET", "path": "/v1/incidents/inc-18", "url": "https://cp.example.test:7879/v1/incidents/inc-18"},
    "panel": "https://panel.example.test/incidents/inc-18"
  }
}
```

| Field | Meaning |
|:--|:--|
| `botId`, `botName` | the bot of the incident's current task (the one working on it now), or the triage bot before one |
| `diagnosis`, `proposal` | cut to 2 KiB with a trailing `…`; `diagnosisTruncated`, `proposalTruncated` say so. `GET /v1/incidents/{id}` has them whole |
| `reason` | why it was resolved, escalated or sent back |
| `needsYou` | what `GET /v1/incidents/{id}` answers: the incident's current task waits for a person's approval where the incident stands. An event is a move, so it is seldom true in a body: the apply task asks after `proposed` was posted |
| `costUsd`, `costUnknown` | what its executions reported; `costUnknown` makes `costUsd` a floor |
| `resolvedAt`, `durationSeconds` | when resolved: when, and how long since it opened |
| `links.incident` | the operator API's route; `url` when the control plane knows its operator address (`serve --api-url`, `--api-listen`), and it needs the bearer token |
| `links.panel` | the panel page, when a panel URL is known |

A command gets the same JSON on stdin and `AXLR_BOTS_DELIVERY_ID`, `AXLR_BOTS_INCIDENT_ID`, `AXLR_BOTS_TENANT_ID` and `AXLR_BOTS_EVENT` in a minimal environment (none of the daemon's keys); exit status 0 is a delivery, anything else a failed attempt with the last line of its stderr; it has 10 s.

### Slack: the Block Kit message

```json
{"text": "[sev2] KubePodCrashLooping in shop: Cause found",
 "blocks": [
  {"type": "header", "text": {"type": "plain_text", "text": "SEV2 · KubePodCrashLooping in shop"}},
  {"type": "section", "text": {"type": "mrkdwn", "text": "*Cause found* · `inc-18` · tenant `default`"}},
  {"type": "section", "fields": [
    {"type": "mrkdwn", "text": "*Status*\nDiagnosed"}, {"type": "mrkdwn", "text": "*Bot*\ntriage"},
    {"type": "mrkdwn", "text": "*Cycle*\n1"}, {"type": "mrkdwn", "text": "*Cost*\n$0.0100"}]},
  {"type": "section", "text": {"type": "mrkdwn", "text": "*Cause*\n```CAUSE: deployment web drifted: replicas 1, desired 3```"}},
  {"type": "actions", "elements": [{"type": "button", "text": {"type": "plain_text", "text": "Open in the panel"},
    "url": "https://ops.example.test/axlr/incidents/inc-18", "action_id": "open-incident"}]}]}
```

| Event | What the message adds to the header and the facts |
|:--|:--|
| opened, reopened, repeated | nothing more |
| diagnosed, applying, verifying | the cause |
| proposed | the cause, the proposal in a code block, **Needs you**: the change waits for a person to approve or deny it |
| resolved | the cause, "Resolved after 12m 30s" and the reason |
| escalated | the cause, the reason, **Needs you**: the bots gave it to a person |

What a bot wrote can neither format nor break the message: the cause and the proposal sit in code blocks, their backticks turned into `ˋ` so they cannot close the fence; a reason loses its marks (`*`, `~`, a backtick, a leading `>`); `&`, `<` and `>` are escaped as Slack asks (so `<!channel>` pings nobody), after the cut, so a section stays within Block Kit's 3000 characters; the header is cut to 150. The goldens the tests compare with are in `application/testdata/outputs/`.

## Delivery: at least once, in order

A delivery is made, then recorded. If the control plane stops between the two, it sends the same delivery again when it starts: a webhook or a command drops the repeat by `deliveryId` (also `Idempotency-Key`); a Slack channel cannot, and may show the message twice. An incident's messages to a target go in the order of its events: a later one waits while an earlier one to that target is still being retried. A target that fails gets no other attempt until the next pass (2 s later), and the outputs run apart from the approval notifications, so a dead Slack webhook never delays a "needs you" notification.

## What the control plane records

| Where | What |
|:--|:--|
| audit `incident.output` | one entry when a delivery is queued (`status: queued`), one per attempt (`attempt`, `status` delivered, pending with `retryAt`, failed) and per skip, each with `delivery`, `target`, `kind`, `incident`, `event` and the `reason`; never the URL or the secret |
| audit `output.add`, `output.remove` | the name, the type, the fingerprint |
| the daemon's log | `daemon: output dlv-… of incident inc-… (incident.opened) to ops-slack: delivered (attempt 1)` |
| the state | `<state>/outputs.json` (0600) and `<state>/deliveries/outputs/{pending,done}/<dlv-id>.json` |

## When it does not work

| Symptom | Cause, and what to do |
|:--|:--|
| `HTTP 404: no_service` or `HTTP 403: invalid_token` in the audit | Slack revoked the webhook or the channel is gone: make a new one, `output remove`, `output add` |
| `output ops-slack: … connection refused` / `timed out` | the host cannot reach the target; the daemon retries for about 21 minutes, then the delivery is `failed` |
| an `incident.output` audit with outcome `failed` and `status: not queued` | queueing failed (the reason says why); the incident moved all the same, and that event is not posted |
| `daemon: outputs: filestore: outputs.json: …` every pass, every queueing failed | `outputs.json` was edited by hand into something the control plane refuses; fix or remove the file |
| no button in the Slack message | no panel URL: `serve --panel-url` or the target's `--panel-url` |
| links without `url` | the event was queued by a command working on the state directory (no daemon answered), which knows neither the operator nor the panel address |
| nothing posted for a sev4 incident | the target's `--severities` leaves it out; `output list` shows the filters |
