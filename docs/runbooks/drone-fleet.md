# Runbook: a port's fleet of autonomous vehicles

A fleet of simulated autonomous vehicles at the Port of Rotterdam (Maasvlakte 2), operated by AXLR Bots: quadcopters, ground rovers, a supply rover and a fixed-wing plane, each an ArduPilot 4.7.2 SITL with the node agent as its sidecar, maintenance stations that run out of battery packs and parts, and a live map. Every launch waits for a person's approval in the panel. The chart is [`examples/helm/drone-fleet`](../../examples/helm/drone-fleet), the services and the bots are in [`examples/drone-fleet`](../../examples/drone-fleet).

```text
 operator (panel) ──approve──┐                               flota.<domain> (live map)
                              ▼                                       ▲
 webhook / timer ──▶ fleet-cp (axlr-bots, in the namespace) ◀──mTLS── fleet-base-0: fleet_base.py + axlr-node
                      │  dispatcher, logistics: delegate          │   stations' stock, bays, depot, RTK base
                      ▼                                           │   bots: dispatcher, logistics (observe)
   quad-0..2, rover-0..1, supply-0, plane-0 (one pod per vehicle) ─┘
   ├─ sitl      ArduCopter X / ArduRover / ArduPlane, RTK GPS, lidars, optical flow, camera
   ├─ router    mavlink-router: 5770 for the pilot and anyone else
   ├─ pilot     vehicle_pilot.py, the companion computer: telemetry.json, the limits, MCP run_mission on 127.0.0.1:8765
   └─ axlr-node the sidecar: the bot pilot-<pod> (act-with-approval) and AXLR, whose mcp.json names the pilot's tool
```

## What each piece does

| Piece | Does |
|---|---|
| `dispatcher` bot | reads `fleet.json`, picks the ready vehicles for the zones a mission needs, and delegates one objective to each pilot (`wait: false`) |
| `logistics` bot | reads the stations' stock and sends the supply rover on a delivery run |
| `pilot-<vehicle>` bots | read `telemetry.json`, plan the route in metres from home, call `run_mission` once: that call waits for a person |
| `vehicle_pilot.py` | refuses a route outside the geofence, the altitude band or the speed range, and a vehicle that is not ready; uploads the mission over MAVLink, launches it, and after landing queues the vehicle at its station: inspection, then a fresh battery pack |
| `fleet_base.py` | polls every vehicle, hands out the bays (a service takes a pack and a part from the station), moves the supply rover's cargo when it reaches a station, writes `fleet.json` for the two bots and serves the map |

The safety limits live in the companion computer, not in the model: a refused route is the tool's error, which the model reads, and nothing moved.

## Before you start

- A cluster with a StorageClass (`local-path` here), an ingress controller and cert-manager for the map, `helm` and `kubectl`.
- The images in a registry the cluster pulls from. On wrx80 they were built with podman and pushed to the lab registry:

```bash
podman build -t $R/fleet-app:0.1.0 examples/drone-fleet                                         # vehicle_pilot.py, fleet_base.py, the map
podman build -t $R/fleet-sitl:4.7.2 examples/drone-fleet/sitl                                    # SITL (checked sha256) + mavlink-router
podman build -f examples/drone-fleet/control-plane.Dockerfile -t $R/control-plane:0.1.0-fleet .  # axlr-bots
```

- The node image of [the sidecar runbook](sidecar.md) and a model provider key.

## 1. Install

```bash
kubectl create namespace port-fleet
grep '^OPENROUTER_API_KEY=' ~/.config/axlr-bots/env | cut -d= -f2- | tr -d '\n' \
  | kubectl -n port-fleet create secret generic fleet-model --from-file=OPENROUTER_API_KEY=/dev/stdin
helm dependency update examples/helm/drone-fleet
helm install fleet examples/helm/drone-fleet -n port-fleet \
  --set base.ingress.enabled=true --set base.ingress.host=flota.example.com --set base.ingress.clusterIssuer=letsencrypt
```

