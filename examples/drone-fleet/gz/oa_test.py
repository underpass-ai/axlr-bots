#!/usr/bin/env python3
"""Drive a rover route in gz with a box on it that no map knows.

    oa_test.py [--route rwg:rwg-yard] [--box 120] [--speed 4] [--until 12]

Runs beside rover-sim, in the same container: connects to the SITL as the
vehicle pilot does (its own Vehicle class), uploads the route as the pilot's
_launch_rover does (home, speed, the route's points, RTL), drives it in AUTO,
and when the rover starts the leg the box is on, drops a static box on the
route --box metres into that leg (the longest leg of the route unless --leg
says which) with a camera 40 m above it. Writes to /out:

  track.csv     the rover at 5 Hz: time, mission item, lat, lon, speed,
                heading, the nav and target bearings, the nearest proximity
  events.txt    what the SITL said (STATUSTEXT) and what the test did
  run.json      the route, the box and the summary: how near the rover came
                to the box, how far it strayed from the route, where it
                rejoined
  top/          the camera above the box, twice a second of sim time
"""

import argparse
import csv
import json
import math
import os
import subprocess
import sys
import threading
import time

sys.path.insert(0, "/app")
import geo  # noqa: E402
import vehicle_pilot  # noqa: E402

mav = vehicle_pilot.mav
ORIGIN = (51.944, 3.999)
GROUND_M = 5.0
WORLD = "maasvlakte2"

BOX = """<sdf version="1.9"><model name="{name}"><static>true</static><link name="box">
<collision name="c"><geometry><box><size>{w} {w} {h}</size></box></geometry></collision>
<visual name="v"><geometry><box><size>{w} {w} {h}</size></box></geometry>
<material><ambient>0.85 0.1 0.1 1</ambient><diffuse>0.85 0.1 0.1 1</diffuse></material></visual>
</link></model></sdf>"""

TOP_CAMERA = """<sdf version="1.9"><model name="top_camera"><static>true</static><link name="l">
<sensor name="top" type="camera"><update_rate>2</update_rate><always_on>1</always_on>
<camera><horizontal_fov>1.2</horizontal_fov><image><width>1280</width><height>720</height></image>
<clip><near>1</near><far>200</far></clip><save enabled="true"><path>/out/top</path></save></camera>
</sensor></link></model></sdf>"""


def enu(lat, lon):
    north, east = geo.offset_m(ORIGIN[0], ORIGIN[1], lat, lon)
    return east, north


def spawn(sdf, name, x, y, z, yaw=0.0, pitch=0.0):
    """gz's create service: an SDF string at a pose in the world."""
    cy, sy = math.cos(yaw / 2), math.sin(yaw / 2)
    cp, sp = math.cos(pitch / 2), math.sin(pitch / 2)
    qw, qx, qy, qz = cy * cp, -sy * sp, cy * sp, sy * cp
    req = (f"sdf: {json.dumps(sdf)} name: \"{name}\" "
           f"pose: {{position: {{x: {x}, y: {y}, z: {z}}} orientation: {{w: {qw}, x: {qx}, y: {qy}, z: {qz}}}}}")
    out = subprocess.run(["gz", "service", "-s", f"/world/{WORLD}/create", "--reqtype", "gz.msgs.EntityFactory",
                          "--reptype", "gz.msgs.Boolean", "--timeout", "10000", "--req", req],
                         capture_output=True, text=True, check=False)
    if "true" not in out.stdout:
        raise RuntimeError(f"gz create {name}: {out.stdout} {out.stderr}")


def seg_distance(px, py, ax, ay, bx, by):
    """Distance from p to segment ab and how far along ab (0 to 1) its foot is."""
    dx, dy = bx - ax, by - ay
    t = ((px - ax) * dx + (py - ay) * dy) / (dx * dx + dy * dy)
    t = max(0.0, min(1.0, t))
    return math.hypot(px - ax - t * dx, py - ay - t * dy), t


