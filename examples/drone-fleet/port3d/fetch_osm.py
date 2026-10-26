#!/usr/bin/env python3
"""Pull the OpenStreetMap data of Maasvlakte 2 from the Overpass API.

One query for the whole area: buildings, water, the coastline, quays and
piers, the yards' land use, roads, rail, cranes, tanks, wind turbines. The
answer goes to build/osm.json as Overpass returns it (`out geom`: every way
carries its own coordinates). The data is © OpenStreetMap contributors,
under the ODbL.

    python3 fetch_osm.py [--date 2026-10-26T07:50:21Z] [--out build/osm.json] [--endpoint URL]

--date asks for the data as it was then (Overpass keeps the history): the
models in the repository were built from 2026-10-26T07:50:21Z.
"""

import argparse
import json
import os
import sys
import time
import urllib.parse
import urllib.request

import area

ENDPOINT = "https://overpass-api.de/api/interpreter"

QUERY = """
[out:json][timeout:240][bbox:{s},{w},{n},{e}]{date};
(
  way["building"]; relation["building"]["type"="multipolygon"];
  way["building:part"];
  way["natural"~"^(water|coastline|beach|sand|scrub|grassland|wetland|heath)$"];
  relation["natural"~"^(water|beach|sand|scrub|grassland|wetland)$"]["type"="multipolygon"];
  way["waterway"~"^(dock|canal|riverbank)$"];
  way["landuse"]; relation["landuse"]["type"="multipolygon"];
  way["man_made"~"^(quay|pier|breakwater|groyne|dyke|crane|storage_tank|silo|tower|mast|chimney|bridge|lighthouse)$"];
  node["man_made"~"^(crane|storage_tank|silo|tower|mast|chimney|lighthouse)$"];
  way["highway"~"^(motorway|trunk|primary|secondary|tertiary|unclassified|residential|service|motorway_link|trunk_link|primary_link|secondary_link)$"];
  way["railway"~"^(rail|siding|spur|yard)$"];
  way["power"="generator"]; node["power"="generator"];
  way["amenity"="parking"];
);
out geom qt;
"""


def fetch(endpoint, query):
    body = urllib.parse.urlencode({"data": query}).encode()
    req = urllib.request.Request(endpoint, data=body, headers={
        "User-Agent": "axlr-bots-port3d/0.1 (github.com/underpass-ai/axlr-bots)",
        "Accept": "application/json",
    })
    for attempt in range(4):
        try:
            with urllib.request.urlopen(req, timeout=300) as resp:
                return json.load(resp)
        except urllib.error.HTTPError as exc:
            if exc.code not in (429, 502, 503, 504) or attempt == 3:
                raise
            wait = 30 * (attempt + 1)
            print(f"overpass: {exc.code}, retrying in {wait} s", file=sys.stderr)
            time.sleep(wait)


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--out", default=os.path.join(here, "build", "osm.json"))
    ap.add_argument("--endpoint", default=ENDPOINT)
    ap.add_argument("--date", help="the data as of this ISO time, e.g. 2026-10-26T07:50:21Z")
    args = ap.parse_args()
    s, w, n, e = area.BBOX
    date = f'[date:"{args.date}"]' if args.date else ""
    data = fetch(args.endpoint, QUERY.format(s=s, w=w, n=n, e=e, date=date))
    if args.date:
        data["as_of"] = args.date  # osm3s.timestamp_osm_base is the server's, not the query's
    os.makedirs(os.path.dirname(args.out), exist_ok=True)
    with open(args.out, "w") as f:
        json.dump(data, f)
    kinds = {}
    for el in data.get("elements", []):
        kinds[el["type"]] = kinds.get(el["type"], 0) + 1
    print(f"{args.out}: {kinds}, OSM data of {data.get('as_of') or data.get('osm3s', {}).get('timestamp_osm_base')}")


if __name__ == "__main__":
    main()
