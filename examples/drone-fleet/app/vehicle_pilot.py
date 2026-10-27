#!/usr/bin/env python3
"""The service beside one vehicle: a MAVLink link, its ground crew and one tool.

It runs in the vehicle's pod, next to the vehicle (ArduPilot SITL: a quad X,
a rover or a plane, behind mavlink-router) and the AXLR Bots sidecar, and
offers:

  <TELEMETRY_FILE>          every second: what the bot reads (its workspace),
                            and the same file in every other bot's workspace
                            under TELEMETRY_DIR (the bots a person adds on
                            this node read what the pilot reads)
  GET  <STATE_LISTEN>/state the same plus the events, for the fleet map
  POST <MCP_LISTEN>/mcp     one MCP tool, run_mission, for the sidecar's AXLR,
                            on loopback only

The safety limits live here, not in the model: a route outside the
geofence, too low, too high, too long or too fast is refused before the
vehicle moves, and so is a vehicle that is not ready. After every mission
the vehicle goes to its maintenance station: it waits for a bay and the
station's stock (the fleet base hands them out), is inspected, and gets a
fresh battery pack; only then is it ready again, so the dispatcher sends the
spares in the meantime. A supply rover (ROLE=supply) takes no waypoints: it
drives from the depot to the stations its mission names, with their cargo.

During a search the fleet base names targets (the datum, each sighting);
the pilot keeps them in telemetry.json in metres from home, and run_mission
takes a search pattern aimed at one of them instead of waypoints
(patterns.py): the companion computer draws the pattern, leads the drift,
and checks it against the same limits. The ships AIS hears are targets too,
by MMSI or name ("go to ship X": an orbit round it, led by its course and
speed); one beyond the geofence is refused with its distance.

A rover takes no waypoints at all: it drives routes precomputed from the
port's map (ROUTES_FILE, written by port3d/routes.py) along its roads and
yards, clear of sheds, container blocks and water, with turns a car can
drive. Its bot picks one by name; a supply run is the route through the
stations its deliveries name, in that order.
"""

import collections
import json
import logging
import math
import os
import re
import threading
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from pymavlink import mavutil

import geo
import mcp_http
import patterns

log = logging.getLogger("vehicle-pilot")
mav = mavutil.mavlink

# What each kind may do unless the environment narrows it.
KINDS = {
    "quad": {"radius": 1500, "alt": (10, 80), "speed": (3, 15)},
    "rover": {"radius": 1500, "alt": (0, 0), "speed": (1, 8)},
    "plane": {"radius": 4000, "alt": (60, 200), "speed": (16, 25)},
}

GPS_FIX = {0: "no GPS", 1: "no fix", 2: "2D", 3: "3D", 4: "DGPS", 5: "RTK float", 6: "RTK fixed"}

# What the vehicle carries, as the chart describes it (shown, not simulated).
HARDWARE = {k: v for k, v in (
    ("autopilot", os.environ.get("HW_AUTOPILOT", "")),
    ("gnss", os.environ.get("HW_GNSS", "")),
    ("lidar", os.environ.get("HW_LIDAR", "")),
    ("camera", os.environ.get("HW_CAMERA", "")),
    ("optical_flow", os.environ.get("HW_FLOW", "")),
) if v}

ROUTES_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "routes.json")
MAX_ROUTE_POINTS = 400
SHIPS_SHOWN = 20         # the nearest ships in telemetry.json; the tool knows every one AIS hears

INSPECTION = {
    "quad": ["motors", "propellers", "frame", "GPS", "compass", "radio link", "battery cells"],
    "rover": ["wheels", "steering", "chassis", "GPS", "compass", "radio link", "battery cells"],
    "plane": ["propeller", "control surfaces", "landing gear", "pitot", "GPS", "radio link", "battery cells"],
}


def env_float(name, default):
    return float(os.environ.get(name, default))


def env_listen(name, default):
    host, _, port = os.environ.get(name, default).rpartition(":")
    return host, int(port)


def now_iso():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


class Limits:
    """The geofence and the envelope every mission must fit."""

    def __init__(self, kind):
        k = KINDS[kind]
        self.max_radius_m = env_float("MAX_RADIUS_M", k["radius"])
        self.min_alt_m = env_float("MIN_ALT_M", k["alt"][0])
        self.max_alt_m = env_float("MAX_ALT_M", k["alt"][1])
        self.min_speed = env_float("MIN_SPEED_M_S", k["speed"][0])
        self.max_speed = env_float("MAX_SPEED_M_S", k["speed"][1])
        self.max_waypoints = int(env_float("MAX_WAYPOINTS", 20))
        self.min_battery = env_float("MIN_BATTERY_PCT", 50)

    def as_dict(self):
        out = {
            "max_radius_m": self.max_radius_m,
            "max_waypoints": self.max_waypoints,
            "speed_m_s": [self.min_speed, self.max_speed],
            "min_battery_pct": self.min_battery,
        }
        if self.max_alt_m > 0:
            out["alt_m"] = [self.min_alt_m, self.max_alt_m]
        return out


