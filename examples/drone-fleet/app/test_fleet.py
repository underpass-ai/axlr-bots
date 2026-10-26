"""Unit tests of the port fleet's services: python -m unittest -v test_fleet (in the app image)."""

import json
import math
import os
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

os.environ.setdefault("PHOTOS_DIR", tempfile.mkdtemp())

import ais
import approvals
import drift
import fleet_base
import geo
import incident
import mcp_http
import patterns
import sprites
import vehicle_pilot
import vision


class GeoTest(unittest.TestCase):
    def test_an_offset_comes_back(self):
        lat, lon = geo.point_at(51.9455, 3.9972, 120.0, -80.0)
        north, east = geo.offset_m(51.9455, 3.9972, lat, lon)
        self.assertAlmostEqual(north, 120.0, places=3)
        self.assertAlmostEqual(east, -80.0, places=3)


class McpTest(unittest.TestCase):
    def setUp(self):
        self.tools = mcp_http.Tools("vehicle", "0")
        self.tools.add({"name": "echo", "inputSchema": {"type": "object"}}, lambda a: {"got": a})
        self.tools.add({"name": "refuse", "inputSchema": {"type": "object"}}, self._refuse)

    @staticmethod
    def _refuse(_):
        raise mcp_http.ToolError("outside the geofence")

    def call(self, method, params=None, rid=1):
        return self.tools.handle({"jsonrpc": "2.0", "id": rid, "method": method, "params": params or {}})

    def test_initialize_answers_the_version_asked_or_its_own(self):
        self.assertEqual(self.call("initialize", {"protocolVersion": "2025-03-26"})["result"]["protocolVersion"], "2025-03-26")
        self.assertEqual(self.call("initialize", {"protocolVersion": "1999-01-01"})["result"]["protocolVersion"], "2025-06-18")

    def test_a_notification_gets_no_answer(self):
        self.assertIsNone(self.tools.handle({"jsonrpc": "2.0", "method": "notifications/initialized"}))

    def test_tools_list_and_call(self):
        self.assertEqual([t["name"] for t in self.call("tools/list")["result"]["tools"]], ["echo", "refuse"])
        out = self.call("tools/call", {"name": "echo", "arguments": {"a": 1}})["result"]
        self.assertFalse(out["isError"])
        self.assertEqual(json.loads(out["content"][0]["text"]), {"got": {"a": 1}})

    def test_a_refusal_is_the_tool_s_error_and_an_unknown_tool_is_a_protocol_error(self):
        out = self.call("tools/call", {"name": "refuse", "arguments": {}})["result"]
        self.assertTrue(out["isError"])
        self.assertIn("geofence", out["content"][0]["text"])
        self.assertEqual(self.call("tools/call", {"name": "nope"})["error"]["code"], -32602)
        self.assertEqual(self.call("resources/list")["error"]["code"], -32601)


class FakeVehicle:
    def __init__(self, **state):
        self.state = {"connected": True, "armed": False, "mode": "STABILIZE", "gps_fix": 6,
                      "home": {"lat": 51.9455, "lon": 3.9972}, "battery_pct": 100, "photos": 0}
        self.state.update(state)

    def snapshot(self):
        return dict(self.state)

    def event(self, _):
        pass


