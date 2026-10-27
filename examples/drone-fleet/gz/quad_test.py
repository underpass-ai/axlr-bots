#!/usr/bin/env python3
"""Fly a port quad in gz: take off to 10 m, hold, land.

    quad_test.py [--name quad-0] [--port 9002] [--alt 10] [--hold 10]

Runs in the fleet-gz image beside a world that already has the quad in it
(fleet-sim --spawn-only): starts ArduCopter 4.7.2 SITL on JSON against the
quad's UDP port with the fleet's defaults (copter.parm, copter-X.parm, the
chart's files/quad.parm at /chart; not gazebo-iris.parm, whose RNGFND1_TYPE
would override quad.parm), connects as the vehicle pilot does (its own
Vehicle class, /app), waits for the EKF to navigate on GPS, arms in GUIDED,
takes off, holds, lands. Writes to /out:

  sitl.log   the SITL's output (not with --no-sitl: --url reaches a SITL
             that runs elsewhere, as in a vehicle's pod)
  run.json   the highest altitude, home against the pad, the SITL's
             warnings (STATUSTEXT, severity warning or worse) after take-off,
             the nadir photo taken at the top of the climb
  nadir.png  that photo
"""

import argparse
import glob
import json
import math
import os
import shutil
import subprocess
import sys
import time

sys.path.insert(0, "/app")
import geo  # noqa: E402
import vehicle_pilot  # noqa: E402

mav = vehicle_pilot.mav
HOME = "51.944,3.999,0,0"


def statustexts(vehicle, mark):
    """(new mark, [(severity, text)]) of the STATUSTEXTs after mark."""
    with vehicle.cond:
        new = [(seq, m) for seq, m in vehicle.inbox if seq > mark]
    out = []
    for seq, m in new:
        mark = seq
        if m.get_type() == "STATUSTEXT":
            out.append((m.severity, m.text.rstrip("\x00").strip()))
    return mark, out


def newest(pattern):
    files = glob.glob(pattern)
    return max(files, key=os.path.getmtime) if files else None


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--name", default="quad-0")
    ap.add_argument("--port", type=int, default=9002)
    ap.add_argument("--pad", default="51.94550,3.99735", help="lat,lon of the quad's pad")
    ap.add_argument("--alt", type=float, default=10.0)
    ap.add_argument("--hold", type=float, default=10.0)
    ap.add_argument("--frames", default="/frames")
    ap.add_argument("--out", default="/out")
    ap.add_argument("--url", default="tcp:127.0.0.1:5760", help="the SITL's MAVLink")
    ap.add_argument("--no-sitl", action="store_true", help="the SITL runs elsewhere (its own pod or container)")
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)
    pad = tuple(float(x) for x in args.pad.split(","))

    if args.no_sitl:
        result = fly(args, pad)
    else:
        sitl = subprocess.Popen(
            ["/opt/sitl/arducopter", "--model=JSON:127.0.0.1", f"--sim-port-out={args.port}", f"--home={HOME}",
             "--defaults=/opt/sitl/copter.parm,/opt/sitl/copter-X.parm,/chart/quad.parm", "--speedup=1",
             "--instance=0", "--wipe"],
            cwd=args.out, stdout=open(os.path.join(args.out, "sitl.log"), "w"), stderr=subprocess.STDOUT)
        try:
            result = fly(args, pad)
        finally:
            sitl.terminate()
    with open(os.path.join(args.out, "run.json"), "w") as f:
        json.dump(result, f, indent=1)
    print(json.dumps(result, indent=1))


def fly(args, pad):
    v = vehicle_pilot.Vehicle(args.url)
    v.start()
    if not v.wait_state(lambda s: s["connected"], 120):
        raise SystemExit("no heartbeat from the SITL")
    mark, ekf_on_gps, before = 0, False, []
    deadline = time.monotonic() + 300
    while time.monotonic() < deadline:
        mark, texts = statustexts(v, mark)
        before += texts
        ekf_on_gps = ekf_on_gps or any("is using GPS" in t for _, t in texts)
        s = v.snapshot()
        if s["home"] is None:
            v.command(mav.MAV_CMD_GET_HOME_POSITION)
        if ekf_on_gps and s["home"] and s["gps_fix"] >= 3:
            break
        time.sleep(1)
    else:
        raise SystemExit("no GPS fix, home or EKF position")
    home = v.snapshot()["home"]
    north, east = geo.offset_m(pad[0], pad[1], home["lat"], home["lon"])
    print(f"home {home['lat']:.7f} {home['lon']:.7f}, {math.hypot(north, east):.1f} m from the pad", flush=True)

    v.set_mode("GUIDED")
    v.arm()
    if not v.command_ack(mav.MAV_CMD_NAV_TAKEOFF, 0, 0, 0, 0, 0, 0, args.alt):
        raise SystemExit("take-off refused")
    print("armed, taking off", flush=True)
    after, alt_max, photo = [], 0.0, None
    t0 = time.monotonic()
    phase, top_at = "climb", None
    while True:
        mark, texts = statustexts(v, mark)
        after += texts
        s = v.snapshot()
        alt_max = max(alt_max, s["alt_m"])
        now = time.monotonic()
        if phase == "climb" and s["alt_m"] >= args.alt * 0.95:
            phase, top_at = "hold", now
        elif phase == "hold" and photo is None and now - top_at > 2:
            # A picture the nadir camera took up there, the pad below it.
            src = newest(os.path.join(args.frames, args.name, "nadir", "*.png"))
            if src:
                photo = os.path.join(args.out, "nadir.png")
                shutil.copy(src, photo)
        elif phase == "hold" and now - top_at > args.hold:
            v.set_mode("LAND")
            phase = "land"
            print(f"held {args.hold:.0f} s at {s['alt_m']:.1f} m, landing", flush=True)
        elif phase == "land" and not s["armed"]:
            break
        if now - t0 > 180:
            after.append((0, "quad_test: timed out"))
            break
        time.sleep(0.2)

    return {
        "vehicle": args.name, "alt_target_m": args.alt, "alt_max_m": round(alt_max, 2),
        "home": home, "home_from_pad_m": round(math.hypot(north, east), 2),
        "warnings_after_takeoff": [t for sev, t in after if sev <= mav.MAV_SEVERITY_WARNING],
        "warnings_before_takeoff": [t for sev, t in before if sev <= mav.MAV_SEVERITY_WARNING],
        "nadir_photo": photo, "duration_s": round(time.monotonic() - t0),
    }


if __name__ == "__main__":
    main()