class Vehicle:
    """One MAVLink link: a reader thread keeps the state and wakes waiters."""

    GCS_SYSTEM = 245
    GCS_COMPONENT = 190

    def __init__(self, url):
        self.url = url
        self.master = None
        self.target = (1, 1)
        self.cond = threading.Condition()
        self.inbox = collections.deque(maxlen=400)
        self.seq = 0
        self.state = {
            "connected": False, "armed": False, "mode": "", "lat": None, "lon": None,
            "alt_m": 0.0, "heading": 0.0, "groundspeed": 0.0,
            "battery_pct": None, "voltage": None, "gps_fix": 0, "satellites": 0,
            "home": None, "mission_seq": 0, "last_heartbeat": 0.0,
            "lidar_down_m": None, "proximity_m": None, "lidars": {}, "flow_quality": None, "photos": 0,
            "hdop": None,
        }
        self.events = collections.deque(maxlen=30)
        self.shots = collections.deque(maxlen=300)
        self.photos = None    # a PhotoFetcher when the camera is the gz world's

    def event(self, text):
        log.info("%s", text)
        with self.cond:
            self.events.append({"t": time.time(), "text": text})

    def start(self):
        threading.Thread(target=self._run, name="mavlink", daemon=True).start()
        threading.Thread(target=self._heartbeat, name="gcs-heartbeat", daemon=True).start()

    def _connect(self):
        while True:
            try:
                master = mavutil.mavlink_connection(
                    self.url, source_system=self.GCS_SYSTEM,
                    source_component=self.GCS_COMPONENT, autoreconnect=True)
                hb = master.wait_heartbeat(timeout=30)
                if hb is not None:
                    self.target = (master.target_system, master.target_component or 1)
                    return master
                master.close()
            except Exception as exc:  # the router may not listen yet
                log.warning("no link to %s yet: %s", self.url, exc)
            time.sleep(3)

    def _run(self):
        self.master = self._connect()
        self.event("link up")
        self._request_streams()
        last_request = time.monotonic()
        while True:
            try:
                msg = self.master.recv_match(blocking=True, timeout=1)
            except Exception as exc:
                log.warning("read: %s", exc)
                time.sleep(1)
                continue
            if time.monotonic() - last_request > 10:
                # Ask again now and then: a restarted vehicle forgets.
                self._request_streams()
                last_request = time.monotonic()
            if msg is None or msg.get_type() == "BAD_DATA":
                self._mark_stale()
                continue
            self._update(msg)
            with self.cond:
                self.seq += 1
                self.inbox.append((self.seq, msg))
                self.cond.notify_all()

    def _heartbeat(self):
        while True:
            if self.master is not None:
                try:
                    self.master.mav.heartbeat_send(mav.MAV_TYPE_GCS, mav.MAV_AUTOPILOT_INVALID, 0, 0, mav.MAV_STATE_ACTIVE)
                except Exception:
                    pass
            time.sleep(1)

    def _request_streams(self):
        ts, tc = self.target
        self.master.mav.request_data_stream_send(ts, tc, mav.MAV_DATA_STREAM_ALL, 4, 1)
        if self.state["home"] is None:
            self.command(mav.MAV_CMD_GET_HOME_POSITION)

    def _mark_stale(self):
        with self.cond:
            if self.state["connected"] and time.time() - self.state["last_heartbeat"] > 5:
                self.state["connected"] = False

    def _update(self, msg):
        kind = msg.get_type()
        if msg.get_srcSystem() != self.target[0]:
            return
        with self.cond:
            s = self.state
            if kind == "HEARTBEAT" and msg.get_srcComponent() == 1:
                s["connected"] = True
                s["last_heartbeat"] = time.time()
                s["armed"] = bool(msg.base_mode & mav.MAV_MODE_FLAG_SAFETY_ARMED)
                s["mode"] = mavutil.mode_string_v10(msg)
            elif kind == "GLOBAL_POSITION_INT":
                s["lat"], s["lon"] = msg.lat / 1e7, msg.lon / 1e7
                s["alt_m"] = msg.relative_alt / 1000.0
                if msg.hdg != 65535:
                    s["heading"] = msg.hdg / 100.0
            elif kind == "VFR_HUD":
                s["groundspeed"] = msg.groundspeed
            elif kind == "SYS_STATUS":
                s["battery_pct"] = None if msg.battery_remaining < 0 else msg.battery_remaining
                s["voltage"] = round(msg.voltage_battery / 1000.0, 2)
            elif kind == "GPS_RAW_INT":
                s["gps_fix"], s["satellites"] = msg.fix_type, msg.satellites_visible
                s["hdop"] = None if msg.eph == 65535 else msg.eph / 100.0
            elif kind == "DISTANCE_SENSOR":
                metres = msg.current_distance / 100.0
                if msg.orientation == mav.MAV_SENSOR_ROTATION_PITCH_270:
                    s["lidar_down_m"] = metres
                elif msg.orientation in (mav.MAV_SENSOR_ROTATION_NONE, mav.MAV_SENSOR_ROTATION_YAW_180):
                    # The forward and aft lidars: the nearer one, so an obstacle shows.
                    s["lidars"][msg.orientation] = metres
                    s["proximity_m"] = min(s["lidars"].values())
            elif kind == "RANGEFINDER":
                s["lidar_down_m"] = round(msg.distance, 2)
            elif kind == "OPTICAL_FLOW":
                s["flow_quality"] = msg.quality
            elif kind == "CAMERA_FEEDBACK":
                s["photos"] += 1
                # Where each photo was taken: the ground station analyses them.
                shot = {"n": s["photos"], "lat": msg.lat / 1e7, "lon": msg.lng / 1e7,
                        "alt_m": round(msg.alt_rel, 1), "yaw": round(msg.yaw), "t": time.time()}
                if self.photos is not None:
                    # Taken by the simulator's camera: fetched in a moment ("" if it fails).
                    shot["photo"] = None
                    self.photos.take(shot)
                self.shots.append(shot)
            elif kind == "HOME_POSITION":
                s["home"] = {"lat": msg.latitude / 1e7, "lon": msg.longitude / 1e7}
            elif kind == "MISSION_CURRENT":
                s["mission_seq"] = msg.seq
            elif kind == "STATUSTEXT":
                text = msg.text.rstrip("\x00").strip()
                if text and msg.severity <= mav.MAV_SEVERITY_WARNING:
                    self.events.append({"t": time.time(), "text": text})

    def snapshot(self):
        with self.cond:
            out = dict(self.state)
            out["lidars"] = dict(self.state["lidars"])
            return out

    def mark(self):
        with self.cond:
            return self.seq

    def wait_for(self, types, after, predicate=lambda m: True, timeout=10.0):
        """The first message of one of types received after mark `after`."""
        deadline = time.monotonic() + timeout
        with self.cond:
            checked = after
            while True:
                for seq, msg in self.inbox:
                    if seq <= checked:
                        continue
                    checked = seq
                    if msg.get_type() in types and msg.get_srcSystem() == self.target[0] and predicate(msg):
                        return msg
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return None
                self.cond.wait(remaining)

    def wait_state(self, predicate, timeout):
        deadline = time.monotonic() + timeout
        with self.cond:
            while not predicate(self.state):
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return False
                self.cond.wait(remaining)
            return True

    def command(self, command, *params):
        ts, tc = self.target
        p = list(params) + [0] * (7 - len(params))
        self.master.mav.command_long_send(ts, tc, command, 0, *p)

    def command_ack(self, command, *params, timeout=5.0):
        mark = self.mark()
        self.command(command, *params)
        ack = self.wait_for({"COMMAND_ACK"}, mark, lambda m: m.command == command, timeout)
        return ack is not None and ack.result == mav.MAV_RESULT_ACCEPTED

    def set_mode(self, name, timeout=8.0):
        mode_id = self.master.mode_mapping().get(name)
        if mode_id is None:
            raise RuntimeError(f"the vehicle has no mode {name}")
        ts, _ = self.target
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            self.master.mav.set_mode_send(ts, mav.MAV_MODE_FLAG_CUSTOM_MODE_ENABLED, mode_id)
            if self.wait_state(lambda s: s["mode"] == name, 2.0):
                return
        raise RuntimeError(f"the vehicle did not switch to {name}")

    def arm(self, timeout=60.0):
        deadline = time.monotonic() + timeout
        while not self.snapshot()["armed"]:
            if time.monotonic() > deadline:
                raise RuntimeError("the vehicle did not arm (pre-arm checks)")
            self.command_ack(mav.MAV_CMD_COMPONENT_ARM_DISARM, 1)
            self.wait_state(lambda s: s["armed"], 3)

    def upload_mission(self, items, timeout=15.0):
        """MAVLink mission protocol: count, the vehicle asks for each item, ack."""
        ts, tc = self.target
        mark = self.mark()
        self.master.mav.mission_count_send(ts, tc, len(items), mav.MAV_MISSION_TYPE_MISSION)
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            msg = self.wait_for({"MISSION_REQUEST_INT", "MISSION_REQUEST", "MISSION_ACK"}, mark, timeout=deadline - time.monotonic())
            if msg is None:
                break
            mark = self.mark()
            if msg.get_type() == "MISSION_ACK":
                if msg.type == mav.MAV_MISSION_ACCEPTED:
                    return
                raise RuntimeError(f"the vehicle refused the mission (ack {msg.type})")
            item = items[msg.seq]
            self.master.mav.mission_item_int_send(
                ts, tc, msg.seq, item["frame"], item["command"], 0, 1,
                item.get("p1", 0), item.get("p2", 0), item.get("p3", 0), item.get("p4", 0),
                int(item.get("lat", 0) * 1e7), int(item.get("lon", 0) * 1e7), float(item.get("alt", 0)),
                mav.MAV_MISSION_TYPE_MISSION)
        raise RuntimeError("the mission upload timed out")


