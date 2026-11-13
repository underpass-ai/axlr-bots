# Runbook: the control plane in a container

Run `axlr-bots serve` from the release image with Docker Compose instead of the systemd user service `axlr-bots install` writes, roll back to the service, and update by changing the image tag. The file is [`distribution/compose.yaml`](../../distribution/compose.yaml); the image is built from [`distribution/control-plane.Dockerfile`](../../distribution/control-plane.Dockerfile). The node agent on the host stays as it is: it reaches the control plane over the same socket or the same TLS address.

## What the image is

| | |
|---|---|
| Image | `ghcr.io/underpass-ai/axlr-bots:<version>`, linux/amd64 and linux/arm64, published by the release workflow on a tag `v<version>` |
| Version | the tag without its `v` (`0.4.0`), stamped into the binary: `axlr-bots version`, `GET /v1/build` (`running.version`) and the panel's reference for nodes and bots |
| Base | distroless `static`, no shell, user 65532 unless the compose file says otherwise |
| Inside | `axlr-bots`, and AXLR's `axlr-tui` (the pin of the node image) for the runs the control plane makes in process when no node can take them |
| Health | `axlr-bots health`: exit 0 when the daemon answers `/healthz` on its socket |

The daemon's stale-binary stop (it exits when a newer `axlr-bots` is installed over the running one, so the service manager starts the new build) never fires in a container: the image is immutable, the running binary is always the installed one, and `GET /v1/build` answers `"stale": false`. An update is a new image and a new container.

## What changes against the service

| Systemd service | Compose |
|---|---|
| `ExecStart=… serve --listen 0.0.0.0:7878 --api-listen 0.0.0.0:7879` | `command:`; the addresses are `AXLR_BOTS_LISTEN` and `AXLR_BOTS_API_LISTEN` (same defaults) |
| `EnvironmentFile=-~/.config/axlr-bots/env` | `env_file:` with the same file; `AXLR_TUI` and `HOME` are overridden, they named host paths |
| the host's network | `network_mode: host`: a broker on `127.0.0.1:4222` and the listeners behave as before; `ports:` does not apply |
| runs as the host account | `user: ${AXLR_BOTS_UID}:${AXLR_BOTS_GID}`, the owner of the state, so the socket, `api.token` and the CA stay usable on the host |
| state in `~/.local/state/axlr-bots` | the same directory, bind-mounted at the same path; `~/.config/axlr-bots` read-only at its path (the env file names certificates there) |
| `Restart=on-failure`, `WantedBy=default.target` | `restart: unless-stopped`: back after a crash and after a reboot, with or without a user session |
| `TimeoutStopSec=30s` | `stop_grace_period: 30s` |

**What the container cannot do.** A notification target or an output of type `command` runs its program inside the container: a distroless image has no shell and none of the host's scripts. Find them before you switch:

```bash
grep -l '"command"' ~/.local/state/axlr-bots/bots/*.json ~/.local/state/axlr-bots/outputs.json
```

Each one found keeps working only on the service: move it to a webhook target ([approvals](approvals.md)) or stay on the service. Likewise an in-process run sees only the image's `axlr-tui` and keeps AXLR's own state on a tmpfs: skills installed for the host account are not there. Runs belong on the nodes; the in-process path is the fallback when none is online.

The variables in the env file reach the container's environment: anyone who may run `docker inspect` on the host reads them, as anyone who may read the env file does. `docker compose config` prints them too.

## Before you start

