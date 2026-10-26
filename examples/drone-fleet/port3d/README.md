# Maasvlakte 2 in 3D

The port the fleet flies over, from OpenStreetMap: a glTF model for the fleet base's 3D view (`/3d`) and a gz sim (Harmonic) world for ArduPilot SITL.

```text
Overpass API ─ fetch_osm.py ─▶ build/osm.json ─ prepare.py (shapely) ─▶ build/features.json
                                                                              │
                                     build_scene.py (Blender, headless) ◀─────┘
                                       ├─▶ ../app/static/port.glb                     the browser: GPU instances
                                       └─▶ gazebo/models/maasvlakte2/meshes/port.glb   gz sim: plain nodes
                       gazebo_world.py ─▶ gazebo/models/maasvlakte2/model.sdf, gazebo/worlds/maasvlakte2.sdf
```

| Step | Makes | Needs |
|---|---|---|
| `fetch_osm.py` | the OSM data of lon 3.965–4.045, lat 51.920–51.965 | Python, the network |
| `prepare.py` | land (the bbox minus the sea and the harbours), the land's top as disjoint patches, buildings with heights, cranes, container stacks, ships, turbines; triangulated | `uv run` (shapely) |
| `build_scene.py` | the scene, `build/port.blend` and the two glb files | Blender 4.2 or later |
| `gazebo_world.py` | the world and the model around the gz glb | Python |

## Build

```bash
cd examples/drone-fleet/port3d
python3 fetch_osm.py --date 2026-10-26T07:50:21Z
uv run prepare.py
blender -b --factory-startup -P build_scene.py -- --features build/features.json \
  --web ../app/static/port.glb --gazebo gazebo/models/maasvlakte2/meshes/port.glb --blend build/port.blend
python3 gazebo_world.py
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

## Gazebo

```bash
GZ_SIM_RESOURCE_PATH=gazebo/models gz sim gazebo/worlds/maasvlakte2.sdf
```

The world sets `spherical_coordinates` to the same origin (ENU), so a NavSat sensor reads the port's real latitude and longitude. gz sim loads glTF Y-up as it is, so the model turns the mesh +90° about X. Checked with gz sim 8.11 (`osrf/ros:jazzy-desktop-full`), server only: a ball dropped at the origin rests on the quay at 5 m, one dropped on the Prinses Amaliahaven at 0, with bullet-featherstone and dartsim. The mesh is the visual and the collision; the rendering has not been looked at in the gz GUI.

The data is © OpenStreetMap contributors, under the [ODbL](https://www.openstreetmap.org/copyright); the 3D view says so on screen.