class PlanTest(unittest.TestCase):
    def pilot(self, kind="quad", **state):
        crew = vehicle_pilot.GroundCrew(FakeVehicle(**state), "quad-0", "quad" if kind == "plane" else kind, "apm", "", 1, 1)
        return vehicle_pilot.Pilot("quad-0", kind, FakeVehicle(**state), vehicle_pilot.Limits(kind), crew, [], "/dev/null")

    def test_the_geofence_altitude_and_speed_are_the_service_s(self):
        p = self.pilot()
        p.check_plan("ok", [{"north_m": 100, "east_m": 0, "alt_m": 30}], 6)
        with self.assertRaisesRegex(mcp_http.ToolError, "geofence"):
            p.check_plan("far", [{"north_m": 3000, "east_m": 0, "alt_m": 30}], 6)
        with self.assertRaisesRegex(mcp_http.ToolError, "altitude"):
            p.check_plan("high", [{"north_m": 10, "east_m": 0, "alt_m": 300}], 6)
        with self.assertRaisesRegex(mcp_http.ToolError, "speed"):
            p.check_plan("fast", [{"north_m": 10, "east_m": 0, "alt_m": 30}], 40)
        with self.assertRaisesRegex(mcp_http.ToolError, "name"):
            p.check_plan("../x", [{"north_m": 10, "east_m": 0, "alt_m": 30}], 6)

    def test_a_rover_needs_no_altitude(self):
        self.assertEqual(self.pilot("rover").check_plan("lane", [{"north_m": 50, "east_m": 5}], 3), [(50.0, 5.0, 0.0)])

    def test_a_vehicle_at_its_station_or_low_is_not_ready(self):
        p = self.pilot(battery_pct=20)
        with self.assertRaisesRegex(mcp_http.ToolError, "battery"):
            p.check_ready()
        p = self.pilot()
        p.crew.stage = "inspection"
        with self.assertRaisesRegex(mcp_http.ToolError, "station"):
            p.check_ready()
        with self.assertRaisesRegex(mcp_http.ToolError, "already out"):
            self.pilot(armed=True).check_ready()

    def test_a_supply_run_stays_within_the_capacity(self):
        p = self.pilot("rover")
        p.role = "supply"
        p.stations = {"apm": {"name": "apm", "lat": 51.946, "lon": 3.998}, "rwg": {"name": "rwg", "lat": 51.941, "lon": 3.984}}
        home = {"lat": 51.9402, "lon": 4.005}
        waypoints, clean = p.check_deliveries([{"station": "apm", "batteries": 3, "parts": 2}], home)
        self.assertEqual(len(waypoints), 1)
        self.assertEqual(clean[0]["batteries"], 3)
        with self.assertRaisesRegex(mcp_http.ToolError, "carries at most"):
            p.check_deliveries([{"station": "apm", "batteries": 7, "parts": 0}, {"station": "rwg", "batteries": 7, "parts": 0}], home)
        with self.assertRaisesRegex(mcp_http.ToolError, "station must be"):
            p.check_deliveries([{"station": "mars", "batteries": 1, "parts": 0}], home)


class RouteTest(unittest.TestCase):
    """A rover drives precomputed routes; its bot names one, never waypoints."""

    ROUTE = {"id": "apm:yard", "kind": "patrol", "from": "apm", "zone": "yard", "about": "lanes", "length_m": 120,
             "points": [[51.9456, 3.9972], [51.9460, 3.9978], [51.9455, 3.9973]]}

    def rover(self, role="patrol", routes=None):
        v = FakeVehicle()
        crew = vehicle_pilot.GroundCrew(v, "rover-0", "rover", "apm", "", 1, 1)
        return vehicle_pilot.Pilot("rover-0", "rover", v, vehicle_pilot.Limits("rover"), crew, [], "/dev/null", role=role,
                                   routes=routes if routes is not None else {"apm:yard": self.ROUTE})

    def test_the_bot_picks_a_route_by_name_and_the_tool_lists_them(self):
        p = self.rover()
        tool = vehicle_pilot.route_tool(p, p.limits)
        self.assertEqual(tool["inputSchema"]["properties"]["route"]["enum"], ["apm:yard"])
        self.assertNotIn("waypoints", tool["inputSchema"]["properties"])
        self.assertEqual(p.route_menu(), [{"route": "apm:yard", "zone": "yard", "about": "lanes", "length_m": 120}])
        with self.assertRaisesRegex(mcp_http.ToolError, "route: one of apm:yard"):
            p.run_mission({"name": "round", "route": "rwg:yard"})

    def test_a_route_is_checked_against_the_geofence_and_the_speed(self):
        p = self.rover()
        home = {"lat": 51.9455, "lon": 3.9972}
        self.assertEqual(len(p.check_route("round", self.ROUTE["points"], 4, home)), 3)
        with self.assertRaisesRegex(mcp_http.ToolError, "geofence"):
            p.check_route("far", [[51.99, 3.9972]], 4, home)
        with self.assertRaisesRegex(mcp_http.ToolError, "speed"):
            p.check_route("fast", self.ROUTE["points"], 30, home)

    def test_a_supply_run_needs_a_route_through_its_stations_in_order(self):
        p = self.rover("supply", routes={("apm",): dict(self.ROUTE, kind="supply", stations=["apm"])})
        p.stations = {"apm": {"name": "apm", "lat": 51.946, "lon": 3.998}, "rwg": {"name": "rwg", "lat": 51.941, "lon": 3.984}}
        with self.assertRaisesRegex(mcp_http.ToolError, "no route through rwg; it can reach apm"):
            p.run_mission({"name": "restock", "deliveries": [{"station": "rwg", "batteries": 1, "parts": 1}]})

    def test_a_rover_parks_when_its_mission_s_rtl_is_done_in_auto(self):
        p = self.rover()
        p._last_item = 9
        self.assertFalse(p._rover_done({"mode": "AUTO", "mission_seq": 5}))
        self.assertTrue(p._rover_done({"mode": "AUTO", "mission_seq": 9}))
        self.assertTrue(p._rover_done({"mode": "HOLD", "mission_seq": 0}))

    def test_the_library_gives_each_rover_its_station_s_routes(self):
        path = os.path.join(tempfile.mkdtemp(), "routes.json")
        with open(path, "w") as f:
            json.dump({"routes": [self.ROUTE, dict(self.ROUTE, id="rwg:yard", **{"from": "rwg"}),
                                  dict(self.ROUTE, id="supply:apm", kind="supply", stations=["apm"], **{"from": "depot"})]}, f)
        self.assertEqual(list(vehicle_pilot.load_routes(path, "rover", "patrol", "apm")), ["apm:yard"])
        self.assertEqual(list(vehicle_pilot.load_routes(path, "rover", "supply", "depot")), [("apm",)])
        self.assertEqual(vehicle_pilot.load_routes(path, "quad", "patrol", "apm"), {})


