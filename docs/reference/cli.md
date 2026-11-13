# Command reference

Two commands come out of this module: `axlr-bots`, the control plane and the operator's CLI, and `axlr-node`, the node agent. Output is JSON on stdout; `task report` prints the report as text. Exit codes: 0 ok, 1 error, 2 usage; `task run` exits 3 when the run stopped for a decision.

```text
axlr-bots [--state <dir>] [--axlr-bin <path>] [--grpc-socket <path>] [--actor <name>] <command>
axlr-bots --control host:7880 --token-file <path> [--ca-file <pem>] <command>   the same commands against a control plane on another host, over gRPC

  serve [--listen <addr>] [--server-names a,b] [--api-listen <addr>] [--grpc-listen <addr>] [--grpc-socket <path>] [--api-token-file <path>] [--api-url <url>]
        [--bus direct|nats] [--nats-url <url>] [--nats-creds <file>]              pushes to the nodes also over a broker (bus port)
        [--nats-cert <file> --nats-key <file>] [--nats-ca <file>]                  mutual TLS to the broker
        [--tick 2s] [--reconcile-every 1m] [--grace <duration>]
        [--min-axlr-version <semver>]                                               the least AXLR the nodes should run, answered by GET /v1/build (also AXLR_BOTS_MIN_AXLR_VERSION)
  ca init | ca show
  ca issue-client --name <cn> --out <dir> [--lifetime <duration>]   client.key, client.crt, ca.crt for a service of the fleet (365 days)
  node list | node get <node-id>
  node enroll-token [--node-id <id>] [--labels k=v,k=v] [--ttl 1h] | node revoke <node-id>
  bot register --file <bot.json> | bot enable <bot-id> | bot pause <bot-id> | bot stop <bot-id> | bot delete <bot-id> | bot list | bot get <bot-id>
  bot update <bot-id> --file <patch.json> | bot events <bot-id> [--limit 100]
  bot stage <bot-id> --file <patch.json> | bot staged <bot-id> | bot apply-staged <bot-id> | bot unstage <bot-id>   a change held for later, naming its version ([staged changes](bot-definition.md#staged-changes))
  bot subscribe <bot-id> <topic> [--mode task|message|note] [--from a,b] | bot unsubscribe <bot-id> <topic> | bot subscriptions <bot-id>
  group list | group get <group> [--tenant <id>]   the teams the bots name, with their members
  group events <group> [--after <event-id>] [--limit 100] [--tenant <id>]   the team's timeline
  group topics <group> [--tenant <id>]   who publishes, who listens, nobody listens
  group publish <group> <name> --message "<text>" [--data '{…}'] [--to a,b] [--key <k>] [--tenant <id>]   the operator's event; stop.all stops everyone and drops their queued tasks
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>] [--urgent]   --urgent goes first, cancels the bot's work in progress and drops its queued tasks
  trigger fire <bot-id> <trigger> --body '<json>' | --file <event.json> [--delivery <id>]
  task run <task-id>                           in process (exit 0 completed, 3 decision required, 1 failed)
  task approve <task-id> | task deny <task-id> | task decide <task-id> approve|deny   [--call <id>]
                                               (exit as task run; 0 too when a node carries the decision;
                                               3 after the first of two approvals; decided as --actor)
  task get <task-id> | task executions <task-id> | task events <task-id> [--limit 100] | task activity <task-id> [--json] [--depth N] [--watch] | task cancel <task-id>
  task list [--bot <id>] [--status <status>] [--since <rfc3339>] [--limit 100]
  task report <task-id> [--path] | task reconcile [--grace <duration>]
  execution get <execution-id> | execution artifact <execution-id> <name> [--out <file>]
  overview [--since <rfc3339>]                 nodes, bots and tasks by status; tasks, executions and cost since local midnight
  health                                       exit 0 when the daemon answers /healthz on the socket, 1 when it does not: a container's healthcheck ([runbook](../runbooks/control-plane-compose.md))
  version                                      the release, commit and time the binary was built from, as GET /v1/build names the running build
  install [--system] [--prefix <dir>] [--state <dir>] [--node-state <dir>] [--unit-dir <dir>] [--env-file <path>]
          [--axlr-bin <path>] [--node-binary <path>] [--listen <addr>] [--server-names a,b] [--api-listen <addr>]
          [--control <url>] [--node-only] [--destdir <dir>] [--no-systemctl]
  uninstall [--system] [--prefix <dir>] … [--purge]

axlr-node --control unix:///path/to/axlr-bots.sock | https://host:7878
          [--node-id <id>] [--labels k=v,k=v] [--state <dir>] [--axlr-bin <path>] [--heartbeat 15s] [--poll 20s]
          [--token-file <path> [--ca-file <pem>] [--enroll-retry 10s]]     over https: enroll first if not enrolled
          [--dedicated]                                                    only the bots placed on this node by name or selector
          [--renew-before 720h]                                            over https: renew the certificate this long before it expires
          [--bus direct|nats] [--nats-url <url>] [--nats-creds <file>]    read the control plane's pushes from a broker
          [--nats-cert <file> --nats-key <file>] [--nats-ca <file>]        mutual TLS to the broker (default: the node's own certificate)
          [--mcp-config <mcp.json>] [--jev-model jev-1.13.0]               the jev runtime (on when TYPESAFE_API_KEY is set)
          [--axlr-mcp-config <mcp.json>]                                   the mcp.json every AXLR execution loads
          [--memory-teams a,b|*]                                           the teams whose bots share one KMP store
axlr-node enroll --control https://host:7878 --token <token> | --token-file <path> [--ca-file <pem>] [--state <dir>] [--host-name <name>]
axlr-node check --control https://host:7878 [--ca-file <pem> | --token-file <path> | --state <dir>] [--timeout 10s] [--renew-before 720h]
axlr-node mcp-relay --socket <path>
axlr-node codex-proxy [--listen 127.0.0.1:8790] [--codex-home ~/.codex] [--refresh-before 48h]
```

