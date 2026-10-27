"""Containers overboard: one incident, from the vessel's call to the report.

A vessel calls in (POST /api/incident): it lost N containers at a position
and a time. The base starts the sea (drift.py) at the loss, runs it to now,
and fires the dispatcher's `incident` webhook. From then on, every second:

  the sea      the containers drift, some reach the shore; the planner's
               datum moves with its cloud of particles
  the photos   vision.py asks for the containers inside each photo's
               frame, draws them in (sprites.py) and hands back what the
               model saw; a sighting from high up (the plane) marks the
               container sighted, one from low down (a quad) confirms it
  dispatching  new sightings fire the dispatcher's `sightings` webhook, at
               most every DISPATCH_EVERY seconds, so a quad goes to look
  the end      every container confirmed or ashore, the time limit, or
               POST /api/incident/end: the dispatcher's `report` webhook
               fires with the numbers, and its report is kept

The truth (where each container really is) never reaches a bot: they read
the datum, the drift and the sightings. It is drawn on the map, marked as
the simulation's.
"""

import logging
import math
import threading
import time

import drift
import geo
import sprites

log = logging.getLogger("incident")

HIGH_M = 60             # a sighting from this high or higher only sights
DISPATCH_EVERY = 75     # seconds between two sightings webhooks
RETASK_AFTER = 480      # a sighting no one confirmed is sent again after this
SETTLE = 20             # a fresh sighting waits this long: a quad close by may confirm it first
TRACK_EVERY = 30        # seconds between two points of the datum's track


def iso(t):
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(t))


def _number(report, key, default, lo, hi):
    value = report.get(key, default)
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not lo <= value <= hi:
        raise ValueError(f"{key}: a number from {lo} to {hi}")
    return float(value)


