#!/usr/bin/env python3
"""Put the AIS ships and the incident's containers into the gz world.

    ais-bridge.py --fleet http://fleet-base:8080/api/fleet [--no-ships] [--no-containers] [--idle]

Once a second it reads the fleet base's /api/fleet and makes the world
match it: a model for each ship AIS hears inside the port model's bounds
(port3d/area.py's BBOX; outside them a ship would float over nothing) and
for each container of a running search. New ones are created
(/world/maasvlakte2/create, the SDF inline), all are moved in one call
(/set_pose_vector), the ones gone are removed (/remove). A ship moves on
from its last report at its speed over ground along its course, for at most
60 s, and points at its heading (its course without one). Ships are boxes
at their AIS length and beam, coloured by type as in /3d, with a
superstructure; their models are named ship-<MMSI>, never by ship name.
Containers are 2.44 x 2.59 m boxes of their length, low in the water.

With --no-ships or --no-containers it makes none of those and removes any it
made; with --idle it does nothing at all.
"""

import argparse
import calendar
import hashlib
import json
import math
import subprocess
import sys
import time
import urllib.request

WORLD = "maasvlakte2"
# port3d/area.py
ORIGIN = (51.9440, 3.9990)
BBOX = (51.920, 3.965, 51.965, 4.045)
EARTH_RADIUS_M = 6378137.0
KNOT_M_S = 0.5144
MAX_EXTRAPOLATE_S = 60

# 3d.html's SHIP_COLOUR; the superstructure aft, amidships or forward as
# (centre, length, beam, height) in shares of the hull's length, beam, height.
SHIP_COLOUR = {"cargo": "#2f8f5b", "tanker": "#a63a32", "passenger": "#3a6fb0", "tug": "#c27a3a",
               "pilot": "#d1a530", "fishing": "#7d5fb2"}
OTHER_COLOUR = "#56606b"
HOUSE = {"aft": (-0.35, 0.12, 0.8, 2.0), "mid": (0.0, 0.6, 0.9, 1.5), "fore": (0.15, 0.3, 0.8, 1.5)}
HOUSE_OF = {"cargo": "aft", "tanker": "aft", "passenger": "mid", "tug": "fore", "pilot": "fore", "fishing": "fore"}
BOX_COLOURS = ["#b8473a", "#2f6f9f", "#c9a23a", "#4f8f5f", "#8a8f96", "#e2e6ea"]

SHIP = """<sdf version="1.9"><model name="{name}"><static>true</static>
  <link name="hull">
    <visual name="hull"><geometry><box><size>{L:.1f} {B:.1f} {H:.1f}</size></box></geometry>
      <material><ambient>{rgb} 1</ambient><diffuse>{rgb} 1</diffuse></material></visual>
    <collision name="hull"><geometry><box><size>{L:.1f} {B:.1f} {H:.1f}</size></box></geometry></collision>
    <visual name="house"><pose>{HX:.1f} 0 {HZ:.1f} 0 0 0</pose><geometry><box><size>{HL:.1f} {HB:.1f} {HH:.1f}</size></box></geometry>
      <material><ambient>0.9 0.9 0.88 1</ambient><diffuse>0.9 0.9 0.88 1</diffuse></material></visual>
  </link></model></sdf>"""

BOX = """<sdf version="1.9"><model name="{name}"><static>true</static>
  <link name="box">
    <visual name="box"><geometry><box><size>{L:.2f} 2.44 2.59</size></box></geometry>
      <material><ambient>{rgb} 1</ambient><diffuse>{rgb} 1</diffuse></material></visual>
    <collision name="box"><geometry><box><size>{L:.2f} 2.44 2.59</size></box></geometry></collision>
  </link></model></sdf>"""


def to_local(lat, lon):
    """(east, north) in metres of (lat, lon) from ORIGIN, as area.to_local."""
    lat0, lon0 = ORIGIN
    north = math.radians(lat - lat0) * EARTH_RADIUS_M
    east = math.radians(lon - lon0) * EARTH_RADIUS_M * math.cos(math.radians(lat0))
    return east, north


def inside(lat, lon):
    s, w, n, e = BBOX
    return s <= lat <= n and w <= lon <= e


def rgb(hex_colour):
    h = hex_colour.lstrip("#")
    return " ".join(f"{int(h[i:i + 2], 16) / 255:.3f}" for i in (0, 2, 4))


def ship_sdf(name, ship):
    """(SDF, hull height) of a ship at its AIS size, coloured and shaped by its type."""
    kind = ship.get("type_name") or ""
    length, beam = ship.get("length_m"), ship.get("beam_m")
    if not length or not beam:
        length, beam = 30.0, 8.0
    length, beam = float(length), float(beam)
    h = max(3.0, 0.06 * length)
    hx, hl, hb, hh = HOUSE[HOUSE_OF.get(kind, "aft")]
    return SHIP.format(name=name, L=length, B=beam, H=h, rgb=rgb(SHIP_COLOUR.get(kind, OTHER_COLOUR)),
                       HX=hx * length, HL=hl * length, HB=hb * beam, HH=hh * h, HZ=h / 2 + hh * h / 2), h


def box_colour(ident):
    """One of six colours, always the same for a container."""
    return BOX_COLOURS[int(hashlib.sha256(ident.encode()).hexdigest(), 16) % len(BOX_COLOURS)]


def box_sdf(name, box):
    return BOX.format(name=name, L=float(box.get("length_m") or 6.1), rgb=rgb(box_colour(box["id"])))


def parse_time(iso):
    try:
        return calendar.timegm(time.strptime(iso, "%Y-%m-%dT%H:%M:%SZ"))
    except (TypeError, ValueError):
        return None


