#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["shapely==2.1.2"]
# ///
"""Turn the OSM extract into the scene's plan, in metres from the origin.

Reads build/osm.json (fetch_osm.py) and writes build/features.json, which
build_scene.py extrudes in Blender. Everything 2D happens here:

  land       the bbox minus the sea (from the coastline: land on its left)
             and the harbours, lakes and ponds; the quays stand GROUND_M up
  surface    the land's top split into disjoint patches, roads first, then
             rail, yards, grass, sand…: no two patches overlap, so nothing
             flickers in a browser at five kilometres
  buildings  footprints with a height: the height tag, the levels, or a
             default by kind and size (almost no building here has one)
  tanks      storage tanks as cylinders
  cranes     quay cranes (nodes) facing the nearest water; the automated
             stacking cranes' blocks (the gantry_crane rectangles) and the
             rail-mounted cranes (lines)
  stacks     container stacks: inside each stacking block, and a grid in the
             container depots without blocks; heights at random, seeded
  ships      container ships at the berths where three or more quay cranes
             stand, and a bulk carrier at the coal terminal
  turbines   wind turbines, masts, chimneys

    uv run prepare.py [--osm build/osm.json] [--out build/features.json]
"""

import argparse
import json
import math
import os
import random
import re

import shapely
from shapely.geometry import LineString, MultiPolygon, Point, Polygon, box
from shapely.ops import linemerge, nearest_points, polygonize, split, unary_union

import area

SEED = 20261026
ROAD_WIDTH = {
    "motorway": 11, "trunk": 11, "primary": 10, "secondary": 9, "tertiary": 8, "unclassified": 7,
    "residential": 6, "service": 5, "motorway_link": 7, "trunk_link": 7, "primary_link": 7, "secondary_link": 7,
}
# The land's top, in the order a patch claims it: the first wins.
SURFACES = ["road", "rail", "yard", "paved", "grass", "scrub", "sand", "works", "base"]
COVER = {
    ("landuse", "grass"): "grass", ("landuse", "meadow"): "grass", ("landuse", "forest"): "scrub",
    ("natural", "grassland"): "grass", ("natural", "scrub"): "scrub", ("natural", "heath"): "scrub",
    ("natural", "wetland"): "scrub", ("natural", "sand"): "sand", ("natural", "beach"): "sand",
    ("landuse", "construction"): "works", ("landuse", "landfill"): "works", ("landuse", "railway"): "paved",
    ("landuse", "industrial"): "paved", ("amenity", "parking"): "paved",
}
# The container terminals: their land use is the yard's asphalt.
TERMINALS = re.compile(r"APM Terminals|Rotterdam World Gateway|Hutchison|Euromax|Container|Depot|crossdock", re.I)
# Where empties are stacked without stacking cranes: a grid of blocks.
DEPOTS = re.compile(r"Kramer Distripark Depot|Delta Container Services|Rotterdam Container Terminal", re.I)


def rnd(v):
    return round(v, 1)


def ring(coords):
    return [[rnd(x), rnd(y)] for x, y in coords[:-1]]


def triangles(poly):
    """Constrained Delaunay triangles of a polygon: their vertices, flat
    [x, y, x, y, …], and three indices into them per triangle."""
    try:
        tris = shapely.constrained_delaunay_triangles(poly if poly.is_valid else poly.buffer(0))
    except shapely.errors.GEOSException:
        return [], []
    verts, index, out = [], {}, []
    for t in getattr(tris, "geoms", []):
        ids = []
        for x, y in list(t.exterior.coords)[:3]:
            key = (rnd(x), rnd(y))
            if key not in index:
                index[key] = len(index)
                verts.extend(key)
            ids.append(index[key])
        if len(set(ids)) == 3:
            out.extend(ids)
    return verts, out


