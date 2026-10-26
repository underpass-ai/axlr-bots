"""The drift of containers lost at sea: wind leeway plus the current.

A container floats low. The wind pushes the part above the water (its
leeway) and the water carries all of it (the current). Daniel et al. (2002)
put a container's downwind leeway near 5 % of the 10 m wind at 40 %
immersion, falling to nothing at 100 %; Breivik et al. (2012, Ocean
Dynamics, arXiv:1201.0603) tested 20 ft containers in the field and found
the search area grows far faster once the cross-wind part is allowed for.
So each container gets its own immersion (40 to 90 %), hence its own
downwind leeway, and a cross-wind leeway of a fifth of that to one side or
the other.

Two things come out of the same model:

  truth   one track per container, the simulation's ground truth: where
          the sprites go in the photos and what a sighting is matched to
  datum   what a search planner knows: a cloud of particles drawn from the
          same uncertainties, started where the vessel says it lost the
          containers; its centre is the datum, its spread the search radius

Flat-earth metres around the loss point are plenty for a few kilometres.
"""

import math
import random

import geo

KNOT = 0.514444
COLOURS = ["#b03a2e", "#1f5fa8", "#2e7d32", "#e67e22", "#7b1fa2", "#00838f", "#c0392b", "#546e7a",
           "#f1c40f", "#8d6e63", "#d35400", "#1565c0"]


def leeway_rate(immersion):
    """The downwind leeway as a share of the 10 m wind (Daniel et al. 2002)."""
    immersion = min(1.0, max(0.4, immersion))
    return 0.05 * (1.0 - immersion) / 0.6


def to_vector(to_deg, speed):
    """(north, east) of a speed towards to_deg (0 north, 90 east)."""
    a = math.radians(to_deg)
    return speed * math.cos(a), speed * math.sin(a)


def bearing(north, east):
    return math.degrees(math.atan2(east, north)) % 360


class Floater:
    """One container in the water: its leeway and where it is."""

    def __init__(self, ident, north, east, immersion, side, heading, colour, length_m=12.2):
        self.id = ident
        self.north = north
        self.east = east
        self.rate = leeway_rate(immersion)
        self.side = side              # +1 the cross-wind part to the right of downwind, -1 to the left
        self.heading = heading
        self.colour = colour
        self.length_m = length_m
        self.ashore = False
        self.velocity = (0.0, 0.0)


class Drift:
    """The sea state of one incident and the containers it carries."""

    def __init__(self, lost_lat, lost_lon, wind_from_deg, wind_m_s, current_to_deg, current_m_s, coast=None, seed=None):
        self.lat0, self.lon0 = lost_lat, lost_lon
        self.wind_from = wind_from_deg
        self.wind = wind_m_s
        self.current_to = current_to_deg
        self.current = current_m_s
        self.coast = sorted(coast or [], key=lambda p: p[0])   # (lat, lon) of the shoreline, east of it is land
        self.rng = random.Random(seed)
        self.containers = []
        self.particles = []

    # -- the forcing ---------------------------------------------------------

    def velocity(self, rate, side, current_scale=1.0, current_turn=0.0):
        """(north, east) metres a second of a floater with this leeway."""
        down = (self.wind_from + 180) % 360
        cn, ce = to_vector(self.current_to + current_turn, self.current * current_scale)
        dn, de = to_vector(down, rate * self.wind)
        xn, xe = to_vector(down + 90 * side, 0.2 * rate * self.wind)
        return cn + dn + xn, ce + de + xe

    def coast_lon(self, lat):
        """The shoreline's longitude at lat, or None where there is none."""
        pts = self.coast
        if len(pts) < 2 or not pts[0][0] <= lat <= pts[-1][0]:
            return None
        for (la1, lo1), (la2, lo2) in zip(pts, pts[1:]):
            if la1 <= lat <= la2:
                k = 0.0 if la2 == la1 else (lat - la1) / (la2 - la1)
                return lo1 + k * (lo2 - lo1)
        return None

    def on_land(self, north, east):
        lat, lon = geo.point_at(self.lat0, self.lon0, north, east)
        shore = self.coast_lon(lat)
        return shore is not None and lon >= shore

    # -- the containers ------------------------------------------------------

    def spill(self, count, vessel_heading_deg, spread_m):
        """count containers along spread_m of the vessel's track, centred on the loss point."""
        hn, he = to_vector(vessel_heading_deg, 1.0)
        for i in range(count):
            along = (i / max(1, count - 1) - 0.5) * spread_m + self.rng.uniform(-15, 15)
            across = self.rng.uniform(-25, 25)
            north = along * hn - across * he
            east = along * he + across * hn
            self.containers.append(Floater(
                f"C{i + 1:02d}", north, east, self.rng.uniform(0.4, 0.9), self.rng.choice((-1, 1)),
                self.rng.uniform(0, 180), COLOURS[i % len(COLOURS)], 12.2 if self.rng.random() < 0.7 else 6.1))
        # The planner's cloud: same spill, the uncertainties drawn again,
        # and the current known to within a fifth in speed and 20 degrees.
        for _ in range(400):
            along = self.rng.uniform(-0.5, 0.5) * spread_m * 1.5
            self.particles.append({
                "north": along * hn, "east": along * he, "rate": leeway_rate(self.rng.uniform(0.4, 0.9)),
                "side": self.rng.choice((-1, 1)), "scale": self.rng.uniform(0.8, 1.2), "turn": self.rng.uniform(-20, 20),
            })

    def step(self, dt):
        for c in self.containers:
            if c.ashore:
                c.velocity = (0.0, 0.0)
                continue
            vn, ve = self.velocity(c.rate, c.side)
            c.velocity = (vn, ve)
            c.north += vn * dt
            c.east += ve * dt
            c.heading = (c.heading + self.rng.uniform(-0.4, 0.4) * dt / 10) % 360
            if self.on_land(c.north, c.east):
                c.ashore = True
        for p in self.particles:
            vn, ve = self.velocity(p["rate"], p["side"], p["scale"], p["turn"])
            p["north"] += vn * dt
            p["east"] += ve * dt

    def advance(self, seconds, dt=10.0):
        """Run the sea for seconds (the time between the loss and the report)."""
        while seconds > 0:
            self.step(min(dt, seconds))
            seconds -= dt

    def position(self, north, east):
        return geo.point_at(self.lat0, self.lon0, north, east)

    # -- the planner's view --------------------------------------------------

    def datum(self):
        """The particles' centre, the radius holding nine in ten, and their mean drift."""
        n = len(self.particles)
        if not n:
            return None
        cn = sum(p["north"] for p in self.particles) / n
        ce = sum(p["east"] for p in self.particles) / n
        dists = sorted(math.hypot(p["north"] - cn, p["east"] - ce) for p in self.particles)
        radius = dists[int(0.9 * (n - 1))]
        vn = ve = 0.0
        for p in self.particles:
            a, b = self.velocity(p["rate"], p["side"], p["scale"], p["turn"])
            vn += a
            ve += b
        vn, ve = vn / n, ve / n
        lat, lon = self.position(cn, ce)
        return {"lat": lat, "lon": lon, "radius_m": round(max(150.0, radius)), "north": cn, "east": ce,
                "drift": {"to_deg": round(bearing(vn, ve)), "speed_m_s": round(math.hypot(vn, ve), 2)}, "velocity": (vn, ve)}

    def mean_velocity(self):
        d = self.datum()
        return d["velocity"] if d else (0.0, 0.0)
