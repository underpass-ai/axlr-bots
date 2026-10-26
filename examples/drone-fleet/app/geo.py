"""Flat-earth offsets around a point: plenty for a few hundred metres."""

import math

EARTH_RADIUS_M = 6378137.0


def point_at(lat, lon, north_m, east_m):
    """The point north_m north and east_m east of (lat, lon)."""
    dlat = north_m / EARTH_RADIUS_M
    dlon = east_m / (EARTH_RADIUS_M * math.cos(math.radians(lat)))
    return lat + math.degrees(dlat), lon + math.degrees(dlon)


def offset_m(lat0, lon0, lat, lon):
    """(north, east) in metres of (lat, lon) from (lat0, lon0)."""
    north = math.radians(lat - lat0) * EARTH_RADIUS_M
    east = math.radians(lon - lon0) * EARTH_RADIUS_M * math.cos(math.radians(lat0))
    return north, east