def polys(geom, tri=False):
    """The polygons of any geometry, as {"o": outer, "h": [holes]}, and with
    tri their top's triangles: "tv" vertices and "t" indices."""
    if geom.is_empty:
        return []
    if isinstance(geom, Polygon):
        geoms = [geom]
    elif isinstance(geom, MultiPolygon):
        geoms = list(geom.geoms)
    else:
        geoms = [g for g in getattr(geom, "geoms", []) if isinstance(g, (Polygon, MultiPolygon))]
        return [p for g in geoms for p in polys(g, tri)]
    out = []
    for p in geoms:
        if p.area < 1:
            continue
        entry = {"o": ring(p.exterior.coords), "h": [ring(i.coords) for i in p.interiors if Polygon(i).area > 1]}
        if tri:
            entry["tv"], entry["t"] = triangles(p)
        out.append(entry)
    return out


def local_line(geometry):
    return [area.to_local(g["lat"], g["lon"]) for g in geometry if g]


class Plan:
    def __init__(self, osm):
        x0, y0, x1, y1 = area.bounds_local()
        self.frame = box(x0, y0, x1, y1)
        self.ways, self.nodes, self.rels = [], [], []
        for el in osm["elements"]:
            {"way": self.ways, "node": self.nodes, "relation": self.rels}[el["type"]].append(el)
        self.timestamp = osm.get("as_of") or osm.get("osm3s", {}).get("timestamp_osm_base")
        self.rng = random.Random(SEED)

    # -- geometry of OSM elements -------------------------------------------

    def area_of(self, el):
        """The polygon of a closed way or a multipolygon relation, or None."""
        if el["type"] == "way":
            pts = local_line(el.get("geometry", []))
            if len(pts) < 4 or pts[0] != pts[-1]:
                return None
            poly = Polygon(pts).buffer(0)
        else:
            outer, inner = [], []
            for m in el.get("members", []):
                if m.get("type") == "way" and m.get("geometry"):
                    line = local_line(m["geometry"])
                    if len(line) >= 2:
                        (inner if m.get("role") == "inner" else outer).append(LineString(line))
            if not outer:
                return None
            poly = unary_union(list(polygonize(linemerge(outer)))).buffer(0)
            if inner:
                poly = poly.difference(unary_union(list(polygonize(linemerge(inner)))).buffer(0))
        poly = poly.intersection(self.frame)
        return None if poly.is_empty else poly

    def tagged(self, test):
        for el in self.ways + self.rels:
            if test(el.get("tags", {})):
                yield el

    # -- land and water -----------------------------------------------------

    def land_and_water(self):
        coast = [LineString(local_line(w["geometry"])) for w in self.tagged(lambda t: t.get("natural") == "coastline")]
        merged = linemerge(coast)
        lines = list(merged.geoms) if hasattr(merged, "geoms") else [merged]
        land = []
        pieces = split(self.frame, unary_union(lines))
        for piece in pieces.geoms:
            p = piece.representative_point()
            best = min(lines, key=lambda ln: ln.distance(p))
            d = best.project(p)
            a = best.interpolate(max(0.0, d - 1))
            b = best.interpolate(min(best.length, d + 1))
            cross = (b.x - a.x) * (p.y - a.y) - (b.y - a.y) * (p.x - a.x)
            if cross > 0:  # left of the coastline: land
                land.append(piece)
        water = []
        for el in self.tagged(lambda t: t.get("natural") == "water" or t.get("waterway") in ("dock", "canal", "riverbank")):
            poly = self.area_of(el)
            if poly is not None:
                water.append(poly)
        self.water = unary_union(water)
        self.land = unary_union(land).difference(self.water).buffer(0)
        # tiny slivers along the frame and between harbour polygons
        self.land = unary_union([p for p in getattr(self.land, "geoms", [self.land]) if p.area > 2000]).simplify(0.5)
        self.wet = self.frame.difference(self.land)

    # -- the land's top -----------------------------------------------------

    def surface(self):
        roads = []
        for w in self.tagged(lambda t: t.get("highway") in ROAD_WIDTH):
            line = local_line(w.get("geometry", []))
            if len(line) >= 2 and not w["tags"].get("bridge") and not w["tags"].get("tunnel"):
                roads.append(LineString(line).buffer(ROAD_WIDTH[w["tags"]["highway"]] / 2, cap_style="flat", join_style="round", quad_segs=2))
        rails = []
        for w in self.tagged(lambda t: t.get("railway") in ("rail", "siding", "spur", "yard")):
            line = local_line(w.get("geometry", []))
            if len(line) >= 2:
                rails.append(LineString(line).buffer(1.8, cap_style="flat", quad_segs=1))
        cover = {k: [] for k in SURFACES}
        cover["road"] = roads
        cover["rail"] = rails
        self.terminals = []
        for el in self.tagged(lambda t: "landuse" in t or "natural" in t or t.get("amenity") == "parking"):
            t = el["tags"]
            name = t.get("name", "")
            kind = None
            if t.get("landuse") == "industrial" and (TERMINALS.search(name) or t.get("industrial") == "port"):
                kind = "yard"
            elif t.get("landuse") == "construction" and TERMINALS.search(name):
                kind = "yard"
            else:
                for (k, v), c in COVER.items():
                    if t.get(k) == v:
                        kind = c
                        break
            if kind is None:
                continue
            poly = self.area_of(el)
            if poly is None:
                continue
            cover[kind].append(poly)
            if kind == "yard" or DEPOTS.search(name):
                self.terminals.append((name, poly))
        taken = shapely.Polygon()
        self.patches = {}
        for kind in SURFACES:
            geom = self.land if kind == "base" else unary_union(cover[kind]).intersection(self.land)
            geom = geom.difference(taken).buffer(0)
            taken = taken.union(geom)
            self.patches[kind] = geom.simplify(0.3)
        self.roads = unary_union(roads)
        self.rails = unary_union(rails)

    # -- buildings, tanks ---------------------------------------------------

    @staticmethod
    def height_of(t, default):
        for key in ("height", "building:height"):
            m = re.match(r"^\s*([\d.]+)", t.get(key, ""))
            if m:
                return float(m.group(1))
        m = re.match(r"^\s*(\d+)", t.get("building:levels", ""))
        if m:
            return int(m.group(1)) * 3.5 + 1
        return default

    def buildings(self):
        self.blds, self.tanks, footprints = [], [], []
        for el in self.tagged(lambda t: "building" in t or t.get("man_made") in ("storage_tank", "silo")):
            t = el["tags"]
            poly = self.area_of(el)
            if poly is None or poly.area < 4:
                continue
            if t.get("man_made") in ("storage_tank", "silo") or t.get("building") in ("storage_tank", "silo"):
                c = poly.centroid
                r = math.sqrt(poly.area / math.pi)
                h = self.height_of(t, round(min(30.0, max(8.0, r * 0.9)), 1))
                self.tanks.append({"x": rnd(c.x), "y": rnd(c.y), "r": rnd(r), "h": rnd(h)})
                footprints.append(poly)
                continue
            kind = t.get("building", "yes")
            a = poly.area
            if kind == "roof":
                h = 7.0
            elif kind == "construction":
                h = 4.0
            elif kind in ("office", "commercial", "civic"):
                h = 14.0
            elif kind in ("warehouse", "industrial"):
                h = 20.0 if a > 20000 else 15.0 if a > 4000 else 10.0
            else:
                h = 16.0 if a > 15000 else 11.0 if a > 3000 else 7.0 if a > 300 else 4.0
            h = self.height_of(t, h)
            for p in polys(poly.simplify(0.3), tri=True):
                self.blds.append(dict(p, z=rnd(h), k="roof" if kind == "roof" else "office" if kind in ("office", "commercial", "civic") else "shed"))
            footprints.append(poly)
        self.footprints = unary_union(footprints) if footprints else shapely.Polygon()

    # -- cranes ---------------------------------------------------------------

    def facing_water(self, x, y):
        """The direction (radians from east) to the nearest water, and how far."""
        p = Point(x, y)
        q = nearest_points(self.wet, p)[0]
        return math.atan2(q.y - y, q.x - x), p.distance(q)

    def cranes(self):
        self.quay, self.blocks, self.rail_cranes = [], [], []
        for n in self.nodes:
            t = n.get("tags", {})
            if t.get("man_made") == "crane" and t.get("crane:type", "gantry_crane") == "gantry_crane":
                x, y = area.to_local(n["lat"], n["lon"])
                if not self.frame.contains(Point(x, y)):
                    continue
                ang, dist = self.facing_water(x, y)
                if dist > 120:
                    continue
                self.quay.append({"x": rnd(x), "y": rnd(y), "a": round(ang, 4), "d": rnd(dist)})
        rails = []
        for w in self.tagged(lambda t: t.get("man_made") == "crane"):
            pts = local_line(w.get("geometry", []))
            if len(pts) == 5 and pts[0] == pts[-1]:
                # an automated stacking crane's block: the long side is the crane's runway
                (ax, ay), (bx, by), (cx, cy) = pts[0], pts[1], pts[2]
                l1, l2 = math.dist(pts[0], pts[1]), math.dist(pts[1], pts[2])
                if l1 >= l2:
                    ang, length, width = math.atan2(by - ay, bx - ax), l1, l2
                else:
                    ang, length, width = math.atan2(cy - by, cx - bx), l2, l1
                mx = sum(p[0] for p in pts[:4]) / 4
                my = sum(p[1] for p in pts[:4]) / 4
                if self.frame.contains(Point(mx, my)):
                    # over 370 m long: a rail terminal's gantry over the tracks, no stacks under it
                    self.blocks.append({"x": rnd(mx), "y": rnd(my), "a": round(ang, 4), "l": rnd(length), "w": rnd(width), "rail": length > 370})
            elif len(pts) >= 2:
                line = LineString(pts).intersection(self.frame)
                if line.length > 40 and isinstance(line, LineString):
                    rails.append((line, w["tags"].get("crane:type", "")))
        # Rail-mounted cranes. Two parallel rails by the water are a quay's
        # unloaders, inland a gantry spanning them; a single rail inland is a
        # stacker-reclaimer's, with a coal or ore pile on either side.
        self.piles = []
        free = self.land.difference(unary_union([self.roads, self.rails, self.footprints]).buffer(4)).buffer(-2)
        used = set()
        for i, (a, kind) in enumerate(rails):
            if i in used:
                continue
            (ax, ay), (bx, by) = a.coords[0], a.coords[-1]
            ang = math.atan2(by - ay, bx - ax)
            pair = None
            for j, (b, _) in enumerate(rails):
                if j <= i or j in used:
                    continue
                (cx, cy), (dx, dy) = b.coords[0], b.coords[-1]
                if abs(math.sin(ang - math.atan2(dy - cy, dx - cx))) < 0.06 and 8 < a.distance(b) < 60:
                    pair = j
                    break
            mid = a.interpolate(0.5, normalized=True)
            _, wet = self.facing_water(mid.x, mid.y)
            count = max(1, min(4, int(a.length // 250)))
            spots = [(k + 0.5) / count for k in range(count)]
            if pair is not None:
                used.add(pair)
                b = rails[pair][0]
                span = a.distance(b)
                for f in spots:
                    p = a.interpolate(f, normalized=True)
                    q = b.interpolate(b.project(p))
                    x, y = (p.x + q.x) / 2, (p.y + q.y) / 2
                    if wet < 70:
                        wa, _ = self.facing_water(x, y)
                        self.rail_cranes.append({"x": rnd(x), "y": rnd(y), "a": round(wa, 4), "s": rnd(span), "k": "unloader"})
                    else:
                        self.rail_cranes.append({"x": rnd(x), "y": rnd(y), "a": round(ang, 4), "s": rnd(span), "k": "gantry"})
                continue
            if wet < 70:
                for f in spots:
                    p = a.interpolate(f, normalized=True)
                    wa, _ = self.facing_water(p.x, p.y)
                    self.rail_cranes.append({"x": rnd(p.x), "y": rnd(p.y), "a": round(wa, 4), "s": 20.0, "k": "unloader"})
                continue
            for f in spots:
                p = a.interpolate(f, normalized=True)
                self.rail_cranes.append({"x": rnd(p.x), "y": rnd(p.y), "a": round(ang + self.rng.uniform(0.6, 1.2) * self.rng.choice([-1, 1]), 4),
                                         "s": 10.0, "k": "stacker"})
            self.piles += self.piles_along(a, ang, free)

    def piles_along(self, line, ang, free, width=44.0, step=60.0):
        """Coal piles either side of a stacker's rail, where the yard is free."""
        out = []
        nx, ny = -math.sin(ang), math.cos(ang)
        for side in (-1, 1):
            off = side * (6 + width / 2)
            run = []
            n = int(line.length // step)
            for k in range(n + 1):
                p = line.interpolate(min(line.length, k * step + step / 2))
                seg = {"x": p.x + off * nx, "y": p.y + off * ny, "a": ang, "l": step, "w": width}
                if k < n and free.contains(Polygon(rect(seg))):
                    run.append(p)
                    continue
                if len(run) >= 2:
                    s, e = run[0], run[-1]
                    length = math.dist((s.x, s.y), (e.x, e.y)) + step - 8
                    cx, cy = (s.x + e.x) / 2 + off * nx, (s.y + e.y) / 2 + off * ny
                    out.append({"x": rnd(cx), "y": rnd(cy), "a": round(ang, 4), "l": rnd(length), "w": width - 4,
                                "h": rnd(self.rng.uniform(11, 16)), "ore": self.rng.random() < 0.3})
                run = []
        return out

    # -- containers -----------------------------------------------------------

    def stacks(self):
        """Container stacks: blocks of columns, heights at random (seeded)."""
        rng = self.rng
        cont_l, cont_w = 12.2, 2.45
        out = []
        for b in self.blocks:
            if b["rail"]:
                continue
            rows = max(1, int((b["w"] - 6) // (cont_w + 0.35)))
            bays = max(1, int((b["l"] - 30) // (cont_l + 0.6)))
            fill = rng.uniform(0.55, 0.9)
            heights = []
            for _ in range(rows * bays):
                heights.append(0 if rng.random() > fill else rng.choice([1, 2, 3, 3, 4, 4, 5]))
            out.append({"x": b["x"], "y": b["y"], "a": b["a"], "rows": rows, "bays": bays, "pitch": [cont_l + 0.6, cont_w + 0.35],
                        "h": "".join(str(h) for h in heights)})
        # Depots without stacking cranes: a grid of blocks inside the yard,
        # clear of roads, rail and buildings.
        blocked = unary_union([self.roads, self.rails, self.footprints]).buffer(6)
        asc = unary_union([Polygon(rect(b)) for b in self.blocks]).buffer(15) if self.blocks else shapely.Polygon()
        for name, poly in self.terminals:
            if not DEPOTS.search(name):
                continue
            free = poly.buffer(-12).difference(blocked).difference(asc)
            if free.is_empty:
                continue
            mrr = poly.minimum_rotated_rectangle
            cs = list(mrr.exterior.coords)
            e1 = (cs[1][0] - cs[0][0], cs[1][1] - cs[0][1])
            e2 = (cs[2][0] - cs[1][0], cs[2][1] - cs[1][1])
            axis = e1 if math.hypot(*e1) >= math.hypot(*e2) else e2
            ang = math.atan2(axis[1], axis[0])
            rows, bays = 6, 8
            bl, bw = bays * (cont_l + 0.6), rows * (cont_w + 0.35)
            ca, sa = math.cos(ang), math.sin(ang)
            c = poly.centroid
            reach = math.hypot(*e1) + math.hypot(*e2)
            step_l, step_w = bl + 14, bw + 10
            nl, nw = int(reach // step_l) + 1, int(reach // step_w) + 1
            for i in range(-nl, nl + 1):
                for j in range(-nw, nw + 1):
                    x = c.x + i * step_l * ca - j * step_w * sa
                    y = c.y + i * step_l * sa + j * step_w * ca
                    blk = {"x": x, "y": y, "a": ang, "l": bl, "w": bw}
                    if not free.contains(Polygon(rect(blk))):
                        continue
                    heights = [rng.choice([0, 2, 3, 4, 4, 5, 5, 6]) for _ in range(rows * bays)]
                    out.append({"x": rnd(x), "y": rnd(y), "a": round(ang, 4), "rows": rows, "bays": bays, "pitch": [cont_l + 0.6, cont_w + 0.35],
                                "h": "".join(str(h) for h in heights)})
        self.stack_blocks = out

    # -- ships ----------------------------------------------------------------

    def ships(self):
        """A ship at every berth with three or more quay cranes in a row."""
        cranes = sorted(self.quay, key=lambda c: (c["x"], c["y"]))
        groups, seen = [], set()
        for i, c in enumerate(cranes):
            if i in seen:
                continue
            group, todo = [], [i]
            seen.add(i)
            while todo:
                k = todo.pop()
                group.append(cranes[k])
                for j, o in enumerate(cranes):
                    if j not in seen and math.dist((o["x"], o["y"]), (cranes[k]["x"], cranes[k]["y"])) < 160:
                        seen.add(j)
                        todo.append(j)
            groups.append(group)
        self.ship_list = []
        for g in groups:
            if len(g) < 3:
                continue
            mx = sum(c["x"] for c in g) / len(g)
            my = sum(c["y"] for c in g) / len(g)
            face = math.atan2(sum(math.sin(c["a"]) for c in g), sum(math.cos(c["a"]) for c in g))
            along = face + math.pi / 2
            spread = max(abs((c["x"] - mx) * math.cos(along) + (c["y"] - my) * math.sin(along)) for c in g)
            length = 400.0 if spread > 150 else 366.0 if spread > 90 else 300.0
            beam = 59.0 if length >= 400 else 51.0 if length >= 366 else 48.0
            # the cranes' seaside legs stand on the quay's edge; the ship lies alongside
            dist = sum(c["d"] for c in g) / len(g)
            off = dist + 4 + beam / 2
            x, y = mx + off * math.cos(face), my + off * math.sin(face)
            hull = Polygon(rect({"x": x, "y": y, "a": along, "l": length, "w": beam}))
            if self.wet.contains(hull.buffer(-3)):
                self.ship_list.append({"x": rnd(x), "y": rnd(y), "a": round(along, 4), "l": length, "b": beam, "k": "container",
                                       "seed": self.rng.randrange(1 << 30)})
        # A bulk carrier at the coal terminal's unloaders.
        unloaders = [c for c in self.rail_cranes if c["k"] == "unloader"]
        if unloaders:
            c = min(unloaders, key=lambda u: self.facing_water(u["x"], u["y"])[1])
            _, dist = self.facing_water(c["x"], c["y"])
            face = c["a"]
            along = face + math.pi / 2
            for length, beam in ((290.0, 45.0), (230.0, 38.0)):
                off = dist + 4 + beam / 2
                x, y = c["x"] + off * math.cos(face), c["y"] + off * math.sin(face)
                hull = Polygon(rect({"x": x, "y": y, "a": along, "l": length, "w": beam}))
                if self.wet.contains(hull.buffer(-3)):
                    self.ship_list.append({"x": rnd(x), "y": rnd(y), "a": round(along, 4), "l": length, "b": beam, "k": "bulk", "seed": 1})
                    break

    # -- tall things ----------------------------------------------------------

    def tall(self):
        self.turbines, self.masts = [], []
        for n in self.nodes:
            t = n.get("tags", {})
            x, y = area.to_local(n["lat"], n["lon"])
            if not self.frame.contains(Point(x, y)):
                continue
            if t.get("power") == "generator" and t.get("generator:source") == "wind":
                hub, rotor = 119.0, 162.0
                m = re.search(r"V(\d{3})", t.get("model", ""))
                if m:
                    rotor = float(m.group(1))
                    hub = max(80.0, rotor * 0.73)
                self.turbines.append({"x": rnd(x), "y": rnd(y), "hub": rnd(self.height_of({"height": t.get("height", "")}, hub)), "rotor": rotor})
            elif t.get("man_made") in ("mast", "tower", "chimney", "lighthouse"):
                kind = t["man_made"]
                h = self.height_of(t, {"mast": 35.0, "tower": 45.0, "chimney": 120.0, "lighthouse": 40.0}[kind])
                self.masts.append({"x": rnd(x), "y": rnd(y), "h": rnd(h), "k": kind})
        for w in self.tagged(lambda t: t.get("man_made") in ("chimney", "tower")):
            poly = self.area_of(w)
            if poly is not None:
                c = poly.centroid
                kind = w["tags"]["man_made"]
                self.masts.append({"x": rnd(c.x), "y": rnd(c.y), "h": rnd(self.height_of(w["tags"], 120.0 if kind == "chimney" else 45.0)), "k": kind,
                                   "r": rnd(max(2.0, math.sqrt(poly.area / math.pi)))})

    def piers(self):
        out = []
        for w in self.tagged(lambda t: t.get("man_made") in ("pier", "breakwater", "groyne")):
            kind = w["tags"]["man_made"]
            poly = self.area_of(w)
            if poly is None:
                line = local_line(w.get("geometry", []))
                if len(line) < 2:
                    continue
                poly = LineString(line).buffer(4 if kind == "pier" else 7, cap_style="flat").intersection(self.frame)
            poly = poly.difference(self.land)
            for p in polys(poly.simplify(0.3), tri=True):
                out.append(dict(p, k=kind))
        self.pier_list = out

    def dump(self):
        x0, y0, x1, y1 = area.bounds_local()
        return {
            "about": "Maasvlakte 2, Port of Rotterdam: OpenStreetMap data (c) OpenStreetMap contributors, ODbL. Metres east/north/up from origin.",
            "osm_timestamp": self.timestamp,
            "origin": list(area.ORIGIN), "bbox": list(area.BBOX), "bounds": [rnd(x0), rnd(y0), rnd(x1), rnd(y1)], "ground_m": area.GROUND_M,
            "land": polys(self.land),
            "surface": {k: polys(v, tri=True) for k, v in self.patches.items()},
            "buildings": self.blds,
            "tanks": self.tanks,
            "quay_cranes": self.quay,
            "asc_blocks": self.blocks,
            "rail_cranes": self.rail_cranes,
            "stacks": self.stack_blocks,
            "ships": self.ship_list,
            "turbines": self.turbines,
            "masts": self.masts,
            "piers": self.pier_list,
            "piles": self.piles,
        }


def rect(b):
    """The corners of a rectangle {x, y, a, l, w}."""
    ca, sa = math.cos(b["a"]), math.sin(b["a"])
    hl, hw = b["l"] / 2, b["w"] / 2
    return [(b["x"] + dx * ca - dy * sa, b["y"] + dx * sa + dy * ca) for dx, dy in ((-hl, -hw), (hl, -hw), (hl, hw), (-hl, hw))]


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--osm", default=os.path.join(here, "build", "osm.json"))
    ap.add_argument("--out", default=os.path.join(here, "build", "features.json"))
    args = ap.parse_args()
    with open(args.osm) as f:
        plan = Plan(json.load(f))
    plan.land_and_water()
    plan.surface()
    plan.buildings()
    plan.cranes()
    plan.stacks()
    plan.ships()
    plan.tall()
    plan.piers()
    data = plan.dump()
    with open(args.out, "w") as f:
        json.dump(data, f, separators=(",", ":"))
    columns = sum(sum(1 for h in s["h"] if h != "0") for s in data["stacks"])
    print(f"{args.out}: land {plan.land.area / 1e6:.1f} km², {len(data['buildings'])} buildings, {len(data['tanks'])} tanks, "
          f"{len(data['quay_cranes'])} quay cranes, {len(data['asc_blocks'])} stacking blocks, {len(data['rail_cranes'])} rail cranes, "
          f"{len(data['stacks'])} stack blocks ({columns} columns), {len(data['ships'])} ships, {len(data['turbines'])} turbines, "
          f"{len(data['masts'])} masts, {len(data['piers'])} piers, {len(data['piles'])} piles; OSM {data['osm_timestamp']}")


if __name__ == "__main__":
    main()
