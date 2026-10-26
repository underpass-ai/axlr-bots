# Documentation catalog

The maintained documentation surface: every page, what it is for, and the code it was checked against. A page is current for the revision it names; the code is the authority where they disagree.

| Page | Purpose | Executable authority | Audited revision |
|:--|:--|:--|:--|
| [README](../README.md) | what AXLR Bots is, install, the first bot, where to read next | `cmd/axlr-bots`, `cmd/axlr-node` | `node-sidecar`, 25 Oct 2026 |
| [Documentation home](index.md) | the task → guide tables | this catalog | `bot-delegation`, 25 Oct 2026 |
| [Bot definition](reference/bot-definition.md) | the fields of a bot, the autonomy levels, the approvals, the tool rules, the triggers, update semantics, tasks | `application/bot_definition.go`, `domain/` | `bot-delegation`, 25 Oct 2026 |
| [Command reference](reference/cli.md) | every command and flag, defaults and environment, reports, logs | `axlr-bots --help`, `axlr-node --help`, `cmd/axlr-bots/run.go` | `bot-delegation`, 25 Oct 2026 |
| [Operator API](reference/operator-api.md) | the listeners, the routes, the idempotency key, the artifacts, the errors | `adapters/unixapi/server.go` | `bot-delegation`, 25 Oct 2026 |
| [Node protocol](reference/node-protocol.md) | node identity, the messages, leases, artifact uploads, events, revocation, what travels | `nodeproto/`, `adapters/unixapi`, `adapters/nodeagent`, `adapters/pki` | `bot-delegation`, 25 Oct 2026 |
| [Install](runbooks/install.md) | install user-level or system-wide, the files written, linger, uninstall | `adapters/systemd/`, `cmd/axlr-bots/install_commands.go` | `91dc705` |
| [Add a host](runbooks/add-a-host.md) | token, `install --node-only`, enroll, rotate, revoke | `cmd/axlr-node/enroll.go`, `application/enroll_node.go`, `application/revoke_node.go` | `91dc705` |
| [Sidecar](runbooks/sidecar.md) | the node image, a token per replica, the demo, the inject templates, what lives where, rotation, trade-offs | `distribution/node.Dockerfile`, `charts/axlr-bots-node/`, `cmd/axlr-node/first_start.go`, `cmd/axlr-node/check.go` | `node-sidecar`, 25 Oct 2026 |
| [Drone fleet](runbooks/drone-fleet.md) | the fleet's pieces, the images, install, enrolling eight nodes, the bots, the panel through a port-forward, a mission, the port in 3D (OSM, Blender, gz sim), containers overboard (drift, search patterns, sightings, the phone page, AIS, the report), limits | `examples/drone-fleet/`, `examples/helm/drone-fleet/` | `drone-fleet`, `drone-fleet-3d`, `containers-overboard`, 26 Oct 2026 |
| [Triggers](runbooks/triggers.md) | add triggers, fire a webhook, check the deduplication and the catch-up | `application/fire_trigger.go`, `application/tick_timers.go` | `91dc705` |
| [Approvals](runbooks/approvals.md) | the approval path with the example bot, the expiry, the notifications (webhook, command, a relay to chat) | `application/approve_task.go`, `application/expire_approvals.go`, `application/deliver_notifications.go`, `adapters/notify/` | `approvals-artifacts`, 25 Oct 2026 |
| [Recovery](runbooks/recovery.md) | tasks left Running, silent nodes, restarts, a lost state | `application/reconcile_tasks.go`, `application/mark_offline_nodes.go` | `91dc705` |
| [Control plane, node and AXLR](architecture/control-plane-node-axlr.md) | responsibilities, the daemon, the trust model, the node as a sidecar, what is next and left out | `adapters/daemon/`, `adapters/nodeagent/`, `charts/axlr-bots-node/` | `bot-delegation`, 25 Oct 2026 |
| [Layout](architecture/layout.md) | the packages, the state directory, build and test | `go list ./...`, `adapters/filestore/` | `bot-delegation`, 25 Oct 2026 |
| [Runtime contract](architecture/runtime-contract.md) | the flags passed to `axlr-tui`, the policy table, correlation, budget, the result mapping, artifacts and the receipt | `adapters/axlr/`, AXLR's [`docs/runtime-contract.md`](https://github.com/underpass-ai/AXLR/blob/main/docs/runtime-contract.md) | `bot-delegation`, 25 Oct 2026 |
| [Delegation (ADR)](architecture/delegation.md) | how a bot asks another, who may ask whom, how depth and budget propagate and stop loops, how the answer returns, the tool's schema | `application/delegate_task.go`, `adapters/nodeagent/delegator.go`, `adapters/axlr/delegation_manifest.go` | `bot-delegation`, 25 Oct 2026 |
| [Vision](architecture/vision.md) | the design note of 21 October 2026, copied as written | none: a design record, not a description of the code | 21 Oct 2026 |
| [Live run](live-run.md) | the demo script's output on a real host, the sidecar on a real cluster, the criteria it meets, the live tests | `examples/demo.sh`, `charts/axlr-bots-node`, the three live tests | `bot-delegation`, 25 Oct 2026 |
| [Brand](brand.md) | the three marks, their palette, how to regenerate them | `docs/assets/brand/emblem.py` | 24 Oct 2026 |

## History boundary

The README before this catalog existed was a working log with the live output inline; it remains in Git history as evidence, not as instructions. Check any historical command against the current implementation before reusing it.
