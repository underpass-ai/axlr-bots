# The orchestra at home

The [orchestra](../../orchestra/README.md) and its rave on a Kubernetes cluster: a control plane whose pushes go through NATS JetStream with mutual TLS, one node per chair, and the score and rave services behind HTTPS, so the Quest opens `/hands` by its name, with no `adb reverse`.

```text
                         Route 53: orquesta / rave.underpassai.com → the ingress's LAN address
 browser, Quest 3S ──HTTPS (Let's Encrypt)──▶ ingress-nginx ──http──▶ score :8790 ─┐   rave :8794
                                                                                    │ :7879 TLS + operator token
 node per chair (StatefulSet per section) ──:7878 mutual TLS (enrollment)──▶ control plane ──tls://nats:4222──┐
      │  http :8790 MCP (/mcp/<chair>) ─▶ score                                          mutual TLS (verify_and_map)
      └──────────────────── tls://nats:4222, mutual TLS, its own consumer only ─────────▶ NATS JetStream ◀─┘
```

| Piece | What | Where |
|---|---|---|
| `orchestra-nats` | NATS 2.15.0, JetStream on a volume, TLS with a client certificate required, users mapped from certificates | [`templates/nats.yaml`](templates/nats.yaml) |
| `orchestra-cp` | the control plane, `serve --bus nats` with its own client certificate (`ca issue-client`) | [`templates/control-plane.yaml`](templates/control-plane.yaml) |
| `vn1`, `vn2`, `va`, `vc`, `cb`, `ob`, `hn` | one StatefulSet per section, ordinals from 1: the pod `vc-1` is the chair `vc-1` and the node `stage-vc-1`, a Jev bot's node (`axlr-bots-node` chart, `bus.kind: nats`) | [`templates/chairs.yaml`](templates/chairs.yaml) |
| `orchestra-score` | the score service, `https://orquesta.underpassai.com` | [`templates/score.yaml`](templates/score.yaml) |
| `orchestra-rave` | the rave, `https://rave.underpassai.com`, samples in the image | [`templates/rave.yaml`](templates/rave.yaml) |
| policies | each pod reaches only what it needs | [`templates/networkpolicy.yaml`](templates/networkpolicy.yaml) |

## Install

The images come from the lab's registry, by digest ([`values.yaml`](values.yaml)); `score-service/Dockerfile` and `rave-service/Dockerfile` build the two services from `examples/orchestra`, `distribution/node.Dockerfile` the node and `examples/drone-fleet/control-plane.Dockerfile` the control plane (`--platform linux/amd64` for wrx80). The DNS names point at the ingress's address before the first install, so cert-manager's DNS-01 and the LAN router agree.

```bash
TYPESAFE_API_KEY=… examples/helm/orchestra/bootstrap.sh --enable ob-1,vc-1
```

[`bootstrap.sh`](bootstrap.sh) installs the chart and makes what the chart does not hold, from the control plane it started: Jev's key, the fleet CA as a ConfigMap, the operator token, one enrollment token per chair, the 36 chairs as Jev bots, the ones named enabled. Running it again changes only what is missing. The broker waits for the fleet CA, the score service for the token, each node for its enrollment token: they start by themselves once bootstrap made them.

**The chairs that run.** wrx80's kubelet takes 110 pods and held about 90 before the orchestra; all 36 chairs need its `maxPods` raised. Until then `chairs.sections[].replicas` runs the two chairs measured live (`ob-1`, `vc-1`); the broker knows all 36 and every bot is registered.

## Mutual TLS, leg by leg

| Leg | TLS | Who proves what |
|---|---|---|
| node → control plane `:7878` | TLS 1.3, the fleet CA | **mutual**: the node's certificate from enrollment; its serial must be the node's current one |
| control plane → NATS `:4222` | TLS, the broker's CA (cert-manager) | **mutual**: `CN=control-plane,O=axlr-bots clients` from `ca issue-client`, the one user that publishes |
| node → NATS `:4222` | TLS, the broker's CA | **mutual**: the same enrollment certificate, `CN=stage-<chair>,O=axlr-bots nodes`; it reads its own consumer and nothing else |
| score → control plane `:7879` | TLS, the fleet CA pinned | server only, plus the operator's bearer token |
| browser, Quest → ingress `:443` | TLS, Let's Encrypt | server only; no login (the names resolve to a LAN address) |
| ingress → score, rave | plain HTTP inside the cluster | none: the policy admits only the ingress controller, the service answers only its names |
| chair → score MCP `:8790` | plain HTTP inside the cluster | none: the policy admits only the chairs, the MCP answers only the internal name |
| score, chairs → Jev | TLS, public CA | API key |

What is left:

| Gap | Why it matters | Next step |
|---|---|---|
| ingress → services and chair → MCP in plain HTTP | anyone on the pod network who passes the policies reads the traffic | backend TLS on the services, or a mesh |
| `:7879` is a bearer token, not a client certificate | a leaked token is the operator | client certificates on the operator API |
| the pages have no login | anyone on the LAN can conduct | ingress basic auth or OIDC, once the Quest's browser is checked with it |
| the fleet CA's key is on the control plane's volume, the broker CA's in a Secret | whoever reads them mints identities | an offline root with an intermediate |
| certificates rotate by hand: the node's (a year), the control plane's broker certificate (a year, delete `nats-client/` and restart) | expiry stops the bus | renewal before expiry; the broker's own rotates with cert-manager but NATS reads it at start (restart or `--signal reload`) |
| the control plane on gx10 (systemd) is unchanged, bus `direct` | the home instance is this one | NATS on a LoadBalancer address if gx10 should join |

## What the bus shows

Measured on 31 Oct 2026, cluster on wrx80, chairs `ob-1` and `vc-1` (Jev bots), four takes: the operator publishes `info.cue.all` with `kubectl exec … group publish`; the clocks are the score service's (`/api/take`), same host.

| | |
|---|---|
| cue → the score service starts the take | 0.17–0.19 s |
| cue → first `play` of each chair | 0.35–0.61 s (8 of 8, median 0.41 s) |
| NATS consumers `node-stage-ob-1`, `node-stage-vc-1` | delivered = acknowledged, 2–3 pushes per take each (the cue and the teammate's `info.chair.played`) |

The times include `kubectl exec` (about 0.1 s). A node on `--bus nats` holds no stream to the control plane, so a chair that hears the cue heard it through the broker.

## Checks

```bash
curl -sI https://orquesta.underpassai.com/hands                       # 200, Let's Encrypt
curl -s -o /dev/null -w '%{http_code}\n' -X POST https://orquesta.underpassai.com/mcp/ob-1   # 403: the MCP is not public
curl -s --max-time 5 https://rave.underpassai.com/audio.wav | wc -c   # about 176 KB a second
```