class PhotoFetcher:
    """The camera's pictures: on each CAMERA_FEEDBACK the latest nadir frame of
    the gz world (PHOTO_URL), kept as photos/<n>.jpg for the ground station."""

    KEEP = 200

    def __init__(self, url, folder, cond, get=None):
        self.url = url
        self.folder = folder
        self.cond = cond
        self.get = get or self._get
        self.queue = collections.deque()
        self.wake = threading.Event()
        os.makedirs(folder, exist_ok=True)

    def start(self):
        threading.Thread(target=self._run, name="photos", daemon=True).start()

    def take(self, shot):
        self.queue.append(shot)
        self.wake.set()

    def _get(self, url):
        with urllib.request.urlopen(url, timeout=1.5) as r:
            return r.read()

    def path(self, n):
        return os.path.join(self.folder, f"{int(n)}.jpg")

    def fetch(self, shot):
        try:
            image = self.get(self.url)
            with open(self.path(shot["n"]), "wb") as f:
                f.write(image)
            photo = f"/photos/{shot['n']}.jpg"
        except Exception as exc:
            log.warning("photo %d: %s", shot["n"], exc)
            photo = ""
        with self.cond:
            shot["photo"] = photo
        stale = self.path(shot["n"] - self.KEEP)
        if shot["n"] > self.KEEP and os.path.exists(stale):
            os.unlink(stale)

    def _run(self):
        while True:
            self.wake.wait()
            self.wake.clear()
            while self.queue:
                self.fetch(self.queue.popleft())


class GroundCrew:
    """After every mission: a bay at the station, an inspection, a battery swap.

    The fleet base hands out the bays and the station's stock (a charged pack
    and a part per service); without them the vehicle waits in the queue.
    """

    def __init__(self, vehicle, vehicle_id, kind, station, crew_url, inspect_s, swap_s):
        self.vehicle = vehicle
        self.vehicle_id = vehicle_id
        self.station = station
        self.crew_url = crew_url.rstrip("/")
        self.items = INSPECTION[kind]
        self.inspect_s = inspect_s
        self.swap_s = swap_s
        self.stage = None          # None, "queued", "inspection", "swap"
        self.since = 0.0
        self.queued_at = None
        self.reason = ""
        self.checklist = []
        self.last_ask = 0.0
        self.services = 0

    def begin(self):
        self.stage = "queued"
        self.queued_at = now_iso()
        self.since = time.monotonic()
        self.reason = ""
        self.vehicle.event(f"at station {self.station}: waiting for a bay")

    def _granted(self):
        """Ask the fleet base for a bay at this station (1 s apart)."""
        if not self.crew_url:
            return True
        if time.monotonic() - self.last_ask < 1:
            return False
        self.last_ask = time.monotonic()
        url = f"{self.crew_url}/api/crew/{self.vehicle_id}?station={self.station}"
        try:
            with urllib.request.urlopen(url, timeout=2) as response:
                answer = json.load(response)
        except Exception as exc:
            log.warning("crew: %s", exc)
            return False
        self.reason = answer.get("reason", "")
        return bool(answer.get("granted"))

    def tick(self, battery_pct):
        if self.stage == "queued":
            if self._granted():
                self.stage = "inspection"
                self.since = time.monotonic()
                self.checklist = []
                self.reason = ""
                self.vehicle.event(f"station {self.station}: inspection")
        elif self.stage == "inspection":
            done = int((time.monotonic() - self.since) / self.inspect_s * len(self.items))
            self.checklist = self.items[:min(done, len(self.items))]
            if done >= len(self.items):
                self.stage = "swap"
                self.since = time.monotonic()
                self.vehicle.event(f"inspection passed; battery swap ({battery_pct} % out)")
        elif self.stage == "swap":
            # ArduPilot's battery monitor takes the fresh pack (disarmed only).
            self.vehicle.command(mav.MAV_CMD_BATTERY_RESET, 0xFF, 100)
            if time.monotonic() - self.since >= self.swap_s and battery_pct is not None and battery_pct >= 99:
                self.stage = None
                self.queued_at = None
                self.services += 1
                self.vehicle.event("fresh battery: ready")

    def as_dict(self):
        out = {"station": self.station, "stage": self.stage, "services": self.services}
        if self.stage == "queued":
            out["queued_at"] = self.queued_at
            out["waiting_for"] = self.reason
        elif self.stage == "inspection":
            out["checked"] = self.checklist
            out["of"] = len(self.items)
        return out


