#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["shapely==2.1.2", "pyyaml==6.0.3"]
# ///
"""Precompute the rovers' routes: across the port's roads and yards, clear of
what is in the way, with turns a car can drive.

Reads build/features.json (the land, its surface, what is in the way) and
the chart's values.yaml (the stations, the depot, the zones and which rovers
live where), and writes ../app/routes.json, which the vehicle pilot offers
its bot as a menu: the bot picks a route by name and never plans one.

  the map     a lattice every GRID_M metres over the land, minus water and
              everything in the way (sheds, container blocks, tanks, coal
              piles) grown by CLEARANCE_M; driving costs 1 a metre on a
              road, 1.3 on a yard, 3 on grass or sand
  the search  A* over the lattice in 8 directions; leaving a stop
              backwards costs TURN_COST_M a radian of turn
  the line    pulled taut where the straight line stays clear and the turn
              stays under 100°, then each corner rounded with an arc of
              TURN_RADIUS_M, smaller only where the legs are short
  patrols     from each rover station to each zone a rover may visit: out,
              a lap through PATROL_POINTS points spread over the zone, back
  supply runs from the depot through every ordered choice of stations and
              back; at a station the rover turns round in a balloon loop

Every route is checked: no point outside the vehicle's geofence, nothing of
it inside an obstacle, and how much of it passes closer than CLEARANCE_M.

    uv run routes.py [--values ../../helm/drone-fleet/values.yaml] [--out ../app/routes.json]
"""

import argparse
import heapq
import itertools
import json
import math
import os

import numpy as np
import shapely
import yaml
from shapely.geometry import LineString, Point, Polygon, box
from shapely.ops import unary_union

import area
from prepare import rect

GRID_M = 5.0             # the lattice
RASTER_M = 1.0           # the obstacle map lines are checked against
TURN_RADIUS_M = 6.0      # the car's: what the arcs aim for
MIN_RADIUS_M = 2.5       # the tightest arc a route may keep where the legs are short
CLEARANCE_M = 1.5        # half the rover's width and a margin
ROVER_RADIUS_M = 1500    # vehicle_pilot.KINDS["rover"]["radius"]
PATROL_POINTS = 6
SURFACE_COST = {"road": 1.0, "rail": 1.3, "yard": 1.3, "paved": 1.3, "works": 1.6, "base": 1.6, "grass": 3.0, "scrub": 3.0, "sand": 3.0}
TURN_COST_M = 12.0       # metres of driving one radian of turn is worth
MAX_PULL_TURN = math.radians(100)
HEADINGS = [(1, 0), (2, 1), (1, 1), (1, 2), (0, 1), (-1, 2), (-1, 1), (-2, 1),
            (-1, 0), (-2, -1), (-1, -1), (-1, -2), (0, -1), (1, -2), (1, -1), (2, -1)]


def heading(a, b):
    return math.atan2(b[1] - a[1], b[0] - a[0])


def turn(a, b, c):
    """The angle between a→b and b→c, 0 to pi."""
    d = heading(b, c) - heading(a, b)
    return abs(math.atan2(math.sin(d), math.cos(d)))


def nearest_heading(ang):
    return min(range(16), key=lambda k: abs(math.atan2(math.sin(math.atan2(HEADINGS[k][1], HEADINGS[k][0]) - ang),
                                                         math.cos(math.atan2(HEADINGS[k][1], HEADINGS[k][0]) - ang))))


