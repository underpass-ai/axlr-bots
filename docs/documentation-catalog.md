# Documentation catalog

The maintained documentation surface: every page, what it is for, and the code it was checked against. A page is current for the revision it names; the code is the authority where they disagree.

| Page | Purpose | Executable authority | Audited revision |
|:--|:--|:--|:--|
| [README](../README.md) | what AXLR Bots is, install, the first bot, where to read next | `cmd/axlr-bots`, `cmd/axlr-node` | `91dc705`, 24 Oct 2026 |
| [Documentation home](index.md) | the task → guide tables | this catalog | `91dc705` |
| [Bot definition](reference/bot-definition.md) | the fields of a bot, the autonomy levels, the tool rules, the triggers, update semantics, tasks | `application/bot_definition.go`, `domain/` | `91dc705` |
| [Command reference](reference/cli.md) | every command and flag, defaults and environment, reports, logs | `axlr-bots --help`, `axlr-node --help`, `cmd/axlr-bots/run.go` | `91dc705` |
| [Operator API](reference/operator-api.md) | the listeners, the routes, the idempotency key, the errors | `adapters/unixapi/server.go` | `91dc705` |
| [Node protocol](reference/node-protocol.md) | node identity, the messages, leases, events, revocation, what travels | `nodeproto/`, `adapters/unixapi`, `adapters/pki` | `91dc705` |
| [Install](runbooks/install.md) | install user-level or system-wide, the files written, linger, uninstall | `adapters/systemd/`, `cmd/axlr-bots/install_commands.go` | `91dc705` |
| [Add a host](runbooks/add-a-host.md) | token, `install --node-only`, enroll, rotate, revoke | `cmd/axlr-node/enroll.go`, `application/enroll_node.go`, `application/revoke_node.go` | `91dc705` |
| [Triggers](runbooks/triggers.md) | add triggers, fire a webhook, check the deduplication and the catch-up | `application/fire_trigger.go`, `application/tick_timers.go` | `91dc705` |
| [Approvals](runbooks/approvals.md) | the approval path with the example bot | `application/approve_task.go`, `adapters/axlr/executor.go` | `91dc705` |
| [Recovery](runbooks/recovery.md) | tasks left Running, silent nodes, restarts, a lost state | `application/reconcile_tasks.go`, `application/mark_offline_nodes.go` | `91dc705` |
| [Control plane, node and AXLR](architecture/control-plane-node-axlr.md) | responsibilities, the daemon, the trust model, what is next and left out | `adapters/daemon/`, `adapters/nodeagent/` | `91dc705` |
| [Layout](architecture/layout.md) | the packages, the state directory, build and test | `go list ./...`, `adapters/filestore/` | `91dc705` |
| [Runtime contract](architecture/runtime-contract.md) | the flags passed to `axlr-tui`, the policy table, correlation, budget, the result mapping | `adapters/axlr/`, AXLR's [`docs/runtime-contract.md`](https://github.com/underpass-ai/AXLR/blob/main/docs/runtime-contract.md) | `91dc705` |
| [Vision](architecture/vision.md) | the design note of 21 October 2026, copied as written | none: a design record, not a description of the code | 21 Oct 2026 |
| [Live run](live-run.md) | the demo script's output on a real host, the criteria it meets, the live tests | `examples/demo.sh`, the three live tests | 24 Oct 2026 |
| [Brand](brand.md) | the marks, their source, how to regenerate them | `docs/assets/brand/emblem.py` | 24 Oct 2026 |

## History boundary

The README before this catalog existed was a working log with the live output inline; it remains in Git history as evidence, not as instructions. Check any historical command against the current implementation before reusing it.