class Pilot:
    """What the tool and the map see of one vehicle."""

    NAME = re.compile(r"^[A-Za-z0-9][A-Za-z0-9 _.-]{0,39}$")

    def __init__(self, vehicle_id, kind, vehicle, limits, crew, zones, telemetry_file, role="patrol", stations=(), capacity=(8, 10), routes=None,
                 telemetry_dir=""):
        self.vehicle_id = vehicle_id
        self.routes = routes or {}  # a patrol rover's: id -> route; a supply rover's: (station, …) -> route
        self.kind = kind
        self.role = role
        self.stations = {st["name"]: st for st in stations}
        self.capacity = capacity
        self.vehicle = vehicle
        self.limits = limits
        self.crew = crew
        self.zones = zones
        self.telemetry_file = telemetry_file
        self.telemetry_dir = telemetry_dir
        self.busy = threading.Lock()
        self.flight = None
        self.flown_m = 0.0
        self.missions = 0
        self._last_pos = None
        self._stopped_since = None
        self._last_item = None  # a rover's mission: the index of its RTL item
        self._camera = 0
        self._photos_at_start = 0
        self.targets = {}
        self._targets_at = 0.0

    # -- the state ---------------------------------------------------------

    def phase(self, s):
        if not s["connected"]:
            return "offline"
        if s["armed"]:
            if self.kind == "rover" and self._rover_done(s):
                return "parking"
            return {"AUTO": "mission", "RTL": "returning", "LAND": "landing", "GUIDED": "takeoff"}.get(s["mode"], s["mode"].lower())
        if self.flight and self.flight["status"] == "starting":
            return "starting"
        if self.crew.stage:
            return self.crew.stage
        if s["gps_fix"] >= 3 and s["home"]:
            return "ready"
        return "booting"

    def state(self):
        s = self.vehicle.snapshot()
        home = s["home"]
        position = None
        if s["lat"] is not None:
            position = {"lat": s["lat"], "lon": s["lon"], "alt_m": round(s["alt_m"], 1)}
            if home:
                north, east = geo.offset_m(home["lat"], home["lon"], s["lat"], s["lon"])
                position.update(north_m=round(north, 1), east_m=round(east, 1))
        zones = {}
        if home:
            for zone in self.zones:
                if zone.get("for") and self.kind not in zone["for"]:
                    continue
                north, east = geo.offset_m(home["lat"], home["lon"], zone["lat"], zone["lon"])
                zones[zone["name"]] = {
                    "north_m": round(north), "east_m": round(east), "radius_m": zone["radius_m"],
                    "distance_m": round(math.hypot(north, east)), "about": zone.get("about", ""),
                }
        stations = {}
        if home and self.role == "supply":
            for st in self.stations.values():
                north, east = geo.offset_m(home["lat"], home["lon"], st["lat"], st["lon"])
                stations[st["name"]] = {"distance_m": round(math.hypot(north, east)), "label": st.get("label", st["name"])}
        with self.vehicle.cond:
            events = list(self.vehicle.events)[-12:]
        out = {
            "vehicle": self.vehicle_id,
            "kind": self.kind,
            "role": self.role,
            "updated": now_iso(),
            "phase": self.phase(s),
            "mode": s["mode"],
            "armed": s["armed"],
            "home": home,
            "position": position,
            "heading": round(s["heading"]),
            "groundspeed_m_s": round(s["groundspeed"], 1),
            "battery_pct": s["battery_pct"],
            "gps": {"fix": s["gps_fix"], "fix_name": GPS_FIX.get(s["gps_fix"], str(s["gps_fix"])), "satellites": s["satellites"], "hdop": s["hdop"]},
            "sensors": {
                "lidar_down_m": s["lidar_down_m"], "lidar_near_m": s["proximity_m"],
                "optical_flow_quality": s["flow_quality"], "photos_total": s["photos"],
            },
            "hardware": HARDWARE,
            "ground_crew": self.crew.as_dict(),
            "limits": self.limits.as_dict(),
            "zones": zones,
            "flight": self.flight,
            "flown_m": round(self.flown_m),
            "missions": self.missions,
            "events": events,
        }
        if self.role == "supply":
            out["stations"] = stations
            out["capacity"] = {"batteries": self.capacity[0], "parts": self.capacity[1]}
        elif self.routes:
            out["routes"] = self.route_menu()
        searched = {k: v for k, v in self.targets.items() if not v.get("ship")}
        if searched and self.role != "supply":
            out["targets"] = {k: {"north_m": round(v["north_m"]), "east_m": round(v["east_m"]), "distance_m": round(math.hypot(v["north_m"], v["east_m"])),
                                  "about": v.get("about", ""), **({"radius_m": v["radius_m"]} if "radius_m" in v else {})}
                              for k, v in searched.items()}
        if self.flies_patterns():
            out["ships"] = self.ship_menu()
        return out

    def flies_patterns(self):
        return self.role != "supply" and not (self.kind == "rover" and self.routes)

    def ship_menu(self):
        """The ships AIS hears, nearest first: what a bot can name as a target."""
        ships = sorted((v | {"mmsi": k} for k, v in self.targets.items() if v.get("ship")),
                       key=lambda v: math.hypot(v["north_m"], v["east_m"]))
        out = []
        for v in ships[:SHIPS_SHOWN]:
            distance = math.hypot(v["north_m"], v["east_m"])
            out.append({"mmsi": v["mmsi"], "name": v["name"], "type": v["type"], "length_m": v["length_m"],
                        "speed_kn": v["speed_kn"], "course_deg": v["course_deg"],
                        "north_m": round(v["north_m"]), "east_m": round(v["east_m"]), "distance_m": round(distance),
                        "inside_geofence": distance <= self.limits.max_radius_m})
        return out

    def route_menu(self):
        """What the bot reads of a patrol rover's routes."""
        return [{"route": rid, "zone": r.get("zone"), "about": r.get("about", ""), "length_m": r["length_m"]}
                for rid, r in self.routes.items()]

    def refresh_targets(self, home):
        """The fleet base's search targets and the ships AIS hears, in metres from home, every 2 s."""
        if not self.crew.crew_url or not home or time.monotonic() - self._targets_at < 2:
            return
        self._targets_at = time.monotonic()
        try:
            with urllib.request.urlopen(f"{self.crew.crew_url}/api/targets", timeout=1.5) as response:
                raw = json.load(response) or {}
        except Exception:
            return
        self.targets = patterns.relative(home, raw.get("targets") or {}, raw.get("ships") or [] if self.flies_patterns() else [])

    def telemetry_paths(self):
        """This pilot's bot's file, and one in each other bot's workspace on the node."""
        paths = [self.telemetry_file]
        if self.telemetry_dir:
            try:
                names = sorted(os.listdir(self.telemetry_dir))
            except OSError:
                names = []
            for name in names:
                path = os.path.join(self.telemetry_dir, name, "telemetry.json")
                if os.path.isdir(os.path.join(self.telemetry_dir, name)) and path != self.telemetry_file:
                    paths.append(path)
        return paths

    def tick(self):
        """Once a second: distance flown, the mission's end, the crew, the file."""
        s = self.vehicle.snapshot()
        if s["armed"] and s["lat"] is not None:
            if self._last_pos:
                n, e = geo.offset_m(self._last_pos[0], self._last_pos[1], s["lat"], s["lon"])
                self.flown_m += math.hypot(n, e)
            self._last_pos = (s["lat"], s["lon"])
        else:
            self._last_pos = None
        if self.flight and self.flight["status"] in ("flying", "starting"):
            self.flight["photos"] = s["photos"] - self._photos_at_start
            with self.vehicle.cond:
                self.flight["shots"] = [x for x in self.vehicle.shots if x["n"] > self._photos_at_start][-100:]
        if self.flight and self.flight["status"] == "flying":
            self._watch_flight(s)
        if not s["armed"]:
            self.crew.tick(s["battery_pct"])
        self.refresh_targets(s["home"])
        state = self.state()
        bot_view = {k: v for k, v in state.items() if k != "events"}
        os.makedirs(os.path.dirname(self.telemetry_file), exist_ok=True)
        for path in self.telemetry_paths():
            tmp = path + ".tmp"
            try:
                with open(tmp, "w") as f:
                    json.dump(bot_view, f, indent=1)
                os.replace(tmp, path)
            except OSError as exc:
                log.warning("telemetry %s: %s", path, exc)

    def _rover_done(self, s):
        """A rover's RTL ends in HOLD; its mission's RTL item ends in AUTO,
        the mission complete: either way it stands at home, armed."""
        return s["mode"] == "HOLD" or (s["mode"] == "AUTO" and self._last_item is not None and s["mission_seq"] >= self._last_item)

    def _watch_flight(self, s):
        if self.kind == "rover" and s["armed"] and self._rover_done(s):
            # park it once it stands still
            if s["groundspeed"] < 0.3:
                self._stopped_since = self._stopped_since or time.monotonic()
                if time.monotonic() - self._stopped_since > 3:
                    self.vehicle.command_ack(mav.MAV_CMD_COMPONENT_ARM_DISARM, 0)
            return
        self._stopped_since = None
        if not s["armed"]:
            self.flight["status"] = "landed"
            self.flight["landed_at"] = now_iso()
            self.flight["flown_m"] = round(self.flown_m)
            self.missions += 1
            self.vehicle.event(f"back home: {self.flight['name']} ({self.flown_m:.0f} m)")
            self.crew.begin()

    # -- the tool ----------------------------------------------------------

    def check_plan(self, name, waypoints, speed):
        lim = self.limits
        if not isinstance(name, str) or not self.NAME.match(name):
            raise mcp_http.ToolError("name: 1 to 40 letters, digits, spaces, '_', '.' or '-'")
        if not isinstance(waypoints, list) or not 1 <= len(waypoints) <= lim.max_waypoints:
            raise mcp_http.ToolError(f"waypoints: 1 to {lim.max_waypoints}")
        if isinstance(speed, bool) or not isinstance(speed, (int, float)) or not lim.min_speed <= speed <= lim.max_speed:
            raise mcp_http.ToolError(f"speed_m_s: from {lim.min_speed:g} to {lim.max_speed:g}")
        plan = []
        for i, wp in enumerate(waypoints):
            try:
                north, east = float(wp["north_m"]), float(wp["east_m"])
                alt = float(wp.get("alt_m", 0)) if lim.max_alt_m > 0 else 0.0
            except (AttributeError, KeyError, TypeError, ValueError):
                raise mcp_http.ToolError(f"waypoint {i}: needs numbers north_m and east_m" + (" and alt_m" if lim.max_alt_m > 0 else ""))
            if not all(math.isfinite(v) for v in (north, east, alt)):
                raise mcp_http.ToolError(f"waypoint {i}: numbers must be finite")
            if math.hypot(north, east) > lim.max_radius_m:
                raise mcp_http.ToolError(f"waypoint {i}: {math.hypot(north, east):.0f} m from home, outside the {lim.max_radius_m:.0f} m geofence")
            if lim.max_alt_m > 0 and not lim.min_alt_m <= alt <= lim.max_alt_m:
                raise mcp_http.ToolError(f"waypoint {i}: altitude {alt:.0f} m, allowed {lim.min_alt_m:.0f} to {lim.max_alt_m:.0f} m")
            plan.append((north, east, alt))
        return plan

    def check_route(self, name, points, speed, home):
        """A precomputed route: the name, the speed, and every point inside
        the geofence (it was drawn from the station's position, not this
        vehicle's home); the plan in metres from home."""
        lim = self.limits
        if not isinstance(name, str) or not self.NAME.match(name):
            raise mcp_http.ToolError("name: 1 to 40 letters, digits, spaces, '_', '.' or '-'")
        if isinstance(speed, bool) or not isinstance(speed, (int, float)) or not lim.min_speed <= speed <= lim.max_speed:
            raise mcp_http.ToolError(f"speed_m_s: from {lim.min_speed:g} to {lim.max_speed:g}")
        if not 1 <= len(points) <= MAX_ROUTE_POINTS:
            raise mcp_http.ToolError(f"the route has {len(points)} points, at most {MAX_ROUTE_POINTS}")
        plan = []
        for lat, lon in points:
            north, east = geo.offset_m(home["lat"], home["lon"], lat, lon)
            if math.hypot(north, east) > lim.max_radius_m:
                raise mcp_http.ToolError(f"the route reaches {math.hypot(north, east):.0f} m from home, outside the {lim.max_radius_m:.0f} m geofence")
            plan.append((north, east, 0.0))
        return plan

    def check_ready(self):
        s = self.vehicle.snapshot()
        if not s["connected"]:
            raise mcp_http.ToolError("no link to the vehicle")
        if s["armed"]:
            raise mcp_http.ToolError(f"the vehicle is already out ({s['mode']})")
        if self.crew.stage:
            raise mcp_http.ToolError(f"the vehicle is at station {self.crew.station} ({self.crew.stage}); send a spare")
        if s["gps_fix"] < 3 or not s["home"]:
            raise mcp_http.ToolError("the vehicle has no GPS fix or no home yet; try again in a minute")
        if s["battery_pct"] is not None and s["battery_pct"] < self.limits.min_battery:
            raise mcp_http.ToolError(f"battery {s['battery_pct']} %, below {self.limits.min_battery:.0f} %; send a spare")
        return s

    def check_deliveries(self, deliveries, home):
        """A supply run: the stations to visit, in order, and what each gets."""
        if not isinstance(deliveries, list) or not 1 <= len(deliveries) <= len(self.stations):
            raise mcp_http.ToolError(f"deliveries: 1 to {len(self.stations)} stations")
        total_b = total_p = 0
        seen = set()
        waypoints = []
        clean = []
        for i, d in enumerate(deliveries):
            if not isinstance(d, dict) or d.get("station") not in self.stations or d["station"] in seen:
                raise mcp_http.ToolError(f"delivery {i}: station must be one of {', '.join(self.stations)}, each once")
            batteries, parts = d.get("batteries", 0), d.get("parts", 0)
            if not all(isinstance(v, int) and not isinstance(v, bool) and v >= 0 for v in (batteries, parts)):
                raise mcp_http.ToolError(f"delivery {i}: batteries and parts are whole numbers, 0 or more")
            seen.add(d["station"])
            total_b += batteries
            total_p += parts
            st = self.stations[d["station"]]
            north, east = geo.offset_m(home["lat"], home["lon"], st["lat"], st["lon"])
            waypoints.append({"north_m": north, "east_m": east})
            clean.append({"station": d["station"], "batteries": batteries, "parts": parts})
        if total_b > self.capacity[0] or total_p > self.capacity[1]:
            raise mcp_http.ToolError(f"the rover carries at most {self.capacity[0]} packs and {self.capacity[1]} parts")
        return waypoints, clean

    def plan_route_or_pattern(self, name, arguments, speed):
        """A quad's or a plane's plan: its waypoints, or a search pattern drawn here (patterns.py)."""
        if arguments.get("pattern") is None:
            return self.check_plan(name, arguments.get("waypoints"), speed), None
        if arguments.get("waypoints"):
            raise mcp_http.ToolError("give waypoints or a pattern, not both")
        try:
            waypoints, centre = patterns.expand(arguments["pattern"], self.targets, speed if isinstance(speed, (int, float)) else 10,
                                                self.limits.max_waypoints, self.limits.min_alt_m)
            target = arguments["pattern"].get("target")
            key = patterns.find(target, self.targets) if target is not None else None
        except patterns.PatternError as exc:
            raise mcp_http.ToolError(str(exc))
        ship = self.targets.get(key) if key else None
        if ship and ship.get("ship"):
            now = math.hypot(ship["north_m"], ship["east_m"])
            then = math.hypot(*centre)
            if max(now, then) > self.limits.max_radius_m:
                mmsi = key
                raise mcp_http.ToolError(
                    f"ship {ship['name'] or mmsi} (MMSI {mmsi}) is {now:.0f} m from home"
                    + (f" and will be {then:.0f} m away when the vehicle gets there" if abs(then - now) >= 50 else "")
                    + f", outside the {self.limits.max_radius_m:.0f} m geofence: nothing moved")
        plan = self.check_plan(name, waypoints, speed)
        return plan, dict(arguments["pattern"], centre_north_m=round(centre[0]), centre_east_m=round(centre[1]))

    def run_mission(self, arguments):
        name = arguments.get("name")
        speed = arguments.get("speed_m_s", round((self.limits.min_speed + self.limits.max_speed) / 2))
        deliveries = None
        pattern = None
        photo_every = arguments.get("photo_every_m", 0) or 0
        if isinstance(photo_every, bool) or not isinstance(photo_every, (int, float)) or not 0 <= photo_every <= 500:
            raise mcp_http.ToolError("photo_every_m: 0 (no photos) to 500")
        route_id = None
        if self.kind == "rover":
            home = self.vehicle.snapshot()["home"]
            if not home:
                raise mcp_http.ToolError("the rover has no home yet; try again in a minute")
        if self.role == "supply":
            waypoints, deliveries = self.check_deliveries(arguments.get("deliveries"), home)
            if self.routes:
                order = tuple(d["station"] for d in deliveries)
                if order not in self.routes:
                    raise mcp_http.ToolError(f"no route through {' then '.join(order)}; it can reach "
                                             f"{', '.join(sorted({s for k in self.routes for s in k}))}")
                route_id = "supply:" + ">".join(order)
                plan = self.check_route(name, self.routes[order]["points"], speed, home)
            else:
                plan = self.check_plan(name, waypoints, speed)
        elif self.kind == "rover" and self.routes:
            route_id = arguments.get("route")
            if route_id not in self.routes:
                raise mcp_http.ToolError(f"route: one of {', '.join(self.routes)}")
            plan = self.check_route(name, self.routes[route_id]["points"], speed, home)
        else:
            plan, pattern = self.plan_route_or_pattern(name, arguments, speed)
        if not self.busy.acquire(blocking=False):
            raise mcp_http.ToolError("another mission is being started on this vehicle")
        try:
            s = self.check_ready()
            home = s["home"]
            route = [{"lat": home["lat"], "lon": home["lon"], "alt_m": 0}]
            for north, east, alt in plan:
                lat, lon = geo.point_at(home["lat"], home["lon"], north, east)
                route.append({"lat": lat, "lon": lon, "alt_m": alt})
            if self.kind == "plane":
                route += self._approach(home)
            legs = sum(math.hypot(*geo.offset_m(a["lat"], a["lon"], b["lat"], b["lon"])) for a, b in zip(route, route[1:] + route[:1]))
            eta = round(legs / speed + 60)
            self.flight = {
                "name": name, "status": "starting", "speed_m_s": speed, "route": route,
                "distance_m": round(legs), "eta_s": eta, "started_at": now_iso(),
            }
            if deliveries:
                self.flight["deliveries"] = deliveries
            if pattern:
                self.flight["pattern"] = pattern
            if route_id:
                self.flight["route_id"] = route_id
            self.flown_m = 0.0
            self.flight["photo_every_m"] = photo_every
            self._photos_at_start = s["photos"]
            self.vehicle.event(f"mission {name}: " + (f"route {route_id}" if route_id else f"{len(plan)} waypoints") + f", {legs:.0f} m"
                               + (f", a photo every {photo_every:g} m" if photo_every else ""))
            self._camera = photo_every
            {"quad": self._launch_quad, "rover": self._launch_rover, "plane": self._launch_plane}[self.kind](route, plan, speed)
            self.flight["status"] = "flying"
            return {
                "accepted": True, "vehicle": self.vehicle_id, "kind": self.kind, "mission": name,
                **({"route": route_id} if route_id else {"waypoints": len(plan)}), "distance_m": round(legs), "eta_s": eta,
                **({"pattern": pattern["kind"]} if pattern else {}),
                "status": "on its way in AUTO; it comes back to its station by itself",
            }
        except mcp_http.ToolError:
            raise
        except Exception as exc:
            if self.flight:
                self.flight["status"] = "failed"
            self.vehicle.event(f"mission {name} failed: {exc}")
            self._safe_stop()
            raise mcp_http.ToolError(f"the mission did not start: {exc}")
        finally:
            self.busy.release()

    def return_home(self, arguments):
        """Brings the vehicle home now: RTL to its station, where a quad
        lands, a rover parks and a plane lines up and lands by itself. It
        stops the mission in flight; a vehicle at its station stays there.
        Coming home is always safe, so it needs no plan and no checks."""
        reason = " ".join(str(arguments.get("reason") or "").split())[:200]
        if not reason:
            raise mcp_http.ToolError("reason: say in one line why it comes home")
        # A mission being started now gets its launch finished first, so
        # the RTL is not overwritten by the take-off.
        if not self.busy.acquire(timeout=30):
            raise mcp_http.ToolError("a mission is still being started on this vehicle; try again in a few seconds")
        try:
            s = self.vehicle.snapshot()
            if not s["connected"]:
                raise mcp_http.ToolError("the vehicle is offline: its autopilot does not answer")
            phase = self.phase(s)
            away = self._distance_home(s)
            if not s["armed"]:
                return {"vehicle": self.vehicle_id, "status": "already home", "phase": phase, "reason": reason}
            if phase in ("returning", "landing", "parking"):
                self.vehicle.event(f"recall: already coming home ({reason})")
                return {"vehicle": self.vehicle_id, "status": "already coming home", "phase": phase,
                        "distance_home_m": away, "reason": reason}
            self.vehicle.set_mode("RTL")
            if self.flight:
                self.flight["recalled"] = reason
                self.flight["recalled_at"] = now_iso()
            self.vehicle.event(f"recalled: {reason}")
            return {
                "accepted": True, "vehicle": self.vehicle_id, "kind": self.kind, "status": "returning to its station in RTL",
                "was": phase, "distance_home_m": away, "reason": reason,
                **({"mission": self.flight["name"]} if self.flight else {}),
            }
        except mcp_http.ToolError:
            raise
        except Exception as exc:
            self.vehicle.event(f"recall failed: {exc}")
            raise mcp_http.ToolError(f"the vehicle did not take RTL: {exc}")
        finally:
            self.busy.release()

    def _distance_home(self, s):
        home = s.get("home")
        if not home or s.get("lat") is None:
            return None
        return round(math.hypot(*geo.offset_m(home["lat"], home["lon"], s["lat"], s["lon"])))

    def _approach(self, home):
        """A plane lands on its home runway: two points behind it on final."""
        heading = env_float("RUNWAY_HEADING", 0)
        back = math.radians(heading + 180)
        out = []
        for dist, alt in ((1800, 60), (900, 35)):
            lat, lon = geo.point_at(home["lat"], home["lon"], dist * math.cos(back), dist * math.sin(back))
            out.append({"lat": lat, "lon": lon, "alt_m": alt, "approach": True})
        return out

    def _camera_on(self):
        if not self._camera:
            return []
        return [{"frame": mav.MAV_FRAME_MISSION, "command": mav.MAV_CMD_DO_SET_CAM_TRIGG_DIST, "p1": self._camera, "p3": 1}]

    def _camera_off(self):
        if not self._camera:
            return []
        return [{"frame": mav.MAV_FRAME_MISSION, "command": mav.MAV_CMD_DO_SET_CAM_TRIGG_DIST, "p1": 0}]

    @staticmethod
    def _wp(point):
        return {"frame": mav.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT, "command": mav.MAV_CMD_NAV_WAYPOINT,
                "lat": point["lat"], "lon": point["lon"], "alt": point["alt_m"]}

    @staticmethod
    def _home_item(home):
        return {"frame": mav.MAV_FRAME_GLOBAL_INT, "command": mav.MAV_CMD_NAV_WAYPOINT, "lat": home["lat"], "lon": home["lon"], "alt": 0}

    def _launch_quad(self, route, plan, speed):
        v = self.vehicle
        home = route[0]
        rel = mav.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT
        items = [self._home_item(home),
                 {"frame": rel, "command": mav.MAV_CMD_NAV_TAKEOFF, "lat": home["lat"], "lon": home["lon"], "alt": plan[0][2]},
                 {"frame": rel, "command": mav.MAV_CMD_DO_CHANGE_SPEED, "p1": 1, "p2": speed, "p3": -1}]
        items += self._camera_on() + [self._wp(p) for p in route[1:]] + self._camera_off()
        items.append({"frame": rel, "command": mav.MAV_CMD_NAV_RETURN_TO_LAUNCH})
        v.upload_mission(items)
        v.set_mode("GUIDED")
        v.arm()
        v.event("armed")
        target = plan[0][2]
        if not v.command_ack(mav.MAV_CMD_NAV_TAKEOFF, 0, 0, 0, 0, 0, 0, target):
            raise RuntimeError("the vehicle refused the takeoff")
        if not v.wait_state(lambda s: s["alt_m"] >= 0.85 * target, 60):
            raise RuntimeError("the quad did not reach its takeoff altitude")
        v.event(f"took off to {target:.0f} m")
        v.set_mode("AUTO")
        v.event("AUTO: flying the mission")

    def _launch_rover(self, route, plan, speed):
        v = self.vehicle
        home = route[0]
        rel = mav.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT
        items = [self._home_item(home),
                 {"frame": rel, "command": mav.MAV_CMD_DO_CHANGE_SPEED, "p1": 1, "p2": speed, "p3": -1}]
        items += self._camera_on() + [self._wp(p) for p in route[1:]] + self._camera_off()
        items.append({"frame": rel, "command": mav.MAV_CMD_NAV_RETURN_TO_LAUNCH})
        self._last_item = len(items) - 1
        v.upload_mission(items)
        v.set_mode("HOLD")
        v.arm()
        v.event("armed")
        v.set_mode("AUTO")
        if not v.wait_state(lambda s: s["groundspeed"] > 0.5, 30):
            raise RuntimeError("the rover did not start moving")
        v.event("AUTO: driving the route")

    def _launch_plane(self, route, plan, speed):
        v = self.vehicle
        home = route[0]
        rel = mav.MAV_FRAME_GLOBAL_RELATIVE_ALT_INT
        climb = min(plan[0][2], 60)
        items = [self._home_item(home),
                 {"frame": rel, "command": mav.MAV_CMD_NAV_TAKEOFF, "p1": 15, "lat": home["lat"], "lon": home["lon"], "alt": climb},
                 {"frame": rel, "command": mav.MAV_CMD_DO_CHANGE_SPEED, "p1": 0, "p2": speed, "p3": -1}]
        patrol = [p for p in route[1:] if not p.get("approach")]
        approach = [p for p in route[1:] if p.get("approach")]
        items += self._camera_on() + [self._wp(p) for p in patrol] + self._camera_off()
        items.append({"frame": rel, "command": mav.MAV_CMD_DO_LAND_START, "lat": home["lat"], "lon": home["lon"], "alt": 0})
        items += [self._wp(p) for p in approach]
        items.append({"frame": rel, "command": mav.MAV_CMD_NAV_LAND, "lat": home["lat"], "lon": home["lon"], "alt": 0})
        v.upload_mission(items)
        v.set_mode("AUTO")
        v.arm()
        v.event("armed: takeoff run")
        if not v.wait_state(lambda s: s["alt_m"] >= 20, 90):
            raise RuntimeError("the plane did not climb")
        v.event("airborne: flying the patrol")

    def _safe_stop(self):
        try:
            s = self.vehicle.snapshot()
            if s["armed"] and (s["alt_m"] > 1 or s["groundspeed"] > 0.5):
                self.vehicle.set_mode("RTL")
            elif s["armed"]:
                self.vehicle.command_ack(mav.MAV_CMD_COMPONENT_ARM_DISARM, 0)
        except Exception as exc:
            log.warning("safe stop: %s", exc)