def ship_pose(ship, h, now):
    """(x, y, z, yaw) of a ship now: moved on from its last report, for at most 60 s."""
    east, north = to_local(ship["lat"], ship["lon"])
    seen = parse_time(ship.get("last_seen"))
    sog, cog = ship.get("sog"), ship.get("cog")
    if seen is not None and sog and cog is not None:
        d = float(sog) * KNOT_M_S * max(0.0, min(MAX_EXTRAPOLATE_S, now - seen))
        east += d * math.sin(math.radians(cog))
        north += d * math.cos(math.radians(cog))
    heading = ship.get("heading")
    if heading is None or heading == 511:      # 511: AIS has no heading
        heading = cog or 0
    return east, north, 0.2 * h, math.radians(90 - float(heading))


def wanted(fleet, now, ships=True, containers=True):
    """{model name: (sdf, (x, y, z, yaw))} of what the world should hold."""
    out = {}
    if ships:
        for ship in fleet.get("ships") or []:
            if ship.get("mmsi") is None or ship.get("lat") is None or not inside(ship["lat"], ship["lon"]):
                continue
            name = f"ship-{int(ship['mmsi'])}"
            sdf, h = ship_sdf(name, ship)
            out[name] = (sdf, ship_pose(ship, h, now))
    inc = fleet.get("incident") or {}
    if containers and inc.get("status") == "searching":
        for box in inc.get("containers") or []:
            if box.get("lat") is None or not inside(box["lat"], box["lon"]):
                continue
            name = f"box-{box['id']}".lower()
            east, north = to_local(box["lat"], box["lon"])
            out[name] = (box_sdf(name, box), (east, north, 0.3, math.radians(90 - float(box.get("heading_deg") or 0))))
    return out


def pose_fields(pose):
    x, y, z, yaw = pose
    return (f"position: {{x: {x:.2f}, y: {y:.2f}, z: {z:.2f}}} "
            f"orientation: {{w: {math.cos(yaw / 2):.6f}, x: 0, y: 0, z: {math.sin(yaw / 2):.6f}}}")


def create_request(name, sdf, pose):
    return f'sdf: {json.dumps(sdf)} name: "{name}" allow_renaming: false pose: {{{pose_fields(pose)}}}'


def pose_vector_request(poses):
    return " ".join(f'pose: {{name: "{name}" {pose_fields(pose)}}}' for name, pose in sorted(poses.items()))


def remove_request(name):
    return f'name: "{name}" type: MODEL'


class Bridge:
    """The world's ships and boxes, kept to what /api/fleet says."""

    def __init__(self, call, ships=True, containers=True):
        self.call = call        # call(service, reqtype, req) -> bool
        self.ships = ships
        self.containers = containers
        self.models = {}        # name -> hash of its SDF
        self.failures = 0

    def _call(self, service, reqtype, req):
        ok = self.call(f"/world/{WORLD}/{service}", reqtype, req)
        self.failures = 0 if ok else self.failures + 1
        if self.failures == 5:
            print(f"ais-bridge: gz service {service} failed 5 times in a row", flush=True)
        return ok

    def tick(self, fleet, now):
        want = wanted(fleet, now, self.ships, self.containers)
        for name in [n for n in self.models if n not in want]:
            if self._call("remove", "gz.msgs.Entity", remove_request(name)):
                del self.models[name]
        for name, (sdf, pose) in want.items():
            digest = hashlib.sha256(sdf.encode()).hexdigest()
            if self.models.get(name) == digest:
                continue
            if name in self.models:
                # Its size or type came in: made again.
                self._call("remove", "gz.msgs.Entity", remove_request(name))
                del self.models[name]
            if self._call("create", "gz.msgs.EntityFactory", create_request(name, sdf, pose)):
                self.models[name] = digest
        live = {n: p for n, (_, p) in want.items() if n in self.models}
        if live:
            self._call("set_pose_vector", "gz.msgs.Pose_V", pose_vector_request(live))

    def counts(self):
        ships = sum(1 for n in self.models if n.startswith("ship-"))
        return ships, len(self.models) - ships


def gz_call(service, reqtype, req):
    out = subprocess.run(["gz", "service", "-s", service, "--reqtype", reqtype, "--reptype", "gz.msgs.Boolean",
                          "--timeout", "2000", "--req", req], capture_output=True, text=True, check=False)
    return "true" in out.stdout


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--fleet", required=True)
    ap.add_argument("--no-ships", action="store_true")
    ap.add_argument("--no-containers", action="store_true")
    ap.add_argument("--idle", action="store_true")
    args = ap.parse_args()
    if args.idle:
        print("ais-bridge: idle", flush=True)
        while True:
            time.sleep(3600)
    bridge = Bridge(gz_call, ships=not args.no_ships, containers=not args.no_containers)
    print(f"ais-bridge: ships {'on' if bridge.ships else 'off'}, containers {'on' if bridge.containers else 'off'}", flush=True)
    last_log = 0.0
    while True:
        t = time.monotonic()
        try:
            with urllib.request.urlopen(args.fleet, timeout=3) as r:
                fleet = json.load(r)
            bridge.tick(fleet, time.time())
        except Exception as exc:
            print(f"ais-bridge: {type(exc).__name__}: {exc}", file=sys.stderr, flush=True)
        if t - last_log >= 10:
            ships, boxes = bridge.counts()
            print(f"ais-bridge: {ships} ships in world, {boxes} boxes", flush=True)
            last_log = t
        time.sleep(max(0.0, 1.0 - (time.monotonic() - t)))


if __name__ == "__main__":
    main()