class BaseTest(unittest.TestCase):
    def base(self):
        layout = {"depot": {"name": "depot", "lat": 51.9402, "lon": 4.005},
                  "stations": [{"name": "apm", "lat": 51.9455, "lon": 3.9972, "bays": 1, "batteries": 1, "parts": 2}]}
        return fleet_base.Base({"quad-0": "", "quad-1": ""}, layout, [], os.path.join(tempfile.mkdtemp(), "fleet.json"))

    def test_a_bay_takes_a_pack_and_a_part_and_a_full_station_waits(self):
        b = self.base()
        self.assertTrue(b.crew("quad-0", "apm")["granted"])
        st = b.stations["apm"]
        self.assertEqual((st["batteries"], st["parts"], st["spent"]), (0, 1, 1))
        self.assertEqual(b.crew("quad-1", "apm"), {"granted": False, "reason": "every bay is busy"})
        b.bays["apm"].clear()
        self.assertEqual(b.crew("quad-1", "apm")["reason"], "no charged battery pack at the station")
        self.assertTrue(b.crew("quad-1", "depot")["granted"])
        self.assertFalse(b.crew("ghost", "apm")["granted"])

    def test_a_supply_rover_unloads_at_the_station_it_reaches(self):
        b = self.base()
        b.stations["apm"]["spent"] = 2
        flight = {"status": "flying", "started_at": "t0", "deliveries": [{"station": "apm", "batteries": 3, "parts": 1}]}
        b.states["supply-0"] = {"vehicle": "supply-0", "flight": flight, "position": {"lat": 51.9402, "lon": 4.005}}
        b._deliveries()
        self.assertEqual(b.cargo["supply-0"]["batteries"], 3)
        b.states["supply-0"]["position"] = {"lat": 51.9455, "lon": 3.9972}
        b._deliveries()
        st = b.stations["apm"]
        self.assertEqual((st["batteries"], st["parts"], st["spent"]), (4, 3, 0))
        self.assertEqual(b.cargo["supply-0"]["spent"], 2)
        flight["status"] = "landed"
        b._deliveries()
        self.assertEqual(b.depot["spent_recharged"], 2)


class PagesTest(unittest.TestCase):
    def test_the_map_the_3d_view_and_the_port_model(self):
        server = ThreadingHTTPServer(("127.0.0.1", 0), fleet_base.Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.shutdown)
        url = f"http://127.0.0.1:{server.server_address[1]}"
        for path, ctype in (("/", "text/html"), ("/3d", "text/html"), ("/m", "text/html"), ("/port.glb", "model/gltf-binary"),
                            ("/theme.css", "text/css"), ("/brand-light.svg", "image/svg+xml"), ("/fonts/Lexend-Regular.ttf", "font/ttf")):
            with urllib.request.urlopen(url + path) as r:
                self.assertEqual(r.headers["Content-Type"].split(";")[0], ctype, path)
                body = r.read()
            if path == "/port.glb":
                self.assertEqual(body[:4], b"glTF")
        with self.assertRaises(urllib.error.HTTPError) as e:
            urllib.request.urlopen(url + "/../fleet_base.py")
        self.assertEqual(e.exception.code, 404)