def supply_tool(pilot, limits):
    names = list(pilot.stations)
    return {
        "name": "run_mission",
        "description": (
            "Send this supply rover on a delivery run: it loads charged battery packs and spare parts at the depot, "
            "drives to the stations in the order given, unloads what each delivery says, picks up the spent packs, "
            f"and drives back to the depot. It carries at most {pilot.capacity[0]} packs and {pilot.capacity[1]} parts in all. "
            "Give no waypoints: it drives the route precomputed through those stations in that order, along the port's roads "
            "and yards, clear of buildings, container blocks and water, turning round at each station like a car. "
            "One call per run; it answers once the rover is on its way."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "name": {"type": "string", "description": "a short run name, e.g. 'restock-north'", "maxLength": 40},
                "deliveries": {
                    "type": "array", "minItems": 1, "maxItems": len(names),
                    "items": {
                        "type": "object",
                        "properties": {
                            "station": {"type": "string", "enum": names},
                            "batteries": {"type": "integer", "minimum": 0, "description": "charged packs to unload"},
                            "parts": {"type": "integer", "minimum": 0, "description": "spare parts to unload"},
                        },
                        "required": ["station", "batteries", "parts"],
                        "additionalProperties": False,
                    },
                },
                "speed_m_s": {"type": "number", "description": f"speed in m/s, {limits.min_speed:g} to {limits.max_speed:g}"},
            },
            "required": ["name", "deliveries"],
            "additionalProperties": False,
        },
    }


