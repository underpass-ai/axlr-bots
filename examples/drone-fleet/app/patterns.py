"""Search patterns as waypoints, the way a ground station draws them.

  expanding_square  from a point where something was seen: legs of one,
                    one, two, two, three, three… track spacings, each turn
                    90 degrees to the right (IAMSAR's SS pattern)
  parallel_track    an area swept in straight legs `spacing_m` apart,
                    `length_m` long, across `width_m` (IAMSAR's PS pattern)
  orbit             `points` waypoints on a circle of `radius_m` around the
                    target, and back to the first: a look all round a ship

A pattern is aimed at a point in metres north and east of the vehicle's
home, or at a target the fleet base names (the datum, a sighting, a ship
from AIS by its MMSI or its name). A moving target is led: the centre moves
with the drift, or with the ship's course and speed, for the time the
vehicle needs to get there, so it arrives where the target is, not where it
was. The waypoints go through the same limits as any route.
"""

import math

import geo

KINDS = ("expanding_square", "parallel_track", "orbit")
KNOT = 0.514444


class PatternError(ValueError):
    pass


def _rot(n, e, heading_deg):
    """(n, e) turned so that 'north' points along heading_deg."""
    a = math.radians(heading_deg)
    return n * math.cos(a) - e * math.sin(a), n * math.sin(a) + e * math.cos(a)