class Yard:
    """The drivable lattice and the obstacle raster, in local metres."""

    def __init__(self, features):
        x0, y0, x1, y1 = features["bounds"]
        self.x0, self.y0 = x0, y0
        frame = box(x0, y0, x1, y1)
        land = unary_union([shapely.make_valid(Polygon(p["o"], p["h"])) for p in features["land"]]).buffer(0)
        self.obstacles = self._obstacles(features)
        blocked = unary_union([self.obstacles.buffer(CLEARANCE_M), frame.difference(land).buffer(CLEARANCE_M),
                               frame.exterior.buffer(CLEARANCE_M)])
        self.blocked_geom = blocked
        # the raster lines are checked against
        rx = np.arange(x0 + RASTER_M / 2, x1, RASTER_M)
        ry = np.arange(y0 + RASTER_M / 2, y1, RASTER_M)
        X, Y = np.meshgrid(rx, ry)
        self.raster = shapely.contains_xy(blocked, X, Y)
        self.rh, self.rw = self.raster.shape
        # the lattice and what a metre costs at each point
        self.nx = int((x1 - x0) // GRID_M)
        self.ny = int((y1 - y0) // GRID_M)
        gx = x0 + GRID_M / 2 + np.arange(self.nx) * GRID_M
        gy = y0 + GRID_M / 2 + np.arange(self.ny) * GRID_M
        GX, GY = np.meshgrid(gx, gy)
        self.cost = np.full(GX.shape, np.inf)
        for kind in reversed(list(SURFACE_COST)):
            geoms = [shapely.make_valid(Polygon(p["o"], p["h"])) for p in features["surface"].get(kind, [])]
            if geoms:
                inside = shapely.contains_xy(unary_union(geoms).buffer(0.5), GX, GY)
                self.cost[inside] = SURFACE_COST[kind]
        self.cost[shapely.contains_xy(blocked, GX, GY)] = np.inf
        # keep the largest connected piece: pockets between container blocks
        # and fenced corners are not anywhere a rover can get to
        self.cost[~self._main_piece()] = np.inf
        self.free = int(np.isfinite(self.cost).sum())
        self.edges = {}  # (i, j, heading) -> the step is clear

    def _main_piece(self):
        free = np.isfinite(self.cost)
        label = np.zeros(free.shape, dtype=np.int32)
        sizes = {}
        n = 0
        for j0, i0 in zip(*np.nonzero(free)):
            if label[j0, i0]:
                continue
            n += 1
            label[j0, i0] = n
            stack, size = [(j0, i0)], 0
            while stack:
                j, i = stack.pop()
                size += 1
                for dj, di in ((1, 0), (-1, 0), (0, 1), (0, -1)):
                    a, b = j + dj, i + di
                    if 0 <= a < self.ny and 0 <= b < self.nx and free[a, b] and not label[a, b]:
                        label[a, b] = n
                        stack.append((a, b))
            sizes[n] = size
        main = max(sizes, key=sizes.get)
        return label == main

    @staticmethod
    def _obstacles(f):
        shapes = []
        for b in f["buildings"]:
            if b["k"] != "roof":
                shapes.append(Polygon(b["o"], b["h"]))
        for t in f["tanks"]:
            shapes.append(Point(t["x"], t["y"]).buffer(t["r"], quad_segs=4))
        for b in f["asc_blocks"]:
            if not b["rail"]:
                shapes.append(Polygon(rect(b)))
        for s in f["stacks"]:
            shapes.append(Polygon(rect({"x": s["x"], "y": s["y"], "a": s["a"], "l": s["bays"] * s["pitch"][0], "w": s["rows"] * s["pitch"][1]})))
        for p in f.get("piles", []):
            shapes.append(Polygon(rect(p)))
        return unary_union([s.buffer(0) for s in shapes])

    def xy(self, i, j):
        return (self.x0 + GRID_M / 2 + i * GRID_M, self.y0 + GRID_M / 2 + j * GRID_M)

    def blocked(self, x, y):
        c, r = int((x - self.x0) / RASTER_M), int((y - self.y0) / RASTER_M)
        return not (0 <= c < self.rw and 0 <= r < self.rh) or self.raster[r, c]

    def clear(self, a, b):
        n = max(1, int(math.dist(a, b) / (RASTER_M * 0.7)))
        for k in range(n + 1):
            if self.blocked(a[0] + (b[0] - a[0]) * k / n, a[1] + (b[1] - a[1]) * k / n):
                return False
        return True

    def snap(self, p, limit=80.0, reach_from=True):
        """The nearest lattice point a straight, clear drive from p reaches
        (reach_from False: the nearest drivable one, p being only an aim)."""
        i0, j0 = int((p[0] - self.x0) // GRID_M), int((p[1] - self.y0) // GRID_M)
        reach = int(limit // GRID_M)
        cands = []
        for j in range(max(0, j0 - reach), min(self.ny, j0 + reach + 1)):
            for i in range(max(0, i0 - reach), min(self.nx, i0 + reach + 1)):
                if math.isfinite(self.cost[j, i]):
                    cands.append((math.dist(p, self.xy(i, j)), i, j))
        for _, i, j in sorted(cands):
            if not reach_from or self.clear(p, self.xy(i, j)):
                return i, j
        raise RuntimeError(f"nothing drivable within {limit:.0f} m of ({p[0]:.0f}, {p[1]:.0f})")

    def search(self, start, goal, head=None, greed=2.0):
        """The cheapest drive between two lattice points: (points, the heading
        of its last step). head: the heading it arrived with, so leaving
        backwards costs a turn; None for any."""
        cost = self.cost
        gx, gy = goal
        best = {start: 0.0}
        prev = {}
        heap = [(0.0, 0.0, start)]
        while heap:
            _, g, cell = heapq.heappop(heap)
            if cell == goal:
                path = [cell]
                while path[-1] in prev:
                    path.append(prev[path[-1]])
                path.reverse()
                last = (path[-1][0] - path[-2][0], path[-1][1] - path[-2][1]) if len(path) > 1 else HEADINGS[head or 0]
                return [self.xy(*p) for p in path], nearest_heading(math.atan2(last[1], last[0]))
            if g > best.get(cell, math.inf):
                continue
            i, j = cell
            for k in range(0, 16, 2):
                di, dj = HEADINGS[k]
                ni, nj = i + di, j + dj
                if not (0 <= ni < self.nx and 0 <= nj < self.ny) or not math.isfinite(cost[nj, ni]):
                    continue
                ok = self.edges.get((i, j, k))
                if ok is None:
                    ok = self.edges[(i, j, k)] = self.clear(self.xy(i, j), self.xy(ni, nj))
                if not ok:
                    continue
                ng = g + math.hypot(di, dj) * GRID_M * 0.5 * (cost[j, i] + cost[nj, ni])
                if cell == start and head is not None:
                    d = abs(k - head) % 16
                    ng += TURN_COST_M * min(d, 16 - d) * math.pi / 8
                if ng < best.get((ni, nj), math.inf):
                    best[(ni, nj)] = ng
                    prev[(ni, nj)] = cell
                    heapq.heappush(heap, (ng + greed * math.hypot(ni - gx, nj - gy) * GRID_M, ng, (ni, nj)))
        return None, None


def pull(yard, pts):
    """Pull the line taut where a straight line stays clear and the turn it
    makes stays under MAX_PULL_TURN."""
    out = [pts[0]]
    i = 0
    while i < len(pts) - 1:
        j = min(len(pts) - 1, i + 80)
        while j > i + 1:
            if yard.clear(pts[i], pts[j]) and (len(out) < 2 or turn(out[-2], out[-1], pts[j]) <= MAX_PULL_TURN):
                break
            j -= 1
        out.append(pts[j])
        i = j
    return out


def corner_radius(a, b, c, radius=TURN_RADIUS_M):
    """The radius fillet() gives the corner at b."""
    th = turn(a, b, c)
    la, lc = math.dist(a, b), math.dist(b, c)
    if th < math.radians(8) or la < 1e-6 or lc < 1e-6:
        return math.inf
    t = min(radius * math.tan(th / 2), 0.49 * la, 0.49 * lc)
    return t / math.tan(th / 2)


def relax(yard, pts):
    """Drop the corners whose arc would be tighter than MIN_RADIUS_M where the
    straight line past them is clear: the line a car would take."""
    pts = list(pts)
    stuck = set()
    while True:
        worst, k = MIN_RADIUS_M, None
        for i in range(1, len(pts) - 1):
            r = corner_radius(pts[i - 1], pts[i], pts[i + 1])
            if r < worst and pts[i] not in stuck:
                worst, k = r, i
        if k is None:
            return pts
        # drop the corner, or the one before or after it, where the line past it is clear
        for m in (k, k + 1, k - 1):
            if 0 < m < len(pts) - 1 and yard.clear(pts[m - 1], pts[m + 1]):
                del pts[m]
                break
        else:
            stuck.add(pts[k])


def fillet(points, radius=TURN_RADIUS_M, step=4.0):
    """Round each corner with an arc of `radius`, smaller where the legs are
    short; returns the points and the tightest radius used."""
    pts = [points[0]]
    tightest = math.inf
    legs = [math.dist(a, b) for a, b in zip(points, points[1:])]
    for i in range(1, len(points) - 1):
        a, b, c = points[i - 1], points[i], points[i + 1]
        th = turn(a, b, c)
        if th < math.radians(8) or legs[i - 1] < 1e-6 or legs[i] < 1e-6:
            pts.append(b)
            continue
        t = min(radius * math.tan(th / 2), 0.49 * legs[i - 1], 0.49 * legs[i])
        r = t / math.tan(th / 2)
        tightest = min(tightest, r)
        u = ((b[0] - a[0]) / legs[i - 1], (b[1] - a[1]) / legs[i - 1])
        v = ((c[0] - b[0]) / legs[i], (c[1] - b[1]) / legs[i])
        p0 = (b[0] - u[0] * t, b[1] - u[1] * t)
        side = 1 if u[0] * v[1] - u[1] * v[0] > 0 else -1
        centre = (p0[0] - side * u[1] * r, p0[1] + side * u[0] * r)
        start = math.atan2(p0[1] - centre[1], p0[0] - centre[0])
        n = max(2, math.ceil(r * th / step))
        for k in range(n + 1):
            ang = start + side * th * k / n
            pts.append((centre[0] + r * math.cos(ang), centre[1] + r * math.sin(ang)))
    pts.append(points[-1])
    return pts, tightest


def balloon(p, d, radius=TURN_RADIUS_M, step=4.0, mirror=False):
    """Turn round at p, arriving heading d: right 60°, left 300°, right 60°
    (a cul-de-sac's loop, 2.7 radii ahead of p), leaving heading d + 180°."""
    out = []
    x, y, h = p[0], p[1], d
    sign = -1 if mirror else 1
    for side, ang in ((-sign, math.radians(60)), (sign, math.radians(300)), (-sign, math.radians(60))):
        cx, cy = x - side * math.sin(h) * radius, y + side * math.cos(h) * radius
        a0 = math.atan2(y - cy, x - cx)
        n = max(2, math.ceil(radius * ang / step))
        for k in range(1, n + 1):
            a = a0 + side * ang * k / n
            out.append((cx + radius * math.cos(a), cy + radius * math.sin(a)))
        x, y = out[-1]
        h += side * ang
    return out


class Planner:
    def __init__(self, yard):
        self.yard = yard
        self.legs = {}

    def leg(self, a, b, head):
        """From point a to point b (local metres), starting with heading index
        head (None: any): the smoothed points, and the heading at b."""
        y = self.yard
        key = (round(a[0], 1), round(a[1], 1), round(b[0], 1), round(b[1], 1), head)
        if key not in self.legs:
            sa, sb = y.snap(a), y.snap(b)
            path, h = y.search(sa, sb, head)
            if path is None:
                raise RuntimeError(f"no drive from ({a[0]:.0f}, {a[1]:.0f}) to ({b[0]:.0f}, {b[1]:.0f})")
            self.legs[key] = (pull(y, [a] + path + [b]), h)
        return self.legs[key]

    def drive(self, stops, turn_at=()):
        """A drive through `stops`; at the stops in turn_at the rover turns round."""
        line = [stops[0]]
        head = None
        for k in range(1, len(stops)):
            pts, head = self.leg(line[-1], stops[k], head)
            line += pts[1:]
            if k in turn_at:
                arrive = heading(line[-2], line[-1])
                for mirror in (False, True):
                    loop = balloon(stops[k], arrive, mirror=mirror)
                    if all(self.yard.clear(p, q) for p, q in zip([stops[k]] + loop, loop)):
                        line += loop
                        head = nearest_heading(arrive + math.pi)
                        break
                else:
                    head = None
        return line

    def finish(self, pts, home, max_radius):
        """Round the corners and check: the route as lat/lon and its facts."""
        clean = [pts[0]]
        for p in pts[1:]:
            if math.dist(p, clean[-1]) > 0.5:
                clean.append(p)
        smooth, tightest = fillet(relax(self.yard, clean))
        smooth = [smooth[0]] + [p for a, p in zip(smooth, smooth[1:]) if math.dist(a, p) > 1.0]
        smooth = list(LineString(smooth).simplify(0.4, preserve_topology=False).coords)
        path = LineString(smooth)
        inside = path.intersection(self.yard.obstacles).length
        near = path.intersection(self.yard.obstacles.buffer(CLEARANCE_M)).length
        farthest = max(math.dist(home, p) for p in smooth)
        return {
            "points": [[round(v, 7) for v in to_latlon(p)] for p in smooth],
            "length_m": round(path.length),
            "tightest_turn_m": round(tightest, 1) if tightest < math.inf else None,
            "inside_obstacles_m": round(inside, 1),
            "within_clearance_m": round(near, 1),
            "farthest_m": round(farthest),
            "fits_geofence": farthest <= max_radius,
        }

    def patrol(self, home, zone):
        """Out to the zone, a lap through PATROL_POINTS points spread over it, back."""
        c = area.to_local(zone["lat"], zone["lon"])
        r = zone["radius_m"]
        start = math.atan2(home[1] - c[1], home[0] - c[0])
        lap = []
        for k in range(PATROL_POINTS):
            a = start + 2 * math.pi * k / PATROL_POINTS
            target = (c[0] + 0.6 * r * math.cos(a), c[1] + 0.6 * r * math.sin(a))
            i, j = self.yard.snap(target, limit=0.5 * r, reach_from=False)
            lap.append(self.yard.xy(i, j))
        return self.drive([home] + lap + [lap[0], home])


def to_latlon(p):
    lat0, lon0 = area.ORIGIN
    lat = lat0 + math.degrees(p[1] / area.EARTH_RADIUS_M)
    lon = lon0 + math.degrees(p[0] / (area.EARTH_RADIUS_M * math.cos(math.radians(lat0))))
    return lat, lon


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--features", default=os.path.join(here, "build", "features.json"))
    ap.add_argument("--values", default=os.path.join(here, "..", "..", "helm", "drone-fleet", "values.yaml"))
    ap.add_argument("--out", default=os.path.join(here, "..", "app", "routes.json"))
    args = ap.parse_args()
    with open(args.features) as f:
        features = json.load(f)
    with open(args.values) as f:
        values = yaml.safe_load(f)
    yard = Yard(features)
    plan = Planner(yard)
    print(f"lattice: {yard.nx} x {yard.ny} every {GRID_M:g} m, {yard.free} points drivable")
    base = values["base"]
    places = {s["name"]: s for s in base["stations"]}
    places[base["depot"]["name"]] = base["depot"]
    routes, report = [], []
    for group in values["vehicles"]:
        if group["kind"] != "rover":
            continue
        radius = float(group.get("env", {}).get("MAX_RADIUS_M", ROVER_RADIUS_M))
        for station in group["stations"]:
            home = area.to_local(places[station]["lat"], places[station]["lon"])
            if group["role"] == "supply":
                others = [s["name"] for s in base["stations"]]
                for k in range(1, len(others) + 1):
                    for order in itertools.permutations(others, k):
                        stops = [home] + [area.to_local(places[s]["lat"], places[s]["lon"]) for s in order] + [home]
                        route_id = "supply:" + ">".join(order)
                        try:
                            r = plan.finish(plan.drive(stops, turn_at=set(range(1, len(stops) - 1))), home, radius)
                        except RuntimeError as exc:
                            report.append(f"{route_id}: {exc}")
                            continue
                        routes.append(dict({"id": route_id, "from": station, "stations": list(order), "kind": "supply"}, **r))
                continue
            for zone in values["zones"]:
                if "rover" not in zone["for"]:
                    continue
                route_id = f"{station}:{zone['name']}"
                try:
                    r = plan.finish(plan.patrol(home, zone), home, radius)
                except RuntimeError as exc:
                    report.append(f"{route_id}: {exc}")
                    continue
                if not r["fits_geofence"]:
                    report.append(f"{route_id}: reaches {r['farthest_m']} m from {station}, outside the {radius:.0f} m geofence: left out")
                    continue
                routes.append(dict({"id": route_id, "from": station, "zone": zone["name"], "about": zone.get("about", ""), "kind": "patrol"}, **r))
    out = {
        "about": "Rover routes across the port's roads and yards (OpenStreetMap, (c) OpenStreetMap contributors, ODbL), precomputed by port3d/routes.py.",
        "osm_timestamp": features.get("osm_timestamp"),
        "turn_radius_m": TURN_RADIUS_M, "clearance_m": CLEARANCE_M,
        "routes": routes,
    }
    with open(args.out, "w") as f:
        json.dump(out, f, separators=(",", ":"))
    for r in routes:
        if r["kind"] == "patrol" or len(r["stations"]) == 1:
            print(f"  {r['id']:32} {r['length_m']:6} m  {len(r['points']):4} points  turn ≥ {r['tightest_turn_m']} m  "
                  f"inside {r['inside_obstacles_m']} m  near {r['within_clearance_m']} m  farthest {r['farthest_m']} m")
    for line in report:
        print("  -", line)
    worst = max(routes, key=lambda r: len(r["points"]))
    print(f"{args.out}: {len(routes)} routes, at most {len(worst['points'])} points ({worst['id']}), "
          f"{os.path.getsize(args.out) / 1e3:.0f} kB")


if __name__ == "__main__":
    main()