def return_home_tool(kind):
    """Every vehicle's way home, whatever its role."""
    how = {"quad": "flies straight back to its pad and lands", "rover": "drives back to its station along the way home and parks",
           "plane": "flies back to its runway, lines up and lands"}[kind]
    return {
        "name": "return_home",
        "description": (
            f"Bring this vehicle home now: it stops its mission and {how} by itself (RTL). Coming home is always safe and needs "
            "no person's approval. Use it when you are recalled or warned: a ship in the zone, the weather, a teammate's warning, "
            "the operator. A vehicle already at its station stays there. Give the reason in one line; it answers at once."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {"reason": {"type": "string", "minLength": 1, "maxLength": 200, "description": "why it comes home, in one line"}},
            "required": ["reason"],
            "additionalProperties": False,
        },
    }


def route_tool(pilot, limits):
    """A patrol rover's tool: one of its precomputed routes, by name."""
    ids = list(pilot.routes)
    menu = "; ".join(f"{rid}: {r.get('zone')} ({r['length_m']} m)" for rid, r in pilot.routes.items())
    return {
        "name": "run_mission",
        "description": (
            "Drive this ground rover on one of its precomputed routes and bring it home: out to the zone, a lap of it and back "
            "to its station, along the port's roads and yard lanes, clear of buildings, container blocks and water, with turns "
            f"a car can drive. Give no waypoints; pick the route whose zone the objective names. Routes: {menu}. "
            f"Speed {limits.min_speed:g} to {limits.max_speed:g} m/s. The service refuses a vehicle that is not ready "
            "(at its station, low battery). One call per mission; it answers once the rover is on its way."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "name": {"type": "string", "description": "a short mission name, e.g. 'rwg-yard-round'", "maxLength": 40},
                "route": {"type": "string", "enum": ids, "description": "the route to drive"},
                "speed_m_s": {"type": "number", "description": f"cruise speed in m/s, {limits.min_speed:g} to {limits.max_speed:g}"},
                "photo_every_m": {"type": "number", "minimum": 0, "maximum": 500, "description": "camera: one photo every so many metres along the route; 0 or absent: none"},
            },
            "required": ["name", "route"],
            "additionalProperties": False,
        },
    }