COAST = [(51.925, 3.976), (51.935, 3.9715), (51.945, 3.967), (51.955, 3.9645), (51.965, 3.9616), (51.975, 3.958)]
REPORT = {"vessel": "MV TEST", "count": 6, "lat": 51.951, "lon": 3.937, "lost_minutes_ago": 20}


class DriftTest(unittest.TestCase):
    def test_leeway_falls_with_immersion(self):
        self.assertAlmostEqual(drift.leeway_rate(0.4), 0.05)
        self.assertAlmostEqual(drift.leeway_rate(1.0), 0.0)
        self.assertAlmostEqual(drift.leeway_rate(0.1), 0.05)

    def test_containers_drift_downwind_with_the_current_and_stop_ashore(self):
        sea = drift.Drift(51.951, 3.937, 270, 10, 90, 0.3, COAST, seed=1)
        sea.spill(5, 0, 200)
        start = [(c.north, c.east) for c in sea.containers]
        sea.advance(600)
        for (n0, e0), c in zip(start, sea.containers):
            self.assertGreater(c.east - e0, 600 * 0.3)       # eastwards: current plus leeway
        d = sea.datum()
        self.assertEqual(round(d["drift"]["to_deg"] / 10), 9)
        self.assertGreaterEqual(d["radius_m"], 150)
        sea.advance(3 * 3600)
        self.assertTrue(all(c.ashore for c in sea.containers))
        lat, lon = sea.position(sea.containers[0].north, sea.containers[0].east)
        self.assertGreaterEqual(lon, sea.coast_lon(lat))


class PatternTest(unittest.TestCase):
    def test_an_expanding_square_grows_by_one_spacing_every_two_legs(self):
        pts = patterns.expanding_square(40, 4)
        self.assertEqual([(round(n), round(e)) for n, e in pts], [(0, 0), (40, 0), (40, 40), (-40, 40), (-40, -40)])

    def test_a_parallel_track_alternates_its_legs(self):
        pts = patterns.parallel_track(1000, 400, 200, 90)
        self.assertEqual(len(pts), 6)
        self.assertAlmostEqual(pts[0][1], -500)               # legs run east-west
        self.assertAlmostEqual(pts[1][1], 500)
        self.assertAlmostEqual(pts[2][1], 500)

    def test_a_target_is_led_by_its_drift_and_bad_patterns_are_refused(self):
        targets = {"S1": {"north_m": 0, "east_m": -1000, "drift_n": 0.0, "drift_e": 0.5}}
        wps, centre = patterns.expand({"kind": "expanding_square", "target": "S1", "spacing_m": 30, "legs": 6, "alt_m": 30}, targets, 10, 20)
        self.assertAlmostEqual(centre[1], -1000 + 0.5 * 100)
        self.assertEqual(len(wps), 7)
        with self.assertRaisesRegex(patterns.PatternError, "targets"):
            patterns.expand({"kind": "expanding_square", "target": "S9"}, targets, 10, 20)
        with self.assertRaisesRegex(patterns.PatternError, "waypoints"):
            patterns.expand({"kind": "parallel_track", "centre_north_m": 0, "centre_east_m": 0, "width_m": 5000, "spacing_m": 100}, {}, 10, 20)

    def test_the_pilot_flies_a_pattern_within_its_limits(self):
        p = PlanTest.pilot(PlanTest(), "quad")
        p.targets = {"datum": {"north_m": 300, "east_m": -400, "drift_n": 0, "drift_e": 0}}
        p.check_ready = lambda: (_ for _ in ()).throw(mcp_http.ToolError("stop here"))
        with self.assertRaisesRegex(mcp_http.ToolError, "stop here"):
            p.run_mission({"name": "sq", "pattern": {"kind": "expanding_square", "target": "datum", "spacing_m": 40, "alt_m": 30}})
        with self.assertRaisesRegex(mcp_http.ToolError, "geofence"):
            p.run_mission({"name": "far", "pattern": {"kind": "expanding_square", "centre_north_m": 1490, "centre_east_m": 0, "spacing_m": 100, "legs": 6, "alt_m": 30}})
        with self.assertRaisesRegex(mcp_http.ToolError, "not both"):
            p.run_mission({"name": "x", "waypoints": [{"north_m": 1, "east_m": 1, "alt_m": 20}], "pattern": {"kind": "expanding_square", "target": "datum"}})


