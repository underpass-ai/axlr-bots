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
| report `{outcome, result, usage, failureReason, traceReference, policy, correlation}` | `POST /v1/nodes/{id}/executions/{exec}/report` | node → control plane | **accepted once**: a repeat answers `accepted: false` with the stored outcome |

The node address serves only these routes and `/healthz`; a node certificate opens no operator route ([API](operator-api.md)).

## The agent's loop

The agent registers at start, heartbeats every 15 s, long-polls for work (20 s), renews the lease every 30 s while it works and reports when it is done. A restarted agent registers again and comes Online. A control plane that lost its state answers a heartbeat with 404 and the agent registers again; a node whose certificate the CA signed is accepted even when the control plane does not know it.

`capabilities` say which `axlr-tui` the node has (`axlr-tui --version`) and whether it takes `--policy` (`policySupported`) and the correlation ids (`correlationSupported`), read once from `--help`, and whether the node is `dedicated` (`axlr-node --dedicated`): a dedicated node is assigned only the tasks of bots whose placement names it or selects its labels, never those of a bot placed on any node, which is what a [sidecar](../runbooks/sidecar.md) is. A control plane older than the field refuses such a registration, and the node keeps retrying rather than take work it was not meant for.

## Assignments and leases

An assignment carries a `run` (the task's objective, model, level, allowed tools, rules, timeout and correlation ids) or a `decide` (the session, the call and the decision, with the task and correlation ids) and a lease of two minutes. The control plane stores the node id and the lease on the execution. A lease the control plane refuses for good (the task was cancelled) stops the run on the node, whose late report is then dropped; a node that stops renewing (it died, or lost the network) is reconciled: the execution and its task fail with the diagnosis once the lease expires ([command reference](cli.md#recovery)).

The trace of the run stays on the node (`<node-state>/traces/<execution>.jsonl`); the execution keeps its reference. The report carries the result object AXLR produced, the usage, the policy record and the correlation record.

## Events

`node.registered`, `node.heartbeat`, `node.offline`, `node.enrolled`, `node.revoked`, `execution.assigned` and `execution.reported` are the node events, in `events.jsonl` with the bot, task, execution and trace ids they concern. The control plane marks Offline the nodes silent for over a minute, on start and every minute.

## Revocation

```bash
axlr-bots node revoke web-2
```

The node is marked `Revoked`, its certificate's serial goes on the denylist (`<state>/pki/revoked.json`, checked on every request a certificate authenticates, so an open connection is cut off too), and the executions it still held fail with their tasks (`revoked: node web-2 was revoked at …`, a `bot.task.failed` event with `"reason": "revoked"`). A revoked node cannot register, heartbeat or take work; a fresh token readmits it with a new certificate, and the old serial stays denied.

## What travels, what stays

| | |
|---|---|
| Out of the node | the registration, heartbeats, polls, lease renewals and reports; the signing request at enrollment |
| Into the node | the assignments (the objective, model, level, tools, rules, timeout and correlation ids, or a decision) and its own certificate once |
| Never leaves the node | its private key, the AXLR session and its trace (the execution keeps a reference), the workspace |
| Never leaves the control plane | the CA key, the token secrets (stored hashed), the operator bearer token |
| Per host | the model key, in that host's environment file |

## Limits today

A node certificate lasts a year and is renewed by enrolling again with a fresh token (no automatic renewal). Revocation is the control plane's own denylist (no CRL or OCSP for other verifiers). The CA key lives in the control plane's state directory (no HSM, no intermediate). Operators authenticate with a bearer token, not client certificates ([API](operator-api.md)).
