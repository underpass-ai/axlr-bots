"""Unit tests of the port fleet's services: python -m unittest -v test_fleet (in the app image)."""

import json
import os
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

os.environ.setdefault("PHOTOS_DIR", tempfile.mkdtemp())

import fleet_base
import geo
import mcp_http
import vehicle_pilot


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
        for path, ctype in (("/", "text/html"), ("/3d", "text/html"), ("/port.glb", "model/gltf-binary")):
            with urllib.request.urlopen(url + path) as r:
                self.assertEqual(r.headers["Content-Type"].split(";")[0], ctype, path)
                body = r.read()
            if path == "/port.glb":
                self.assertEqual(body[:4], b"glTF")
        with self.assertRaises(urllib.error.HTTPError) as e:
            urllib.request.urlopen(url + "/../fleet_base.py")
        self.assertEqual(e.exception.code, 404)


if __name__ == "__main__":
    unittest.main()