class ShipTargetTest(unittest.TestCase):
    # quad-0's home at the APM station; one ship 800 m north-east, one 4 km out, moving.
    HOME = {"lat": 51.9455, "lon": 3.9972}
    NEAR = {"mmsi": "244123456", "name": "NORDIC  STAR", "type_name": "cargo", "length_m": 300, "lat": 51.9505, "lon": 4.0050, "sog": None, "cog": None}
    FAR = {"mmsi": "211000001", "name": "OUTBOUND", "type_name": "tanker", "length_m": 180, "lat": 51.9800, "lon": 3.9972, "sog": 10.0, "cog": 0.0}

    def test_an_orbit_closes_on_its_first_point(self):
        pts = patterns.orbit(100, 4)
        self.assertEqual([(round(n), round(e)) for n, e in pts], [(100, 0), (0, 100), (-100, 0), (0, -100), (100, 0)])

    def test_a_ship_is_found_by_mmsi_or_name_and_led_by_its_course(self):
        targets = patterns.relative(self.HOME, {}, [self.NEAR, self.FAR])
        self.assertEqual(patterns.find("244123456", targets), "244123456")
        self.assertEqual(patterns.find("nordic star", targets), "244123456")
        with self.assertRaisesRegex(patterns.PatternError, "2 ships"):
            patterns.find("X", dict(targets, a={"ship": True, "name": "X"}, b={"ship": True, "name": "x"}))
        with self.assertRaisesRegex(patterns.PatternError, "ships AIS hears"):
            patterns.find("GHOST", targets)
        far = targets["211000001"]
        _, centre = patterns.expand({"kind": "orbit", "target": "OUTBOUND", "radius_m": 100, "alt_m": 40}, targets, 10, 20)
        lead = math.hypot(far["north_m"], far["east_m"]) / 10
        self.assertAlmostEqual(centre[0], far["north_m"] + 10 * patterns.KNOT * lead, places=3)

    def test_the_pilot_orbits_a_ship_inside_its_geofence_and_refuses_one_outside(self):
        p = PlanTest.pilot(PlanTest(), "quad")
        p.targets = patterns.relative(self.HOME, {}, [self.NEAR, self.FAR])
        p.check_ready = lambda: (_ for _ in ()).throw(mcp_http.ToolError("stop here"))
        with self.assertRaisesRegex(mcp_http.ToolError, "stop here"):
            p.run_mission({"name": "look", "pattern": {"kind": "orbit", "target": "Nordic Star", "radius_m": 120, "alt_m": 60}})
        with self.assertRaisesRegex(mcp_http.ToolError, r"OUTBOUND \(MMSI 211000001\) is 38\d\d m from home and will be \d+ m away .*1500 m geofence: nothing moved"):
            p.run_mission({"name": "far", "pattern": {"kind": "orbit", "target": "211000001", "alt_m": 60}})
        menu = p.ship_menu()
        self.assertEqual([(s["mmsi"], s["inside_geofence"]) for s in menu], [("244123456", True), ("211000001", False)])

    def test_every_bot_on_the_node_gets_the_telemetry(self):
        root = tempfile.mkdtemp()
        for bot in ("pilot-quad-0", "operador-quad-0"):
            os.makedirs(os.path.join(root, bot))
        p = PlanTest.pilot(PlanTest(), "quad")
        p.telemetry_file, p.telemetry_dir = os.path.join(root, "pilot-quad-0", "telemetry.json"), root
        p.tick = vehicle_pilot.Pilot.tick.__get__(p)
        p.vehicle.state.update(lat=51.9455, lon=3.9972, alt_m=0, heading=0, groundspeed=0, satellites=20, hdop=0.5,
                               lidar_down_m=None, proximity_m=None, flow_quality=None, mission_seq=0)
        p.vehicle.cond = threading.Condition()
        p.vehicle.events, p.vehicle.shots = [], []
        p.targets = patterns.relative(self.HOME, {}, [self.NEAR])
        p.tick()
        for bot in ("pilot-quad-0", "operador-quad-0"):
            with open(os.path.join(root, bot, "telemetry.json")) as f:
                seen = json.load(f)
            self.assertEqual((seen["vehicle"], [s["name"] for s in seen["ships"]]), ("quad-0", ["NORDIC  STAR"]))
            self.assertNotIn("targets", seen)                  # a ship is not a search target

    def test_the_base_serves_ships_to_the_vehicles_and_the_bots(self):
        b = fleet_base.Base({}, {"stations": [], "depot": {"lat": 51.9402, "lon": 4.00485}}, [], "/dev/null")
        b.ais = type("Ships", (), {"ships": lambda self: [ShipTargetTest.NEAR, ShipTargetTest.FAR], "summary": lambda self: {}})()
        self.assertEqual(sorted(s["mmsi"] for s in b.targets()["ships"]), ["211000001", "244123456"])
        view = b._ships_view(b.ais.ships())
        self.assertEqual(view[0]["mmsi"], "244123456")
        self.assertEqual(set(view[0]), {"mmsi", "name", "type", "length_m", "lat", "lon", "speed_kn", "course_deg"})
        b.states["quad-0"] = {"home": self.HOME}
        route, distance = b.route_of("quad-0", {"pattern": {"kind": "orbit", "target": "244123456", "radius_m": 80, "points": 8}})
        self.assertEqual(len(route), 11)
        self.assertGreater(distance, 1000)