The release must be called `fleet`: the nodes dial `https://fleet-cp:7878` (`axlr-bots-node.controlPlane.url`). The control plane's first start runs `ca init`; the sidecars wait for their tokens.

## 2. Enroll the eight nodes

One token per pod, into one Secret, without printing them:

```bash
python3 - <<'EOF'
import base64, json, subprocess
nodes = ["fleet-base-0", "quad-0", "quad-1", "quad-2", "rover-0", "rover-1", "supply-0", "plane-0"]
cli = ["kubectl", "-n", "port-fleet", "exec", "fleet-cp-0", "-c", "control-plane", "--", "/usr/local/bin/axlr-bots", "--state", "/var/lib/axlr-bots"]
data = {n: base64.b64encode(json.loads(subprocess.run(cli + ["node", "enroll-token", "--node-id", n, "--ttl", "2h"], capture_output=True, text=True, check=True).stdout)["token"].encode()).decode() for n in nodes}
secret = {"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "fleet-enrollment", "namespace": "port-fleet"}, "data": data}
subprocess.run(["kubectl", "apply", "-f", "-"], input=json.dumps(secret), text=True, check=True)
EOF
```

Check: `node list` (through the control plane pod, below) shows the eight `Online` within a minute or two, as kubelet refreshes the Secret's volume.

## 3. Register the bots

```bash
reg() { kubectl -n port-fleet exec -i fleet-cp-0 -c control-plane -- /busybox/sh -c \
  "cat > /tmp/bot.json && axlr-bots --state /var/lib/axlr-bots bot register --file /tmp/bot.json && axlr-bots --state /var/lib/axlr-bots bot enable $1"; }
cd examples/drone-fleet/bots
reg dispatcher < dispatcher.json
reg logistics < logistics.json
for v in quad-0 quad-1 quad-2 rover-0 rover-1 plane-0; do sed "s/VEHICLE/$v/g" pilot.json | reg pilot-$v; done
sed "s/VEHICLE/supply-0/g" supply-pilot.json | reg pilot-supply-0
```

The pilots are `act-with-approval` with `local_read` (only `telemetry.json`) and `run_mission`; the dispatcher and logistics are `observe`, with the vehicles' pilots as their delegation targets.

## 4. The panel

The operator API is the control plane Service's port 7879. From a workstation, through the host that runs the cluster:

```bash
ssh -L 127.0.0.1:17879:127.0.0.1:17879 wrx80 kubectl -n port-fleet port-forward svc/fleet-cp 17879:7879
```

Copy the CA (`/var/lib/axlr-bots/ca/ca.crt`) and the token (`/var/lib/axlr-bots/api.token`, mode 0600) from the pod, and point a panel profile at `https://127.0.0.1:17879` (the control plane's certificate names `127.0.0.1` for this): `XDG_CONFIG_HOME=~/.config/axlr-fleet-panel axlr-bots-panel`.

## 5. A mission

```bash
kubectl -n port-fleet exec fleet-cp-0 -c control-plane -- axlr-bots --state /var/lib/axlr-bots \
  task submit dispatcher --key storm-check-1 \
  --objective "Storm passed: check the quay cranes of both terminals, the Euromax yard lanes and the sea defence."
```

The dispatcher delegates; each chosen pilot plans and stops in `WaitingApproval` with its `run_mission` call (the route, the speed, the photo spacing) on the panel's task screen. Approve: the vehicle takes off on the map; deny: it stays on its pad and the pilot says so. After landing the vehicle waits at its station for a bay, is inspected and gets a fresh pack; a station without packs or parts keeps it waiting, which is the `logistics` bot's cue (`task submit logistics …`).

## Limits

- The vehicles are SITL: the sensors are SITL's backends (rangefinders, optical flow, camera trigger), RTK is SITL's fix 6 with no correction link, and the map's hardware labels describe what the vehicles stand for.
- Each sidecar's AXLR reads its MCP configuration from its own state volume (`~/.config/axlr/mcp.json`, written by an init container): the pilot's tool is per node, not per bot. An `act-with-approval` bot asks for every MCP call.
- The bays and the stock live in the base's memory: a restart of `fleet-base-0` starts them over.
