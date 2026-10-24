# Architecture: layout

One Go module, hexagonal: the domain depends on nothing, the application holds the ports and the use cases, the adapters implement the ports, `compose` wires them and the two commands call it. One file per type; every package keeps at least 80 % statement coverage. How the pieces are split between the control plane, the node and AXLR is in [control plane, node and AXLR](control-plane-node-axlr.md).

## Packages

| Package | Role |
|---|---|
| `domain` | Bot, Task, Execution, Node, Enrollment and Timer aggregates; value objects validated in constructors (autonomy level, tool rules, triggers with their schedules and credentials, placement, leases); a node's certificate serial and its `Revoked` status; events (vision §11.2) under a schema version with the correlation ids; audit entries |
| `application` | Ports (`BotRepository`, `TaskRepository`, `ExecutionRepository`, `TimerRepository`, `NodeRepository`, `EnrollmentRepository`, `Certifier`, `CertificateDenylist`, `EventPublisher`, `EventReader`, `AuditSink`, `RuntimeExecutor`, `DecisionExecutor`, `TraceLocator`, `ArtifactStore`, `NodeTransport`) and use cases (`RegisterBot`, `EnableBot`, `UpdateBot`, `SubmitTask`, `FireTrigger`, `TickTimers`, `RunTask`, `ApproveTask`, `CancelTask`, `ListExecutions`, `ListEvents`, `RegisterNode`, `HeartbeatNode`, `NextAssignment`, `RenewLease`, `ReportExecution`, `CreateEnrollmentToken`, `EnrollNode`, `AuthenticateNode`, `RevokeNode`, `MarkOfflineNodes`, `ReconcileTasks`, `RunNextPending`, `GetReport`, …); `SerialRunner` serialises runs, assignments, reports and revocations and interrupts the run a cancel names |
| `nodeproto` | The messages of the node protocol, shared by the server and the node's client; the enrollment token's encoding |
| `adapters/pki` | The fleet CA (ECDSA P-256; the key under `<state>/ca`, mode 0600), the node certificates it signs from a node's request (common name = node id, a year), the control plane's own certificate (issued at start for the host's names), the node's key and request, its stored credentials, and the TLS 1.3 configurations of both ends |
| `adapters/filestore` | One JSON file per aggregate under the state directory, written atomically (temp file, fsync, rename); the timers; the enrollments (the hash of each token, never the token); the denylist of revoked certificate serials; the report artifacts |
| `adapters/jsonl` | Append-only `events.jsonl` (read back per bot for `GET /v1/bots/{id}/events`) and `audit.jsonl` |
| `adapters/axlr` | Runs `axlr-tui --exec --json [--policy <file>] [--correlation <file> --budget <file>]` as a subprocess and reads the execution result object; keeps the raw lines as the trace and the policy and correlation files as evidence. Used by the node, and by the control plane in single-process mode |
| `adapters/unixapi` | The API: a `Service` interface with `Local` (in process) and `Client` (HTTP) implementations; the `Server` serves everything on the Unix socket, the node protocol alone on the node TLS address and the operator routes alone on the operator TLS address; the `Client` is also the node's `NodeTransport` |
| `adapters/daemon` | The control plane as a service: the API on the socket and the two TLS addresses, in-process runs for bots no node may run, the timer triggers on every tick, reconciliation and offline nodes on a timer |
| `adapters/nodeagent` | The node agent's loop: register, heartbeat, poll, run, renew, report |
| `adapters/systemd` | Renders and installs the two units and the environment file (or the node's alone, for a host that joins a control plane elsewhere); removes them |
| `adapters/system` | Wall clock, random ids |
| `compose` | The composition root the commands share |
| `cmd/axlr-bots`, `cmd/axlr-node` | The two commands |
| `internal/fakeaxlr` | A shell stand-in for `axlr-tui` the tests run |

## The state directory

```text
<state>/
  bots/<bot-id>.json       tasks/<task-id>.json      executions/<exec-id>.json   nodes/<node-id>.json
  timers/<bot-id>~<trigger>.json (the next fire)      api.token (0600, with --api-listen)
  enrollments/<enr-id>.json (the hash of each token)  ca/ca.key (0600), ca/ca.crt   pki/revoked.json
  events.jsonl             audit.jsonl               axlr-bots.sock (while the daemon runs)
  policies/<exec-id>.json  correlation/<exec-id>.json  artifacts/<task-id>/report.md
  traces/, workspaces/     (single-process runs; a node keeps its own under its state)

<node-state>/
  pki/node.key (0600), pki/node.crt, pki/ca.crt, pki/enrollment.json   (once enrolled)
  traces/, policies/, correlation/, workspaces/<bot-id>/
```

Every aggregate is one JSON file written atomically; the idempotency key, the Pending tasks and the Running executions are found by scanning the directory, which is enough for a host and is the store to replace first ([control plane, node and AXLR](control-plane-node-axlr.md#what-is-next)).

## Build and test

```bash
go build ./... && go test -race -count=1 -cover ./...
```

On a small machine use `GOFLAGS=-p=2` and run one `go test` at a time. The AXLR adapter is tested against a helper process that imitates `axlr-tui`; the commands, the API, the daemon and the node agent against `internal/fakeaxlr` (a shell stand-in that answers `--help` and `--version`, records its arguments and plays a scenario, including the approval flow with a marker file); the installer renders into a `--destdir`. The node protocol over TLS is tested in `adapters/pki` (the handshakes), `adapters/unixapi` (enrollment end to end, reused and expired tokens, a certificate on another node's routes, a revoked serial on an open and on a new connection, rotation), `application` and `adapters/daemon` (two enrolled nodes and a bot placed by selector). The triggers are tested in `domain` (the HMAC and secret checks, the cron parser, the fire keys), `application` (a redelivered webhook, a timer that catches up once, a paused bot), `adapters/unixapi` (the operator routes, the bearer token, the webhook credentials, `413`) and `adapters/daemon` (a timer fires once more after a restart).

Three tests run the real binary and a model key when asked ([live run](../live-run.md#the-live-tests)):

```bash
AXLR_LIVE=1 AXLR_TUI=/path/to/axlr-tui OPENROUTER_API_KEY=... go test ./adapters/axlr -run TestLiveExec -v
AXLR_BOTS_LIVE=1 AXLR_TUI=/path/to/axlr-tui OPENROUTER_API_KEY=... go test ./adapters/daemon -run TestLiveTwoNodesOverTLS -v
AXLR_BOTS_LIVE=1 go test ./cmd/axlr-bots -run TestLiveWebhookRunsOnTheNode -v    # against the daemon installed on this host
```