class IncidentTest(unittest.TestCase):
    def test_a_report_needs_a_vessel_and_a_position(self):
        with self.assertRaisesRegex(ValueError, "vessel"):
            incident.Incident({"lat": 51.9, "lon": 3.9}, COAST)
        with self.assertRaisesRegex(ValueError, "lat, lon"):
            incident.Incident({"vessel": "X"}, COAST)
        with self.assertRaisesRegex(ValueError, "count"):
            incident.Incident(dict(REPORT, count=500), COAST)

    def frame_on(self, inc, cid, alt):
        c = next(c for c in inc.sea.containers if c.id == cid)
        lat, lon = inc.sea.position(c.north, c.east)
        return vision.frame(lat, lon, alt)

    def test_the_plane_sights_a_quad_confirms_and_the_dispatcher_is_called_once(self):
        now = 1_800_000_000.0
        inc = incident.Incident(REPORT, COAST, now=now, seed=3)
        self.assertEqual(inc.found(), 0)
        bbox = self.frame_on(inc, "C02", 100)
        seen = inc.observe("plane-0", "plane", 100, now + 1, [{"x": 0.5, "y": 0.5}, {"x": 0.02, "y": 0.02}], bbox, "plane-0-1-1")
        self.assertEqual(seen[0]["container"], "C02")
        self.assertEqual(inc.state["C02"]["state"], "sighted")
        self.assertEqual(len(inc.sightings), 2)               # one matched, one false
        self.assertIsNone(inc.due_dispatch(now + 5))           # too fresh: a quad nearby may confirm it
        due = inc.due_dispatch(now + 100)
        self.assertEqual(due["count"], 2)
        self.assertIsNone(inc.due_dispatch(now + 200))         # both just tasked
        self.assertIn("S1", inc.targets(now + 200))
        bbox = self.frame_on(inc, "C02", 30)
        inc.observe("quad-0", "quad", 30, now + 300, [{"x": 0.5, "y": 0.5}], bbox, "quad-0-1-1")
        self.assertEqual(inc.state["C02"]["state"], "confirmed")
        self.assertEqual(inc.found(), 1)
        view = inc.bot_view(now + 300)
        self.assertNotIn("containers", view)                   # no truth for the bots
        self.assertEqual([s["id"] for s in view["open_sightings"]], ["S2"])

    def test_the_search_ends_at_its_time_limit_with_a_report(self):
        now = 1_800_000_000.0
        inc = incident.Incident(dict(REPORT, max_minutes=5), COAST, now=now, seed=3)
        self.assertFalse(inc.tick(now + 60))
        self.assertTrue(inc.tick(now + 301))
        self.assertEqual(inc.status, "ended")
        event = inc.report_event(now + 301)
        self.assertEqual(event["missing"] + event["found"] + event["ashore"], 6)
        self.assertIn("C01", event["log"])
        snap = inc.snapshot(now + 400)
        self.assertEqual(snap["elapsed_s"], 301)

    def test_the_sprites_land_where_the_frame_says(self):
        from PIL import Image
        import io
        blank = io.BytesIO()
        Image.new("RGB", (640, 480), (20, 60, 50)).save(blank, "JPEG")
        bbox = vision.frame(51.95, 3.94, 100)
        lat, lon = geo.point_at(51.95, 3.94, 20, -30)
        _, placed = sprites.compose(blank.getvalue(), bbox, [("C01", lat, lon, 45, 12.2, "#b03a2e"), ("C02", 52.5, 3.94, 0, 6.1, "#1f5fa8")])
        self.assertEqual(len(placed), 1)
        _, x, y = placed[0]
        mlat, mlon = incident.merc_to_latlon(bbox[0] + x * (bbox[2] - bbox[0]), bbox[3] - y * (bbox[3] - bbox[1]))
        self.assertLess(abs(geo.offset_m(lat, lon, mlat, mlon)[0]), 1)


