"""Tests of the fleet-gz scripts that run without gz: python -m unittest test_gz."""

import importlib.machinery
import importlib.util
import json
import math
import os
import sys
import tempfile
import threading
import time
import unittest
import urllib.request
import xml.etree.ElementTree as ET

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "port3d"))
import area  # noqa: E402


def load(name, filename):
    loader = importlib.machinery.SourceFileLoader(name, os.path.join(HERE, filename))
    spec = importlib.util.spec_from_loader(name, loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


fleet_sim = load("fleet_sim", "fleet-sim")
port_iris = load("port_iris", "port_iris.py")
fpv_server = load("fpv_server", "fpv-server.py")
ais_bridge = load("ais_bridge", "ais-bridge.py")
MODELS = os.path.join(HERE, "models")
VEHICLES = os.path.join(HERE, "vehicles.example.json")


class FleetSimTest(unittest.TestCase):
    def setUp(self):
        self.vehicles = fleet_sim.load_vehicles(VEHICLES)

    def test_to_local_is_area_to_local(self):
        for lat, lon in [(51.9455, 3.99735), (51.920, 3.965), (51.965, 4.045)]:
            self.assertEqual(fleet_sim.to_local(lat, lon), area.to_local(lat, lon))

    def test_pad_pose(self):
        quad = {"name": "q", "kind": "quad", "lat": 51.944, "lon": 3.999, "heading": 0, "port": 9002}
        x, y, z, yaw = fleet_sim.pad_pose(quad)
        self.assertAlmostEqual(x, 0)
        self.assertAlmostEqual(y, 0)
        self.assertAlmostEqual(z, 5.2)
        self.assertAlmostEqual(yaw, math.pi / 2)  # north is 90 degrees from east
        rover = dict(quad, kind="rover", heading=90)
        self.assertAlmostEqual(fleet_sim.pad_pose(rover)[2], 5.4)
        self.assertAlmostEqual(fleet_sim.pad_pose(rover)[3], 0)  # east

    def test_example_is_the_chart_pads_without_the_plane(self):
        self.assertEqual([v["name"] for v in self.vehicles],
                         ["quad-0", "quad-1", "quad-2", "rover-0", "rover-1", "supply-0"])
        self.assertEqual([v["port"] for v in self.vehicles], [9002, 9012, 9022, 9032, 9042, 9052])

    def test_templates_fill_and_parse(self):
        for v in self.vehicles:
            sdf = fleet_sim.model_sdf(v, MODELS)
            self.assertNotIn("{NAME}", sdf)
            self.assertNotIn("{PORT}", sdf)
            root = ET.fromstring(sdf)
            model = root.find("model")
            self.assertEqual(model.get("name"), v["name"])
            plugin = model.find("plugin[@name='ArduPilotPlugin']")
            self.assertEqual(plugin.findtext("fdm_addr"), "0.0.0.0")
            self.assertEqual(plugin.findtext("fdm_port_in"), str(v["port"]))
            self.assertEqual(plugin.findtext("lock_step"), "0")
            self.assertEqual(plugin.findtext("connectionTimeoutMaxCount"), "1000")
            topics = {s.findtext("topic") for s in model.iter("sensor") if s.get("type") == "camera"}
            self.assertEqual(topics, set(fleet_sim.camera_topics(v)))
            for s in model.iter("sensor"):
                if s.get("type") == "camera":
                    self.assertTrue(s.find("camera/save").get("enabled") == "true")

    def test_quad_rates_and_rangefinders(self):
        sdf = ET.fromstring(fleet_sim.model_sdf(self.vehicles[0], MODELS)).find("model")
        cams = {s.get("name"): s for s in sdf.iter("sensor") if s.get("type") == "camera"}
        self.assertEqual(cams["nadir"].findtext("update_rate"), "2")
        self.assertEqual(cams["nadir"].findtext("camera/image/width"), "640")
        self.assertEqual(cams["fpv"].findtext("update_rate"), "5")
        self.assertEqual(cams["fpv"].findtext("camera/image/width"), "960")
        lidars = [s.findtext("topic") for s in sdf.iter("sensor") if s.get("type") == "gpu_lidar"]
        self.assertEqual(lidars, ["/quad-0/rng1", "/quad-0/rng2", "/quad-0/rng3"])

    def test_rover_has_no_chase_camera(self):
        sdf = fleet_sim.model_sdf(self.vehicles[3], MODELS)
        self.assertNotIn("chase_camera", sdf)
        self.assertNotIn("port_rover/rng", sdf)

    def test_create_request(self):
        req = fleet_sim.create_request(self.vehicles[0], "<sdf/>")
        self.assertIn('name: "quad-0"', req)
        self.assertIn('sdf: "<sdf/>"', req)
        self.assertIn("allow_renaming: false", req)
        self.assertIn("z: 5.200", req)

    def test_load_vehicles_refuses(self):
        for bad in ([{"name": "plane-0", "kind": "plane", "port": 1}],
                    [{"name": "Quad 0", "kind": "quad", "port": 1}],
                    [{"name": "q", "kind": "quad", "port": 1}, {"name": "q", "kind": "quad", "port": 2}]):
            with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
                json.dump(bad, f)
            with self.assertRaises(ValueError):
                fleet_sim.load_vehicles(f.name)
            os.unlink(f.name)

    def test_parse_rtf(self):
        self.assertEqual(fleet_sim.parse_rtf("sim_time {\n}\nreal_time_factor: 0.9731\n"), 0.9731)
        self.assertIsNone(fleet_sim.parse_rtf(""))


class PortModelTest(unittest.TestCase):
    """What a vehicle stands on is the sea and the land's top, never the port's mesh."""

    def test_collisions_are_simple(self):
        link = ET.parse(os.path.join(MODELS, "maasvlakte2", "model.sdf")).find("model/link")
        self.assertIn("port.glb", link.find("visual/geometry/mesh/uri").text)
        cols = {c.get("name"): c for c in link.findall("collision")}
        self.assertEqual(set(cols), {"sea", "ground"})
        self.assertIsNotNone(cols["sea"].find("geometry/box"))
        sea_top = float(cols["sea"].findtext("pose").split()[2]) + float(cols["sea"].findtext("geometry/box/size").split()[2]) / 2
        self.assertEqual(sea_top, 0)
        self.assertTrue(cols["ground"].findtext("geometry/mesh/uri").endswith("ground.obj"))

    def test_ground_is_the_land_top_facing_up(self):
        verts, faces = [], []
        with open(os.path.join(MODELS, "maasvlakte2", "meshes", "ground.obj")) as f:
            for line in f:
                kind, *rest = line.split()
                if kind == "v":
                    verts.append(tuple(map(float, rest)))
                elif kind == "f":
                    faces.append([int(r.split("/")[0]) - 1 for r in rest])
        self.assertTrue(all(z == area.GROUND_M for _, _, z in verts))
        self.assertLess(len(faces), 5000)
        x0, y0, x1, y1 = area.bounds_local()
        self.assertTrue(all(x0 - 1 <= x <= x1 + 1 and y0 - 1 <= y <= y1 + 1 for x, y, _ in verts))
        for a, b, c in faces:
            (ax, ay, _), (bx, by, _), (cx, cy, _) = verts[a], verts[b], verts[c]
            self.assertGreater((bx - ax) * (cy - ay) - (by - ay) * (cx - ax), 0)


class PortIrisTest(unittest.TestCase):
    IRIS = """<model name='iris_with_standoffs'><link name='base_link'>
      <collision name='base_collision'>
        <pose>0 0 0 0 0 0</pose>
        <geometry>
          <mesh>
            <uri>model://iris_with_standoffs/meshes/iris_collision.stl</uri>
          </mesh>
        </geometry>
      </collision>""" + "".join(f"""
      <collision name='{leg}_leg_collision'>
        <pose>{x} {y} -0.11 0 0 0</pose>
        <geometry>
          <cylinder>
            <radius>0.005</radius>
            <length>0.17</length>
          </cylinder>
        </geometry>
      </collision>""" for leg, x, y in (("front_left", 0.123, 0.22), ("front_right", 0.123, -0.22),
                                         ("rear_left", -0.140, 0.21), ("rear_right", -0.140, -0.21))) + "</link></model>"

    def test_box_body_sphere_feet(self):
        link = ET.fromstring(port_iris.simplify(self.IRIS)).find("link")
        shapes = {c.get("name"): c.find("geometry")[0].tag for c in link.findall("collision")}
        self.assertEqual(shapes.pop("base_collision"), "box")
        self.assertEqual(set(shapes.values()), {"sphere"})
        for c in link.findall("collision")[1:]:
            z = float(c.findtext("pose").split()[2])
            self.assertAlmostEqual(z - float(c.findtext("geometry/sphere/radius")), -0.195)

    def test_refuses_another_iris(self):
        with self.assertRaises(ValueError):
            port_iris.simplify(self.IRIS.replace("0.17", "0.2"))

    def test_quad_includes_it_under_its_name(self):
        inc = ET.fromstring(fleet_sim.model_sdf(fleet_sim.load_vehicles(VEHICLES)[0], MODELS)).find("model/include")
        self.assertEqual(inc.findtext("uri"), "model://port_iris")
        self.assertEqual(inc.findtext("name"), "iris_with_standoffs")
class AisBridgeTest(unittest.TestCase):
    NOW = 1_800_000_000

    def fleet(self, **ship):
        base = {"mmsi": 244123456, "name": "SECRET NAME", "lat": 51.95, "lon": 4.0, "sog": 10, "cog": 90, "heading": 80,
                "length_m": 200, "beam_m": 30, "type_name": "cargo",
                "last_seen": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(self.NOW - 30))}
        base.update(ship)
        far = dict(base, mmsi=1, lat=52.02)          # outside the port model
        return {"ships": [base, far], "incident": {"status": "searching", "containers": [
            {"id": "C01", "lat": 51.93, "lon": 3.97, "heading_deg": 45, "length_m": 12.2}]}}

    def test_wanted_filters_and_extrapolates(self):
        want = ais_bridge.wanted(self.fleet(), self.NOW)
        self.assertEqual(sorted(want), ["box-c01", "ship-244123456"])
        sdf, (x, y, z, yaw) = want["ship-244123456"]
        east, north = area.to_local(51.95, 4.0)
        self.assertAlmostEqual(x - east, 10 * 0.5144 * 30, delta=0.5)     # 30 s east at 10 kn
        self.assertAlmostEqual(y, north, delta=0.5)
        self.assertAlmostEqual(z, 0.2 * 12)                                 # H = 0.06 L
        self.assertAlmostEqual(yaw, math.radians(10))                       # heading 80, not course 90
        self.assertNotIn("SECRET", sdf)
        root = ET.fromstring(sdf)
        self.assertEqual(root.find("model/link/visual/geometry/box/size").text, "200.0 30.0 12.0")
        old = ais_bridge.wanted(self.fleet(last_seen="2020-01-01T00:00:00Z"), self.NOW)["ship-244123456"][1]
        self.assertAlmostEqual(old[0] - east, 10 * 0.5144 * 60, delta=0.5)  # at most 60 s
        box = want["box-c01"][1]
        self.assertAlmostEqual(box[2], 0.3)
        self.assertEqual(ais_bridge.wanted(self.fleet(), self.NOW, ships=False, containers=False), {})
        ended = self.fleet()
        ended["incident"]["status"] = "ended"
        self.assertEqual(sorted(ais_bridge.wanted(ended, self.NOW)), ["ship-244123456"])

    def test_ship_without_data(self):
        sdf, h = ais_bridge.ship_sdf("ship-1", {"heading": 511})
        self.assertIn("30.0 8.0 3.0", sdf)
        self.assertIn(ais_bridge.rgb("#56606b"), sdf)
        sdf, _ = ais_bridge.ship_sdf("ship-2", {"type_name": "passenger", "length_m": 100, "beam_m": 20})
        house = ET.fromstring(sdf).find("model/link/visual[@name='house']")
        self.assertEqual(house.find("pose").text, "0.0 0 7.5 0 0 0")         # amidships; H 6, HH 9
        _, (_, _, _, yaw) = list(ais_bridge.wanted({"ships": [{"mmsi": 3, "lat": 51.95, "lon": 4.0, "cog": 180, "heading": 511}]},
                                                    self.NOW).values())[0]
        self.assertAlmostEqual(yaw, math.radians(-90))                      # course when no heading

    def test_one_pose_call_a_tick_and_removal(self):
        calls = []
        bridge = ais_bridge.Bridge(lambda s, t, r: calls.append((s, r)) or True)
        bridge.tick(self.fleet(), self.NOW)
        services = [s.rsplit("/", 1)[1] for s, _ in calls]
        self.assertEqual(services, ["create", "create", "set_pose_vector"])
        self.assertEqual(calls[-1][1].count("pose: {"), 2)
        self.assertIn('name: "ship-244123456"', calls[-1][1])
        calls.clear()
        bridge.tick(self.fleet(), self.NOW + 1)
        self.assertEqual([s.rsplit("/", 1)[1] for s, _ in calls], ["set_pose_vector"])
        calls.clear()
        bridge.tick({"ships": []}, self.NOW + 2)
        self.assertEqual(sorted(r for s, r in calls), ['name: "box-c01" type: MODEL', 'name: "ship-244123456" type: MODEL'])
        self.assertEqual(bridge.counts(), (0, 0))
        # Static data arriving: the ship is made again at its size.
        bridge.tick(self.fleet(length_m=None), self.NOW)
        calls.clear()
        bridge.tick(self.fleet(), self.NOW)
        self.assertEqual([s.rsplit("/", 1)[1] for s, _ in calls], ["remove", "create", "set_pose_vector"])

    def test_create_request_parses_as_text(self):
        sdf, pose = ais_bridge.wanted(self.fleet(), self.NOW)["box-c01"]
        req = ais_bridge.create_request("box-c01", sdf, pose)
        self.assertTrue(req.startswith("sdf: \""))
        self.assertIn("allow_renaming: false pose: {position:", req)


