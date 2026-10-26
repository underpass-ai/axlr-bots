#!/usr/bin/env python3
"""The fleet base: the stations' stock, the chargers' queue, the live map.

It polls every vehicle's /state, and keeps what no single vehicle knows:

  stations   bays (how many vehicles are serviced at once), charged battery
             packs, spare parts, and the spent packs waiting for pick-up
  depot      where the supply rover loads packs and parts and drops spent ones
  crew       GET /api/crew/<vehicle>?station=…: a vehicle back from a mission
             asks for a bay; it gets one when a bay is free and the station
             holds a charged pack and a part, which the grant takes
  deliveries a supply rover's mission names the stations and what it carries;
             when it reaches a station the cargo is unloaded and the spent
             packs are loaded; back at the depot they are recharged

It writes <FLEET_FILE> every second for the dispatcher and logistics bots
(their read-only workspace) and serves the map: GET /, /api/fleet and the
event stream /api/stream.
"""

import json
import logging
import math
import os
import threading
import time
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import geo
import vision

log = logging.getLogger("fleet-base")

ARRIVAL_M = 25
STATIC = os.path.join(os.path.dirname(os.path.abspath(__file__)), "static")
TYPES = {".html": "text/html; charset=utf-8", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png"}


def now_iso():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


class Base:
    def __init__(self, vehicles, layout, zones, fleet_file):
        self.vehicles = vehicles          # {id: state url}
        self.layout = layout
        self.zones = zones
        self.fleet_file = fleet_file
        self.lock = threading.Lock()
        self.states = {}
        self.bays = {}                    # station -> {vehicle: granted at}
        self.stations = {}
        for s in layout["stations"]:
            self.stations[s["name"]] = {
                "name": s["name"], "label": s.get("label", s["name"]), "lat": s["lat"], "lon": s["lon"],
                "bays": int(s.get("bays", 2)), "batteries": int(s.get("batteries", 3)), "parts": int(s.get("parts", 4)),
                "max_batteries": int(s.get("max_batteries", 8)), "max_parts": int(s.get("max_parts", 10)),
                "spent": 0, "serviced": 0, "waiting": [],
            }
            self.bays[s["name"]] = {}
        depot = layout["depot"]
        self.depot = {"name": depot.get("name", "depot"), "label": depot.get("label", "Depot"), "lat": depot["lat"], "lon": depot["lon"],
                      "spent_recharged": 0}
        self.cargo = {}                   # supply vehicle -> {"batteries", "parts", "spent"}
        self.delivered = {}               # (vehicle, flight started_at) -> set of stations done
        self.events = []
        self.pool = ThreadPoolExecutor(max_workers=max(4, len(vehicles)))
        self.vision = vision.from_env()

    def event(self, text):
        log.info("%s", text)
        self.events.append({"t": time.time(), "text": text})
        del self.events[:-40]

    # -- polling -----------------------------------------------------------

    def poll(self):
        def fetch(item):
            vid, url = item
            try:
                with urllib.request.urlopen(url, timeout=1.5) as r:
                    return vid, json.load(r)
            except Exception:
                return vid, None
        results = list(self.pool.map(fetch, self.vehicles.items()))
        with self.lock:
            for vid, state in results:
                if state is None:
                    old = self.states.get(vid) or {"vehicle": vid}
                    self.states[vid] = dict(old, phase="offline")
                else:
                    self.states[vid] = state
            self._release_bays()
            self._deliveries()
            for vid, state in self.states.items():
                flight = state.get("flight") or {}
                if flight.get("shots"):
                    self.vision.offer(vid, state.get("kind", ""), flight)
            snapshot = self._snapshot()
        view = self._bot_view(snapshot)
        for path in self.fleet_file.split(","):
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path + ".tmp", "w") as f:
                json.dump(view, f, indent=1)
            os.replace(path + ".tmp", path)
        return snapshot

    def _release_bays(self):
        for station, held in self.bays.items():
            for vid in list(held):
                crew = (self.states.get(vid) or {}).get("ground_crew") or {}
                if crew.get("stage") not in ("queued", "inspection", "swap") and time.time() - held[vid] > 10:
                    del held[vid]
            waiting = [vid for vid, s in self.states.items()
                       if ((s.get("ground_crew") or {}).get("stage") == "queued") and (s.get("ground_crew") or {}).get("station") == station
                       and vid not in held]
            self.stations[station]["waiting"] = sorted(waiting, key=lambda v: (self.states[v]["ground_crew"].get("queued_at") or ""))

    def _deliveries(self):
        for vid, s in self.states.items():
            flight = s.get("flight") or {}
            deliveries = flight.get("deliveries")
            if not deliveries:
                continue
            key = (vid, flight.get("started_at"))
            done = self.delivered.setdefault(key, set())
            cargo = self.cargo.setdefault(vid, {"batteries": 0, "parts": 0, "spent": 0})
            if flight.get("status") == "flying" and not done and "loaded" not in done:
                for d in deliveries:
                    cargo["batteries"] += int(d.get("batteries", 0))
                    cargo["parts"] += int(d.get("parts", 0))
                done.add("loaded")
                self.event(f"{vid} loaded at the depot: {cargo['batteries']} packs, {cargo['parts']} parts")
            pos = s.get("position")
            if pos and flight.get("status") == "flying":
                for d in deliveries:
                    st = self.stations.get(d.get("station"))
                    if st is None or st["name"] in done:
                        continue
                    n, e = geo.offset_m(st["lat"], st["lon"], pos["lat"], pos["lon"])
                    if math.hypot(n, e) <= ARRIVAL_M:
                        give_b = min(int(d.get("batteries", 0)), cargo["batteries"], st["max_batteries"] - st["batteries"])
                        give_p = min(int(d.get("parts", 0)), cargo["parts"], st["max_parts"] - st["parts"])
                        st["batteries"] += give_b
                        st["parts"] += give_p
                        cargo["batteries"] -= give_b
                        cargo["parts"] -= give_p
                        cargo["spent"] += st["spent"]
                        picked, st["spent"] = st["spent"], 0
                        done.add(st["name"])
                        self.event(f"{vid} at {st['label']}: +{give_b} packs, +{give_p} parts, took {picked} spent")
            if flight.get("status") in ("landed", "failed") and "unloaded" not in done:
                if cargo["spent"] or cargo["batteries"] or cargo["parts"]:
                    self.depot["spent_recharged"] += cargo["spent"]
                    self.event(f"{vid} back at the depot: {cargo['spent']} spent packs to recharge, {cargo['batteries']} packs and {cargo['parts']} parts returned")
                self.cargo[vid] = {"batteries": 0, "parts": 0, "spent": 0}
                done.add("unloaded")

    # -- the crew ----------------------------------------------------------

    def crew(self, vid, station):
        """A bay for vid at station, when one is free and the stock allows."""
        with self.lock:
            if station == self.depot["name"] and vid in self.vehicles:
                return {"granted": True}  # the depot never runs out
            st = self.stations.get(station)
            if st is None or vid not in self.vehicles:
                return {"granted": False, "reason": "unknown vehicle or station"}
            held = self.bays[station]
            if vid in held:
                return {"granted": True}
            queue = st["waiting"] or [vid]
            if vid != queue[0] and vid in queue:
                return {"granted": False, "reason": f"in the queue, {queue.index(vid)} ahead"}
            if len(held) >= st["bays"]:
                return {"granted": False, "reason": "every bay is busy"}
            if st["batteries"] < 1:
                return {"granted": False, "reason": "no charged battery pack at the station"}
            if st["parts"] < 1:
                return {"granted": False, "reason": "no spare parts at the station"}
            st["batteries"] -= 1
            st["parts"] -= 1
            st["spent"] += 1
            st["serviced"] += 1
            held[vid] = time.time()
            self.event(f"{vid} serviced at {st['label']}: {st['batteries']} packs, {st['parts']} parts left")
            return {"granted": True}

    # -- views -------------------------------------------------------------

    def _snapshot(self):
        stations = []
        for name, st in self.stations.items():
            stations.append(dict(st, in_bays=sorted(self.bays[name])))
        return {
            "updated": now_iso(),
            "depot": self.depot,
            "rtk": self.layout.get("rtk"),
            "stations": stations,
            "zones": self.zones,
            "cargo": self.cargo,
            "photos": self.vision.recent(60),
            "vehicles": [self.states.get(v) or {"vehicle": v, "phase": "offline"} for v in self.vehicles],
            "events": self.events[-20:],
        }

    def _bot_view(self, snap):
        """What the dispatcher and the logistics bots read: short and flat."""
        vehicles = []
        for s in snap["vehicles"]:
            crew = s.get("ground_crew") or {}
            pos = s.get("position") or {}
            entry = {
                "vehicle": s.get("vehicle"), "kind": s.get("kind"), "role": s.get("role", "patrol"),
                "phase": s.get("phase"), "battery_pct": s.get("battery_pct"), "station": crew.get("station"),
                "missions": s.get("missions"), "zones_distance_m": {k: v["distance_m"] for k, v in (s.get("zones") or {}).items()},
            }
            if pos:
                entry["position"] = {"lat": pos.get("lat"), "lon": pos.get("lon")}
            vehicles.append(entry)
        return {
            "updated": snap["updated"],
            "vehicles": vehicles,
            "stations": [{k: st[k] for k in ("name", "label", "bays", "in_bays", "waiting", "batteries", "max_batteries", "parts", "max_parts", "spent", "serviced")} for st in snap["stations"]],
            "depot": {"name": self.depot["name"], "label": self.depot["label"]},
            "zones": [{k: z.get(k) for k in ("name", "about", "for")} for z in self.zones],
            "findings": self.vision.findings(),
        }

    def snapshot(self):
        with self.lock:
            return self._snapshot()


