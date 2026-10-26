"""Build Maasvlakte 2 in Blender from build/features.json and export it.

    blender -b --factory-startup -P build_scene.py -- \
        --features build/features.json --web ../app/static/port.glb \
        --gazebo gazebo/models/maasvlakte2/meshes/port.glb --blend build/port.blend

The scene is in metres: X east, Y north, Z up, from area.ORIGIN; the sea at
0, the quays and yards at GROUND_M. Two exports of the same scene:

  --web     for the browser: every container column, the cranes, tanks,
            turbines and masts as GPU instances (EXT_mesh_gpu_instancing,
            one draw call each in three.js)
  --gazebo  for gz sim (assimp reads neither extension): the same port with
            each row of a stacking block as one box, and no compression

Nothing here reads the network or needs a package outside Blender's own
Python; the stacks' and the ships' variety comes from a seeded random.
"""

import argparse
import json
import math
import os
import random
import sys

import bmesh
import bpy
from mathutils import Matrix, Vector
from mathutils.geometry import tessellate_polygon

SEED = 20261026

# sRGB colours: (hex, roughness, metallic)
MATERIALS = {
    "water": ("#1f4a63", 0.12, 0.0),
    "base": ("#c4bba5", 0.95, 0.0),
    "yard": ("#4b4e53", 0.9, 0.0),
    "paved": ("#8e8d88", 0.9, 0.0),
    "road": ("#2e3135", 0.85, 0.0),
    "rail": ("#5a4b3e", 0.95, 0.0),
    "grass": ("#6f8d4a", 1.0, 0.0),
    "scrub": ("#566f3c", 1.0, 0.0),
    "sand": ("#d8c89a", 1.0, 0.0),
    "works": ("#a88f6a", 1.0, 0.0),
    "quay": ("#6a675f", 0.9, 0.0),
    "wall": ("#b9bdc3", 0.8, 0.0),
    "roof": ("#7f868e", 0.7, 0.1),
    "office": ("#d7dce2", 0.5, 0.1),
    "canopy": ("#9aa0a6", 0.6, 0.2),
    "tank": ("#e6e5df", 0.5, 0.2),
    "pier": ("#7c7972", 0.9, 0.0),
    "breakwater": ("#5c5a55", 1.0, 0.0),
    "sts": ("#2e6db0", 0.5, 0.3),
    "asc": ("#c9ccd0", 0.5, 0.3),
    "unloader": ("#d9a21f", 0.5, 0.3),
    "machine": ("#e3e3df", 0.5, 0.2),
    "coal": ("#1b1b1d", 1.0, 0.0),
    "ore": ("#7b3b2a", 1.0, 0.0),
    "hull": ("#1d2633", 0.6, 0.1),
    "hull_bulk": ("#5b1f1b", 0.6, 0.1),
    "deck": ("#3c463f", 0.8, 0.0),
    "hatch": ("#8a2f22", 0.7, 0.0),
    "deckhouse": ("#ecebe6", 0.5, 0.0),
    "funnel": ("#b02a25", 0.5, 0.0),
    "turbine": ("#f2f2ef", 0.4, 0.0),
    "mast": ("#9ea3a8", 0.5, 0.5),
    "chimney": ("#cfcac2", 0.8, 0.0),
    "container": ("#ffffff", 0.75, 0.15),
    "stack_block": ("#8f6e57", 0.8, 0.0),
}
CONTAINER_COLOURS = ["#2f6fa8", "#2f6fa8", "#b8322d", "#d97c22", "#2f7d4b", "#8b9095", "#e3e1da", "#6b3f2a",
                     "#c9a227", "#1f3f68", "#2b8c8c", "#a8a8a0", "#7a1f2b", "#3b7fc4", "#d0d0c8"]
CONT_L, CONT_W, CONT_H = 12.19, 2.44, 2.59
COLUMN_LEVELS = 8
COLUMN_PATTERNS = 4


def srgb_to_linear(c):
    return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4


def hex_rgb(h):
    return tuple(srgb_to_linear(int(h[i:i + 2], 16) / 255) for i in (1, 3, 5))


def material(name):
    if name in bpy.data.materials:
        return bpy.data.materials[name]
    hexcol, rough, metal = MATERIALS[name]
    m = bpy.data.materials.new(name)
    m.use_nodes = True
    m.use_backface_culling = name not in ("canopy",)
    bsdf = m.node_tree.nodes["Principled BSDF"]
    bsdf.inputs["Base Color"].default_value = (*hex_rgb(hexcol), 1)
    bsdf.inputs["Roughness"].default_value = rough
    bsdf.inputs["Metallic"].default_value = metal
    if name == "container":
        attr = m.node_tree.nodes.new("ShaderNodeVertexColor")
        attr.layer_name = "Color"
        m.node_tree.links.new(attr.outputs["Color"], bsdf.inputs["Base Color"])
    return m