class IncidentBaseTest(unittest.TestCase):
    def test_the_dispatcher_reads_how_far_each_quad_is_from_each_target(self):
        layout = {"depot": {"name": "depot", "lat": 51.94, "lon": 4.0}, "stations": [], "coast": [list(p) for p in COAST]}
        b = fleet_base.Base({"quad-0": ""}, layout, [], os.path.join(tempfile.mkdtemp(), "fleet.json"))
        b.states["quad-0"] = {"vehicle": "quad-0", "kind": "quad", "home": {"lat": 51.957, "lon": 3.967}}
        b.incident = incident.Incident(REPORT, COAST, seed=3)
        c = b.incident.sea.containers[0]
        lat, lon = b.incident.sea.position(c.north, c.east)
        b.incident.observe("plane-0", "plane", 100, time.time() - 60, [{"x": 0.5, "y": 0.5}], vision.frame(lat, lon, 100), "p")
        view = b._bot_view(b._snapshot())["incident"]
        self.assertIn("quad-0", view["datum"]["distance_from_home_m"])
        self.assertGreater(view["open_sightings"][0]["distance_from_home_m"]["quad-0"], 500)
        self.assertNotIn("containers", view)


class ImageryTest(unittest.TestCase):
    @staticmethod
    def jpeg(colour):
        from PIL import Image
        import io
        out = io.BytesIO()
        Image.new("RGB", (64, 48), colour).save(out, "JPEG")
        return out.getvalue()

    def test_a_white_frame_falls_back_to_esri_and_two_white_frames_to_no_photo(self):
        sea, white = self.jpeg((30, 70, 60)), self.jpeg((255, 255, 255))
        bbox = vision.frame(51.955, 3.94, 100)
        asked = []

        def serve(answers):
            def get(url):
                asked.append(url)
                return answers["pdok" if "service.pdok.nl" in url else "esri"]
            return get
        self.assertEqual(vision.fetch_imagery(bbox, get=serve({"pdok": sea, "esri": white}))[1], "pdok")
        self.assertIn("CRS=EPSG:3857", asked[0])
        self.assertEqual(vision.fetch_imagery(bbox, get=serve({"pdok": white, "esri": sea})), (sea, "esri"))
        with self.assertRaisesRegex(vision.NoImagery, "pdok: blank; esri: blank"):
            vision.fetch_imagery(bbox, get=serve({"pdok": white, "esri": white}))
        self.assertTrue(vision.blank(b"not a jpeg"))
        self.assertIn("Beeldmateriaal Nederland", vision.CREDITS["pdok"])


class VisionParseTest(unittest.TestCase):
    def test_the_answer_is_bounded_whatever_comes_around_it(self):
        out = vision.parse('```json\n{"containers": [{"x": 0.2, "y": 0.4}, {"x": 7, "y": 0}, {"y": 1}], "summary": "two boxes"}\n```')
        self.assertEqual(out["containers"], [{"x": 0.2, "y": 0.4}])
        self.assertEqual(vision.parse("no json")["containers"], [])


