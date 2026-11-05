# Runbook: add a host

Join a second host to a control plane installed with `--listen` ([install](install.md#one-control-plane-nodes-over-tls)). Every connection goes out from the new host; the control plane never reaches into it. The identity model (the fleet CA, the node certificate, the one-time token) is in the [node protocol](../reference/node-protocol.md#identity).

## On the control plane's host

1. The daemon must listen on an address the new host reaches: `install --listen 0.0.0.0:7878 --server-names cp.example.internal`, port 7878 open inbound. `--server-names` adds the names the new host will use to the server certificate.
2. Mint a one-time token bound to the new node's id and labels:

```bash
axlr-bots node enroll-token --node-id web-2 --labels zone=eu --ttl 1h
# {"token": "axlr1.…", "id": "enr-…", "nodeId": "web-2", "labels": {"zone": "eu"}, "expiresAt": "…"}
```

The token is one line and is printed once; it carries the CA certificate, so nothing else is copied. Check: the `node.enroll-token` entry in `<state>/audit.jsonl` names the token id.

## On the new host

As the user the node will run as, with the repository checked out. The token and the control plane's URL are the only things copied from the first host; the model key is this host's own.

```bash
go build -o bin/ ./cmd/...
export OPENROUTER_API_KEY=...                                  # this host's key, into its env file
bin/axlr-bots install --node-only --control https://cp.example.internal:7878 --axlr-bin /path/to/axlr-tui
~/.local/bin/axlr-node enroll --control https://cp.example.internal:7878 --token 'axlr1.…' --state ~/.local/state/axlr-node
systemctl --user enable --now axlr-node.service
loginctl enable-linger "$USER"
```

`install --node-only` puts `axlr-node`, its unit (with `AXLR_NODE_CONTROL=https://cp.example.internal:7878` in its environment and no dependency on a local control plane) and the env file in place, and no control plane. `enroll` generates the node's key on this host, verifies the control plane against the CA the token carries (or `--ca-file`), and stores the key, the certificate, the CA and the enrollment record under `~/.local/state/axlr-node/pki`. `--state` must be the directory the unit runs with: `enroll` would otherwise follow `XDG_STATE_HOME` when it is set, and the unit would not find the credentials.

Check: `enroll` prints the node id, the certificate's serial and its expiry; the agent's first journal line (`journalctl --user -u axlr-node -n 2`) names the certificate and the serial. A refused enrollment is `409` for a used or expired token and `403` for an unknown one; mint a new token, the old one is burnt.

## Back on the control plane

```bash
axlr-bots node list                         # web-2 Online, certificateSerial and certificateNotAfter set, labels {"zone": "eu", "host": "web-2"}
axlr-bots bot register --file bot-eu.json   # "placement": {"selector": {"zone": "eu"}} runs on web-2 only
```

Check: a task of a bot placed on `web-2` (`"placement": {"nodes": ["web-2"]}` or a matching selector) shows `"nodeId": "web-2"` on its execution.

## Rotate a node's certificate

The agent renews its own certificate: at start and every day it looks at the expiry, and from 30 days before it (`--renew-before`, `AXLR_NODE_RENEW_BEFORE`; `720h` by default) it draws a new key, asks `POST /v1/nodes/{id}/certificate` over the connection its current certificate opens, writes the new key, certificate and CA under `<state>/pki` and presents the new certificate from its next call; the old serial goes on the denylist ([node protocol](../reference/node-protocol.md#renewal)). A renewal that fails is logged and tried again the next day. Check: `axlr-node check --state ~/.local/state/axlr-node` prints the certificate's `notAfter` and `renewalDue` (`no`; `yes`: the agent renews at its next check; `expired`: only a fresh token helps); on the control plane `node get web-2` shows the new serial and `certificateNotAfter`, and the node's events carry a `node.renewed`.

To rotate by hand (a key you no longer trust, a certificate that expired while the host was off), mint a fresh token for the same node id and run `axlr-node enroll` again on the host, then restart `axlr-node.service`. The previous certificate goes on the denylist and stops being accepted at the next request. Check: `node list` shows the new serial and its expiry in `certificateNotAfter`.

A node whose record the control plane lost (a state restored from before the node was enrolled) needs no new token: its next registration recreates the record pinned to the certificate it holds, and every later call is checked against that serial. The recreated record has only the labels the agent reports (`reportedLabels`); the token's labels went with the lost record, so mint a token with them and enroll again when they matter. On a known node the token's labels (`operatorLabels`) win over the agent's wherever both name a key, and `labels` is the result.

## Retire or revoke a host

```bash
axlr-bots node revoke web-2
```

The node is `Revoked`, its certificate's serial goes on the denylist, so the next request it makes is refused (a connection it holds open is not cut; the next request on it is), and the executions it still held fail with their tasks (`revoked: node web-2 was revoked at …`), and the node stops their runs at its next lease renewal. The agent itself stops asking at the first refusal: its journal says once `node: web-2 is revoked: … To bring it back, mint a fresh token …`, and then nothing (the unit stays active, idle). To readmit the host, mint a fresh token for the same node id, run `axlr-node enroll` with it and restart `axlr-node.service`. Check: `node get web-2` shows `Revoked`; the tasks it held are `Failed` with a `bot.task.failed` event, `"reason": "revoked"`. A fresh token readmits the host with a new certificate; the old serial stays denied. On the host itself, `axlr-bots uninstall` removes the node unit and binary.

The same node can run inside a Kubernetes pod, beside a service: the [sidecar runbook](sidecar.md) mints one token per replica into a Secret, and the pod enrolls itself on its first start.
