# Maasvlakte 2 in 3D

The port the fleet flies over, from OpenStreetMap: a glTF model for the fleet base's 3D view (`/3d`) and a gz sim (Harmonic) world for ArduPilot SITL.

```text
Overpass API ─ fetch_osm.py ─▶ build/osm.json ─ prepare.py (shapely) ─▶ build/features.json
                                                                              │
                                     build_scene.py (Blender, headless) ◀─────┘
                                       ├─▶ ../app/static/port.glb                     the browser: GPU instances
                                       └─▶ gazebo/models/maasvlakte2/meshes/port.glb   gz sim: plain nodes
                       gazebo_world.py ─▶ gazebo/models/maasvlakte2/model.sdf, gazebo/worlds/maasvlakte2.sdf
                             routes.py ─▶ ../app/routes.json                            the rovers' routes
```

| Step | Makes | Needs |
|---|---|---|
| `fetch_osm.py` | the OSM data of lon 3.965–4.045, lat 51.920–51.965 | Python, the network |
| `prepare.py` | land (the bbox minus the sea and the harbours), the land's top as disjoint patches, buildings with heights, cranes, container stacks, ships, turbines; triangulated | `uv run` (shapely) |
| `build_scene.py` | the scene, `build/port.blend` and the two glb files | Blender 4.2 or later |
| `gazebo_world.py` | the world and the model around the gz glb | Python |
| `routes.py` | the rovers' routes: patrols from each rover station, supply runs from the depot | `uv run` (shapely, PyYAML) |

## Build

```bash
cd examples/drone-fleet/port3d
python3 fetch_osm.py --date 2026-10-26T07:50:21Z
uv run prepare.py
blender -b --factory-startup -P build_scene.py -- --features build/features.json \
  --web ../app/static/port.glb --gazebo gazebo/models/maasvlakte2/meshes/port.glb --blend build/port.blend
python3 gazebo_world.py
uv run routes.py                         # reads ../../helm/drone-fleet/values.yaml: run it again when a station moves
```

`--date` asks Overpass for the data as it was then: the committed models were built from 2026-10-26T07:50:21Z, and `prepare.py` and `build_scene.py` draw the stacks and the ships from a fixed seed, so the same date gives the same scene. Leave it out for today's map. On the lab, Blender runs on wrx80 (`ssh wrx80`; copy the directory there and the two glb files back).

## The frame

Metres east, north and up of 51.9440 N, 3.9990 E: Blender's X, Y, Z; glTF's x, y, −z. The sea is at 0, the quays and yards at 5 m (`area.GROUND_M`). The fleet's `alt_m` is above the vehicle's home, so the 3D view draws a vehicle at 5 m plus `alt_m`.

## What is real and what is made up

| From OSM | Made up |
|---|---|
| coastline, harbours, the Slufter, land use, roads, rail | heights: almost no building has one, so a default by kind and size |
| building footprints, storage tanks, piers, breakwaters | the container stacks: inside each stacking crane's block (OSM maps the blocks), heights at random |
| quay cranes (points, turned to face the nearest water), stacking blocks, crane rails | ships at the berths with three or more quay cranes, a bulk carrier at the coal terminal |
| wind turbines (Vestas V162), masts, chimneys | coal and ore piles beside the stacker-reclaimers' rails |

## The rovers' routes

A rover's bot does not plan: it picks one of the routes `routes.py` precomputed, and the vehicle pilot drives it.

```text
features.json ─▶ the lattice: every 5 m of land, minus water and what is in the way grown by 1.5 m
                 a metre costs 1 on a road, 1.3 on a yard, 1.6 on works, 3 on grass or sand
              ─▶ A* in 8 directions; leaving a stop backwards costs 12 m a radian of turn
              ─▶ pulled taut where clear (turns ≤ 100°), corners rounded with 6 m arcs
              ─▶ checked: inside the geofence, nothing inside an obstacle
```

| Route | From | Is |
|---|---|---|
| `<station>:<zone>` | each rover's station | out to the zone, a lap through six points spread over it, back |
| `supply:<a>><b>…` | the depot | through the stations in that order and back, a balloon loop at each to turn round |

Only the zones within a rover's geofence (1500 m) get a route: from Euromax the Euromax yard and the Distripark, from RWG the RWG yard. The supply runs cover every ordered choice of stations.

## Gazebo

```bash
GZ_SIM_RESOURCE_PATH=gazebo/models gz sim gazebo/worlds/maasvlakte2.sdf
```

The world sets `spherical_coordinates` to the same origin (ENU), so a NavSat sensor reads the port's real latitude and longitude. gz sim loads glTF Y-up as it is, so the model turns the mesh +90° about X. Checked with gz sim 8.11 (`osrf/ros:jazzy-desktop-full`), server only: a ball dropped at the origin rests on the quay at 5 m, one dropped on the Prinses Amaliahaven at 0, with bullet-featherstone and dartsim. The mesh is the visual and the collision; the rendering has not been looked at in the gz GUI.

