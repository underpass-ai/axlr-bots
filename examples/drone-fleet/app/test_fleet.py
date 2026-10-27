"""Unit tests of the port fleet's services: python -m unittest -v test_fleet (in the app image)."""

import json
import math
import os
import re
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


class ReturnHomeTest(unittest.TestCase):
    class Autopilot(FakeVehicle):
        def __init__(self, **state):
            super().__init__(**state)
            self.modes, self.events = [], []

        def set_mode(self, name, timeout=8.0):
            self.modes.append(name)
            self.state["mode"] = name

        def event(self, text):
            self.events.append(text)

    def pilot(self, kind="quad", **state):
        v = self.Autopilot(**state)
        crew = vehicle_pilot.GroundCrew(v, "quad-1", kind, "apm", "", 1, 1)
        return vehicle_pilot.Pilot("quad-1", kind, v, vehicle_pilot.Limits(kind), crew, [], "/dev/null"), v

    def test_a_vehicle_on_a_mission_turns_home_and_says_why(self):
        p, v = self.pilot(armed=True, mode="AUTO", lat=51.95, lon=4.0)
        p.flight = {"name": "north-quay", "status": "flying"}
        out = p.return_home({"reason": "  a ship\nis entering the zone "})
        self.assertEqual(v.modes, ["RTL"])
        self.assertTrue(out["accepted"])
        self.assertEqual((out["was"], out["mission"], out["reason"]), ("mission", "north-quay", "a ship is entering the zone"))
        self.assertGreater(out["distance_home_m"], 0)
        self.assertEqual(p.flight["recalled"], "a ship is entering the zone")
        self.assertIn("recalled: a ship is entering the zone", v.events)

    def test_a_vehicle_at_home_or_already_coming_stays_as_it_is(self):
        p, v = self.pilot()
        self.assertEqual(p.return_home({"reason": "x"})["status"], "already home")
        p, v = self.pilot(armed=True, mode="RTL")
        self.assertEqual(p.return_home({"reason": "x"})["status"], "already coming home")
        self.assertEqual(v.modes, [])

    def test_a_recall_needs_a_reason_and_an_autopilot(self):
        p, v = self.pilot(armed=True, mode="AUTO")
        with self.assertRaisesRegex(mcp_http.ToolError, "reason"):
            p.return_home({})
        p, v = self.pilot(connected=False)
        with self.assertRaisesRegex(mcp_http.ToolError, "offline"):
            p.return_home({"reason": "x"})
        p, v = self.pilot(armed=True, mode="AUTO")
        v.set_mode = lambda name, timeout=8.0: (_ for _ in ()).throw(RuntimeError("no ack"))
        with self.assertRaisesRegex(mcp_http.ToolError, "did not take RTL"):
            p.return_home({"reason": "x"})

    def test_the_tool_is_for_every_kind(self):
        for kind in ("quad", "rover", "plane"):
            tool = vehicle_pilot.return_home_tool(kind)
            self.assertEqual(tool["name"], "return_home")
            self.assertEqual(tool["inputSchema"]["required"], ["reason"])
            self.assertIn("no person", tool["description"])


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


class RestockTest(unittest.TestCase):
    def test_no_second_restock_while_one_waits_for_a_person(self):
        b = fleet_base.Base({}, {"stations": [], "depot": {"lat": 51.9402, "lon": 4.00485}}, [], "/dev/null")
        b.states["supply-0"] = {"role": "supply"}
        fired = []
        b.dispatch.fire = lambda *a: fired.append(a) or "task-1"
        b.cp = type("CP", (), {"waiting": lambda self: [{"botId": "pilot-supply-0"}]})()
        self.assertTrue(b._supply_waiting())
        b.cp = type("CP", (), {"waiting": lambda self: [{"botId": "pilot-quad-0"}]})()
        self.assertFalse(b._supply_waiting())


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


