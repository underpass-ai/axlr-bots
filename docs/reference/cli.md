# Command reference

Two commands come out of this module: `axlr-bots`, the control plane and the operator's CLI, and `axlr-node`, the node agent. Output is JSON on stdout; `task report` prints the report as text. Exit codes: 0 ok, 1 error, 2 usage; `task run` exits 3 when the run stopped for a decision.

```text
axlr-bots [--state <dir>] [--axlr-bin <path>] [--socket <path>] [--actor <name>] <command>

  serve [--listen <addr>] [--server-names a,b] [--api-listen <addr>] [--api-token-file <path>] [--api-url <url>]
        [--bus direct|nats] [--nats-url <url>] [--nats-creds <file>]              pushes to the nodes also over a broker (bus port)
        [--nats-cert <file> --nats-key <file>] [--nats-ca <file>]                  mutual TLS to the broker
        [--tick 2s] [--reconcile-every 1m] [--grace <duration>]
  ca init | ca show
  ca issue-client --name <cn> --out <dir> [--lifetime <duration>]   client.key, client.crt, ca.crt for a service of the fleet (365 days)
  node list | node get <node-id>
  node enroll-token [--node-id <id>] [--labels k=v,k=v] [--ttl 1h] | node revoke <node-id>
  bot register --file <bot.json> | bot enable <bot-id> | bot pause <bot-id> | bot stop <bot-id> | bot list | bot get <bot-id>
  bot update <bot-id> --file <patch.json> | bot events <bot-id> [--limit 100]
  bot subscribe <bot-id> <topic> [--mode task|message|note] [--from a,b] | bot unsubscribe <bot-id> <topic> | bot subscriptions <bot-id>
  group list | group get <group> [--tenant <id>]   the teams the bots name, with their members
  group events <group> [--after <event-id>] [--limit 100] [--tenant <id>]   the team's timeline
  group topics <group> [--tenant <id>]   who publishes, who listens, nobody listens
  group publish <group> <name> --message "<text>" [--data '{…}'] [--to a,b] [--key <k>] [--tenant <id>]   the operator's event; stop.all stops everyone
  incident open --tenant <id> --bot <bot-id> --group-key "<key>" --title "<text>" [--severity sev1..sev4] [--fingerprints a,b] [--source <name>] [--objective "<text>"]   report an alert group: a new incident and its triage task, a repeat, or a reopening
  incident list [--tenant <id>] [--status Open|Diagnosed|Resolved|Escalated] [--limit 100] | incident get <incident-id> | incident resolve <incident-id> [--reason "<text>"]   the incidents, one incident, close one
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>] [--urgent]
  trigger fire <bot-id> <trigger> --body '<json>' | --file <event.json> [--delivery <id>]
  task run <task-id>                           in process (exit 0 completed, 3 decision required, 1 failed)
  task approve <task-id> | task deny <task-id> | task decide <task-id> approve|deny   [--call <id>]
  task get <task-id> | task executions <task-id> | task events <task-id> [--limit 100] | task cancel <task-id>
  task list [--bot <id>] [--status <status>] [--since <rfc3339>] [--limit 100]
  task report <task-id> [--path] | task reconcile [--grace <duration>]
  execution get <execution-id> | execution artifact <execution-id> <name> [--out <file>]
  overview [--since <rfc3339>]                 nodes, bots and tasks by status; tasks, executions and cost since today
  install [--system] [--prefix <dir>] [--state <dir>] [--node-state <dir>] [--unit-dir <dir>] [--env-file <path>]
          [--axlr-bin <path>] [--node-binary <path>] [--listen <addr>] [--server-names a,b] [--api-listen <addr>]
          [--control <url>] [--node-only] [--destdir <dir>] [--no-systemctl]
  uninstall [--system] [--prefix <dir>] … [--purge]

axlr-node --control unix:///path/to/axlr-bots.sock | https://host:7878
          [--node-id <id>] [--labels k=v,k=v] [--state <dir>] [--axlr-bin <path>] [--heartbeat 15s] [--poll 20s]
          [--token-file <path> [--ca-file <pem>] [--enroll-retry 10s]]     over https: enroll first if not enrolled
          [--dedicated]                                                    only the bots placed on this node by name or selector
          [--bus direct|nats] [--nats-url <url>] [--nats-creds <file>]    read the control plane's pushes from a broker
          [--nats-cert <file> --nats-key <file>] [--nats-ca <file>]        mutual TLS to the broker (default: the node's own certificate)
          [--mcp-config <mcp.json>] [--jev-model jev-1.13.0]               the jev runtime (on when TYPESAFE_API_KEY is set)
          [--axlr-mcp-config <mcp.json>]                                   the mcp.json every AXLR execution loads
axlr-node enroll --control https://host:7878 --token <token> | --token-file <path> [--ca-file <pem>] [--state <dir>] [--host-name <name>]
axlr-node check --control https://host:7878 [--ca-file <pem> | --token-file <path> | --state <dir>] [--timeout 10s]
axlr-node mcp-relay --socket <path>
```

## Defaults and environment