def expanding_square(spacing, legs, heading_deg=0.0):
    """The corners, starting at the centre (0, 0), first leg along heading_deg."""
    points = [(0.0, 0.0)]
    n = e = 0.0
    direction = 0
    for leg in range(legs):
        length = spacing * (leg // 2 + 1)
        dn, de = [(1, 0), (0, 1), (-1, 0), (0, -1)][direction]
        n, e = n + dn * length, e + de * length
        points.append((n, e))
        direction = (direction + 1) % 4
    return [_rot(pn, pe, heading_deg) for pn, pe in points]


def parallel_track(length, width, spacing, heading_deg=0.0):
    """Leg ends of a lawnmower centred on (0, 0), legs along heading_deg."""
    count = max(1, int(width // spacing) + 1)
    offset = (count - 1) * spacing / 2
    points = []
    for i in range(count):
        across = i * spacing - offset
        ends = [(-length / 2, across), (length / 2, across)]
        if i % 2:
            ends.reverse()
        points += ends
    return [_rot(pn, pe, heading_deg) for pn, pe in points]


def orbit(radius, points, heading_deg=0.0):
    """points corners of a circle around (0, 0), from heading_deg clockwise, closed."""
    corners = [(radius * math.cos(math.radians(heading_deg + 360 * i / points)), radius * math.sin(math.radians(heading_deg + 360 * i / points)))
               for i in range(points)]
    return corners + corners[:1]


def relative(home, targets, ships=()):
    """The base's targets ({name: {lat, lon, drift?}}) and AIS ships, in metres
    from home with their motion in m/s: what expand() aims at. A ship is
    named by its MMSI and carries its name, so either finds it."""
    out = {}
    for name, t in (targets or {}).items():
        try:
            north, east = geo.offset_m(home["lat"], home["lon"], float(t["lat"]), float(t["lon"]))
        except (KeyError, TypeError, ValueError):
            continue
        drift = t.get("drift") or (targets.get("datum") or {}).get("drift") or {}
        dn = de = 0.0
        if isinstance(drift.get("to_deg"), (int, float)) and isinstance(drift.get("speed_m_s"), (int, float)):
            a = math.radians(drift["to_deg"])
            dn, de = drift["speed_m_s"] * math.cos(a), drift["speed_m_s"] * math.sin(a)
        entry = {"north_m": north, "east_m": east, "drift_n": dn, "drift_e": de, "about": str(t.get("about", ""))[:120]}
        if isinstance(t.get("radius_m"), (int, float)):
            entry["radius_m"] = round(t["radius_m"])
        out[str(name)[:20]] = entry
    for s in ships or ():
        try:
            mmsi = str(s["mmsi"])
            north, east = geo.offset_m(home["lat"], home["lon"], float(s["lat"]), float(s["lon"]))
        except (KeyError, TypeError, ValueError):
            continue
        sog, cog = s.get("sog"), s.get("cog")
        moving = isinstance(sog, (int, float)) and isinstance(cog, (int, float))
        a = math.radians(cog) if moving else 0.0
        v = sog * KNOT if moving else 0.0
        out[mmsi] = {"north_m": north, "east_m": east, "drift_n": v * math.cos(a), "drift_e": v * math.sin(a), "ship": True,
                     "name": str(s.get("name") or "")[:40], "type": s.get("type_name") or "", "length_m": s.get("length_m"),
                     "speed_kn": round(sog, 1) if moving else None, "course_deg": round(cog) if moving else None}
    return out


def _norm(name):
    return " ".join(str(name).upper().split())


def find(target, targets):
    """A target's key: the name itself (datum, S3, an MMSI) or a ship's name."""
    if not isinstance(target, str) or not target.strip():
        raise PatternError("pattern.target: a name from telemetry.json's targets or ships")
    if target in targets:
        return target
    want = _norm(target)
    hits = [k for k, t in targets.items() if t.get("ship") and _norm(t.get("name", "")) == want]
    if len(hits) == 1:
        return hits[0]
    if hits:
        raise PatternError(f"pattern.target: {len(hits)} ships are called {target!r}; name one by its MMSI ({', '.join(sorted(hits))})")
    names = ", ".join(sorted(k for k, t in targets.items() if not t.get("ship"))) or "none now"
    ships = sum(1 for t in targets.values() if t.get("ship"))
    raise PatternError(f"pattern.target: {target!r} is neither one of telemetry.json's targets ({names}) "
                       f"nor one of the {ships} ships AIS hears now (by MMSI or name)")


def _num(p, key, default, lo, hi):
    value = p.get(key, default)
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not lo <= value <= hi:
        raise PatternError(f"pattern.{key}: a number from {lo:g} to {hi:g}")
    return float(value)


def expand(pattern, targets, speed, max_waypoints, alt_default=0.0):
    """Waypoints [{north_m, east_m, alt_m}] for a pattern, and where it is centred.

    targets: {name: {"north_m", "east_m", "drift_n", "drift_e"}} (drift in m/s), as relative() makes them.
    """
    if not isinstance(pattern, dict) or pattern.get("kind") not in KINDS:
        raise PatternError("pattern.kind: expanding_square, parallel_track or orbit")
    target = pattern.get("target")
    if target is not None:
        t = targets[find(target, targets)]
        cn, ce = float(t["north_m"]), float(t["east_m"])
        # Lead a drifting target by the time the vehicle needs to reach it.
        lead = math.hypot(cn, ce) / max(speed, 0.5)
        cn += float(t.get("drift_n", 0.0)) * lead
        ce += float(t.get("drift_e", 0.0)) * lead
    else:
        cn = _num(pattern, "centre_north_m", None, -20000, 20000) if "centre_north_m" in pattern else None
        ce = _num(pattern, "centre_east_m", None, -20000, 20000) if "centre_east_m" in pattern else None
        if cn is None or ce is None:
            raise PatternError("pattern: a target, or centre_north_m and centre_east_m")
    spacing = _num(pattern, "spacing_m", 40, 10, 1000)
    heading = _num(pattern, "heading_deg", 0, 0, 360)
    alt = _num(pattern, "alt_m", alt_default, 0, 500)
    if pattern["kind"] == "expanding_square":
        legs = int(_num(pattern, "legs", 8, 2, max_waypoints - 1))
        shape = expanding_square(spacing, legs, heading)
    elif pattern["kind"] == "orbit":
        radius = _num(pattern, "radius_m", 80, 20, 1000)
        points = int(_num(pattern, "points", 8, 4, min(24, max_waypoints - 1)))
        shape = orbit(radius, points, heading)
    else:
        length = _num(pattern, "length_m", 1000, 50, 10000)
        width = _num(pattern, "width_m", 600, 0, 10000)
        shape = parallel_track(length, width, spacing, heading)
        if len(shape) > max_waypoints:
            raise PatternError(f"pattern: {len(shape) // 2} legs need {len(shape)} waypoints, more than {max_waypoints}; widen spacing_m or narrow width_m")
    return [{"north_m": round(cn + n, 1), "east_m": round(ce + e, 1), "alt_m": alt} for n, e in shape], (cn, ce)
