# Node protocol

What a node and the control plane say to each other. Every message goes **out from the node**; the control plane never opens a connection to a host. The transport is HTTP+JSON (`net/http` is in the standard library and the JSON is the same the CLI prints), over the Unix socket on the control plane's host or over mutual TLS on the node address ([identity](#identity)). The messages are the same on both paths; the types live in the `nodeproto` package, shared by the server and the node's client.

## Identity

The control plane runs the fleet's CA (ECDSA P-256, the key under `<state>/ca`, mode 0600). Each node holds a client certificate the CA signed from the node's own request: common name = node id, valid a year. The control plane's own certificate is issued by the same CA at start for the host's names (the listen address, the host name, `localhost`, the loopback, every interface address when it listens on all of them, plus `--server-names`) and reissued before it expires; it lives in memory, nothing of it on disk. Both ends speak TLS 1.3.

Over the TLS address a node **is** its certificate: the server takes the node id from the common name, refuses a URL node id (or a registration body) that names another node, and refuses the certificate when its serial is on the denylist or is not the one recorded at the node's last enrollment. Over the socket, where there is no certificate, the node id in the URL is trusted (same host, same user). A node the control plane does not know but whose certificate the CA signed may register: that is how a node comes back after the control plane lost its state.

## Messages

| Message | Route | Direction | Idempotency |
|---|---|---|---|
| enroll `{tokenId, secret, hostName, csr}` | `POST /v1/enroll` | node → control plane, **without a certificate** | once per token: the answer is the node's certificate, the CA and the node |
| register `{id, hostName, labels, capabilities}` | `POST /v1/nodes` | node → control plane | per node id: a repeat describes the node anew; a registration without labels keeps the node's |
| heartbeat | `POST /v1/nodes/{id}/heartbeat` | node → control plane | yes |
| next assignment | `GET /v1/nodes/{id}/assignments/next?wait=20s` | node → control plane (long poll; `204` when none) | each execution is handed out once; an unleased one assigned to the node (a queued decision) goes first |
| renew lease | `POST /v1/nodes/{id}/executions/{exec}/lease` | node → control plane | yes; refused for another node's or an ended execution |
| artifact (the bytes; `Content-Type`, `Content-Length`, `X-Axlr-Artifact-Sha256`) | `PUT /v1/nodes/{id}/executions/{exec}/artifacts/{name}` | node → control plane, before the report | per name: a repeat replaces it; refused once the execution has ended ([below](#artifacts)) |
| report `{outcome, result, usage, failureReason, traceReference, policy, correlation, receipt}` | `POST /v1/nodes/{id}/executions/{exec}/report` | node → control plane (at most 8 MiB) | **accepted once**: a repeat answers `accepted: false` with the stored outcome |
| delegate `{bot, objective, wait}` | `POST /v1/nodes/{id}/executions/{exec}/delegations` | node → control plane, while the execution runs | per asking task, bot and objective: a repeat answers the same child, `201` when created, `200` otherwise; a refusal is `200` with `refused: {reason, message}` ([below](#delegation)) |
| read a delegation | `GET /v1/nodes/{id}/executions/{exec}/delegations/{task}?wait=20s` | node → control plane (long poll, at most 30 s) | yes; only a child of the execution's own task |

The node address serves only these routes and `/healthz`; a node certificate opens no operator route ([API](operator-api.md)).

## The agent's loop

The agent registers at start, heartbeats every 15 s, long-polls for work (20 s), renews the lease every 30 s while it works and reports when it is done. A restarted agent registers again and comes Online. A control plane that lost its state answers a heartbeat with 404 and the agent registers again; a node whose certificate the CA signed is accepted even when the control plane does not know it.

`capabilities` say which `axlr-tui` the node has (`axlr-tui --version`) and whether it takes `--policy` (`policySupported`) and the correlation ids (`correlationSupported`), read once from `--help`, and whether the node is `dedicated` (`axlr-node --dedicated`): a dedicated node is assigned only the tasks of bots whose placement names it or selects its labels, never those of a bot placed on any node, which is what a [sidecar](../runbooks/sidecar.md) is. A control plane older than the field refuses such a registration, and the node keeps retrying rather than take work it was not meant for.

## Assignments and leases

An assignment carries a `run` (the task's objective, model, level, allowed tools, rules, timeout, correlation ids and, when it has any, its `budgets` of model and tool calls) or a `decide` (the session, the call and the decision, with the task and correlation ids) and a lease of two minutes. The control plane stores the node id and the lease on the execution. A lease the control plane refuses for good (the task was cancelled) stops the run on the node, whose late report is then dropped; a node that stops renewing (it died, or lost the network) is reconciled: the execution and its task fail with the diagnosis once the lease expires ([command reference](cli.md#recovery)).

The trace of the run stays on the node (`<node-state>/traces/<execution>.jsonl`); the execution keeps its reference. The report carries the result object AXLR produced, the usage, the policy record and the correlation record.

## Delegation

A run (or a decision) of a bot that may delegate carries `delegation: {targets, depth, maxDepth}`, and its allowed tools include `axlr_bots_delegate`. For it the agent opens `<node-state>/run/<execution>.sock` (directory 0700), and the AXLR adapter writes `<node-state>/mcp/<execution>.json`, an MCP manifest whose command is `axlr-node mcp-relay --socket <sock>`, and passes it with `--plugin` and `--approve all` (only under an enforced policy; [runtime contract](../architecture/runtime-contract.md#the-policy)). The model's call reaches the agent through the relay; the agent sends **delegate** with the node's certificate and, when the call waits, **reads the delegation** until the child ends or `waitUntil` (the parent's deadline less 10 s) passes. While it waits, the agent counts the execution as idle and takes the next assignment, so a child placed on the same node runs. The control plane takes the asking bot, task, depth, chain and budget from the execution the node holds, never from the message, and checks them in the order the [ADR](../architecture/delegation.md#3-how-depth-and-budget-propagate) gives. A node older than the field ignores `delegation` and `budgets` and never offers the tool.

## Artifacts

When AXLR's result lists artifacts with a size and a SHA-256 (the `axlr_deliver` contract; older builds list none, or none with a hash), the node opens each file by its reference **inside the bot's workspace only** (a reference that is absolute, holds `..` or resolves through a link outside the workspace is refused, and so is anything but a regular file of at most 32 MiB) and puts its bytes to the control plane, one request per artifact, while it still renews the lease; then it reports. The control plane takes an upload only from the node the execution is assigned to and while the execution runs, streams it to a temporary file, and keeps it only when its length is the `Content-Length` and its hash the header's (`400 validation` otherwise, or when the execution already keeps 64 artifacts or 256 MiB; `413` past 32 MiB; `409 state` from another node or for an execution that ended). The report then marks `stored: true` the artifacts the control plane holds with that name, size and hash, whatever the node claimed. An artifact the node could not read or the control plane refused is logged on the node, audited (`artifact.upload`, denied) and listed without `stored`.

The report carries AXLR's receipt line (`axlr.execution-receipt/v1`, at most 1 MiB) as `receipt` when the build writes one; the execution keeps it. **Upgrade the control plane before the nodes**: the control plane refuses unknown fields, so a control plane older than these fields refuses a report that carries them, while a new control plane takes the reports of older nodes as before.

## Events

`node.registered`, `node.heartbeat`, `node.offline`, `node.enrolled`, `node.revoked`, `execution.assigned` and `execution.reported` are the node events, and `bot.task.delegated` and `bot.delegation.refused` the delegation's, on the asking bot's stream, in `events.jsonl` with the bot, task, execution and trace ids they concern. The control plane marks Offline the nodes silent for over a minute, on start and every minute.

## Revocation

```bash
axlr-bots node revoke web-2
```

The node is marked `Revoked`, its certificate's serial goes on the denylist (`<state>/pki/revoked.json`, checked on every request a certificate authenticates, so an open connection is cut off too), and the executions it still held fail with their tasks (`revoked: node web-2 was revoked at …`, a `bot.task.failed` event with `"reason": "revoked"`). A revoked node cannot register, heartbeat or take work; a fresh token readmits it with a new certificate, and the old serial stays denied.

## What travels, what stays

| | |
|---|---|
| Out of the node | the registration, heartbeats, polls, lease renewals and reports; the bytes of the artifacts an execution delivered; an execution's delegations (the target and the objective the model wrote); the signing request at enrollment |
| Into the node | the assignments (the objective, model, level, tools, rules, timeout, budgets and correlation ids, the delegation targets, or a decision), the state and answer of a child an execution delegated, and its own certificate once |
| Never leaves the node | its private key, the AXLR session and its trace (the execution keeps a reference), the workspace beyond the delivered artifacts |
| Never leaves the control plane | the CA key, the token secrets (stored hashed), the operator bearer token |
| Per host | the model key, in that host's environment file |

## Limits today

A node certificate lasts a year and is renewed by enrolling again with a fresh token (no automatic renewal). Revocation is the control plane's own denylist (no CRL or OCSP for other verifiers). The CA key lives in the control plane's state directory (no HSM, no intermediate). Operators authenticate with a bearer token, not client certificates ([API](operator-api.md)).