class Handler(BaseHTTPRequestHandler):
    """The map's port: the page, /api/fleet and /api/stream."""

    base = None
    crew_api = False
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        pass

    def _send(self, status, body, ctype="application/json", cache="no-store"):
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", cache)
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        url = urllib.parse.urlparse(self.path)
        if url.path == "/api/fleet":
            return self._send(200, json.dumps(self.base.snapshot()).encode())
        if url.path == "/api/stream":
            return self._stream()
        if url.path.startswith("/api/crew/") and self.crew_api:
            vid = url.path.rsplit("/", 1)[1]
            station = urllib.parse.parse_qs(url.query).get("station", [""])[0]
            return self._send(200, json.dumps(self.base.crew(vid, station)).encode())
        if url.path == "/healthz":
            return self._send(200, b"ok\n", "text/plain")
        if url.path.startswith("/photos/") and url.path.endswith(".jpg") and not self.crew_api:
            path = self.base.vision.image_path(url.path[len("/photos/"):-4])
            if path is None:
                return self._send(404, b"not found\n", "text/plain")
            with open(path, "rb") as f:
                return self._send(200, f.read(), "image/jpeg", "max-age=86400")
        if self.crew_api:
            return self._send(404, b"not found\n", "text/plain")
        name = "index.html" if url.path in ("/", "") else url.path.lstrip("/")
        path = os.path.realpath(os.path.join(STATIC, name))
        if not path.startswith(STATIC + os.sep) or not os.path.isfile(path):
            return self._send(404, b"not found\n", "text/plain")
        with open(path, "rb") as f:
            self._send(200, f.read(), TYPES.get(os.path.splitext(path)[1], "application/octet-stream"), "max-age=60")

    def _stream(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Accel-Buffering", "no")
        self.send_header("Connection", "close")
        self.end_headers()
        self.close_connection = True
        try:
            while True:
                data = json.dumps(self.base.snapshot())
                self.wfile.write(f"data: {data}\n\n".encode())
                self.wfile.flush()
                time.sleep(1)
        except (BrokenPipeError, ConnectionResetError, OSError):
            return


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s %(message)s")
    vehicles = {}
    for entry in os.environ.get("VEHICLES", "").split(","):
        entry = entry.strip()
        if entry:
            vid, _, url = entry.partition("=")
            vehicles[vid] = url
    with open(os.environ.get("LAYOUT_FILE", "/etc/fleet/layout.json")) as f:
        layout = json.load(f)
    zones = []
    zones_file = os.environ.get("ZONES_FILE", "/etc/fleet/zones.json")
    if os.path.exists(zones_file):
        with open(zones_file) as f:
            zones = json.load(f).get("zones", [])
    base = Base(vehicles, layout, zones, os.environ.get("FLEET_FILE", "/var/lib/fleet/fleet.json"))
    Handler.base = base
    host, _, port = os.environ.get("LISTEN", "0.0.0.0:8080").rpartition(":")
    server = ThreadingHTTPServer((host, int(port)), Handler)
    server.daemon_threads = True
    threading.Thread(target=server.serve_forever, name="http", daemon=True).start()
    # The vehicles' crew API on a port of its own, which the ingress never names.
    CrewHandler = type("CrewHandler", (Handler,), {"crew_api": True})
    host, _, port = os.environ.get("CREW_LISTEN", "0.0.0.0:8081").rpartition(":")
    crew = ThreadingHTTPServer((host, int(port)), CrewHandler)
    crew.daemon_threads = True
    threading.Thread(target=crew.serve_forever, name="crew", daemon=True).start()
    log.info("fleet base: %d vehicles, %d stations, on %s", len(vehicles), len(base.stations), server.server_address)
    while True:
        started = time.monotonic()
        try:
            base.poll()
        except Exception as exc:
            log.warning("poll: %s", exc)
        time.sleep(max(0.2, 1 - (time.monotonic() - started)))


if __name__ == "__main__":
    main()