def load_routes(path, kind, role, station):
    """The routes this rover may drive, from the library routes.py wrote."""
    if kind != "rover" or not os.path.exists(path):
        return {}
    with open(path) as f:
        library = json.load(f).get("routes", [])
    if role == "supply":
        return {tuple(r["stations"]): r for r in library if r["kind"] == "supply" and r["from"] == station}
    return {r["id"]: r for r in library if r["kind"] == "patrol" and r["from"] == station}


def mission_tool(kind, limits):
    lim = limits.as_dict()
    what = {
        "quad": "Fly this quadcopter along a route and bring it home: it takes off from its pad, flies the waypoints in order, returns to its station and lands.",
        "rover": "Drive this ground rover along a route on the quays and yards and bring it home: it drives the waypoints in order and returns to its station.",
        "plane": "Fly this plane along a patrol route: it takes off from its runway, flies the waypoints in order, then lines up and lands on the runway by itself.",
    }[kind]
    point = {
        "north_m": {"type": "number", "description": "metres north of home (negative: south)"},
        "east_m": {"type": "number", "description": "metres east of home (negative: west)"},
    }
    required = ["north_m", "east_m"]
    if limits.max_alt_m > 0:
        point["alt_m"] = {"type": "number", "description": f"metres above home, {limits.min_alt_m:g} to {limits.max_alt_m:g}"}
        required.append("alt_m")
    return {
        "name": "run_mission",
        "description": (
            f"{what} Waypoints are metres north and east of the vehicle's home. "
            f"Limits: within {lim['max_radius_m']:g} m of home, at most {lim['max_waypoints']} waypoints, "
            f"speed {limits.min_speed:g} to {limits.max_speed:g} m/s. Instead of waypoints, a search pattern: the service draws it "
            "around a target named in telemetry.json's targets (the datum, a sighting) or a ship from its ships (by MMSI or name: "
            "pattern orbit, to look all round it), leads the target's drift or the ship's course and speed, and checks it like a route; "
            "a ship beyond the geofence is refused with its distance. "
            "The service refuses a route outside the limits and a vehicle that is not ready (at its station, low battery). "
            "One call per mission; it answers once the vehicle is on its way."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "name": {"type": "string", "description": "a short mission name, e.g. 'quay-7-cranes'", "maxLength": 40},
                "waypoints": {
                    "type": "array", "minItems": 1, "maxItems": limits.max_waypoints,
                    "items": {"type": "object", "properties": point, "required": required, "additionalProperties": False},
                },
                "speed_m_s": {"type": "number", "description": f"cruise speed in m/s, {limits.min_speed:g} to {limits.max_speed:g}"},
                "photo_every_m": {"type": "number", "minimum": 0, "maximum": 500, "description": "camera: one photo every so many metres along the route; 0 or absent: none"},
                "pattern": {
                    "type": "object",
                    "description": "a search pattern instead of waypoints",
                    "properties": {
                        "kind": {"type": "string", "enum": list(patterns.KINDS),
                                 "description": "expanding_square: from a point where something was seen, outwards; parallel_track: straight legs across an area; "
                                                "orbit: a circle of points round the target, e.g. a ship"},
                        "target": {"type": "string", "description": "a name from telemetry.json's targets, e.g. 'datum' or 'S3', or a ship from its ships: its MMSI or its name"},
                        "centre_north_m": {"type": "number", "description": "without a target: the centre, metres north of home"},
                        "centre_east_m": {"type": "number", "description": "without a target: the centre, metres east of home"},
                        "spacing_m": {"type": "number", "description": "track spacing in metres, 10 to 1000"},
                        "legs": {"type": "integer", "description": f"expanding_square: legs, 2 to {limits.max_waypoints - 1}"},
                        "radius_m": {"type": "number", "description": "orbit: the circle's radius in metres, 20 to 1000 (default 80)"},
                        "points": {"type": "integer", "description": f"orbit: points on the circle, 4 to {min(24, limits.max_waypoints - 1)} (default 8)"},
                        "length_m": {"type": "number", "description": "parallel_track: leg length in metres"},
                        "width_m": {"type": "number", "description": "parallel_track: width of the area across the legs, in metres"},
                        "heading_deg": {"type": "number", "description": "direction of the first leg (parallel_track: of the legs), 0 north, 90 east"},
                        **({"alt_m": {"type": "number", "description": f"metres above home, {limits.min_alt_m:g} to {limits.max_alt_m:g}"}} if limits.max_alt_m > 0 else {}),
                    },
                    "required": ["kind"],
                    "additionalProperties": False,
                },
            },
            "required": ["name"],
            "additionalProperties": False,
        },
    }


