# ADR: who may approve

Status: accepted, 8 November 2026; amended 13 November 2026: one approval per call.

## Context

Until now any holder of the operator bearer token could approve or deny any call. The CLI's `--actor` named who was acting, but only in process: through the daemon the decision was recorded as the daemon's own actor, and the panel sent no name at all. There is no user system in the control plane, and this ADR does not add one.

## Decisions (8 November 2026)

| # | Decision |
|:--|:--|
| D1 | **The actor is asserted, not proven.** A decision names who takes it: `X-Axlr-Actor` on `POST /v1/tasks/{id}/decide`, `/approve` and `/deny`, or `decidedBy` in the decide body (both given must agree). The CLI sends its global `--actor` (default `cli:$USER`); the panel sends its `controlPlane.actor` ("Decide as" on the Settings screen) on every request. The token holder says the name; the control plane checks nothing behind it. Per-actor tokens are future work ([axlr-bots#137](https://github.com/underpass-ai/axlr-bots/issues/137)) |
| D2 | A bot's `approvals` block takes `approvers: ["alice", "oncall"]`: who may approve **and** deny its calls, compared without the CLI's `cli:` prefix and without case (`alice`, `cli:alice` and `Alice` are one person). A `policy:` name is no approver. Absent or empty is anyone with the token, as before. A decision by another actor, or by nobody named on a bot that lists approvers, is refused with `403 forbidden` and audited `approval.refused` with the actor; nothing changes. No person decides as `policy:…`, and on a daemon, where names count, nobody decides under the control plane's own actor: both `403` |
| D3 | A policy is not a person. `autoApprove` and the team's events (`policy:autoApprove`, `policy:team-event`) are not checked against `approvers` |
| D4 | Compatibility: a bot without the new field, and a task or a bot stored before it, behave and read back exactly as before |

Why asserted and not proven (D1): the smallest change that makes `approvers` mean something is to stop dropping the name the operator already gives. A login, roles or an identity provider would be a product of their own; per-actor tokens (#137) are the next step that turns the name into a fact.

## How the decisions are carried out

### The `approvals` block

```json
{
  "approvals": {
    "ttl": "30m",
    "onExpire": "deny",
    "approvers": ["alice", "oncall"]
  }
}
```

`approvers`: at most 32 names, each without spaces or control characters, at most 128 characters, no repeats (compared without `cli:` and case), no `policy:` name. Anything else is refused at register and update.

### The rules, in order

```text
decision ─▶ actor named? ─valid─▶ task WaitingApproval, wait open, in time … (as before)
            │
            ├─ bot lists approvers, actor absent or not listed ──▶ 403 forbidden, approval.refused
            ├─ deny ───────────────────────────────────────────▶ the wait ends denied
            └─ approve ────────────────────────────────────────▶ the call runs
```

### What an operator reads

`bot.task.decided` says `decidedBy`, the named actor. The audit log's `actor` is the named actor for every line of the decision.

### Layer map

| Layer | What changed |
|:--|:--|
| domain | `Approvals` (approvers, `MayDecide`), `ValidateActor`, `ActorKey` |
| application | `ApproveTask.Execute(…, actor)`: the named actor becomes the use case's actor (audit, events); `mayDecide` |
| adapters/unixapi | `X-Axlr-Actor` and `decidedBy` on the decide routes; the contract |
| cmd/axlr-bots | `task approve|deny|decide` send `--actor` |
| panel | `controlPlane.actor` ("Decide as"), sent as `X-Axlr-Actor` |

## Left out

- Per-actor tokens and any proof of the actor ([#137](https://github.com/underpass-ai/axlr-bots/issues/137)).
- Approvers per namespace: one list per bot.
- More than one approval per call.