class AisTest(unittest.TestCase):
    def test_positions_and_static_data_make_one_ship(self):
        a = ais.AIS("", [[51.9, 3.8], [52.05, 4.15]])
        a.handle({"MessageType": "PositionReport", "MetaData": {"MMSI": 244123456, "ShipName": "NORDIC  ", "latitude": 51.96, "longitude": 4.01},
                  "Message": {"PositionReport": {"Sog": 11.5, "Cog": 284.1, "TrueHeading": 511}}})
        a.handle({"MessageType": "ShipStaticData", "MetaData": {"MMSI": 244123456},
                  "Message": {"ShipStaticData": {"Type": 71, "Dimension": {"A": 200, "B": 100, "C": 24, "D": 24}, "Name": "NORDIC"}}})
        a.handle({"MessageType": "PositionReport", "MetaData": {"MMSI": 1, "latitude": 91, "longitude": 0}, "Message": {"PositionReport": {}}})
        a.handle({"MessageType": "PositionReport", "MetaData": {"MMSI": 2, "latitude": 51.95, "longitude": 4.0},
                  "Message": {"PositionReport": {"Sog": 102.3, "Cog": 360, "TrueHeading": 511}}})
        moored = next(s for s in a.ships() if s["mmsi"] == "2")
        self.assertEqual((moored["sog"], moored["cog"]), (None, None))
        ship = next(s for s in a.ships() if s["mmsi"] == "244123456")
        self.assertEqual(len(a.ships()), 2)
        self.assertEqual((ship["name"], ship["type_name"], ship["length_m"], ship["beam_m"], ship["heading"]), ("NORDIC", "cargo", 300, 48, None))
        self.assertEqual(a.summary()["status"], "live")

    def test_static_data_keeps_the_hull_around_the_antenna_and_the_draught(self):
        a = ais.AIS("", [[51.9, 3.8], [52.05, 4.15]])
        a.handle({"MessageType": "PositionReport", "MetaData": {"MMSI": 7, "latitude": 51.95, "longitude": 4.0}, "Message": {"PositionReport": {"Sog": 0.1, "Cog": 10}}})
        a.handle({"MessageType": "ShipStaticData", "MetaData": {"MMSI": 7},
                  "Message": {"ShipStaticData": {"Type": 82, "Dimension": {"A": 250, "B": 50, "C": 10, "D": 40}, "MaximumStaticDraught": 14.2,
                                                 "ImoNumber": 9876543, "CallSign": "PDAB ", "Name": "TANK"}}})
        ship = a.ships()[0]
        self.assertEqual((ship["length_m"], ship["beam_m"], ship["dim"], ship["draught_m"], ship["imo"], ship["callsign"]),
                         (300, 50, {"a": 250, "b": 50, "c": 10, "d": 40}, 14.2, 9876543, "PDAB"))

    def test_the_service_serves_the_ships_and_the_base_reads_them(self):
        a = ais.AIS("", [[51.9, 3.8], [52.05, 4.15]])
        a.handle({"MessageType": "PositionReport", "MetaData": {"MMSI": 9, "ShipName": "ONE", "latitude": 51.95, "longitude": 4.0}, "Message": {"PositionReport": {}}})
        ais.Handler.ais = a
        server = ThreadingHTTPServer(("127.0.0.1", 0), ais.Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            remote = ais.Remote(f"http://127.0.0.1:{server.server_address[1]}")
            remote.start()
            for _ in range(50):
                if remote.ships():
                    break
                time.sleep(0.05)
            self.assertEqual([s["name"] for s in remote.ships()], ["ONE"])
            self.assertEqual(remote.summary()["status"], "live")
        finally:
            server.shutdown()


class ApprovalsTest(unittest.TestCase):
    def test_the_code_is_needed_and_compared_whole(self):
        path = os.path.join(tempfile.mkdtemp(), "code")
        with open(path, "w") as f:
            f.write("4711\n")

        class CP:
            enabled = True
        a = approvals.Approvals(CP(), path, lambda v, args: (None, 0))
        self.assertTrue(a.authorised("4711"))
        self.assertFalse(a.authorised("471"))
        self.assertFalse(a.authorised(""))
        self.assertFalse(approvals.Approvals(None, path, None).authorised("4711"))
        with self.assertRaises(ValueError):
            a.decide("../etc", "approve", "")


if __name__ == "__main__":
    unittest.main()