class StateHandler(BaseHTTPRequestHandler):
    pilot = None
    photos = None

    def log_message(self, fmt, *args):
        pass

    def do_GET(self):
        if self.path == "/state":
            body, status, ctype = json.dumps(self.pilot.state()).encode(), 200, "application/json"
        elif self.path == "/healthz":
            body, status, ctype = b"ok\n", 200, "text/plain"
        elif self.photos is not None and re.fullmatch(r"/photos/[0-9]{1,9}\.jpg", self.path):
            try:
                with open(self.photos.path(int(self.path[8:-4])), "rb") as f:
                    body, status, ctype = f.read(), 200, "image/jpeg"
            except FileNotFoundError:
                body, status, ctype = b"no such photo\n", 404, "text/plain"
        else:
            body, status, ctype = b"not found\n", 404, "text/plain"
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def pick(env_list, index, default=""):
    """The index-th entry of a ';'-separated list: one per StatefulSet replica."""
    items = [x.strip() for x in os.environ.get(env_list, "").split(";") if x.strip()]
    return items[index] if index < len(items) else default


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s %(message)s")
    kind = os.environ.get("VEHICLE_KIND", "quad")
    if kind not in KINDS:
        raise SystemExit(f"VEHICLE_KIND must be one of {', '.join(KINDS)}")
    vehicle_id = os.environ.get("VEHICLE_ID") or os.environ.get("POD_NAME") or kind
    index = int(os.environ.get("POD_INDEX", "0"))
    station = os.environ.get("STATION") or pick("STATIONS", index, "base")
    zones = []
    zones_file = os.environ.get("ZONES_FILE", "")
    if zones_file and os.path.exists(zones_file):
        with open(zones_file) as f:
            zones = json.load(f).get("zones", [])
    role = os.environ.get("ROLE", "patrol")
    stations = []
    layout_file = os.environ.get("LAYOUT_FILE", "")
    if layout_file and os.path.exists(layout_file):
        with open(layout_file) as f:
            stations = json.load(f).get("stations", [])
    capacity = (int(env_float("CARRY_BATTERIES", 8)), int(env_float("CARRY_PARTS", 10)))
    routes = load_routes(os.environ.get("ROUTES_FILE", ROUTES_FILE), kind, role, station)
    limits = Limits(kind)
    vehicle = Vehicle(os.environ.get("MAVLINK_URL", "tcp:127.0.0.1:5770"))
    crew = GroundCrew(vehicle, vehicle_id, kind, station, os.environ.get("CREW_URL", ""),
                      env_float("INSPECTION_S", 30), env_float("SWAP_S", 10))
    pilot = Pilot(vehicle_id, kind, vehicle, limits, crew, zones, os.environ.get("TELEMETRY_FILE", "/var/lib/vehicle/telemetry.json"),
                  role=role, stations=stations, capacity=capacity, routes=routes, telemetry_dir=os.environ.get("TELEMETRY_DIR", ""))
    if os.environ.get("PHOTO_URL"):
        vehicle.photos = PhotoFetcher(os.environ["PHOTO_URL"], os.environ.get("PHOTOS_DIR", "/var/lib/vehicle/photos"), vehicle.cond)
        vehicle.photos.start()
    vehicle.start()

    StateHandler.pilot = pilot
    StateHandler.photos = vehicle.photos
    state_server = ThreadingHTTPServer(env_listen("STATE_LISTEN", "0.0.0.0:8080"), StateHandler)
    threading.Thread(target=state_server.serve_forever, name="state", daemon=True).start()

    tools = mcp_http.Tools(server_name="vehicle", version="0.1.0")
    if role == "supply":
        tool = supply_tool(pilot, limits)
    elif routes:
        tool = route_tool(pilot, limits)
    else:
        tool = mission_tool(kind, limits)
    tools.add(tool, pilot.run_mission)
    tools.add(return_home_tool(kind), pilot.return_home)
    mcp_server = ThreadingHTTPServer(env_listen("MCP_LISTEN", "127.0.0.1:8765"), mcp_http.handler(tools))
    threading.Thread(target=mcp_server.serve_forever, name="mcp", daemon=True).start()
    log.info("%s %s at station %s: %d routes, state on %s, MCP on %s", kind, vehicle_id, station, len(routes), state_server.server_address, mcp_server.server_address)

    while True:
        try:
            pilot.tick()
        except Exception as exc:
            log.warning("tick: %s", exc)
        time.sleep(1)


if __name__ == "__main__":
    main()
