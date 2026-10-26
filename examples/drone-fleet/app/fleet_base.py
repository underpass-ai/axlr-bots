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

  incident   containers overboard (incident.py): POST /api/incident starts the
             drift, the photos look for the containers, the dispatcher's
             webhooks fire as the search goes, the report comes back
  ships      live AIS around the port: read from the fleet's `ais` service
             (AIS_URL), or its own connection with a key file (ais.py); the
             bots get them in fleet.json and the vehicles in /api/targets,
             so a bot can send a vehicle to a ship
  phone      GET /m: the calls waiting for a person, approve or deny
             (approvals.py), behind the approver's code

It writes <FLEET_FILE> every second for the dispatcher and logistics bots
(their read-only workspace) and serves the map: GET /, the 3D view /3d (the
port model is static/port.glb, from port3d/), /api/fleet and the event
stream /api/stream. Everything simulated is marked so: the snapshot says
"simulation": true and every page shows it.
"""

import hmac
import json
import logging
import math
import os
import queue
import threading
import time
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import ais
import approvals
import control_plane
import geo
import incident
import patterns
import vision

log = logging.getLogger("fleet-base")

ARRIVAL_M = 25
STATIC = os.path.join(os.path.dirname(os.path.abspath(__file__)), "static")
TYPES = {".html": "text/html; charset=utf-8", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png",
         ".glb": "model/gltf-binary", ".ttf": "font/ttf", ".txt": "text/plain; charset=utf-8"}
PAGES = {"/": "index.html", "": "index.html", "/3d": "3d.html", "/m": "m.html"}
MAX_BODY = 64 << 10
RESTOCK_EVERY = 300
SHIPS_FOR_BOTS = 40     # the ships nearest the depot in fleet.json


def now_iso():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


class Base:
    def __init__(self, vehicles, layout, zones, fleet_file):
        self.vehicles = vehicles          # {id: state url}
        self.layout = layout
        self.zones = zones
        self.fleet_file = fleet_file
        self.lock = threading.RLock()
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
        self.vision.search = self
        self.coast = [tuple(p) for p in layout.get("coast") or []]
        self.cp = control_plane.ControlPlane(os.environ.get("CP_URL", ""), os.environ.get("CP_CA_FILE", ""),
                                             os.environ.get("CP_TOKEN_FILE", ""), os.environ.get("CP_WEBHOOK_SECRET_FILE", ""))
        self.dispatch = incident.Dispatcher(self.cp if self.cp.enabled else None, os.environ.get("DISPATCHER_BOT", "dispatcher"),
                                            os.environ.get("LOGISTICS_BOT", "logistics"), os.environ.get("REPORT_BOT", "reporter"))
        self.incident = None
        self.incident_secret_file = os.environ.get("INCIDENT_SECRET_FILE", "")
        self.auto_restock = os.environ.get("AUTO_RESTOCK", "") == "1"
        self.cp_jobs = queue.Queue()
        threading.Thread(target=self._cp_worker, name="control-plane", daemon=True).start()
        self.last_costs = 0.0
        self.ais = ais.from_env()
        self.ais.start()
        self.approvals = approvals.Approvals(self.cp if self.cp.enabled else None, os.environ.get("APPROVER_CODE_FILE", ""), self.route_of)

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
            self._search(time.time())
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

    # -- the search ----------------------------------------------------------

    def _search(self, now):
        """One second of the incident: the sea, the webhooks it calls for, the costs."""
        inc = self.incident
        if self.auto_restock and now - getattr(self, "_restocked", 0) > RESTOCK_EVERY:
            low = [st["label"] for st in self.stations.values() if st["batteries"] < 2 or st["parts"] < 2]
            supply_ready = any(s.get("role") == "supply" and s.get("phase") == "ready" for s in self.states.values())
            if low and supply_ready:
                self._restocked = now
                self.event("stations running low: " + ", ".join(low) + "; logistics called")
                self.cp_jobs.put(("fire", "restock", {"stations": ", ".join(low)}, f"restock-{int(now)}", self.dispatch.logistics))
        if inc is None:
            return
        if inc.tick(now):
            self.event(f"search {inc.id} ended: {inc.end_reason}; {inc.found()}/{inc.count} found")
        due = inc.due_dispatch(now)
        if due:
            self.event(f"sightings {due['sightings']}: dispatcher called")
            self.cp_jobs.put(("fire", "sightings", due, f"{inc.id}-sightings-{int(now)}", None))
        if inc.status == "ended" and inc.report_task is None and not getattr(inc, "report_asked", False):
            inc.report_asked = True
            self.cp_jobs.put(("report", inc))
        if now - self.last_costs > 15 and (inc.status == "searching" or not inc.report or now - (inc.ended_at or now) < 600):
            self.last_costs = now
            self.cp_jobs.put(("costs", inc))

    def _cp_worker(self):
        """Calls to the control plane, one at a time, off the polling loop."""
        while True:
            job = self.cp_jobs.get()
            try:
                if job[0] == "fire":
                    _, trigger, event, key, bot = job
                    if trigger == "restock" and self._supply_waiting():
                        log.info("restock skipped: a supply run is already waiting for a person")
                        continue
                    task = self.dispatch.fire(trigger, event, key, bot)
                    if task and self.incident:
                        with self.lock:
                            self.incident.tasks.append(task)
                elif job[0] == "report":
                    inc = job[1]
                    with self.lock:
                        event = inc.report_event(time.time())
                    inc.report_task = self.dispatch.fire("report", event, f"{inc.id}-report", self.dispatch.reporter) or ""
                elif job[0] == "costs":
                    inc = job[1]
                    cost = self.dispatch.bots_cost(inc.started_at)
                    report = self.dispatch.report_of(inc.report_task) if inc.report_task and not inc.report else None
                    with self.lock:
                        inc.cost["bots_usd"] = round(cost, 4)
                        if report:
                            inc.report = report
                            self.event(f"report {report['name']} delivered")
            except Exception as exc:
                log.warning("control plane job %s: %s", job[0], exc)

    def _supply_waiting(self):
        """A restock already asked for and not decided: the supply rover's pilot or the restock bot waits."""
        with self.lock:
            pilots = {"pilot-" + vid for vid, st in self.states.items() if st.get("role") == "supply"}
        try:
            return any(t.get("botId") in pilots | {self.dispatch.logistics} for t in self.cp.waiting())
        except Exception as exc:
            log.warning("waiting tasks: %s", exc)
            return False

    def start_incident(self, report):
        with self.lock:
            if self.incident and self.incident.status == "searching":
                raise ValueError(f"a search is on: {self.incident.id}")
            inc = incident.Incident(report, self.coast)
            self.incident = inc
            self.last_costs = 0.0
            self.event(f"MAYDAY relay: {inc.vessel} lost {inc.count} containers at {inc.lost['lat']:.4f}, {inc.lost['lon']:.4f}")
            d = inc.sea.datum()
            event = {"incident": inc.id, "vessel": inc.vessel, "count": inc.count, "lat": inc.lost["lat"], "lon": inc.lost["lon"],
                     "lost_at": incident.iso(inc.lost_at), "reported_at": incident.iso(inc.started_at),
                     "wind": f"from {inc.wind['from_deg']:.0f} deg at {inc.wind['speed_m_s']:.0f} m/s",
                     "current": f"to {inc.current['to_deg']:.0f} deg at {inc.current['speed_m_s']:.2f} m/s",
                     "datum": f"{d['lat']:.5f}, {d['lon']:.5f}, radius {d['radius_m']} m, drifting to {d['drift']['to_deg']} deg at {d['drift']['speed_m_s']} m/s"}
        self.cp_jobs.put(("fire", "incident", event, inc.id, None))
        return {"incident": inc.id, "total": inc.count, "datum": {"lat": d["lat"], "lon": d["lon"], "radius_m": d["radius_m"]}}

    def end_incident(self, reason):
        with self.lock:
            if not self.incident or self.incident.status != "searching":
                raise ValueError("no search is on")
            self.incident.end(reason)
            self.event(f"search {self.incident.id} ended: {reason}")
            return {"incident": self.incident.id, "found": self.incident.found(), "total": self.incident.count}

    # Hooks for vision.py, called from its workers.
    def searching(self):
        with self.lock:
            return self.incident is not None and self.incident.status == "searching"

    def floaters(self):
        with self.lock:
            return self.incident.floaters() if self.incident else []

    def observe(self, vehicle, kind, alt_m, t, containers, bbox, photo_id, cost):
        with self.lock:
            if not self.incident:
                return []
            self.incident.cost["vision_usd"] = round(self.incident.cost["vision_usd"] + cost, 5)
            seen = self.incident.observe(vehicle, kind, alt_m, t, containers, bbox, photo_id)
            for d in seen:
                if d["container"]:
                    st = self.incident.state[d["container"]]
                    if st["confirmed_by"] == vehicle and st["confirmed_at"] == t:
                        self.event(f"{vehicle} confirmed a container ({self.incident.found()}/{self.incident.count})")
            return seen

    def targets(self):
        """What the vehicles aim a pattern at: the search's targets and the ships AIS hears."""
        with self.lock:
            targets = self.incident.targets(time.time()) if self.incident else {}
        return {"targets": targets, "ships": self.ais.ships()}

    def route_of(self, vehicle, args):
        """The route a pending run_mission would fly, for the phone page: [[lat, lon]], metres."""
        with self.lock:
            state = self.states.get(vehicle) or {}
            home = state.get("home")
            targets = self.incident.targets(time.time()) if self.incident else {}
        ships = self.ais.ships()
        if not home:
            return None, 0
        try:
            if args.get("deliveries"):
                pts = [(self.stations[d["station"]]["lat"], self.stations[d["station"]]["lon"]) for d in args["deliveries"] if d.get("station") in self.stations]
            else:
                waypoints = args.get("waypoints")
                if args.get("pattern"):
                    ne = patterns.relative(home, targets, ships)
                    speed = args.get("speed_m_s") if isinstance(args.get("speed_m_s"), (int, float)) else 10
                    waypoints, _ = patterns.expand(args["pattern"], ne, speed, 24)
                pts = [geo.point_at(home["lat"], home["lon"], float(w["north_m"]), float(w["east_m"])) for w in waypoints or []]
        except (patterns.PatternError, KeyError, TypeError, ValueError):
            return None, 0
        route = [[home["lat"], home["lon"]]] + [[round(a, 6), round(b, 6)] for a, b in pts] + [[home["lat"], home["lon"]]]
        return route, approvals.distance(route)

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
            "simulation": True,
            "incident": self.incident.snapshot(time.time()) if self.incident else None,
            "ships": self.ais.ships(),
            "ais": self.ais.summary(),
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
        } | ({"ships": self._ships_view(snap["ships"])} if snap["ships"] else {}) | ({"incident": self._incident_view(snap)} if self.incident else {})

    def _ships_view(self, ships):
        """The ships nearest the depot, for the bots: who, where, how fast, which way."""
        def far(s):
            return math.hypot(*geo.offset_m(self.depot["lat"], self.depot["lon"], s["lat"], s["lon"]))
        out = []
        for s in sorted(ships, key=far)[:SHIPS_FOR_BOTS]:
            out.append({"mmsi": s["mmsi"], "name": s.get("name", ""), "type": s.get("type_name", ""), "length_m": s.get("length_m"),
                        "lat": round(s["lat"], 5), "lon": round(s["lon"], 5),
                        "speed_kn": s.get("sog"), "course_deg": None if s.get("cog") is None else round(s["cog"])})
        return out

    def _incident_view(self, snap):
        """The incident for the bots, with how far each vehicle is from each target."""
        now = time.time()
        view = self.incident.bot_view(now)
        targets = self.incident.targets(now)
        homes = {s.get("vehicle"): s.get("home") for s in snap["vehicles"] if s.get("home") and s.get("role", "patrol") != "supply"}

        def distances(lat, lon):
            return {vid: round(math.hypot(*geo.offset_m(h["lat"], h["lon"], lat, lon))) for vid, h in homes.items()}
        if "datum" in targets:
            view["datum"]["distance_from_home_m"] = distances(targets["datum"]["lat"], targets["datum"]["lon"])
        for s in view["open_sightings"]:
            t = targets.get(s["id"])
            if t:
                s.update(lat=round(t["lat"], 5), lon=round(t["lon"], 5), distance_from_home_m=distances(t["lat"], t["lon"]))
        return view

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
        if url.path == "/api/targets" and self.crew_api:
            return self._send(200, json.dumps(self.base.targets()).encode())
        if url.path == "/api/approvals" and not self.crew_api:
            if not self.base.approvals.authorised(self.headers.get("X-Approver-Code", "")):
                return self._send(401, b'{"error": "the approver code is required"}')
            try:
                return self._send(200, json.dumps(self.base.approvals.pending()).encode())
            except Exception as exc:
                return self._send(502, json.dumps({"error": str(exc)[:200]}).encode())
        if url.path == "/api/incident/report" and not self.crew_api:
            return self._report()
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
        name = PAGES.get(url.path, url.path.lstrip("/"))
        path = os.path.realpath(os.path.join(STATIC, name))
        if not path.startswith(STATIC + os.sep) or not os.path.isfile(path):
            return self._send(404, b"not found\n", "text/plain")
        with open(path, "rb") as f:
            self._send(200, f.read(), TYPES.get(os.path.splitext(path)[1], "application/octet-stream"), "max-age=60")

    def _report(self):
        inc = self.base.incident
        if not inc or not inc.report:
            return self._send(404, b"no report yet\n", "text/plain")
        try:
            body, _ = self.base.cp.artifact(inc.report["execution"], inc.report["name"])
        except Exception as exc:
            return self._send(502, f"the control plane: {exc}\n".encode(), "text/plain")
        return self._send(200, body, "text/markdown; charset=utf-8")

    def _body(self):
        length = int(self.headers.get("Content-Length") or 0)
        if length > MAX_BODY:
            raise ValueError("body too large")
        raw = self.rfile.read(length) if length else b"{}"
        value = json.loads(raw or b"{}")
        if not isinstance(value, dict):
            raise ValueError("a JSON object")
        return value

    def _incident_allowed(self):
        path = self.base.incident_secret_file
        if not path:
            return False
        try:
            with open(path) as f:
                secret = f.read().strip()
        except OSError:
            return False
        presented = self.headers.get("X-Incident-Secret", "")
        return bool(secret and presented) and hmac.compare_digest(presented.encode(), secret.encode())

    def do_POST(self):
        url = urllib.parse.urlparse(self.path)
        try:
            if self.crew_api:
                return self._send(404, b"not found\n", "text/plain")
            if url.path in ("/api/incident", "/api/incident/end"):
                if not self._incident_allowed():
                    return self._send(403, b'{"error": "the incident secret is required"}')
                body = self._body()
                if url.path == "/api/incident":
                    return self._send(201, json.dumps(self.base.start_incident(body)).encode())
                return self._send(200, json.dumps(self.base.end_incident(str(body.get("reason", "ended by the operator"))[:80])).encode())
            parts = url.path.strip("/").split("/")
            if len(parts) == 4 and parts[:2] == ["api", "approvals"] and parts[3] in ("approve", "deny"):
                if not self.base.approvals.authorised(self.headers.get("X-Approver-Code", "")):
                    return self._send(401, b'{"ok": false, "error": "the approver code is required"}')
                call = urllib.parse.parse_qs(url.query).get("call", [""])[0]
                try:
                    self.base.approvals.decide(parts[2], parts[3], call)
                except control_plane.APIError as exc:
                    return self._send(409 if exc.status == 409 else 502, json.dumps({"ok": False, "error": exc.message}).encode())
                return self._send(200, b'{"ok": true}')
            return self._send(404, b"not found\n", "text/plain")
        except ValueError as exc:
            return self._send(400, json.dumps({"error": str(exc)[:200]}).encode())

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