class MeshBuilder:
    """Vertices, faces and a material per face, turned into one mesh."""

    def __init__(self):
        self.verts, self.faces, self.mats, self.cols = [], [], [], []
        self.slots = []

    def slot(self, mat):
        if mat not in self.slots:
            self.slots.append(mat)
        return self.slots.index(mat)

    def face(self, pts, mat, colour=None):
        base = len(self.verts)
        self.verts.extend(pts)
        self.faces.append(tuple(range(base, base + len(pts))))
        self.mats.append(self.slot(mat))
        self.cols.append(colour)

    def quad_box(self, corners, mat, colour=None, skip_bottom=True):
        """A box from its 8 corners: bottom 0-3, top 4-7, counter-clockwise."""
        b, t = corners[:4], corners[4:]
        self.face([t[0], t[1], t[2], t[3]], mat, colour)
        if not skip_bottom:
            self.face([b[3], b[2], b[1], b[0]], mat, colour)
        for i in range(4):
            j = (i + 1) % 4
            self.face([b[i], b[j], t[j], t[i]], mat, colour)

    def box(self, cx, cy, cz, sx, sy, sz, mat, colour=None, skip_bottom=True):
        x0, x1, y0, y1, z0, z1 = cx - sx / 2, cx + sx / 2, cy - sy / 2, cy + sy / 2, cz - sz / 2, cz + sz / 2
        self.quad_box([(x0, y0, z0), (x1, y0, z0), (x1, y1, z0), (x0, y1, z0),
                       (x0, y0, z1), (x1, y0, z1), (x1, y1, z1), (x0, y1, z1)], mat, colour, skip_bottom)

    def beam(self, p0, p1, w, h, mat):
        """A square beam from p0 to p1, w wide and h deep."""
        p0, p1 = Vector(p0), Vector(p1)
        d = (p1 - p0).normalized()
        hint = Vector((0, 0, 1)) if abs(d.z) < 0.9 else Vector((1, 0, 0))
        side = d.cross(hint).normalized()
        up = side.cross(d).normalized()
        s, u = side * (w / 2), up * (h / 2)
        c = [p0 - s - u, p0 + s - u, p0 + s + u, p0 - s + u, p1 - s - u, p1 + s - u, p1 + s + u, p1 - s + u]
        self.hexa([tuple(v) for v in c], mat)

    def hexa(self, c, mat):
        """Six faces from eight corners: p0's end 0-3, p1's end 4-7."""
        self.face([c[0], c[3], c[2], c[1]], mat)
        self.face([c[4], c[5], c[6], c[7]], mat)
        self.face([c[0], c[1], c[5], c[4]], mat)
        self.face([c[1], c[2], c[6], c[5]], mat)
        self.face([c[2], c[3], c[7], c[6]], mat)
        self.face([c[3], c[0], c[4], c[7]], mat)

    def cylinder(self, cx, cy, z0, z1, r0, r1, mat, seg=16, cap=True, cone=0.0):
        ring0 = [(cx + r0 * math.cos(2 * math.pi * i / seg), cy + r0 * math.sin(2 * math.pi * i / seg), z0) for i in range(seg)]
        ring1 = [(cx + r1 * math.cos(2 * math.pi * i / seg), cy + r1 * math.sin(2 * math.pi * i / seg), z1) for i in range(seg)]
        for i in range(seg):
            j = (i + 1) % seg
            self.face([ring0[i], ring0[j], ring1[j], ring1[i]], mat)
        if cap:
            if cone > 0:
                apex = (cx, cy, z1 + cone)
                for i in range(seg):
                    self.face([ring1[i], ring1[(i + 1) % seg], apex], mat)
            else:
                self.face(ring1, mat)

    def polygon(self, outer, holes, z0, z1, top_mat, wall_mat, walls=True, top=True, tris=None):
        """A footprint (outer ring and holes) extruded from z0 to z1. tris:
        its top's triangles from prepare.py, (flat vertices, indices)."""
        if top and tris and tris[1]:
            tv, idx = tris
            flat = [(tv[i], tv[i + 1]) for i in range(0, len(tv), 2)]
            ti = self.slot(top_mat)
            base = len(self.verts)
            self.verts.extend((x, y, z1) for x, y in flat)
            for k in range(0, len(idx) - 2, 3):
                a, b, c = idx[k], idx[k + 1], idx[k + 2]
                pa, pb, pc = flat[a], flat[b], flat[c]
                if (pb[0] - pa[0]) * (pc[1] - pa[1]) - (pb[1] - pa[1]) * (pc[0] - pa[0]) < 0:
                    b, c = c, b
                self.faces.append((base + a, base + b, base + c))
                self.mats.append(ti)
                self.cols.append(None)
            top = False
        rings = [clean_ring(outer, ccw=True)] + [clean_ring(h, ccw=False) for h in holes]
        rings = [r for r in rings if len(r) >= 3]
        if not rings or len(rings[0]) < 3:
            return
        if top:
            flat = [p for r in rings for p in r]
            tris = tessellate_polygon([[(x, y, 0) for x, y in r] for r in rings])
            ti = self.slot(top_mat)
            base = len(self.verts)
            self.verts.extend((x, y, z1) for x, y in flat)
            for a, b, c in tris:
                pa, pb, pc = flat[a], flat[b], flat[c]
                if (pb[0] - pa[0]) * (pc[1] - pa[1]) - (pb[1] - pa[1]) * (pc[0] - pa[0]) < 0:
                    b, c = c, b
                self.faces.append((base + a, base + b, base + c))
                self.mats.append(ti)
                self.cols.append(None)
        if walls:
            for r in rings:
                for i in range(len(r)):
                    a, b = r[i], r[(i + 1) % len(r)]
                    self.face([(a[0], a[1], z0), (b[0], b[1], z0), (b[0], b[1], z1), (a[0], a[1], z1)], wall_mat)

    def build(self, name):
        me = bpy.data.meshes.new(name)
        me.from_pydata(self.verts, [], self.faces)
        for mat in self.slots:
            me.materials.append(material(mat))
        me.polygons.foreach_set("material_index", self.mats)
        if any(c is not None for c in self.cols):
            attr = me.color_attributes.new("Color", "BYTE_COLOR", "CORNER")
            flat = []
            for poly, col in zip(me.polygons, self.cols):
                rgb = col or (1.0, 1.0, 1.0)
                flat.extend([rgb[0], rgb[1], rgb[2], 1.0] * poly.loop_total)
            attr.data.foreach_set("color", flat)
        me.validate()
        me.shade_flat()
        return me


