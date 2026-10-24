# Operator API

The vision's minimal API (§11.1) plus the routes the CLI uses. Everything is HTTP+JSON; the JSON is the same the CLI prints.

## Listeners

| Listener | Flag | Serves | Credential |
|---|---|---|---|
| Unix socket `<state>/axlr-bots.sock` (mode 0660) | always | every route: the CLI's path | the socket's permissions (same host, same user); the caller is trusted |
| node address (TLS 1.3) | `serve --listen <addr>` | `/healthz`, enrollment and the five node routes only ([protocol](node-protocol.md)) | the node's client certificate |
| operator address (TLS 1.3) | `serve --api-listen <addr>` | the operator routes below and `/healthz`; no node route | `Authorization: Bearer <token>`; a webhook its own secret or signature |

The TLS certificate of both addresses is issued by the fleet CA (`ca init`; clients pass `--cacert <state>/ca/ca.crt`). The bearer token is generated at the first start with `--api-listen`, written to `<state>/api.token` (mode 0600, `--api-token-file` names another path) and never logged. A node certificate is no credential on the operator address, which keeps node identity and operator access apart (operator client certificates would need their own CA profile; the bearer token is the one credential). Bodies are bounded (1 MiB; a webhook 256 KiB) and unknown fields are refused.

Every CLI command tries the socket first (`--socket`, `AXLR_BOTS_SOCKET`, default `<state>/axlr-bots.sock`): when the daemon answers, the command is carried by it; when nothing answers, the command works on the state directory directly and a submitted task is spooled for the daemon's next start.

## Routes

| Route | Does | Idempotency |
|---|---|---|
| `POST /v1/bots` | register a bot from its definition | per id: a repeat is `409 conflict` |
| `GET /v1/bots`, `GET /v1/bots/{id}` | list, show (credentials redacted) | |
| `PATCH /v1/bots/{id}` | replace the fields the patch names | the patch is the whole value of each field |
| `POST /v1/bots/{id}/enable` | let the bot accept tasks | |
| `POST /v1/bots/{id}/tasks` `{objective, correlationId?}` | submit a task; `Idempotency-Key` header (or `idempotencyKey` in the body; the header wins) | same bot and key: same task, `200` instead of `201` |
| `GET /v1/bots/{id}/events?limit=100` | the bot's newest events, oldest first (at most 1000) | |
| `POST /v1/triggers/{bot}/{trigger}` | fire a webhook with the body as the event; `Idempotency-Key` is the delivery id ([triggers](../runbooks/triggers.md)) | same delivery (or same body): same task, `200` |
| `GET /v1/tasks/{id}` | show a task: status, trigger, input, trace id, executions | |
| `GET /v1/tasks/{id}/executions` | the task's executions with their correlation, policy, usage and result | |
| `POST /v1/tasks/{id}/cancel` | stop a task: Pending never runs, Running ends `Cancelled` (the in-process run is interrupted, a node's run stops at its next lease renewal, the late report is dropped), WaitingApproval never gets its decision | a Cancelled task answers `200` with `changed: false`; Completed or Failed is `409` |
| `POST /v1/tasks` `{botId, objective, idempotencyKey?}` | the older form of submit, with the same header | as above |
| `POST /v1/tasks/{id}/run`, `POST /v1/tasks/{id}/decide`, `GET /v1/tasks/{id}/report`, `GET /v1/executions/{id}` | the CLI's run, approve/deny, report and execution routes | |
| `GET /v1/nodes`, `GET /v1/nodes/{id}`, `POST /v1/enrollments`, `POST /v1/nodes/{id}/revoke` | the nodes: list, show, mint a token, revoke | |

## The idempotency key

A manual submission carries the caller's key (`task submit --key`, the `Idempotency-Key` header). The same bot and key always name the same task: the second call answers `200` with the task the first created and nothing runs twice. A webhook fire derives its key from the trigger and the delivery id (or the body), a timer from the trigger and its slot, so a redelivery or a restarted scheduler finds the task it already created ([triggers](../runbooks/triggers.md)). A task also keeps a trace id (`correlationId`, minted as `trace-…` unless the caller passes one) that every execution and event repeats ([runtime contract](../architecture/runtime-contract.md#correlation)).

## Errors

`{"kind": "validation|not_found|state|conflict|forbidden|unauthorized|too_large|internal", "error": "…"}` with the matching status (`400`, `404`, `409`, `409`, `403`, `401`, `413`, `500`).

## Examples

```bash
state=~/.local/state/axlr-bots
curl --cacert $state/ca/ca.crt -H "Authorization: Bearer $(cat $state/api.token)" https://cp.example.internal:7879/v1/bots
curl --cacert $state/ca/ca.crt -H "Authorization: Bearer $(cat $state/api.token)" -H "Idempotency-Key: deploy-42" \
     -d '{"objective": "List the files in your workspace"}' https://cp.example.internal:7879/v1/bots/bot/tasks
curl --cacert $state/ca/ca.crt -H "X-Axlr-Secret: $secret" -H "Idempotency-Key: gh-delivery-1" \
     -d '{"type": "deploy", "service": "web"}' https://cp.example.internal:7879/v1/triggers/bot/deploy
```
