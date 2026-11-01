# Runbook: install

Install the control plane and the node agent on one host, user-level, under `systemctl --user`. The [command reference](../reference/cli.md) lists every flag of `install`; [add a host](add-a-host.md) joins further hosts.

## Before you start

1. Go 1.26 on the host.
2. An `axlr-tui` with `--exec`: [AXLR](https://github.com/underpass-ai/AXLR) v0.14.0 or later, from its [release archives](https://github.com/underpass-ai/AXLR/releases) or built in a checkout (`GOWORK=off go -C tui build -o axlr-tui ./cmd/axlr-tui`). Releases before 0.14.0 have no `--exec`. v0.14.0 also takes `--policy`, `--correlation` and `--budget`; the node reads `axlr-tui --help` once and passes what the build takes ([runtime contract](../architecture/runtime-contract.md)).
3. A model key for the bots' model, `OPENROUTER_API_KEY`, in the shell that runs the installer. It is copied into the environment file (mode 0600) and never printed.
4. A systemd user session (`systemctl --user status` answers).

## One host over the socket

```bash
go build -o bin/ ./cmd/...
OPENROUTER_API_KEY=... bin/axlr-bots install --axlr-bin /path/to/axlr-tui
systemctl --user enable --now axlr-bots.service axlr-node.service
```

Check: `systemctl --user status axlr-bots axlr-node` shows both `active (running)`; `axlr-bots node list` shows this host `Online`; `journalctl --user -u axlr-bots -n 3` ends with `daemon: serving on …/axlr-bots.sock …, tick 2s`.

Without `--listen` the control plane serves its Unix socket only and the node unit talks over it (`AXLR_NODE_CONTROL=unix://…`): no CA, no enrollment, the host name as the node id. Only the TLS install below lets a second host join.

## One control plane, nodes over TLS

```bash
go build -o bin/ ./cmd/...
export OPENROUTER_API_KEY=...
bin/axlr-bots install --axlr-bin /path/to/axlr-tui \
    --listen 0.0.0.0:7878 --server-names cp.example.internal --control https://cp.example.internal:7878 \
    --api-listen 127.0.0.1:7879                              # the operator API over TLS (optional)
axlr-bots ca init                                            # the fleet CA, once per fleet
systemctl --user enable --now axlr-bots.service
token=$(axlr-bots node enroll-token --node-id "$(hostname -s | tr 'A-Z' 'a-z')" | sed -n 's/.*"token": "\([^"]*\)".*/\1/p')
axlr-node enroll --control https://cp.example.internal:7878 --token "$token" --state ~/.local/state/axlr-node
systemctl --user enable --now axlr-node.service
```

Check: `axlr-bots node list` shows this host `Online` with `certificateSerial` set; the node's first journal line names its client certificate and the serial; `axlr-bots ca show` prints the CA's fingerprint. Without `ca init` the daemon refuses to listen and the unit restarts until it exists.

`--listen 127.0.0.1:7878 --control https://127.0.0.1:7878` keeps the TLS path on one host; the node then authenticates with a certificate as a node elsewhere would. `--listen` means TLS: there is no plaintext node address.

## What the installer writes

| File | What |
|---|---|
| `~/.local/bin/axlr-bots`, `~/.local/bin/axlr-node` | the binaries (`install` copies the ones that run) |
| `~/.config/systemd/user/axlr-bots.service` | `Type=simple`, `ExecStart=… serve [--listen <addr>] [--server-names …] [--api-listen <addr>]`, `Restart=on-failure`, `NoNewPrivileges=yes`, `EnvironmentFile=-~/.config/axlr-bots/env`, `WantedBy=default.target`, `Documentation=` this repository |
| `~/.config/systemd/user/axlr-node.service` | `After=`/`Wants=axlr-bots.service` (or `network-online.target` with `--node-only`), `Environment=AXLR_NODE_CONTROL=<--control, or unix://…/axlr-bots.sock>`, `ExecStart=axlr-node --state ~/.local/state/axlr-node`, the host name as the `host=` label |
| `~/.config/axlr-bots/env` | `OPENROUTER_API_KEY=` (or a commented placeholder when the installer had none) and `AXLR_TUI=`; an existing file is never overwritten |
| `~/.local/state/axlr-bots/` | the control plane's state ([layout](../architecture/layout.md#the-state-directory)) |
| `~/.local/state/axlr-node/` | the node's state: traces, policy files, workspaces, and `pki/` once enrolled |

A user service cannot sandbox the filesystem (`ProtectSystem=` and `ProtectHome=` need privileges a user manager lacks); the unit says so. `--destdir <dir>` renders everything under a directory (packaging, tests); `--no-systemctl` writes the files and runs nothing.

## Change the model key or the AXLR build

1. Edit `~/.config/axlr-bots/env`: the `OPENROUTER_API_KEY=` or `AXLR_TUI=` line.
2. `systemctl --user restart axlr-bots.service axlr-node.service`.

Check: the node's registration line in the journal (`node: registered as … policy true correlation true`) reflects the new build's flags.

## Jev bots and a broker on a host

Both units read the environment file (`EnvironmentFile=-~/.config/axlr-bots/env`, or `/etc/axlr-bots/env` with `--system`), so what the installer has no flag for goes there; then `systemctl --user restart axlr-bots.service axlr-node.service`.

| Line in the env file | Effect |
|---|---|
| `TYPESAFE_API_KEY=…` | the node offers the `jev` runtime and is assigned Jev bots ([bot definition](../reference/bot-definition.md#runtime)); `AXLR_NODE_MCP_CONFIG=` names another `mcp.json` than `~/.config/axlr/mcp.json`, `AXLR_NODE_JEV_MODEL=` another pinned version than `jev-1.13.0` |
| `AXLR_BOTS_BUS=nats`, `AXLR_BOTS_NATS_URL=tls://…:4222`, `AXLR_BOTS_NATS_CERT=`, `AXLR_BOTS_NATS_KEY=`, `AXLR_BOTS_NATS_CA=` | the control plane also publishes its pushes to a broker ([deploy a broker](../architecture/bus-port.md#deploy-a-broker)) |
| `AXLR_NODE_BUS=nats`, `AXLR_NODE_NATS_URL=tls://…:4222`, `AXLR_NODE_NATS_CA=` | the node reads its pushes from the broker; over `https://` it shows the broker its own certificate from enrollment |

Check: the node's journal says `node: jev runtime on (jev-1.13.0)`, or `node: TYPESAFE_API_KEY is not set: Jev bots are not run here`; `axlr-bots node get <id>` lists `jev` under the capabilities' `runtimes`.

## Keep it running after logout

```bash
loginctl enable-linger "$USER"
```

Check: `loginctl show-user "$USER" -p Linger` answers `Linger=yes`.

## Uninstall

```bash
axlr-bots uninstall           # disable --now both units, remove them and the binaries
axlr-bots uninstall --purge   # also both state directories and the env file (the key)
```

Check: `systemctl --user status axlr-bots axlr-node` reports both units as not found.

## The system-wide variant

`install --system` (as root) puts the binaries under `/usr/local/bin`, the units under `/etc/systemd/system`, the env file at `/etc/axlr-bots/env` and the state under `/var/lib/axlr-bots` and `/var/lib/axlr-node`, all run by a dedicated user `axlr-bots` (`useradd --system`, in `systemd-journal`) with `ProtectSystem=strict`, `ProtectHome=yes`, `StateDirectory=` and `HOME=` the state directory so `axlr-tui` writes its own files there. `uninstall --system` reverses it. With `--listen`, run `ca init` (and `node enroll-token`) as the service user, or `chown -R axlr-bots: /var/lib/axlr-bots/ca` afterwards: the key is mode 0600 and the daemon must read it. The system-wide variant has not been exercised with TLS yet; the user-level one has.