class FpvServerTest(unittest.TestCase):
    def setUp(self):
        from PIL import Image
        self.dir = tempfile.TemporaryDirectory()
        self.root = self.dir.name
        for cam in ("fpv", "nadir"):
            os.makedirs(os.path.join(self.root, "quad-0", cam))
            for i in range(25):
                p = os.path.join(self.root, "quad-0", cam, f"{i:04d}.png")
                Image.new("RGB", (64, 48), (i * 10, 0, 0)).save(p)
                os.utime(p, (1000 + i, 1000 + i))

    def tearDown(self):
        self.dir.cleanup()

    def test_newest_and_prune(self):
        folder = os.path.join(self.root, "quad-0", "fpv")
        self.assertTrue(fpv_server.newest_stable(folder, settle=0).endswith("0024.png"))
        self.assertIsNone(fpv_server.newest_stable(os.path.join(self.root, "none"), settle=0))
        self.assertEqual(fpv_server.prune(folder), 5)
        self.assertEqual(len(os.listdir(folder)), 20)

    def test_label(self):
        from PIL import Image
        import io
        path = os.path.join(self.root, "quad-0", "fpv", "0024.png")
        im = Image.open(io.BytesIO(fpv_server.to_jpeg(path, fpv_server.LABEL)))
        self.assertEqual(im.format, "JPEG")
        self.assertLess(sum(im.getpixel((2, im.height - 2))), 60)  # the label's black band

    def test_http(self):
        fpv_server.Handler.frames = self.root
        srv = fpv_server.Server(("127.0.0.1", 0), fpv_server.Handler)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        base = f"http://127.0.0.1:{srv.server_address[1]}"
        try:
            self.assertEqual(urllib.request.urlopen(base + "/healthz").read(), b"ok\n")
            for path in ("/latest/quad-0.jpg", "/latest/quad-0-fpv.jpg"):
                r = urllib.request.urlopen(base + path)
                self.assertEqual(r.headers["Content-Type"], "image/jpeg")
                self.assertEqual(r.read()[:2], b"\xff\xd8")
            for path in ("/latest/rover-9.jpg", "/latest/..%2Fetc.jpg", "/nope"):
                with self.assertRaises(urllib.error.HTTPError) as e:
                    urllib.request.urlopen(base + path)
                self.assertEqual(e.exception.code, 404)
            r = urllib.request.urlopen(base + "/fpv/quad-0")
            self.assertTrue(r.headers["Content-Type"].startswith("multipart/x-mixed-replace"))
            self.assertEqual(r.readline(), b"--frame\r\n")
            r.close()
        finally:
            srv.shutdown()


if __name__ == "__main__":
    unittest.main()