class SimulatorCameraTest(unittest.TestCase):
    """PR C: the photo is the gz world's camera's, fetched by the vehicle."""

    def test_the_pilot_keeps_and_serves_each_photo(self):
        folder = tempfile.mkdtemp()
        cond = threading.Condition()
        photos = vehicle_pilot.PhotoFetcher("http://fleet-gz:8081/latest/quad-0.jpg", folder, cond, get=lambda url: b"jpeg-bytes")
        shot = {"n": 7, "photo": None}
        photos.fetch(shot)
        self.assertEqual(shot["photo"], "/photos/7.jpg")
        with open(photos.path(7), "rb") as f:
            self.assertEqual(f.read(), b"jpeg-bytes")
        def down(url):
            raise OSError("refused")
        failed = {"n": 8, "photo": None}
        vehicle_pilot.PhotoFetcher("http://x", folder, cond, get=down).fetch(failed)
        self.assertEqual(failed["photo"], "")                # no photo: the station uses the aerial one
        # Only the newest 200 are kept.
        photos.fetch({"n": 207, "photo": None})
        self.assertFalse(os.path.exists(photos.path(7)))
        vehicle_pilot.StateHandler.photos = photos
        srv = ThreadingHTTPServer(("127.0.0.1", 0), vehicle_pilot.StateHandler)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        base = f"http://127.0.0.1:{srv.server_address[1]}"
        try:
            r = urllib.request.urlopen(base + "/photos/207.jpg")
            self.assertEqual(r.headers["Content-Type"], "image/jpeg")
            self.assertEqual(vision.fetch_vehicle_photo(base + "/state", "/photos/207.jpg"), b"jpeg-bytes")
            for path in ("/photos/7.jpg", "/photos/..%2F207.jpg", "/photos/x.jpg"):
                with self.assertRaises(urllib.error.HTTPError):
                    urllib.request.urlopen(base + path)
        finally:
            srv.shutdown()
            vehicle_pilot.StateHandler.photos = None
        with self.assertRaises(ValueError):
            vision.fetch_vehicle_photo(base + "/state", "/etc/passwd")

    def test_the_station_waits_for_the_vehicle_s_photo(self):
        v = vision.Vision(tempfile.mkdtemp(), "m", "", 1, 12, workers=0)
        v.jobs = __import__("queue").Queue()
        now = time.time()
        flight = {"started_at": 1, "shots": [{"n": 1, "lat": 51.95, "lon": 3.99, "alt_m": 35, "yaw": 90, "t": now, "photo": None}]}
        v.offer("quad-0", "quad", flight)
        self.assertTrue(v.jobs.empty())                       # still fetching
        flight["shots"][0]["photo"] = "/photos/1.jpg"
        v.offer("quad-0", "quad", flight)
        job = v.jobs.get_nowait()
        self.assertEqual((job["photo"], job["yaw"]), ("/photos/1.jpg", 90))

    def test_the_lens_frame_has_no_floor(self):
        x1, _, x2, _ = vision.frame(51.95, 3.99, 35, min_width=0)
        self.assertAlmostEqual((x2 - x1) * math.cos(math.radians(51.95)), 63, delta=0.5)
        x1, _, x2, _ = vision.frame(51.95, 3.99, 35)
        self.assertAlmostEqual((x2 - x1) * math.cos(math.radians(51.95)), 120, delta=0.5)

    def test_a_detection_turns_with_the_heading(self):
        lat, lon = 51.95, 3.99
        bbox = vision.frame(lat, lon, 35, min_width=0)       # 63 x 47 m
        # The top centre of the image is the bow: 23.6 m ahead.
        for yaw, north, east in ((0, 23.6, 0), (90, 0, 23.6), (180, -23.6, 0), (270, 0, -23.6)):
            plat, plon = incident.Incident._turned(0.5, 0, bbox, yaw)
            n, e = geo.offset_m(lat, lon, plat, plon)
            self.assertAlmostEqual(n, north, delta=0.3)
            self.assertAlmostEqual(e, east, delta=0.3)
        # The right edge is starboard: heading east, that is south.
        n, e = geo.offset_m(lat, lon, *incident.Incident._turned(1, 0.5, bbox, 90))
        self.assertAlmostEqual(n, -31.5, delta=0.3)
        self.assertAlmostEqual(e, 0, delta=0.3)

    def test_a_turned_photo_finds_its_container(self):
        now = 1_800_000_000.0
        inc = incident.Incident(REPORT, COAST, now=now, seed=3)
        c = next(c for c in inc.sea.containers if c.id == "C02")
        clat, clon = inc.sea.position(c.north, c.east)
        # The quad is 15 m west of the container heading east: it is 15 m ahead, above centre.
        qlat, qlon = geo.point_at(clat, clon, 0, -15)
        bbox = vision.frame(qlat, qlon, 30, min_width=0)
        fy = 0.5 - 15 / ((bbox[3] - bbox[1]) * math.cos(math.radians(qlat)))
        seen = inc.observe("quad-0", "quad", 30, now + 1, [{"x": 0.5, "y": fy}], bbox, "q", yaw=90)
        self.assertEqual(seen[0]["container"], "C02")
        # Without the turn the same detection lands over 20 m off.
        n, e = geo.offset_m(clat, clon, *incident.merc_to_latlon((bbox[0] + bbox[2]) / 2, bbox[3] - fy * (bbox[3] - bbox[1])))
        self.assertGreater(math.hypot(n, e), 20)


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