## Defaults and environment

| Setting | Flag | Environment | Default |
|---|---|---|---|
| state directory | `--state` | `AXLR_BOTS_STATE`, then `XDG_STATE_HOME/axlr-bots` | `~/.local/state/axlr-bots` |
| the AXLR binary | `--axlr-bin` | `AXLR_TUI` | `axlr-tui` on `PATH` |
| how commands talk to the control plane | `--transport grpc\|rest` | `AXLR_BOTS_TRANSPORT` | `grpc`. `rest` is the HTTP API this one replaces, kept until it is removed |
| the daemon's gRPC socket | `--grpc-socket` (`""` never uses a daemon) | `AXLR_BOTS_GRPC_SOCKET` | `<state>/axlr-bots-grpc.sock`; an explicit empty `--socket` also means no daemon unless `--grpc-socket` names one |
| the daemon's HTTP socket | `--socket` (`""` never uses a daemon) | `AXLR_BOTS_SOCKET` | `<state>/axlr-bots.sock`; with `--transport rest` the commands use it, and over gRPC `trigger fire` does (a webhook trigger is an HTTP route, not an RPC) |
| a control plane on another host | `--control host:7880` (its gRPC operator address; a `https://` or `grpcs://` in front is accepted), `--token-file` (the operator token), `--ca-file` (the fleet CA, `ca show`) | `AXLR_BOTS_CONTROL`, `AXLR_BOTS_TOKEN_FILE`, `AXLR_BOTS_CA_FILE` | none: the socket. With `--control` every command but `serve`, `ca`, `install`, `uninstall` and `task reconcile` goes to that address (`health` too), never opens the state directory and never falls back to it; `--token-file` is required, `--ca-file` defaults to the system's CAs. With `--transport rest` it is the HTTPS operator address, `https://host:7879` |
| node address, server names, operator address | `serve --listen`, `--server-names`, `--api-listen` | `AXLR_BOTS_LISTEN`, `AXLR_BOTS_SERVER_NAMES`, `AXLR_BOTS_API_LISTEN` | none: the socket only |
| operator gRPC address ([Operator API over gRPC](operator-grpc.md)) | `serve --grpc-listen`, `--grpc-socket` | `AXLR_BOTS_GRPC_LISTEN`, `AXLR_BOTS_GRPC_SOCKET` | none: the socket `<state>/axlr-bots-grpc.sock` only |
| operator token file | `serve --api-token-file` | | `<state>/api.token` |
| the operator API's base URL in approval notifications | `serve --api-url` | `AXLR_BOTS_API_URL` | `https://` and the `--api-listen` address (the host's name for a wildcard address) |
| control plane: bus | `serve --bus direct\|nats`, `--nats-url`, `--nats-creds`, `--nats-cert`, `--nats-key`, `--nats-ca` | `AXLR_BOTS_BUS`, `AXLR_BOTS_NATS_URL`, `AXLR_BOTS_NATS_CREDS`, `AXLR_BOTS_NATS_CERT`, `AXLR_BOTS_NATS_KEY`, `AXLR_BOTS_NATS_CA` | `direct`: the nodes' own streams only; `nats` also publishes every push to the broker ([bus port](../architecture/bus-port.md#configuration)) |
| who acts (audit), and who decides | `--actor`: `task approve`, `deny` and `decide` send it as who decides, the name a bot's `approvals.approvers` lists ([who may decide](../runbooks/approvals.md#who-may-decide)) | | `cli:<$USER>` (`cli:unknown` when `USER` is unset) |
| node: control plane, id, labels, state | `--control`, `--node-id`, `--labels`, `--state` | `AXLR_NODE_CONTROL`, `AXLR_NODE_ID`, `AXLR_NODE_LABELS`, `AXLR_NODE_STATE` | the state is `~/.local/state/axlr-node`; over the socket the id is the host name; over `https://` it is the certificate's, and a `--node-id` that disagrees is refused |
| node: the enrollment token (`axlr-node enroll` only) | `--token` | `AXLR_NODE_TOKEN` | none |
| node: token file, pinned CA | `--token-file`, `--ca-file` | `AXLR_NODE_TOKEN_FILE`, `AXLR_NODE_CA_FILE` | none; `enroll` takes `--ca-file` from the flag only (`run` and `check` read `AXLR_NODE_CA_FILE`) |
| node: dedicated | `--dedicated` | `AXLR_NODE_DEDICATED=true` | off: the node also runs bots placed on any node |
| node: certificate renewal | `--renew-before` | `AXLR_NODE_RENEW_BEFORE` | `720h`: the agent renews its certificate from 30 days before it expires, checking at start and every day ([renewal](node-protocol.md#renewal)); a value past the certificate's life renews at every check, to see a renewal on a test node |
| node: bus | `--bus direct\|nats`, `--nats-url`, `--nats-creds`, `--nats-cert`, `--nats-key`, `--nats-ca` | `AXLR_NODE_BUS`, `AXLR_NODE_NATS_URL`, `AXLR_NODE_NATS_CREDS`, `AXLR_NODE_NATS_CERT`, `AXLR_NODE_NATS_KEY`, `AXLR_NODE_NATS_CA` | `direct`: the control plane's pushes over the node's own stream; `nats`: read from the broker, showing a `tls://` broker the node's own certificate from enrollment unless `--nats-cert` names another ([bus port](../architecture/bus-port.md)) |
| node: AXLR's servers | `--axlr-mcp-config` | `AXLR_NODE_AXLR_MCP_CONFIG` | none: each run loads the operator's `~/.config/axlr/mcp.json`. Name a file of the node's own (an absolute path) so a server the operator added there does not join the bots' runs, nor fail them when it is down |
| node: shared memory | `--memory-teams` | `AXLR_NODE_MEMORY_TEAMS` | none: every bot has a KMP store of its own. Name the teams whose bots share one store, `team-a,team-b`, or `*` for every team ([memory store per team](../architecture/memory-store-per-team.md)); a bot of a team not named keeps its own |
| node: Jev | `--mcp-config`, `--jev-model` | `AXLR_NODE_MCP_CONFIG`, `AXLR_NODE_JEV_MODEL`; the key `TYPESAFE_API_KEY` | `~/.config/axlr/mcp.json` (the node's AXLR's) and `jev-1.13.0`; the jev runtime is on when `TYPESAFE_API_KEY` is set ([bot definition](bot-definition.md#runtime)) |

`axlr-node --token-file` is a sidecar's first start ([sidecar](../runbooks/sidecar.md)): with no certificate under its state the node enrolls with the token in the file, refusing a token whose node id is not `--node-id` before sending it; with a certificate it keeps it, unless the file holds a token other than the one the certificate came from (`pki/enrollment.json` records its id), which rotates it; a rotation that fails keeps the certificate. Until it has a certificate it retries every `--enroll-retry`, reading the file again each time, and never exits for it. `axlr-node mcp-relay` is not run by hand: AXLR starts it as the stdio MCP server of an execution whose bot may delegate, and it pipes its stdin and stdout to the agent's socket for that execution, `<state>/run/<execution>.sock` ([delegation](../architecture/delegation.md)). `axlr-node codex-proxy` serves the OpenAI Responses API on a loopback address and sends each request on to Codex's backend with the host user's ChatGPT login, which it refreshes as Codex does; AXLR names it as a `local_models` entry ([Codex subscription](../runbooks/codex-subscription.md)). `axlr-node check` asks the node address's `/healthz` over TLS 1.3, verifying the control plane against the CA in `--ca-file`, the token's, or the one enrollment stored, and presents no certificate; it changes nothing. When the state holds a node certificate it also prints its serial (`certificate`), its expiry (`notAfter`) and `renewalDue` (`no`, `yes` or `expired`).

Every command tries the socket first (the gRPC one; with `--transport rest`, the HTTP one): when the daemon answers, the command is carried by it, so a `task submit` reaches the running service and never touches the state files. When nothing answers, the command works on the state directory: the task is spooled and runs at the daemon's next start, or with `task run` in process.

## The gRPC connection

The commands speak the operator API of [`api/proto/axlrbots/operator/v1`](../../api/proto/axlrbots/operator/v1) (the contract is in `common.proto`). Two ways in:

- **The daemon's socket.** `--grpc-socket` (default `<state>/axlr-bots-grpc.sock`) is a Unix socket with no credentials: who can open the file may operate. A command probes it with the cheapest read (`GetBuild`, two seconds); when nothing answers it works on the state directory, as before.
- **Another host.** `--control host:7880` dials the control plane's gRPC operator address over TLS 1.3. The server certificate is checked against `--ca-file` (the fleet CA from `ca show`; the system's CAs when absent) and the operator token from `--token-file` goes in the `authorization` metadata key as `Bearer <token>` on every call, streams included. A wrong token is `unauthorized`, a node certificate opens nothing.

`--actor` (default `cli:<user>`) is who decides: `task approve`, `deny` and `decide` send it as `decided_by` and in the `x-axlr-actor` metadata key, which a bot's approvers are compared with. An error comes back as a gRPC status with an `ErrorDetail`, and the command prints the same message it printed over HTTP (`bot nope not found`, a `state` refusal in the control plane's words); a failure with no detail (the control plane did not answer) names the address. `health` has no RPC of its own: it asks `GetBuild`, and a control plane that answers with a refusal still counts as there.

Artifacts and transcripts arrive in chunks and are checked against the size and SHA-256 of their first message. The activity snapshot is one message: the client takes up to 64 MiB, where gRPC's default is 4.

## Task activity

`task activity <task-id>` shows everything that happened under a task, from `ActivityService`:

```
axlr-bots task activity <task-id> [--json] [--depth N] [--watch]
```

- The task, then each execution with its outcome, node, duration, cost (`cost unknown` when it is not known: unknown is not free) and tokens; the tools it ran with state (`ok`, `FAILED`, `running`) and duration, the decisions it asked and the tasks it delegated, from the task's `bot.task.progress` events; then the task's own timeline, and the tasks it delegated indented under it, recursively. A last line sums the usage of the whole tree.
- Where the control plane stopped recording an execution's progress (400 events or 256 KiB), a line marks the cut with its reason and what was dropped, and the tools after it, read from the execution's transcript, follow marked `[from transcript]`.
- `--json` prints the tree (task, executions with `progressCut` and `completedItems`, events, children, `subtreeUsage`); `--depth N` follows N levels of delegated tasks (default: the delegation limit).
- `--watch` prints the tree, then reprints it after each change until every task in it has finished (exit 0) or Ctrl-C; with `--json` one line per change. It resumes nothing by itself: a lost stream ends the command with the error.
- It needs gRPC: against the in-process fallback (no daemon) it says so and exits 1.

## Reports

When an execution completes, its answer is written to `<state>/artifacts/<task-id>/report.md` on the control plane, listed in the execution's `result.artifacts` as `report.md`, and named in the `bot.task.completed` event and the daemon's log line.

```bash
axlr-bots task report <task-id>          # the text
axlr-bots task report --path <task-id>   # only where it is
```

## Recovery

`task reconcile` fails the tasks a crash left Running, with a diagnosis, and the Running executions no task runs (`orphaned`: a crash between saving an execution and its task left them); the daemon does the same on start and every `--reconcile-every`. The daemon never fails a run it has in flight. `task reconcile` works on the files: it takes the lock on the state directory (`<state>/.lock`), so while the daemon serves it is refused, and so is `ca init`. A command that finds no daemon on the socket falls back to the files under the same lock; when a daemon holds it (busy, or on another socket) the command says so and exits 1 instead of racing it. The procedure is the [recovery runbook](../runbooks/recovery.md).

## Logs and audit

Both services log to the journal: `journalctl --user -u axlr-bots -u axlr-node -f`. The control plane appends every event to `<state>/events.jsonl` (`bot events <id>` reads them back per bot) and every operator action to `<state>/audit.jsonl` with the actor.