class Incident:
    """One search. Its methods are called with the base's lock held."""

    def __init__(self, report, coast, now=None, seed=None):
        now = time.time() if now is None else now
        vessel = str(report.get("vessel", "")).strip()[:60]
        if not vessel:
            raise ValueError("vessel: the calling vessel's name")
        self.count = int(_number(report, "count", 14, 1, 40))
        lat = _number(report, "lat", None, -90, 90) if "lat" in report else None
        lon = _number(report, "lon", None, -180, 180) if "lon" in report else None
        if lat is None or lon is None:
            raise ValueError("lat, lon: where the containers went overboard")
        minutes_ago = _number(report, "lost_minutes_ago", 20, 0, 180)
        self.id = str(report.get("id") or "inc-" + time.strftime("%Y%m%d-%H%M%S", time.gmtime(now)))[:40]
        self.vessel = vessel
        self.started_at = now
        self.lost_at = now - minutes_ago * 60
        self.lost = {"lat": lat, "lon": lon}
        self.wind = {"from_deg": _number(report, "wind_from_deg", 250, 0, 360), "speed_m_s": _number(report, "wind_m_s", 10, 0, 40)}
        self.current = {"to_deg": _number(report, "current_to_deg", 25, 0, 360), "speed_m_s": _number(report, "current_m_s", 0.25, 0, 3)}
        self.max_s = _number(report, "max_minutes", 40, 5, 240) * 60
        self.sea = drift.Drift(lat, lon, self.wind["from_deg"], self.wind["speed_m_s"], self.current["to_deg"],
                               self.current["speed_m_s"], coast, seed=seed)
        self.sea.spill(self.count, _number(report, "vessel_heading_deg", 220, 0, 360), _number(report, "spread_m", 600, 0, 5000))
        self.sea.advance(now - self.lost_at)
        self.last_step = now
        self.status = "searching"
        self.ended_at = None
        self.end_reason = ""
        self.state = {c.id: {"state": "missing", "sighted_by": None, "sighted_at": None, "confirmed_by": None, "confirmed_at": None}
                      for c in self.sea.containers}
        self.sightings = []           # one per container seen, then one per unmatched spot
        self.track = [self._datum_point()]
        self.last_track = now
        self.last_dispatch = 0.0
        self.tasked = {}              # sighting id -> when a sightings webhook named it
        self.tasks = []               # tasks the incident's webhooks created
        self.report_task = None
        self.report = None
        self.cost = {"bots_usd": 0.0, "vision_usd": 0.0}
        self.photos = 0

    # -- the sea -------------------------------------------------------------

    def _datum_point(self):
        d = self.sea.datum()
        return [round(d["lat"], 6), round(d["lon"], 6)]

    def tick(self, now):
        """Advance the sea to now; say whether the search just ended."""
        if self.status != "searching":
            return False
        dt = now - self.last_step
        if dt > 0:
            self.sea.advance(dt, dt=min(dt, 5.0))
            self.last_step = now
        for c in self.sea.containers:
            if c.ashore and self.state[c.id]["state"] in ("missing", "sighted"):
                self.state[c.id]["state"] = "ashore"
        if now - self.last_track >= TRACK_EVERY:
            self.track.append(self._datum_point())
            del self.track[:-240]
            self.last_track = now
        settled = all(s["state"] in ("confirmed", "ashore") for s in self.state.values())
        if settled or now - self.started_at >= self.max_s:
            self.end("every container found or ashore" if settled else "time limit", now)
            return True
        return False

    def end(self, reason, now=None):
        if self.status == "searching":
            self.status = "ended"
            self.ended_at = time.time() if now is None else now
            self.end_reason = reason

    def floaters(self):
        """(id, lat, lon, heading, length, colour) of each container, for the sprites."""
        out = []
        for c in self.sea.containers:
            lat, lon = self.sea.position(c.north, c.east)
            out.append((c.id, lat, lon, c.heading, c.length_m, c.colour))
        return out

    # -- what the photos saw -------------------------------------------------

    def observe(self, vehicle, kind, alt_m, t, detections, bbox_merc, photo_id, yaw=None):
        """Map the model's containers in one photo to the sea. Returns them annotated.

        yaw None: the photo is north up (an aerial photo of the frame). A
        number: the camera is fixed to the vehicle, the image's top is its bow
        at that heading (degrees from north, clockwise), so a detection is
        turned by it about the frame's centre."""
        if self.status != "searching":
            return []
        self.photos += 1
        x1, y1, x2, y2 = bbox_merc
        width_m = (x2 - x1) * math.cos(math.radians(self.lost["lat"]))
        radius = max(25.0, 0.1 * width_m)
        confirms = kind == "quad" and alt_m < HIGH_M
        out = []
        for det in detections:
            if yaw is None:
                mx, my = x1 + det["x"] * (x2 - x1), y2 - det["y"] * (y2 - y1)
                lat, lon = merc_to_latlon(mx, my)
            else:
                lat, lon = self._turned(det["x"], det["y"], bbox_merc, yaw)
            best, best_d = None, radius
            for c in self.sea.containers:
                clat, clon = self.sea.position(c.north, c.east)
                d = math.hypot(*geo.offset_m(lat, lon, clat, clon))
                if d <= best_d:
                    best, best_d = c, d
            out.append({"x": round(det["x"], 3), "y": round(det["y"], 3), "container": best.id if best else None})
            north, east = geo.offset_m(self.lost["lat"], self.lost["lon"], lat, lon)
            if best is None and self.sea.on_land(north, east):
                continue   # something on the beach or the quay: not a container afloat
            self._sighting(best, lat, lon, t, vehicle, photo_id, confirms)
        return out

    @staticmethod
    def _turned(fx, fy, bbox_merc, yaw):
        """(lat, lon) of a point at fractions (fx, fy) of a photo whose top is at heading yaw."""
        x1, y1, x2, y2 = bbox_merc
        clat, clon = merc_to_latlon((x1 + x2) / 2, (y1 + y2) / 2)
        k = math.cos(math.radians(clat))
        width_m, height_m = (x2 - x1) * k, (y2 - y1) * k
        right_m = (fx - 0.5) * width_m
        up_m = (0.5 - fy) * height_m
        psi = math.radians(yaw)
        east = right_m * math.cos(psi) + up_m * math.sin(psi)
        north = -right_m * math.sin(psi) + up_m * math.cos(psi)
        return geo.point_at(clat, clon, north, east)

    def _sighting(self, container, lat, lon, t, vehicle, photo_id, confirms):
        if container is None:
            for s in self.sightings:
                if s["container"] is None and math.hypot(*geo.offset_m(s["lat"], s["lon"], lat, lon)) < 50:
                    s.update(lat=lat, lon=lon, t=t, vehicle=vehicle, photo=photo_id)
                    return
            self.sightings.append({"id": f"S{len(self.sightings) + 1}", "t": t, "vehicle": vehicle, "photo": photo_id,
                                   "lat": lat, "lon": lon, "container": None, "confirmed": False})
            return
        st = self.state[container.id]
        found = next((s for s in self.sightings if s["container"] == container.id), None)
        if found is None:
            found = {"id": f"S{len(self.sightings) + 1}", "container": container.id, "confirmed": False}
            self.sightings.append(found)
        found.update(t=t, vehicle=vehicle, photo=photo_id, lat=lat, lon=lon)
        if confirms and st["state"] != "confirmed":
            st.update(state="confirmed", confirmed_by=vehicle, confirmed_at=t)
            found["confirmed"] = True
            log.info("%s confirmed %s (%s)", vehicle, container.id, found["id"])
        elif st["state"] == "missing":
            st.update(state="sighted", sighted_by=vehicle, sighted_at=t)
            log.info("%s sighted %s (%s)", vehicle, container.id, found["id"])
        if confirms and not st["sighted_by"]:
            st.update(sighted_by=vehicle, sighted_at=t)

    def predicted(self, s, now):
        """Where a sighting should be now: seen there, carried by the mean drift since."""
        vn, ve = self.sea.mean_velocity()
        age = max(0.0, now - s["t"])
        return geo.point_at(s["lat"], s["lon"], vn * age, ve * age)

    # -- dispatching ---------------------------------------------------------

    def open_sightings(self, now):
        """Sightings no quad confirmed yet and not sent out recently."""
        out = []
        for s in self.sightings:
            if s["confirmed"]:
                continue
            if s["container"] and self.state[s["container"]]["state"] in ("confirmed", "ashore"):
                continue
            if now - self.tasked.get(s["id"], 0) < RETASK_AFTER or now - s["t"] < SETTLE:
                continue
            out.append(s)
        return out

    def due_dispatch(self, now):
        if self.status != "searching" or now - self.last_dispatch < DISPATCH_EVERY:
            return None
        todo = self.open_sightings(now)
        if not todo:
            return None
        self.last_dispatch = now
        for s in todo:
            self.tasked[s["id"]] = now
        ids = ", ".join(s["id"] for s in todo)
        return {"incident": self.id, "sightings": ids, "count": len(todo), "found": self.found(), "total": self.count,
                "minutes": round((now - self.started_at) / 60)}

    # -- counts and views ----------------------------------------------------

    def found(self):
        return sum(1 for s in self.state.values() if s["state"] == "confirmed")

    def sighted(self):
        return sum(1 for s in self.state.values() if s["sighted_by"] or s["state"] == "confirmed")

    def ashore(self):
        return sum(1 for s in self.state.values() if s["state"] == "ashore")

    def elapsed(self, now):
        return (self.ended_at or now) - self.started_at

    def targets(self, now):
        """What a pilot may aim a pattern at: the datum and the open sightings, as of now."""
        if self.status != "searching":
            return {}
        d = self.sea.datum()
        out = {"datum": {"lat": d["lat"], "lon": d["lon"], "radius_m": d["radius_m"], "drift": d["drift"],
                         "about": f"the most probable position of the {self.count} containers now"}}
        for s in self.sightings:
            if s["confirmed"] or (s["container"] and self.state[s["container"]]["state"] in ("confirmed", "ashore")):
                continue
            lat, lon = self.predicted(s, now)
            out[s["id"]] = {"lat": lat, "lon": lon, "seen_s_ago": round(now - s["t"]), "by": s["vehicle"],
                            "about": f"a floating container {s['vehicle']} saw, carried by the drift since"}
        return out

    def bot_view(self, now):
        """The incident as the dispatcher reads it in fleet.json: no truth in it."""
        d = self.sea.datum()
        view = {
            "id": self.id, "status": self.status, "vessel": self.vessel, "total": self.count,
            "found": self.found(), "sighted": self.sighted(), "ashore": self.ashore(),
            "minutes": round(self.elapsed(now) / 60, 1),
            "lost": dict(self.lost, at=iso(self.lost_at)),
            "wind": self.wind, "current": self.current,
            "datum": {"lat": round(d["lat"], 5), "lon": round(d["lon"], 5), "radius_m": d["radius_m"], "drift": d["drift"]},
            "open_sightings": [{"id": s["id"], "by": s["vehicle"], "seen_s_ago": round(now - s["t"])}
                               for s in self.sightings if not s["confirmed"] and not (s["container"] and self.state[s["container"]]["state"] in ("confirmed", "ashore"))],
        }
        if self.status == "ended":
            view["end"] = self.end_reason
            view["log"] = self.log_lines()
        return view

    def log_lines(self):
        lines = []
        for c in self.sea.containers:
            st = self.state[c.id]
            text = {"confirmed": f"confirmed by {st['confirmed_by']} at {iso(st['confirmed_at'] or 0)[11:16]}Z",
                    "ashore": "washed ashore", "sighted": f"sighted by {st['sighted_by']} at {iso(st['sighted_at'] or 0)[11:16]}Z, not confirmed",
                    "missing": "not found"}[st["state"]]
            lines.append(f"{c.id} ({'40' if c.length_m > 10 else '20'} ft): {text}")
        return lines

    def snapshot(self, now):
        d = self.sea.datum()
        containers = []
        for c in self.sea.containers:
            lat, lon = self.sea.position(c.north, c.east)
            vn, ve = c.velocity
            containers.append(dict(self.state[c.id], id=c.id, lat=lat, lon=lon, heading_deg=round(c.heading), length_m=c.length_m,
                                   colour=c.colour, drift={"to_deg": round(drift.bearing(vn, ve)), "speed_m_s": round(math.hypot(vn, ve), 2)}))
        sightings = []
        for s in self.sightings:
            lat, lon = self.predicted(s, now)
            sightings.append(dict(s, now={"lat": lat, "lon": lon}))
        cost = dict(self.cost, total_usd=round(self.cost["bots_usd"] + self.cost["vision_usd"], 4))
        return {
            "id": self.id, "title": f"{self.count} containers overboard", "vessel": self.vessel,
            "reported_at": self.started_at, "lost_at": self.lost_at, "lost": self.lost, "total": self.count,
            "status": self.status, "started_at": self.started_at, "ended_at": self.ended_at, "end_reason": self.end_reason,
            "elapsed_s": round(self.elapsed(now), 1), "wind": self.wind, "current": self.current,
            "drift": d["drift"], "datum": {"lat": d["lat"], "lon": d["lon"], "radius_m": d["radius_m"]},
            "datum_track": self.track, "containers": containers, "sightings": sightings,
            "found": self.found(), "sighted": self.sighted(), "ashore": self.ashore(),
            "cost": cost, "report": self.report, "photos_analysed": self.photos,
        }

    def report_event(self, now):
        """What the report webhook carries: the numbers and the log."""
        return {"incident": self.id, "vessel": self.vessel, "total": self.count, "found": self.found(), "sighted": self.sighted(),
                "ashore": self.ashore(), "missing": self.count - self.found() - self.ashore(),
                "minutes": round(self.elapsed(now) / 60, 1), "end": self.end_reason,
                "cost_usd": round(self.cost["bots_usd"] + self.cost["vision_usd"], 3), "photos": self.photos,
                "lost_at": iso(self.lost_at), "reported_at": iso(self.started_at),
                "lat": self.lost["lat"], "lon": self.lost["lon"], "log": "; ".join(self.log_lines())}


