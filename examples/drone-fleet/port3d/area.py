"""The modelled area and its local frame, shared by every step.

The scene is in metres east, north and up from ORIGIN (Blender's X, Y, Z;
glTF's x, y, -z). The quays and yards stand GROUND_M above the sea, which
is at 0.
"""

import math

# south, west, north, east
BBOX = (51.920, 3.965, 51.965, 4.045)
ORIGIN = (51.9440, 3.9990)
GROUND_M = 5.0
EARTH_RADIUS_M = 6378137.0


def to_local(lat, lon):
    """(east, north) in metres of (lat, lon) from ORIGIN, as geo.offset_m."""
    lat0, lon0 = ORIGIN
    north = math.radians(lat - lat0) * EARTH_RADIUS_M
    east = math.radians(lon - lon0) * EARTH_RADIUS_M * math.cos(math.radians(lat0))
    return east, north


def bounds_local():
    """(min east, min north, max east, max north) of BBOX."""
    s, w, n, e = BBOX
    x0, y0 = to_local(s, w)
    x1, y1 = to_local(n, e)
    return x0, y0, x1, y1
