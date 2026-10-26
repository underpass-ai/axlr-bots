"""Search patterns as waypoints, the way a ground station draws them.

  expanding_square  from a point where something was seen: legs of one,
                    one, two, two, three, three… track spacings, each turn
                    90 degrees to the right (IAMSAR's SS pattern)
  parallel_track    an area swept in straight legs `spacing_m` apart,
                    `length_m` long, across `width_m` (IAMSAR's PS pattern)

A pattern is aimed at a point in metres north and east of the vehicle's
home, or at a target the fleet base names (the datum, a sighting). A
drifting target is led: the centre moves with the drift for the time the
vehicle needs to get there, so it arrives where the object is, not where it
was. The waypoints go through the same limits as any route.
"""

import math

KINDS = ("expanding_square", "parallel_track")


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


def _num(p, key, default, lo, hi):
    value = p.get(key, default)
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not lo <= value <= hi:
        raise PatternError(f"pattern.{key}: a number from {lo:g} to {hi:g}")
    return float(value)


def expand(pattern, targets, speed, max_waypoints, alt_default=0.0):
    """Waypoints [{north_m, east_m, alt_m}] for a pattern, and where it is centred.

    targets: {name: {"north_m", "east_m", "drift_n", "drift_e"}} (drift in m/s).
    """
    if not isinstance(pattern, dict) or pattern.get("kind") not in KINDS:
        raise PatternError("pattern.kind: expanding_square or parallel_track")
    target = pattern.get("target")
    if target is not None:
        if not isinstance(target, str) or target not in targets:
            names = ", ".join(sorted(targets)) or "none now"
            raise PatternError(f"pattern.target: one of the targets in telemetry.json ({names})")
        t = targets[target]
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
    else:
        length = _num(pattern, "length_m", 1000, 50, 10000)
        width = _num(pattern, "width_m", 600, 0, 10000)
        shape = parallel_track(length, width, spacing, heading)
        if len(shape) > max_waypoints:
            raise PatternError(f"pattern: {len(shape) // 2} legs need {len(shape)} waypoints, more than {max_waypoints}; widen spacing_m or narrow width_m")
    return [{"north_m": round(cn + n, 1), "east_m": round(ce + e, 1), "alt_m": alt} for n, e in shape], (cn, ce)