def merc_to_latlon(x, y):
    r = 6378137.0
    lon = math.degrees(x / r)
    lat = math.degrees(2 * math.atan(math.exp(y / r)) - math.pi / 2)
    return lat, lon


class Dispatcher:
    """Fires the incident's webhooks and keeps the costs, off the base's lock.

    `cp` is a control_plane.ControlPlane or None (then nothing is fired and
    the counters show the vision's cost only).
    """

    def __init__(self, cp, bot="dispatcher", logistics="logistics", reporter="reporter"):
        self.cp = cp
        self.bot = bot
        self.logistics = logistics
        self.reporter = reporter
        self.lock = threading.Lock()
        self.costs = {}               # execution id -> (final, cost)
        self.last_restock = 0.0

    def fire(self, trigger, event, key, bot=None):
        if not (self.cp and self.cp.enabled):
            log.info("no control plane: %s not fired", trigger)
            return None
        try:
            answer = self.cp.fire(bot or self.bot, trigger, event, key)
            task = (answer or {}).get("task", {}).get("id")
            log.info("fired %s/%s: %s", bot or self.bot, trigger, task)
            return task
        except Exception as exc:
            log.warning("fire %s/%s: %s", bot or self.bot, trigger, exc)
            return None

    def bots_cost(self, since):
        """The model cost of every task started since `since` (this namespace's fleet)."""
        if not (self.cp and self.cp.enabled):
            return 0.0
        total = 0.0
        for task in self.cp.tasks_since(iso(since - 5)):
            for exec_id in task.get("executionIds") or []:
                final, cost = self.costs.get(exec_id, (False, 0.0))
                if not final:
                    try:
                        e = self.cp.execution(exec_id)
                    except Exception:
                        continue
                    cost = float(((e or {}).get("usage") or {}).get("costUsd") or 0.0)
                    final = bool((e or {}).get("endedAt"))
                    self.costs[exec_id] = (final, cost)
                total += cost
        return total

    def report_of(self, task_id):
        """The report the report task delivered: (execution, artifact name) or None."""
        if not (self.cp and self.cp.enabled and task_id):
            return None
        for e in reversed(self.cp.executions(task_id) or []):
            for a in ((e.get("result") or {}).get("artifacts") or []):
                if a.get("stored") and a.get("name", "").endswith(".md"):
                    return {"task": task_id, "execution": e["id"], "name": a["name"], "url": "api/incident/report"}
        return None