### A rover that avoids what the map does not know

The routes keep clear of what OpenStreetMap knows. For the rest (a truck, a person, a fallen container) the rover has ArduPilot's own avoidance: its forward rangefinders feed proximity, simple avoidance slows it and stops it short, and BendyRuler steers round. In gz the rangefinders see the port and anything dropped in it.

```text
gz sim (port, rover, box) ── ArduPilotPlugin (JSON, lockstep) ── ArduRover 4.7.2 SITL ── tcp 5760 ── oa_test.py
   gpu_lidar x4 ─▶ rng_1..rng_4 ─▶ RNGFND1..4 (TYPE 100) ─▶ PRX1_TYPE 4 ─▶ AVOID (slow, stop) + OA BendyRuler (steer round)
```

| File | Is |
|---|---|
| `gazebo/Dockerfile` | gz sim 8.11 (`osrf/ros:jazzy-desktop-full`), ArduPilot's gz plugin (`ardupilot_gazebo`, pinned, one patch), the ArduRover 4.7.2 SITL the fleet runs, pymavlink. Built on wrx80 and kept there |
| `gazebo/models/port_rover` | a car-sized rover: 3.6 × 1.8 m, Ackermann steering ±30° on SERVO1, rear wheels on SERVO3 (10 m/s at full throttle), an IMU; the Intel RealSense D455 as three lidar columns of 29° (0.3 to 16 m) and the aft lidar, rangefinders 1 to 4 in the chart's order; a chase camera |
| `gazebo/rover-gz.parm` | what the model is: its cruise throttle and steering |
| `gazebo/rover-sim` | starts gz (server, headless rendering), puts the rover on the RWG pad, starts ArduRover with SITL's defaults, the chart's `files/rover.parm` and `rover-gz.parm` |
| `gazebo/oa_test.py` | drives a route as the vehicle pilot does (its own `Vehicle` class; home, speed, the route's points, RTL), drops a box on it that no map knows, records the run |
| `gazebo/plot_run.py` | draws a run: the route, the track, the box, where BendyRuler steered, the speed |

On wrx80 (`ssh wrx80`, the repository copied there):

```bash
cd examples/drone-fleet/port3d
podman build -t localhost/port-gz-rover:4.7.2 -f gazebo/Dockerfile gazebo
mkdir -p /tmp/rover-oa
podman run -d --name rover-oa -w /out -v /tmp/rover-oa:/out \
  -v $PWD/gazebo:/gazebo:ro -v $PWD/../app:/app:ro -v $PWD/../../helm/drone-fleet/files:/chart:ro \
  localhost/port-gz-rover:4.7.2 /gazebo/rover-sim
podman exec rover-oa bash -c '. /opt/ros/jazzy/setup.sh; python3 /gazebo/oa_test.py --route rwg:rwg-yard --box 120'
podman exec rover-oa python3 /gazebo/plot_run.py /out
podman rm -f rover-oa
```

`/tmp/rover-oa` then holds `track.csv`, `events.txt`, `run.json`, `run.png`, SITL's dataflash log (`logs/*.BIN`, with `OABR` for BendyRuler) and the cameras' pictures (`chase/`, `top/`). Nothing is published on the host: gz, the SITL and the test talk inside the container. It runs at real time on wrx80's CPUs (llvmpipe; no GPU).

What it takes, found the hard way:

| Trap | So |
|---|---|
| ArduRover 4.7.2's JSON backend tests the wrong bits for `rng_1`..`rng_6` (the keys moved by three; master is fixed): `rng_1` never reaches rangefinder 1 and it fails pre-arm with *No Data* | `ardupilot_gazebo-attitude.patch`: the plugin sends the Euler angles beside the quaternion, which sets the bit 4.7.2 tests; it keeps using the quaternion |
| The plugin steers by the steering joints' positions, which gz keeps only if a system asks for them | the model loads `JointStatePublisher` for those joints; without it the wheels never turn |
| JSON SITL puts the vehicle at `--home` plus its gz position | `--home` is the world's origin, 51.944 N 3.999 E, not the pad |
| ArduRover arms and starts AUTO before its EKF uses GPS, and then drives on dead reckoning | the test waits for *EKF3 IMU0 is using GPS* |
| gz renders a camera only while something subscribes to it | a `gz topic -e` on each camera's image, thrown away |

What a run round a box showed, and the parameters with their reasons, are in the [runbook](../../../docs/runbooks/drone-fleet.md#8-obstacles-no-map-knows).

The data is © OpenStreetMap contributors, under the [ODbL](https://www.openstreetmap.org/copyright); the 3D view says so on screen.
