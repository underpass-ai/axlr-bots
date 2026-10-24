# Bots

A bot has no role baked in: its definition is what an operator writes, and the same install serves every definition. A bot exists without running anything; a task makes the control plane start an execution for it, and when the execution ends the bot is still there (bot ≠ execution).

## The definition

[`examples/bot.json`](../../examples/bot.json) is the example bot: it reads the files of its workspace and does what each task asks.

```json
{
  "id": "bot", "tenant": "default", "name": "bot", "owner": "operator",
  "mission": "Do what each task asks, within the policy.",
  "level": "observe",
  "allowedTools": ["local_read", "local_search", "local_list"],
  "toolRules": {"allow": [], "deny": []},
  "triggers": [{"type": "manual"}],
  "placement": {},
  "model": {"model": "anthropic/claude-haiku-5.5", "routingPolicy": "cost-optimized", "allowRemoteProviders": true, "maxContextTokens": 32000},
  "budgets": {"maxModelCallsPerTask": 10, "maxToolCallsPerTask": 20, "maxDelegationDepth": 0, "taskTimeoutSeconds": 300}
}
```

| Field | What it says |
|---|---|
| `id`, `tenant`, `name`, `owner` | the identity; `owner` is who answers for the bot. The identity never changes after registration |
| `mission` | the standing responsibility; every task's objective is read against it |
| `level` | the autonomy level ([below](#autonomy-levels)); default `observe` |
| `allowedTools` | AXLR's tool names (`local_read`, `local_search`, `local_list`, `local_write`, `local_exec`, …); nothing else may be called |
| `toolRules` | narrows the allowed tools by their arguments ([below](#tool-rules)) |
| `triggers` | how tasks reach the bot: `manual` (the default), `webhook`, `timer`; a bot that declares triggers takes only those ([below](#triggers)) |
| `placement` | where the bot's tasks run: `{"nodes": ["web-1"]}`, `{"selector": {"zone": "eu"}}`, or nothing for any node ([nodes](node-protocol.md)) |
| `model` | the model policy: `model` is passed to AXLR as `--model`; `routingPolicy`, `allowRemoteProviders` and `maxContextTokens` are validated and stored with the bot |
| `budgets` | `taskTimeoutSeconds` bounds every execution (15 minutes when absent); `maxModelCallsPerTask` and `maxToolCallsPerTask` go to AXLR as the execution's budget when the build takes `--budget` ([runtime contract](../architecture/runtime-contract.md)); `maxDelegationDepth` is stored for the delegation to come |

## Autonomy levels

The levels are the vision's (§10.4). The policy is applied outside the model: AXLR refuses a call the policy forbids before any approval card, and the model reads the refusal as the tool's result.

| Level | What the bot may do |
|---|---|
| `observe` | read only; with `local_exec` allowed, only the commands the rules name |
| `propose` | write a plan or a diff, nothing else |
| `act-with-approval` | every allowed write or command waits for a person ([approvals](../runbooks/approvals.md)) |
| `act-within-policy` | every allowed call runs; the tool rules are the limit |

How each level is handed to AXLR (`--approve`, the policy file) is in the [runtime contract](../architecture/runtime-contract.md#the-policy).

## Tool rules

`toolRules` narrow the allowed tools by their arguments in the runtime contract's words: a glob over each argument's value as text (`*` within a path segment, `**` across segments, `?` one character; an array such as `args` is matched as its compact JSON). A tool with allow rules may only be called as a rule says; a tool without rules is allowed as a whole; a deny rule wins. A rule that names a tool outside `allowedTools` is refused, so a narrower tool list cannot strand a rule.

```json
"toolRules": {
  "allow": [{"tool": "local_exec", "arguments": {"program": "systemctl", "args": "[\"status\",*"}}],
  "deny":  [{"tool": "local_write", "arguments": {"path": "**/.env"}}]
}
```

## Triggers

A trigger is how tasks reach a bot. It has a type, a name unique within the bot (the type when not given) and, for a webhook or a timer, an objective template the fire renders over the event (`text/template`; a field the template names and the event lacks refuses the fire, so an objective never says `<no value>`). A bot that declares triggers takes only those; the default is `manual` alone.

Every fire creates a task through the same path as a manual submission, under an **idempotency key derived from the trigger and the event**, so a redelivered webhook or a scheduler that restarts and fires the same slot finds the task it already created and starts nothing (vision §14.7). Every fire is a `bot.trigger.fired` event with the task, the key and `deduplicated` true or false; the task keeps the trigger and the event as its `input`.

```json
"triggers": [
  {"type": "manual"},
  {"type": "webhook", "name": "deploy", "secret": "<16 to 256 characters>",
   "objective": "Handle event {{.type}} for {{.service}}"},
  {"type": "webhook", "name": "alerts", "signingKey": "<16 to 256 characters>",
   "objective": "Look at the alert {{.alertname}} on {{.instance}}"},
  {"type": "timer", "name": "clock", "every": "10m",
   "objective": "Say the current time of this host (the trigger fired at {{.firedAt}})."},
  {"type": "timer", "name": "nightly", "cron": "0 3 * * 1-5", "objective": "Write the nightly report"}
]
```

| Type | Fired by | Key | Authentication |
|---|---|---|---|
| `manual` | `task submit --key`, `POST /v1/bots/{id}/tasks` with `Idempotency-Key` | the caller's key | the socket, or the operator bearer token |
| `webhook` | `POST /v1/triggers/{bot}/{trigger}` with a JSON object as the body | `webhook:<name>:<hash>` of the `Idempotency-Key` header (the delivery id) when the sender set one, else of the body | the trigger's `secret` in `X-Axlr-Secret`, or the HMAC-SHA256 of the body under its `signingKey` in `X-Axlr-Signature: sha256=<hex>` (`X-Hub-Signature-256` is read too); never the bearer token. Over the socket the caller is trusted, as for every route |
| `timer` | the daemon, on every tick | `timer:<name>:<unix seconds of the slot>` | none: it is the control plane's own |

**Webhook.** A webhook is fired from outside with the trigger's own credential: the shared `secret` compared in constant time, or a signature over the body under the `signingKey` (what GitHub and most senders produce). An unknown bot or trigger, a missing or wrong credential and a tampered body are all `403`, so the route tells a stranger nothing; a body past 256 KiB is `413`. The secret and the signing key are stored with the bot (file mode 0600) and redacted in every answer of the API and the CLI. The body is stored on the task as its `input`; only the rendered objective reaches AXLR.

**Timer.** A timer runs `every` so often (at least a second; the first fire is when it is armed) or on a five-field `cron` expression evaluated in the host's time zone (numbers only; Sunday is 0 or 7; a day of month and a day of week both restricted match either, as in cron). Its state is one file, `<state>/timers/<bot>~<trigger>.json`, with the next fire.

Catch-up: a daemon that comes back after a long stop finds the slot in the past, **fires once for it** with that slot's key (so a crash between the fire and the save fires the same slot again and finds the same task) and arms the next slot after now; the slots in between are not replayed. A paused bot's timers are advanced, not fired; a changed schedule re-arms the timer; a removed trigger drops it. The timer's event carries `trigger`, `scheduledAt` and `firedAt`, which the template may use.

The procedure, with the checks, is the [triggers runbook](../runbooks/triggers.md).

## Register, enable, update

```bash
axlr-bots bot register --file examples/bot.json      # POST /v1/bots; a repeat of the id is 409
axlr-bots bot enable bot                             # the bot accepts tasks from now on
axlr-bots bot update bot --file patch.json           # PATCH /v1/bots/{id}
axlr-bots bot get bot                                # credentials redacted
axlr-bots bot events bot --limit 100                 # the bot's newest events, oldest first
```

A patch replaces the fields it names (`mission`, `level`, `allowedTools`, `toolRules`, `triggers`, `placement`, `model`, `budgets`) with their whole new value; the identity and the status do not change that way, and a patch that strands a tool rule or names a bad trigger changes nothing. [`examples/bot-triggers-patch.json`](../../examples/bot-triggers-patch.json) adds a timer and a webhook to the example bot; [`examples/bot-act-with-approval.json`](../../examples/bot-act-with-approval.json) is the example bot one level up, with `local_write` allowed.

## Tasks

A task is one objective for one bot. It is created by a manual submission or a trigger, under an idempotency key, and runs as one or more executions (a new one after each approval). Its states are `Pending`, `Running`, `WaitingApproval`, `Completed`, `Failed` and `Cancelled`; every task carries a trace id (`correlationId`) that every execution and event repeats.

```bash
axlr-bots task submit bot --objective "List the files in your workspace" --key list-1
axlr-bots task get task-…
axlr-bots task report task-…
```