class Recorder:
    """Reads the Vehicle's inbox: proximity, nav bearings and what the SITL says."""

    def __init__(self, vehicle, events):
        self.v = vehicle
        self.events = events
        self.mark = 0
        self.proximity = {}
        self.nav = (None, None)
        self.ekf_on_gps = False

    def poll(self):
        with self.v.cond:
            new = [(seq, m) for seq, m in self.v.inbox if seq > self.mark]
        for seq, m in new:
            self.mark = seq
            kind = m.get_type()
            # Proximity's sectors are ids 10 to 17 (AP_Proximity.h), the rangefinders 0 up.
            if kind == "DISTANCE_SENSOR" and 10 <= m.id <= 17 and m.min_distance <= m.current_distance < m.max_distance:
                self.proximity[m.orientation] = (m.current_distance / 100.0, time.monotonic())
            elif kind == "NAV_CONTROLLER_OUTPUT":
                self.nav = (m.nav_bearing, m.target_bearing)
            elif kind == "STATUSTEXT":
                text = m.text.rstrip(chr(0))
                self.events.write(f"{time.strftime('%H:%M:%S')} SITL: {text}\n")
                if "is using GPS" in text:
                    self.ekf_on_gps = True

    def nearest(self):
        now = time.monotonic()
        live = [d for d, t in self.proximity.values() if now - t < 1.0]
        return min(live) if live else None


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--route", default="rwg:rwg-yard")
    ap.add_argument("--leg", type=int, help="the route's leg (from point n to n+1) the box is on")
    ap.add_argument("--box", type=float, default=120.0, help="metres into the leg (0: no box)")
    ap.add_argument("--box-size", type=float, default=2.4)
    ap.add_argument("--speed", type=float, default=4.0)
    ap.add_argument("--until", type=int, help="stop at this mission item (default: the whole route)")
    ap.add_argument("--url", default="tcp:127.0.0.1:5760")
    ap.add_argument("--out", default="/out")
    args = ap.parse_args()

    with open("/app/routes.json") as f:
        route = next(r for r in json.load(f)["routes"] if r["id"] == args.route)
    pts = route["points"]
    xy = [enu(*p) for p in pts]
    legs = [math.hypot(b[0] - a[0], b[1] - a[1]) for a, b in zip(xy, xy[1:])]
    leg = args.leg if args.leg is not None else max(range(len(legs)), key=legs.__getitem__)
    (ax, ay), (bx, by) = xy[leg], xy[leg + 1]
    f = min(args.box / legs[leg], 0.9)
    box = (ax + f * (bx - ax), ay + f * (by - ay))
    leg_heading = math.atan2(by - ay, bx - ax)

    os.makedirs(os.path.join(args.out, "top"), exist_ok=True)
    events = open(os.path.join(args.out, "events.txt"), "a", buffering=1)
    say = lambda text: (events.write(f"{time.strftime('%H:%M:%S')} test: {text}\n"), print(text, flush=True))

    v = vehicle_pilot.Vehicle(args.url)
    v.start()
    rec = Recorder(v, events)
    say(f"route {args.route}: {len(pts)} points, {route['length_m']} m; box {args.box_size} m "
        f"on leg {leg} ({legs[leg]:.0f} m) at {box[0]:.1f} E {box[1]:.1f} N")
    if not v.wait_state(lambda s: s["connected"], 120):
        raise SystemExit("no heartbeat from the SITL")
    deadline = time.monotonic() + 300
    while time.monotonic() < deadline:
        rec.poll()
        s = v.snapshot()
        if s["home"] is None:
            v.command(mav.MAV_CMD_GET_HOME_POSITION)
        # The EKF must navigate on GPS before AUTO, or the rover drives on dead
        # reckoning: ArduRover arms and starts the mission without it.
        if s["home"] and s["gps_fix"] >= 3 and rec.ekf_on_gps:
            break
        time.sleep(1)
    else:
        raise SystemExit("no GPS fix, home or EKF position")
    home = v.snapshot()["home"]
    say(f"home {home['lat']:.7f} {home['lon']:.7f}")

    # The vehicle pilot's rover mission: home, the speed, the route, RTL.
    rel = mav.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT
    items = [{"frame": mav.MAV_FRAME_GLOBAL_INT, "command": mav.MAV_CMD_NAV_WAYPOINT, "lat": home["lat"], "lon": home["lon"], "alt": 0},
             {"frame": rel, "command": mav.MAV_CMD_DO_CHANGE_SPEED, "p1": 1, "p2": args.speed, "p3": -1}]
    items += [{"frame": rel, "command": mav.MAV_CMD_NAV_WAYPOINT, "lat": lat, "lon": lon, "alt": 0} for lat, lon in pts]
    items.append({"frame": rel, "command": mav.MAV_CMD_NAV_RETURN_TO_LAUNCH})
    first_wp = 2  # mission item of pts[0]
    v.upload_mission(items)
    say(f"mission: {len(items)} items")
    v.set_mode("HOLD")
    v.arm()
    v.set_mode("AUTO")
    say("armed, AUTO")

    t0 = time.monotonic()
    box_dropped = args.box <= 0
    closest = (float("inf"), None)
    stray = 0.0
    with open(os.path.join(args.out, "track.csv"), "w", newline="") as fcsv:
        w = csv.writer(fcsv)
        w.writerow(["t_s", "item", "lat", "lon", "east_m", "north_m", "speed_m_s", "heading", "nav_bearing",
                    "target_bearing", "proximity_m", "off_route_m", "to_box_m", "mode"])
        while True:
            rec.poll()
            s = v.snapshot()
            t = time.monotonic() - t0
            if s["lat"] is None:
                time.sleep(0.2)
                continue
            x, y = enu(s["lat"], s["lon"])
            if not box_dropped and s["mission_seq"] >= first_wp + leg:
                spawn(BOX.format(name="box", w=args.box_size, h=2.0), "box", box[0], box[1], GROUND_M + 1.0, leg_heading)
                spawn(TOP_CAMERA, "top_camera", box[0], box[1], GROUND_M + 40,
                      yaw=leg_heading + math.pi / 2, pitch=math.pi / 2)
                # gz renders a camera only while something listens to it.
                subprocess.Popen(["gz", "topic", "-e", "-t", f"/world/{WORLD}/model/top_camera/link/l/sensor/top/image"],
                                 stdout=subprocess.DEVNULL)
                box_dropped = True
                say(f"box dropped at {box[0]:.1f} E {box[1]:.1f} N, the rover {math.hypot(x - box[0], y - box[1]):.0f} m away")
            off = min(seg_distance(x, y, *xy[i], *xy[i + 1])[0] for i in range(len(xy) - 1))
            to_box = math.hypot(x - box[0], y - box[1]) if args.box > 0 else None
            if box_dropped and to_box is not None and to_box < closest[0]:
                closest = (to_box, (x, y))
            if box_dropped and to_box is not None and to_box < 40:
                stray = max(stray, off)
            nav, target = rec.nav
            prox = rec.nearest()
            w.writerow([f"{t:.1f}", s["mission_seq"], f"{s['lat']:.7f}", f"{s['lon']:.7f}", f"{x:.2f}", f"{y:.2f}",
                        f"{s['groundspeed']:.2f}", f"{s['heading']:.0f}", nav, target,
                        "" if prox is None else f"{prox:.2f}", f"{off:.2f}", "" if to_box is None else f"{to_box:.2f}", s["mode"]])
            done = s["mode"] in ("HOLD",) or (not s["armed"] and t > 30)
            if args.until and s["mission_seq"] >= args.until:
                v.set_mode("HOLD")
                say(f"reached item {s['mission_seq']}: HOLD")
                done = True
            if s["mission_seq"] >= len(items) - 1 and s["groundspeed"] < 0.2 and t > 60:
                say("route done")
                done = True
            if done:
                break
            time.sleep(0.2)

    summary = {
        "route": args.route, "leg": leg, "box_m_into_leg": args.box, "box_size_m": args.box_size,
        "box_enu": [round(box[0], 2), round(box[1], 2)], "speed_m_s": args.speed,
        "closest_to_box_centre_m": None if closest[1] is None else round(closest[0], 2),
        "closest_to_box_side_m": None if closest[1] is None else round(closest[0] - args.box_size / 2, 2),
        "max_off_route_near_box_m": round(stray, 2), "duration_s": round(time.monotonic() - t0),
    }
    with open(os.path.join(args.out, "run.json"), "w") as f:
        json.dump({"summary": summary, "route_enu": [[round(a, 2), round(b, 2)] for a, b in xy]}, f, indent=1)
    say(json.dumps(summary))


if __name__ == "__main__":
    main()
