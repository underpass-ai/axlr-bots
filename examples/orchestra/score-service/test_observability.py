"""The Grafana dashboards against the metrics: a panel that asks for a name nobody exposes stays empty forever."""

import glob
import importlib.util
import json
import os
import re
import unittest

import metrics
import synth
import team_exporter
import telemetry
from test_hands import hands

HERE = os.path.dirname(os.path.abspath(__file__))
OBSERVABILITY = os.path.join(HERE, "..", "observability")
BUILTIN = {"up"}
NAME = re.compile(r"\b((?:orchestra|axlr_team|process)_[a-zA-Z0-9_]+)\b")


def exposed():
    """Every name the score service (with its take, hands and stream watched) and the team exporter can expose."""
    registry = metrics.Registry()
    h, o, _, _, _ = hands()
    telemetry.watch_orchestra(o, registry=registry)
    telemetry.watch_hands(h, registry=registry)
    telemetry.watch_stream(synth.Stream(), registry=registry)
    return telemetry.REGISTRY.names() | registry.names() | team_exporter.Team("t").registry.names() | BUILTIN


def dashboards():
    out = {}
    for path in sorted(glob.glob(os.path.join(OBSERVABILITY, "grafana", "dashboards", "*.json"))):
        with open(path, encoding="utf-8") as f:
            out[os.path.basename(path)] = json.load(f)
    return out


class DashboardsTest(unittest.TestCase):
    def test_there_are_three(self):
        self.assertEqual(sorted(dashboards()), ["conductor.json", "orchestra.json", "pipeline.json"])

    def test_every_metric_a_panel_asks_for_is_exposed(self):
        names, missing = exposed(), []
        for file, d in dashboards().items():
            for p in d["panels"]:
                for t in p.get("targets", ()):
                    for name in NAME.findall(t["expr"]):
                        if name not in names:
                            missing.append(f"{file} · {p['title']}: {name}")
        self.assertEqual(missing, [])

    def test_panels_use_the_provisioned_datasource_and_fit_the_grid(self):
        for file, d in dashboards().items():
            ids = [p["id"] for p in d["panels"]]
            self.assertEqual(len(ids), len(set(ids)), file)
            self.assertEqual({v["name"] for v in d["templating"]["list"]}, {"job", "team"})
            for p in d["panels"]:
                g = p["gridPos"]
                self.assertLessEqual(g["x"] + g["w"], 24, f"{file} · {p['title']}")
                if p["type"] != "row":
                    self.assertEqual(p["datasource"], {"type": "prometheus", "uid": "prometheus"}, p["title"])
                    self.assertTrue(p["targets"], p["title"])
                for t in p.get("targets", ()):
                    self.assertEqual(t["expr"].count("{"), t["expr"].count("}"), t["expr"])
                    self.assertEqual(t["expr"].count("("), t["expr"].count(")"), t["expr"])
                    if "orchestra_" in t["expr"]:
                        self.assertIn('job="$job"', t["expr"].replace('job=~"$job|team"', 'job="$job"'), t["expr"])

    def test_the_files_are_what_dashboards_py_writes(self):
        spec = importlib.util.spec_from_file_location("dashboards", os.path.join(OBSERVABILITY, "dashboards.py"))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        built = module.build()
        for name, d in dashboards().items():
            self.assertEqual(module.text(built[name]), module.text(d), f"{name}: run python3 observability/dashboards.py")

    def test_the_prometheus_jobs_are_the_ones_the_dashboards_name(self):
        with open(os.path.join(OBSERVABILITY, "prometheus", "prometheus.yml")) as f:
            jobs = re.findall(r"job_name:\s*(\S+)", f.read())
        self.assertEqual(jobs, ["score", "score-test", "team", "prometheus"])


if __name__ == "__main__":
    unittest.main()