class AisApiTest(unittest.TestCase):
    """The ais service against its OpenAPI file, and the base reading it."""

    SPEC = json.load(open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "ais-openapi.json")))
    TYPES = {"string": str, "integer": int, "number": (int, float), "object": dict, "array": list, "null": type(None), "boolean": bool}

    def check(self, value, schema, where="$"):
        if "$ref" in schema:
            return self.check(value, self.SPEC["components"]["schemas"][schema["$ref"].rsplit("/", 1)[1]], where)
        if "oneOf" in schema:
            errors = []
            for option in schema["oneOf"]:
                try:
                    return self.check(value, option, where)
                except AssertionError as exc:
                    errors.append(str(exc))
            self.fail(f"{where}: matches no option: {errors}")
        kinds = schema.get("type")
        if kinds:
            kinds = kinds if isinstance(kinds, list) else [kinds]
            ok = any(isinstance(value, self.TYPES[k]) and not (k in ("integer", "number") and isinstance(value, bool)) for k in kinds)
            self.assertTrue(ok, f"{where}: {value!r} is not {kinds}")
        if "enum" in schema:
            self.assertIn(value, schema["enum"], where)
        if isinstance(value, dict):
            for key in schema.get("required", []):
                self.assertIn(key, value, f"{where}: {key} missing")
            for key, sub in schema.get("properties", {}).items():
                if key in value:
                    self.check(value[key], sub, f"{where}.{key}")
        if isinstance(value, list) and "items" in schema:
            for i, item in enumerate(value):
                self.check(item, schema["items"], f"{where}[{i}]")

    def response(self, path, status):
        template = next(t for t in self.SPEC["paths"] if re.fullmatch(re.sub(r"\{[^}]+\}", "[^/]+", t), path.split("?")[0]))
        return self.SPEC["paths"][template]["get"]["responses"][str(status)]["content"]["application/json"]["schema"]

    def get(self, path):
        try:
            with urllib.request.urlopen(self.url + path, timeout=3) as r:
                return r.status, json.load(r)
        except urllib.error.HTTPError as exc:
            return exc.code, json.load(exc)

    def setUp(self):
        a = self.ais = ais.AIS("", [[51.9, 3.8], [52.05, 4.15]])
        a.handle({"MessageType": "PositionReport", "MetaData": {"MMSI": 244123456, "ShipName": "NORDIC STAR", "latitude": 51.95, "longitude": 4.0},
                  "Message": {"PositionReport": {"Sog": 102.3, "Cog": 360, "TrueHeading": 511, "NavigationalStatus": 15}}})
        a.handle({"MessageType": "ShipStaticData", "MetaData": {"MMSI": 244123456},
                  "Message": {"ShipStaticData": {"Type": 71, "Dimension": {"A": 200, "B": 100, "C": 24, "D": 24}, "MaximumStaticDraught": 12.5, "Name": "NORDIC STAR"}}})
        a.handle({"MessageType": "PositionReport", "MetaData": {"MMSI": 211000001, "ShipName": "TUG ONE", "latitude": 51.99, "longitude": 4.1},
                  "Message": {"PositionReport": {"Sog": 8.2, "Cog": 90.5, "TrueHeading": 91, "NavigationalStatus": 0}}})
        ais.Handler.ais = a
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), ais.Handler)
        self.server.daemon_threads = True
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()

    def test_every_answer_matches_the_openapi_file(self):
        for path, status in [("/v1/ships", 200), ("/v1/ships?bbox=51.9,3.9,51.96,4.05", 200), ("/v1/ships?type=cargo&name=nordic", 200),
                             ("/v1/ships?bbox=x", 400), ("/v1/ships/244123456", 200), ("/v1/ships/999", 404),
                             ("/v1/ships/244123456/track?since=0", 200), ("/v1/ships/244123456/track?since=2026-10-26T18:00:00Z", 200),
                             ("/v1/ships/244123456/track?since=yesterday", 400)]:
            got, body = self.get(path)
            self.assertEqual(got, status, path)
            self.check(body, self.response(path, status), path)

    def test_filters_and_not_available_values(self):
        _, body = self.get("/v1/ships?bbox=51.9,3.9,51.96,4.05")
        self.assertEqual([s["mmsi"] for s in body["ships"]], ["244123456"])
        _, body = self.get("/v1/ships?type=70&name=Star")
        self.assertEqual([s["name"] for s in body["ships"]], [])          # 71 is cargo, but not type 70
        _, body = self.get("/v1/ships?type=CARGO&name=star")
        self.assertEqual([s["name"] for s in body["ships"]], ["NORDIC STAR"])
        _, ship = self.get("/v1/ships/244123456")
        self.assertEqual((ship["sog"], ship["cog"], ship["heading"], ship["nav_status"]), (None, None, None, None))
        self.assertEqual((ship["length_m"], ship["beam_m"], ship["dim"], ship["draught_m"], ship["type"]), (300, 48, {"a": 200, "b": 100, "c": 24, "d": 24}, 12.5, 71))
        _, track = self.get("/v1/ships/211000001/track")
        self.assertEqual([(p["lat"], p["sog"]) for p in track["points"]], [(51.99, 8.2)])

    def test_the_stream_sends_each_update_and_the_base_reads_the_list(self):
        events = []
        def read():
            with urllib.request.urlopen(self.url + "/v1/stream", timeout=5) as r:
                for line in r:
                    if line.startswith(b"event:") or line.startswith(b"data:"):
                        events.append(line.decode().strip())
                    if len(events) >= 2:
                        return
        reader = threading.Thread(target=read, daemon=True)
        reader.start()
        for _ in range(50):
            if self.ais.subscribers:
                break
            time.sleep(0.02)
        self.ais.handle({"MessageType": "PositionReport", "MetaData": {"MMSI": 211000001, "latitude": 51.991, "longitude": 4.1}, "Message": {"PositionReport": {"Sog": 8.0, "Cog": 91}}})
        reader.join(3)
        self.assertEqual(events[0], "event: position")
        self.check(json.loads(events[1][len("data:"):]), {"$ref": "#/components/schemas/Ship"})
        remote = ais.Remote(self.url)
        remote.start()
        for _ in range(50):
            if remote.ships():
                break
            time.sleep(0.05)
        self.assertEqual(sorted(s["mmsi"] for s in remote.ships()), ["211000001", "244123456"])
        self.assertEqual(remote.summary()["status"], "live")


class ApprovalsTest(unittest.TestCase):
    def test_a_bot_a_person_added_shows_the_route_of_the_vehicle_it_runs_on(self):
        class CP:
            enabled = True
            asked = []

            def get(self, path):
                self.asked.append(path)
                return {"placement": {"nodes": ["quad-1"]}} if path == "/v1/bots/operador-quad-1" else {}
        a = approvals.Approvals(CP(), "", lambda v, args: (None, 0))
        self.assertEqual(a.vehicle_of("pilot-quad-0"), "quad-0")
        self.assertEqual(a.vehicle_of("operador-quad-1"), "quad-1")
        self.assertEqual(a.vehicle_of("operador-quad-1"), "quad-1")
        self.assertEqual(a.vehicle_of("dispatcher"), "")
        self.assertEqual(CP.asked, ["/v1/bots/operador-quad-1", "/v1/bots/dispatcher"])

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