1. Docker with Compose v2 (`docker compose version`).
2. The image can be pulled: `docker pull ghcr.io/underpass-ai/axlr-bots:<version>`. If the package is private, `docker login ghcr.io` first.
3. The state path, with `/axlr-bots.sock`, is at most 107 bytes (a Unix socket's limit, the same as for the service).
4. The host's `axlr-bots` CLI and `axlr-node` are the same release as the image, or the CLI is used through the container: `docker compose exec control-plane axlr-bots <command>`.

The compose file reads these variables; keep them in a `.env` beside a copy of the file, so every `docker compose` command sees the same ones:

```bash
mkdir -p ~/.config/axlr-bots/compose
version=0.4.0
curl -fsSL "https://raw.githubusercontent.com/underpass-ai/axlr-bots/v$version/distribution/compose.yaml" \
  -o ~/.config/axlr-bots/compose/compose.yaml
cat > ~/.config/axlr-bots/compose/.env <<EOF
AXLR_BOTS_VERSION=$version
AXLR_BOTS_UID=$(id -u)
AXLR_BOTS_GID=$(id -g)
EOF
cd ~/.config/axlr-bots/compose
```

| Variable | Default | |
|---|---|---|
| `AXLR_BOTS_VERSION` | none, required | the image tag, without `v` |
| `AXLR_BOTS_UID`, `AXLR_BOTS_GID` | none, required | `id -u`, `id -g` of the state's owner |
| `AXLR_BOTS_STATE_DIR` | `~/.local/state/axlr-bots` | the state, mounted at the same path |
| `AXLR_BOTS_CONFIG` | `~/.config/axlr-bots` | the directory of the env file, read-only |
| `AXLR_BOTS_LISTEN` | `0.0.0.0:7878` | the node protocol, mutual TLS |
| `AXLR_BOTS_API_LISTEN` | `0.0.0.0:7879` | the operator API, bearer token |

A host that never ran the control plane runs `docker compose run --rm control-plane ca init` once before `up`: `--listen` needs the fleet CA.

## From the service to compose

The state lock refuses a second `serve` on the same state, so the service stops before the container starts. Mask it, not only disable it: the node unit `install` writes (and any unit written after it, such as an input adapter's) has `Wants=axlr-bots.service`, and would start the service again at its next start, a reboot included. A masked unit is never started; the units that want it still start, and stopping it stops none of them.

```bash
docker compose pull                                   # before the stop: the gap is the start alone
systemctl --user disable --now axlr-bots.service
systemctl --user mask axlr-bots.service
docker compose up -d
```

Check that nothing pulls it back: `systemctl --user is-enabled axlr-bots.service` answers `masked`.

Check:

- `docker compose ps` shows the service `healthy` (within the 20 s start period).
- `docker compose logs control-plane | head` names the socket, both addresses and `daemon: build <commit> (<time>)`.
- `docker compose exec control-plane axlr-bots version` prints `axlr-bots <version> (…)`.
- `axlr-bots node list` (or through `exec`) shows the host's nodes `Online` again; a node reconnects on its own.
- The operator API: `curl -sk https://127.0.0.1:7879/healthz` answers `{"status":"ok"}`, and `GET /v1/build` with the token answers `running.version` = the image's version.

Leave the unit file and `~/.local/bin/axlr-bots` in place: they are the rollback. Do not run `axlr-bots uninstall`.

## Back to the service

```bash
docker compose down
systemctl --user unmask axlr-bots.service
systemctl --user enable --now axlr-bots.service
```

Check: `systemctl --user status axlr-bots` is `active (running)` and `journalctl --user -u axlr-bots -n 3` ends with `daemon: serving on …`. The files the container wrote belong to the host account, so the service reads them as its own.

Roll back to a binary of the same release as the image, or newer: an older build may not read what a newer one wrote to the state. The release's `axlr-bots-v<version>-linux-<arch>.tar.gz` has both binaries.

## Update

Change the tag, pull, recreate:

```bash
sed -i 's/^AXLR_BOTS_VERSION=.*/AXLR_BOTS_VERSION=0.4.1/' .env
docker compose pull
docker compose up -d                                  # recreates the container on the new image
```

Check as above: `healthy`, and `axlr-bots version` names the new release. Compare the new release's `distribution/compose.yaml` with your copy and take its changes; read its notes for anything the state needs.

To go back one release, set the previous tag and `up -d` again; the same caution as above applies to the state.

## When it does not work

| Symptom | Cause |
|---|---|
| `required variable AXLR_BOTS_VERSION is missing a value` | no `.env` beside the file, or the command ran in another directory |
| `the state … is in use by another process` and the container restarts | the service runs, or a unit with `Wants=axlr-bots.service` started it again: `systemctl --user disable --now axlr-bots.service` and `mask` it |
| `bind: invalid argument` on the socket | the state path is too long for a Unix socket |
| `permission denied` on the state | `AXLR_BOTS_UID`/`AXLR_BOTS_GID` are not the state's owner |
| `serve --listen/--api-listen: …ca…` | no fleet CA in the state: `docker compose run --rm control-plane ca init` |
| the bus does not connect | the env file names certificates outside `AXLR_BOTS_CONFIG`: they are not mounted; mount their directory read-only at its path |
| an approval or an output never arrives | a `command` target: see what the container cannot do |
