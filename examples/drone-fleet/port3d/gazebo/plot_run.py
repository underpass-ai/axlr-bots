#!/usr/bin/env python3
"""Draw an oa_test.py run: the route, where the rover drove, the box.

    plot_run.py [/out]

Reads run.json, track.csv and SITL's newest dataflash log (logs/*.BIN) in the
directory and writes run.png: the whole route, the stretch round the box with
where BendyRuler was steering (OABR, Act 1), the speed along the box's leg, and
the avoidance parameters the log says the rover had.
"""

import csv
import glob
import json
import math
import os
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402
from matplotlib.patches import Polygon  # noqa: E402
from pymavlink import mavutil  # noqa: E402

ORIGIN = (51.944, 3.999)
EARTH_RADIUS_M = 6378137.0
INK, COPPER, ROUTE, BOX, OA = "#1d1d1f", "#b8732e", "#9a9a9a", "#d0342c", "#2a6fdb"


def enu(lat, lon):
    north = math.radians(lat - ORIGIN[0]) * EARTH_RADIUS_M
    east = math.radians(lon - ORIGIN[1]) * EARTH_RADIUS_M * math.cos(math.radians(ORIGIN[0]))
    return east, north


SHOWN = ("PRX1_TYPE", "AVOID_MARGIN", "OA_TYPE", "OA_BR_LOOKAHEAD", "OA_MARGIN_MAX")


def read_log(path):
    """Where the rover was while BendyRuler steered (POS samples during OABR Act 1), and the parameters."""
    log = mavutil.mavlink_connection(path)
    active, out, params = False, [], {}
    while True:
        m = log.recv_match(type=["OABR", "POS", "PARM"])
        if m is None:
            return out, params
        if m.get_type() == "PARM":
            if m.Name in SHOWN:
                params[m.Name] = m.Value
        elif m.get_type() == "OABR":
            active = bool(m.Act)
        elif active:
            out.append(enu(m.Lat, m.Lng))


def main():
    d = sys.argv[1] if len(sys.argv) > 1 else "/out"
    run = json.load(open(os.path.join(d, "run.json")))
    s = run["summary"]
    route = run["route_enu"]
    rows = list(csv.DictReader(open(os.path.join(d, "track.csv"))))
    tx = [float(r["east_m"]) for r in rows]
    ty = [float(r["north_m"]) for r in rows]
    logs = sorted(glob.glob(os.path.join(d, "logs", "*.BIN")))
    oa, params = read_log(logs[-1]) if logs else ([], {})
    bx, by = s["box_enu"]
    a, b = route[s["leg"]], route[s["leg"] + 1]
    h = math.atan2(b[1] - a[1], b[0] - a[0])
    half = s["box_size_m"] / 2
    corners = [(bx + half * (c * math.cos(h) - k * math.sin(h)), by + half * (c * math.sin(h) + k * math.cos(h)))
               for c, k in ((1, 1), (-1, 1), (-1, -1), (1, -1))]

    fig = plt.figure(figsize=(15, 8.5), dpi=110)
    grid = fig.add_gridspec(2, 2, width_ratios=[1, 1.25], height_ratios=[3, 1])
    whole, near, speed = fig.add_subplot(grid[:, 0]), fig.add_subplot(grid[0, 1]), fig.add_subplot(grid[1, 1])

    for ax in (whole, near):
        ax.plot([p[0] for p in route], [p[1] for p in route], color=ROUTE, lw=6, alpha=0.45, solid_capstyle="round",
                label=f"route {s['route']} (precomputed)")
        ax.plot(tx, ty, color=COPPER, lw=1.8, label="rover (gz, ArduRover 4.7.2 SITL)")
        ax.add_patch(Polygon(corners, closed=True, color=BOX, label=f"box {s['box_size_m']} m, not on the map"))
        ax.set_aspect("equal")
        ax.set_xlabel("east of 51.944 N 3.999 E (m)")
        ax.set_ylabel("north (m)")
        ax.grid(color="#e4e1dc", lw=0.6)
    if oa:
        near.scatter([p[0] for p in oa], [p[1] for p in oa], s=7, color=OA, zorder=3, label="BendyRuler steering (OABR Act 1)")
    whole.plot(tx[:1], ty[:1], "o", color=INK, ms=6, label="start: the RWG rover pad")
    whole.set_title("The whole route")
    whole.legend(loc="lower right", fontsize=8)
    span = 45
    near.set_xlim(bx - span * 1.4, bx + span * 1.4)
    near.set_ylim(by - span, by + span)
    near.set_title(f"Round the box: {s['closest_to_box_side_m']} m from its side at the closest, "
                   f"{s['max_off_route_near_box_m']} m off the route at most")
    near.legend(loc="lower right", fontsize=8)

    # Along the box's leg, metres past the box (negative before it): it runs on through a detour.
    lx, ly = math.cos(h), math.sin(h)
    along = [(x - bx) * lx + (y - by) * ly for x, y in zip(tx, ty)]
    # Mission item 2 is the route's first point, so the box's leg ends at item leg + 3.
    near_box = [(u, float(r["speed_m_s"])) for u, r in zip(along, rows) if int(r["item"]) == s["leg"] + 3]
    speed.plot([u for u, _ in near_box], [v for _, v in near_box], color=COPPER, lw=1.5)
    speed.axvline(0, color=BOX, lw=1)
    speed.set_xlim(-60, 60)
    speed.set_ylim(0, max(v for _, v in near_box) + 0.5)
    speed.set_xlabel("along the route from the box (m): before < 0 < after")
    speed.set_ylabel("speed (m/s)")
    speed.grid(color="#e4e1dc", lw=0.6)

    fig.suptitle("ArduRover's own obstacle avoidance in gz sim, Maasvlakte 2: "
                 + ", ".join(f"{k} {params[k]:g}" for k in SHOWN if k in params), fontsize=11)
    fig.tight_layout()
    fig.savefig(os.path.join(d, "run.png"))
    print(os.path.join(d, "run.png"))


if __name__ == "__main__":
    main()