| Setting | Flag | Environment | Default |
|---|---|---|---|
| state directory | `--state` | `AXLR_BOTS_STATE`, then `XDG_STATE_HOME/axlr-bots` | `~/.local/state/axlr-bots` |
| the AXLR binary | `--axlr-bin` | `AXLR_TUI` | `axlr-tui` on `PATH` |
| the daemon's socket | `--socket` (`""` never uses a daemon) | `AXLR_BOTS_SOCKET` | `<state>/axlr-bots.sock` |
| node address, server names, operator address | `serve --listen`, `--server-names`, `--api-listen` | `AXLR_BOTS_LISTEN`, `AXLR_BOTS_SERVER_NAMES`, `AXLR_BOTS_API_LISTEN` | none: the socket only |
| operator token file | `serve --api-token-file` | | `<state>/api.token` |
| the operator API's base URL in approval notifications | `serve --api-url` | `AXLR_BOTS_API_URL` | `https://` and the `--api-listen` address (the host's name for a wildcard address) |
| control plane: bus | `serve --bus direct\|nats`, `--nats-url`, `--nats-creds`, `--nats-cert`, `--nats-key`, `--nats-ca` | `AXLR_BOTS_BUS`, `AXLR_BOTS_NATS_URL`, `AXLR_BOTS_NATS_CREDS`, `AXLR_BOTS_NATS_CERT`, `AXLR_BOTS_NATS_KEY`, `AXLR_BOTS_NATS_CA` | `direct`: the nodes' own streams only; `nats` also publishes every push to the broker ([bus port](../architecture/bus-port.md#configuration)) |
| who acts (audit) | `--actor` | | the user |
| node: control plane, id, labels, state | `--control`, `--node-id`, `--labels`, `--state` | `AXLR_NODE_CONTROL`, `AXLR_NODE_ID`, `AXLR_NODE_LABELS`, `AXLR_NODE_STATE` | the state is `~/.local/state/axlr-node`; over the socket the id is the host name; over `https://` it is the certificate's, and a `--node-id` that disagrees is refused |
| node: the enrollment token (`axlr-node enroll` only) | `--token` | `AXLR_NODE_TOKEN` | none |
| node: token file, pinned CA | `--token-file`, `--ca-file` | `AXLR_NODE_TOKEN_FILE`, `AXLR_NODE_CA_FILE` | none |
| node: dedicated | `--dedicated` | `AXLR_NODE_DEDICATED=true` | off: the node also runs bots placed on any node |
| node: bus | `--bus direct\|nats`, `--nats-url`, `--nats-creds`, `--nats-cert`, `--nats-key`, `--nats-ca` | `AXLR_NODE_BUS`, `AXLR_NODE_NATS_URL`, `AXLR_NODE_NATS_CREDS`, `AXLR_NODE_NATS_CERT`, `AXLR_NODE_NATS_KEY`, `AXLR_NODE_NATS_CA` | `direct`: the control plane's pushes over the node's own stream; `nats`: read from the broker, showing a `tls://` broker the node's own certificate from enrollment unless `--nats-cert` names another ([bus port](../architecture/bus-port.md)) |
| node: AXLR's servers | `--axlr-mcp-config` | `AXLR_NODE_AXLR_MCP_CONFIG` | none: each run loads the operator's `~/.config/axlr/mcp.json`. Name a file of the node's own (an absolute path) so a server the operator added there does not join the bots' runs, nor fail them when it is down |
| node: Jev | `--mcp-config`, `--jev-model` | `AXLR_NODE_MCP_CONFIG`, `AXLR_NODE_JEV_MODEL`; the key `TYPESAFE_API_KEY` | `~/.config/axlr/mcp.json` (the node's AXLR's) and `jev-1.13.0`; the jev runtime is on when `TYPESAFE_API_KEY` is set ([bot definition](bot-definition.md#runtime)) |

`axlr-node --token-file` is a sidecar's first start ([sidecar](../runbooks/sidecar.md)): with no certificate under its state the node enrolls with the token in the file, refusing a token whose node id is not `--node-id` before sending it; with a certificate it keeps it, unless the file holds a token other than the one the certificate came from (`pki/enrollment.json` records its id), which rotates it; a rotation that fails keeps the certificate. Until it has a certificate it retries every `--enroll-retry`, reading the file again each time, and never exits for it. `axlr-node mcp-relay` is not run by hand: AXLR starts it as the stdio MCP server of an execution whose bot may delegate, and it pipes its stdin and stdout to the agent's socket for that execution, `<state>/run/<execution>.sock` ([delegation](../architecture/delegation.md)). `axlr-node check` asks the node address's `/healthz` over TLS 1.3, verifying the control plane against the CA in `--ca-file`, the token's, or the one enrollment stored, and presents no certificate; it changes nothing.

Every command tries the socket first: when the daemon answers, the command is carried by it, so a `task submit` reaches the running service and never touches the state files. When nothing answers, the command works on the state directory: the task is spooled and runs at the daemon's next start, or with `task run` in process.

## Reports

When an execution completes, its answer is written to `<state>/artifacts/<task-id>/report.md` on the control plane, listed in the execution's `result.artifacts` as `report.md`, and named in the `bot.task.completed` event and the daemon's log line.

```bash
axlr-bots task report <task-id>          # the text
axlr-bots task report --path <task-id>   # only where it is
```

## Recovery

`task reconcile` fails the tasks a crash left Running, with a diagnosis; the daemon does the same on start and every `--reconcile-every`. The procedure is the [recovery runbook](../runbooks/recovery.md).

## Logs and audit

Both services log to the journal: `journalctl --user -u axlr-bots -u axlr-node -f`. The control plane appends every event to `<state>/events.jsonl` (`bot events <id>` reads them back per bot) and every operator action to `<state>/audit.jsonl` with the actor.