def clean_ring(pts, ccw):
    out = []
    for p in pts:
        p = (float(p[0]), float(p[1]))
        if not out or abs(out[-1][0] - p[0]) > 0.05 or abs(out[-1][1] - p[1]) > 0.05:
            out.append(p)
    while len(out) > 2 and abs(out[0][0] - out[-1][0]) < 0.05 and abs(out[0][1] - out[-1][1]) < 0.05:
        out.pop()
    area2 = sum(out[i][0] * out[(i + 1) % len(out)][1] - out[(i + 1) % len(out)][0] * out[i][1] for i in range(len(out)))
    if (area2 > 0) != ccw:
        out.reverse()
    return out


class Scene:
    def __init__(self, f):
        self.f = f
        self.g = f["ground_m"]
        self.rng = random.Random(SEED)
        self.col = bpy.context.scene.collection
        self.web_only, self.gz_only = [], []
        self.meshes = {}

    def obj(self, name, mesh, parent=None, loc=(0, 0, 0), rot=0.0, scale=(1, 1, 1), web=True, gz=True):
        o = bpy.data.objects.new(name, mesh)
        o.location = loc
        o.rotation_euler = (0, 0, rot)
        o.scale = scale
        if parent is not None:
            o.parent = parent
        self.col.objects.link(o)
        if not gz:
            self.web_only.append(o)
        if not web:
            self.gz_only.append(o)
            o.hide_render = True
        return o

    def group(self, name, web=True, gz=True):
        e = bpy.data.objects.new(name, None)
        self.col.objects.link(e)
        if not gz:
            self.web_only.append(e)
        if not web:
            self.gz_only.append(e)
        return e

    def mesh(self, key, make):
        if key not in self.meshes:
            mb = MeshBuilder()
            make(mb)
            self.meshes[key] = mb.build(key)
        return self.meshes[key]

    # -- ground -------------------------------------------------------------

    def ground(self):
        x0, y0, x1, y1 = self.f["bounds"]
        m = 6000
        mb = MeshBuilder()
        mb.face([(x0 - m, y0 - m, 0), (x1 + m, y0 - m, 0), (x1 + m, y1 + m, 0), (x0 - m, y1 + m, 0)], "water")
        self.obj("water", mb.build("water"))
        mb = MeshBuilder()
        for p in self.f["land"]:
            mb.polygon(p["o"], p["h"], -6, self.g, None, "quay", top=False)
        for kind, ps in self.f["surface"].items():
            for p in ps:
                mb.polygon(p["o"], p["h"], self.g, self.g, kind, None, walls=False, tris=(p.get("tv"), p.get("t")))
        self.obj("land", mb.build("land"))
        mb = MeshBuilder()
        for p in self.f["piers"]:
            top = 3.5 if p["k"] == "pier" else 4.0
            mb.polygon(p["o"], p["h"], -3, top, "pier" if p["k"] == "pier" else "breakwater", "pier" if p["k"] == "pier" else "breakwater", tris=(p.get("tv"), p.get("t")))
        self.obj("piers", mb.build("piers"))

    def buildings(self):
        mb = MeshBuilder()
        for b in self.f["buildings"]:
            if b["k"] == "roof":
                mb.polygon(b["o"], b["h"], self.g + b["z"] - 1.2, self.g + b["z"], "canopy", "canopy", tris=(b.get("tv"), b.get("t")))
                continue
            wall = "office" if b["k"] == "office" else "wall"
            mb.polygon(b["o"], b["h"], self.g, self.g + b["z"], "roof", wall, tris=(b.get("tv"), b.get("t")))
        self.obj("buildings", mb.build("buildings"))
        tank = self.mesh("tank", lambda mb: mb.cylinder(0, 0, 0, 1, 1, 1, "tank", seg=24, cone=0.06))
        grp = self.group("tanks")
        for t in self.f["tanks"]:
            self.obj("tank", tank, grp, (t["x"], t["y"], self.g), 0, (t["r"], t["r"], t["h"]))

    # -- cranes ---------------------------------------------------------------

    @staticmethod
    def sts(mb, mat, gauge=30.0, legs_h=46.0, outreach=68.0, backreach=25.0, boom_up=False, width=18.0):
        """A quay crane: +X over the water, the rails at x = ±gauge/2."""
        hx, hy, zb = gauge / 2, width / 2, legs_h + 4
        for x in (-hx, hx):
            for y in (-hy, hy):
                mb.box(x, y, legs_h / 2, 2.2, 2.2, legs_h, mat)
            mb.box(x, 0, legs_h, 2.6, width + 2.2, 2.6, mat)
            mb.box(x, 0, 1.5, 2.0, width + 4, 1.4, mat)
        for y in (-hy, hy):
            mb.box(0, y, legs_h, gauge, 2.2, 2.4, mat)
            mb.beam((-hx, y, legs_h * 0.55), (hx, y, legs_h - 1), 1.0, 1.0, mat)
        apex_x, apex_z = -hx + 6, zb + 30
        for y in (-4.5, 4.5):
            mb.beam((-hx - backreach, y, zb), (hx, y, zb), 2.0, 3.2, mat)
            mb.beam((-hx, y, legs_h), (apex_x, y, apex_z), 1.6, 1.6, mat)
            mb.beam((hx, y, legs_h), (apex_x, y, apex_z), 1.6, 1.6, mat)
            mb.beam((apex_x, y, apex_z), (-hx - backreach, y, zb + 1.5), 0.6, 0.6, mat)
            if boom_up:
                tip = (hx + outreach * math.cos(math.radians(78)), y, zb + outreach * math.sin(math.radians(78)))
                mb.beam((hx, y, zb), tip, 2.0, 3.0, mat)
                mb.beam((apex_x, y, apex_z), (tip[0], y, tip[2] - 4), 0.6, 0.6, mat)
            else:
                mb.beam((hx, y, zb), (hx + outreach, y, zb), 2.0, 3.0, mat)
                mb.beam((apex_x, y, apex_z), (hx + outreach * 0.45, y, zb + 1.5), 0.6, 0.6, mat)
                mb.beam((apex_x, y, apex_z), (hx + outreach * 0.95, y, zb + 1.5), 0.6, 0.6, mat)
        mb.box(apex_x, 0, apex_z, 3, 11, 2, mat)
        mb.box(-hx - backreach + 8, 0, zb + 4.5, 14, 10, 6, "machine")
        mb.box(hx + (6 if boom_up else outreach * 0.4), 0, zb - 3.5, 6, 7, 3, "machine")

    @staticmethod
    def portal(mb, mat, span, height=24.0, depth=12.0, cantilever=0.0):
        """A gantry over a block: legs at y = ±span/2, +X along its rails."""
        hy, hx = span / 2, depth / 2
        for x in (-hx, hx):
            for y in (-hy, hy):
                mb.box(x, y, height / 2, 1.6, 1.6, height, mat)
            mb.box(x, 0, height, 1.8, span + 2 * cantilever + 1.6, 2.4, mat)
        for y in (-hy, hy):
            mb.box(0, y, 1.0, depth + 3, 1.6, 1.4, mat)
            mb.box(0, y, height, depth, 1.4, 1.4, mat)
        mb.box(0, hy * 0.3, height - 2.5, 5, 6, 3, "machine")

    @staticmethod
    def stacker(mb, mat):
        """A stacker-reclaimer on its rails (+X along its boom)."""
        mb.box(0, 0, 3, 14, 9, 6, mat)
        mb.box(0, 0, 10, 5, 5, 8, mat)
        mb.beam((0, 0, 13), (48, 0, 8), 2.4, 2.4, mat)
        mb.beam((0, 0, 20), (48, 0, 9), 0.5, 0.5, mat)
        mb.beam((0, 0, 14), (-20, 0, 17), 2.0, 2.0, mat)
        mb.box(-21, 0, 17, 6, 5, 5, "machine")
        mb.box(0, 0, 21, 3, 3, 3, "machine")
        mb.box(49, 0, 8, 9, 2, 9, "machine")

    def cranes(self):
        f = self.f
        ships = f["ships"]

        def near_ship(c):
            for s in ships:
                along = (c["x"] - s["x"]) * math.cos(s["a"]) + (c["y"] - s["y"]) * math.sin(s["a"])
                across = -(c["x"] - s["x"]) * math.sin(s["a"]) + (c["y"] - s["y"]) * math.cos(s["a"])
                if abs(along) < s["l"] / 2 + 10 and abs(across) < s["b"] / 2 + 90:
                    return True
            return False

        down = self.mesh("sts_boom_down", lambda mb: self.sts(mb, "sts"))
        up = self.mesh("sts_boom_up", lambda mb: self.sts(mb, "sts", boom_up=True))
        grp = self.group("quay_cranes")
        for c in f["quay_cranes"]:
            # the seaside leg about 4 m from the quay's edge
            shift = max(-40.0, min(40.0, c["d"] - 4 - 15))
            x, y = c["x"] + shift * math.cos(c["a"]), c["y"] + shift * math.sin(c["a"])
            self.obj("quay_crane", down if near_ship(c) else up, grp, (x, y, self.g), c["a"])
        grp = self.group("stacking_cranes")
        for b in f["asc_blocks"]:
            span = round(b["w"])
            if b["rail"]:
                m = self.mesh(f"rail_gantry_{span}", lambda mb, s=span: self.portal(mb, "unloader", s, height=26, depth=14, cantilever=9))
                n = 2
            else:
                m = self.mesh(f"asc_{span}", lambda mb, s=span: self.portal(mb, "asc", s, height=24, depth=12))
                n = 2
            for k in range(n):
                along = (k - (n - 1) / 2) * b["l"] * 0.5 + self.rng.uniform(-0.15, 0.15) * b["l"]
                x, y = b["x"] + along * math.cos(b["a"]), b["y"] + along * math.sin(b["a"])
                self.obj("stacking_crane", m, grp, (x, y, self.g), b["a"])
        grp = self.group("rail_cranes")
        for c in f["rail_cranes"]:
            if c["k"] == "unloader":
                m = self.mesh(f"unloader_{round(c['s'])}", lambda mb, s=round(c["s"]): self.sts(mb, "unloader", gauge=s, legs_h=30, outreach=40, backreach=18, width=16))
            elif c["k"] == "gantry":
                m = self.mesh(f"gantry_{round(c['s'])}", lambda mb, s=round(c["s"]): self.portal(mb, "unloader", s, height=22, depth=12, cantilever=8))
            else:
                m = self.mesh("stacker", lambda mb: self.stacker(mb, "unloader"))
            self.obj(c["k"], m, grp, (c["x"], c["y"], self.g), c["a"])

    # -- containers -----------------------------------------------------------

    def column_meshes(self):
        """Columns of 1..COLUMN_LEVELS containers, COLUMN_PATTERNS colourings each."""
        rng = random.Random(SEED + 1)
        cols = [hex_rgb(h) for h in CONTAINER_COLOURS]
        out = {}
        for levels in range(1, COLUMN_LEVELS + 1):
            for p in range(COLUMN_PATTERNS):
                mb = MeshBuilder()
                for lv in range(levels):
                    colour = rng.choice(cols)
                    z0 = lv * CONT_H
                    x0, x1, y0, y1 = -CONT_L / 2, CONT_L / 2, -CONT_W / 2, CONT_W / 2
                    b = [(x0, y0, z0), (x1, y0, z0), (x1, y1, z0), (x0, y1, z0)]
                    t = [(x, y, z0 + CONT_H - 0.06) for x, y, _ in b]
                    for i in range(4):
                        j = (i + 1) % 4
                        mb.face([b[i], b[j], t[j], t[i]], "container", colour)
                    if lv == levels - 1:
                        mb.face(t, "container", colour)
                    else:
                        # the seam between two containers, slightly in
                        inset = [(x * 0.985, y * 0.95, z0 + CONT_H - 0.06) for x, y, _ in b]
                        top = [(x * 0.985, y * 0.95, z0 + CONT_H) for x, y, _ in b]
                        for i in range(4):
                            j = (i + 1) % 4
                            mb.face([inset[i], inset[j], top[j], top[i]], "container", (0.05, 0.05, 0.05))
                out[(levels, p)] = mb.build(f"containers_{levels}_{p}")
        return out

    def stacks(self):
        cols = self.column_meshes()
        grp = self.containers_grp = self.group("containers", gz=False)
        coarse = MeshBuilder()
        rng = self.rng
        for s in self.f["stacks"]:
            ca, sa = math.cos(s["a"]), math.sin(s["a"])
            pl, pw = s["pitch"]
            rows, bays = s["rows"], s["bays"]
            for r in range(rows):
                row_h = 0
                for k in range(bays):
                    h = int(s["h"][r * bays + k])
                    row_h = max(row_h, h)
                    if not h:
                        continue
                    dx = (k - (bays - 1) / 2) * pl
                    dy = (r - (rows - 1) / 2) * pw
                    x, y = s["x"] + dx * ca - dy * sa, s["y"] + dx * sa + dy * ca
                    self.obj("container_column", cols[(min(h, COLUMN_LEVELS), rng.randrange(COLUMN_PATTERNS))], grp, (x, y, self.g), s["a"])
                if row_h:
                    # one box per row for gz sim, as tall as the row's highest column
                    dy = (r - (rows - 1) / 2) * pw
                    hl, hw = bays * pl / 2, CONT_W / 2
                    corners = [(-hl, dy - hw), (hl, dy - hw), (hl, dy + hw), (-hl, dy + hw)]
                    pts = [(s["x"] + x * ca - y * sa, s["y"] + x * sa + y * ca) for x, y in corners]
                    coarse.quad_box([(x, y, self.g) for x, y in pts] + [(x, y, self.g + row_h * CONT_H) for x, y in pts], "stack_block")
        self.columns = cols
        self.coarse = coarse

    # -- ships ----------------------------------------------------------------

    def ships(self):
        hulls = MeshBuilder()
        grp = self.containers_grp
        for s in self.f["ships"]:
            rng = random.Random(s["seed"])
            L, B = s["l"], s["b"]
            bulk = s["k"] == "bulk"
            deck = 9.0 if bulk else 11.0
            ca, sa = math.cos(s["a"]), math.sin(s["a"])

            def P(x, y):
                return (s["x"] + x * ca - y * sa, s["y"] + x * sa + y * ca)

            bow = L * 0.14
            outline = [(-L / 2, -B / 2 + 2), (-L / 2 + 6, -B / 2), (L / 2 - bow, -B / 2), (L / 2, 0), (L / 2 - bow, B / 2), (-L / 2 + 6, B / 2), (-L / 2, B / 2 - 2)]
            hulls.polygon([P(x, y) for x, y in outline], [], -4, deck, "deck", "hull_bulk" if bulk else "hull")

            def block(x, y, z, sx, sy, sz, mat):
                hx, hy = sx / 2, sy / 2
                pts = [P(x - hx, y - hy), P(x + hx, y - hy), P(x + hx, y + hy), P(x - hx, y + hy)]
                hulls.quad_box([(px, py, z) for px, py in pts] + [(px, py, z + sz) for px, py in pts], mat)

            if bulk:
                house_x = -L / 2 + 14
                block(house_x, 0, deck, 16, B - 8, 18, "deckhouse")
                block(house_x - 6, 0, deck + 18, 5, 6, 8, "funnel")
                n = 7
                for k in range(n):
                    hx = -L / 2 + 34 + (k + 0.5) * (L - 34 - bow - 6) / n
                    block(hx, 0, deck, (L - 40 - bow) / n * 0.7, B * 0.62, 2.2, "hatch")
                continue
            house_x = L / 2 - bow - L * 0.22
            funnel_x = -L / 2 + 30
            block(house_x, 0, deck, 14, B - 6, 34, "deckhouse")
            block(house_x, 0, deck + 34, 6, B + 2, 2, "deckhouse")
            block(funnel_x, 0, deck, 10, 10, 30, "funnel")
            rows = int((B - 4) // (CONT_W + 0.15))
            x = -L / 2 + 14
            while x < L / 2 - bow - 4:
                if abs(x - house_x) < 14 or abs(x - funnel_x) < 12:
                    x += CONT_L + 1.4
                    continue
                tiers = rng.choice([4, 5, 6, 6, 7, 8])
                for r in range(rows):
                    y = (r - (rows - 1) / 2) * (CONT_W + 0.15)
                    h = max(1, min(COLUMN_LEVELS, tiers - (1 if abs(y) > B / 2 - 5 else 0) - rng.choice([0, 0, 0, 1])))
                    px, py = P(x + CONT_L / 2, y)
                    self.obj("container_column", self.columns[(h, rng.randrange(COLUMN_PATTERNS))], grp, (px, py, deck), s["a"])
                pts = [P(x, -(rows * (CONT_W + 0.15)) / 2), P(x + CONT_L, -(rows * (CONT_W + 0.15)) / 2),
                       P(x + CONT_L, (rows * (CONT_W + 0.15)) / 2), P(x, (rows * (CONT_W + 0.15)) / 2)]
                self.coarse.quad_box([(a, b, deck) for a, b in pts] + [(a, b, deck + tiers * CONT_H) for a, b in pts], "stack_block")
                x += CONT_L + 1.4
        self.obj("ships", hulls.build("ships"))
        self.obj("stacks_coarse", self.coarse.build("stacks_coarse"), web=False)

    # -- tall things ----------------------------------------------------------

    def tall(self):
        def tower(mb):
            mb.cylinder(0, 0, 0, 118.3, 3.2, 1.9, "turbine", seg=16, cap=False)
            mb.box(-2.5, 0, 120, 14, 4.6, 4.4, "turbine")

        def rotor(mb):
            # the hub on the X axis, the blades in the YZ plane: the disc faces +X
            mb.beam((-3.0, 0, 0), (3.5, 0, 0), 4.0, 4.0, "turbine")
            for k in range(3):
                a = 2 * math.pi * k / 3 + 0.3
                u = Vector((0, math.cos(a), math.sin(a)))
                v = Vector((0, -math.sin(a), math.cos(a)))
                t = Vector((0.3, 0, 0))
                r0, r1 = u * 2.0, u * 81.0
                w0, w1 = v * 1.9, v * 0.45
                c = [r0 - t - w0, r0 - t + w0, r0 + t + w0, r0 + t - w0, r1 - t - w1, r1 - t + w1, r1 + t + w1, r1 + t - w1]
                mb.hexa([tuple(p) for p in c], "turbine")

        tm = self.mesh("turbine_tower", tower)
        rm = self.mesh("turbine_rotor", rotor)
        grp = self.group("turbines")
        rgrp = self.group("turbine_rotors")
        yaw = math.radians(225)  # facing the prevailing south-westerly
        for t in self.f["turbines"]:
            s = t["hub"] / 118.3
            self.obj("turbine", tm, grp, (t["x"], t["y"], self.g), yaw, (s, s, s))
            hx, hy = t["x"] + 5.5 * s * math.cos(yaw), t["y"] + 5.5 * s * math.sin(yaw)
            self.obj("turbine_rotor", rm, rgrp, (hx, hy, self.g + 120 * s), yaw, (s, s, s))
        mast = self.mesh("mast", lambda mb: (mb.cylinder(0, 0, 0, 35, 0.6, 0.35, "mast", seg=8, cap=False), mb.box(0, 0, 35, 4, 4, 1.2, "mast")))
        grp = self.group("masts")
        chimneys = MeshBuilder()
        for m in self.f["masts"]:
            if m["k"] == "mast":
                s = m["h"] / 35
                self.obj("mast", mast, grp, (m["x"], m["y"], self.g), 0, (s, s, s))
            else:
                r = m.get("r", 4.0 if m["k"] == "chimney" else 3.0)
                chimneys.cylinder(m["x"], m["y"], self.g, self.g + m["h"], r, r * 0.6, "chimney", seg=16, cap=True)
        self.obj("chimneys", chimneys.build("chimneys"))
        def pile(mb, mat):
            # a ridge: 1 long (X), 1 wide (Y), 1 high, sloped ends
            b = [(-0.5, -0.5, 0), (0.5, -0.5, 0), (0.5, 0.5, 0), (-0.5, 0.5, 0)]
            r0, r1 = (-0.38, 0, 1), (0.38, 0, 1)
            mb.face([b[0], b[1], r1, r0], mat)
            mb.face([b[2], b[3], r0, r1], mat)
            mb.face([b[1], b[2], r1], mat)
            mb.face([b[3], b[0], r0], mat)
        coal = self.mesh("pile_coal", lambda mb: pile(mb, "coal"))
        ore = self.mesh("pile_ore", lambda mb: pile(mb, "ore"))
        grp = self.group("piles")
        for p in self.f.get("piles", []):
            self.obj("pile", ore if p["ore"] else coal, grp, (p["x"], p["y"], self.g), p["a"], (p["l"], p["w"], p["h"]))

    # -- export ---------------------------------------------------------------

    def export(self, path, gz):
        bpy.ops.object.select_all(action="DESELECT")
        skip = set(self.web_only if gz else self.gz_only)
        for o in bpy.context.scene.objects:
            if o.type in ("MESH", "EMPTY") and o not in skip and (o.parent is None or o.parent not in skip):
                o.select_set(True)
        os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
        bpy.ops.export_scene.gltf(
            filepath=path, export_format="GLB", use_selection=True, export_yup=True,
            export_gpu_instances=not gz, export_draco_mesh_compression_enable=False,
            export_texcoords=False, export_materials="EXPORT", export_vertex_color="MATERIAL",
            export_extras=False, export_apply=False, export_animations=False, export_cameras=False, export_lights=False)
        print(f"{path}: {os.path.getsize(path) / 1e6:.2f} MB")


def preview_rig(f):
    """A sun and a camera for renders of the .blend; neither is exported."""
    sun = bpy.data.objects.new("sun", bpy.data.lights.new("sun", "SUN"))
    sun.data.energy = 3.5
    sun.rotation_euler = (math.radians(50), 0, math.radians(200))
    bpy.context.scene.collection.objects.link(sun)
    cam = bpy.data.objects.new("camera", bpy.data.cameras.new("camera"))
    cam.data.lens = 28
    cam.data.clip_end = 30000
    cam.location = (-2600, -3600, 2400)
    cam.rotation_euler = (math.radians(58), 0, math.radians(-35))
    bpy.context.scene.collection.objects.link(cam)
    bpy.context.scene.camera = cam
    world = bpy.data.worlds.new("sky") if not bpy.context.scene.world else bpy.context.scene.world
    world.use_nodes = True
    world.node_tree.nodes["Background"].inputs["Color"].default_value = (0.55, 0.68, 0.85, 1)
    world.node_tree.nodes["Background"].inputs["Strength"].default_value = 0.8
    bpy.context.scene.world = world


def main():
    argv = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []
    ap = argparse.ArgumentParser()
    ap.add_argument("--features", required=True)
    ap.add_argument("--web")
    ap.add_argument("--gazebo")
    ap.add_argument("--blend")
    args = ap.parse_args(argv)
    with open(args.features) as fh:
        f = json.load(fh)
    for o in list(bpy.data.objects):
        bpy.data.objects.remove(o)
    s = Scene(f)
    s.ground()
    s.buildings()
    s.cranes()
    s.stacks()
    s.ships()
    s.tall()
    preview_rig(f)
    n = sum(1 for o in bpy.data.objects if o.type == "MESH")
    print(f"scene: {n} mesh objects, {len(bpy.data.meshes)} meshes")
    if args.blend:
        os.makedirs(os.path.dirname(os.path.abspath(args.blend)), exist_ok=True)
        bpy.ops.wm.save_as_mainfile(filepath=os.path.abspath(args.blend))
    if args.web:
        s.export(args.web, gz=False)
    if args.gazebo:
        s.export(args.gazebo, gz=True)


if __name__ == "__main__":
    main()
