"""The Prometheus registry and GET /metrics: the text format a scrape reads, and who may read it."""

import re
import threading
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

import mcp_http
import metrics
import server
import telemetry
from test_hands import hands
from test_score_service import CONFIG

SAMPLE = re.compile(r'^[a-zA-Z_:][a-zA-Z0-9_:]*(\{([a-zA-Z_][a-zA-Z0-9_]*="(\\.|[^"\\])*",?)*\})? (-?[0-9.e+-]+|\+Inf|-Inf|NaN)$')


def assert_exposition(test, text):
    """Every line a HELP, a TYPE, a comment or a sample Prometheus can parse; one HELP and TYPE per family."""
    seen = set()
    for line in text.splitlines():
        if line.startswith("# HELP ") or line.startswith("# TYPE "):
            key = tuple(line.split(" ", 3)[1:3])
            test.assertNotIn(key, seen, line)
            seen.add(key)
            continue
        if line.startswith("#"):
            continue
        test.assertRegex(line, SAMPLE)


class RegistryTest(unittest.TestCase):
    def test_counter_gauge_and_histogram_render_as_prometheus_reads_them(self):
        r = metrics.Registry()
        c = r.counter("x_calls_total", "Calls.", ("tool",))
        c.labels(tool="play").inc()
        c.labels("play").inc(2)
        g = r.gauge("x_level", "A level.")
        g.set(0.25)
        h = r.histogram("x_seconds", "Time.", ("kind",), buckets=(0.1, 1))
        for v in (0.05, 0.5, 5):
            h.labels("a").observe(v)
        text = r.render().decode()
        assert_exposition(self, text)
        self.assertIn("# TYPE x_calls_total counter\n", text)
        self.assertIn('x_calls_total{tool="play"} 3\n', text)
        self.assertIn("x_level 0.25\n", text)
        # buckets are cumulative, +Inf counts everything, then the sum and the count
        self.assertIn('x_seconds_bucket{kind="a",le="0.1"} 1\nx_seconds_bucket{kind="a",le="1"} 2\nx_seconds_bucket{kind="a",le="+Inf"} 3\n'
                      'x_seconds_sum{kind="a"} 5.55\nx_seconds_count{kind="a"} 3\n', text)

    def test_label_values_are_escaped(self):
        r = metrics.Registry()
        r.counter("x_total", "Line one\nline two \\ end.", ("why",)).labels('say "hi"\\\nnow').inc()
        text = r.render().decode()
        assert_exposition(self, text)
        self.assertIn('x_total{why="say \\"hi\\"\\\\\\nnow"} 1', text)
        self.assertIn("# HELP x_total Line one\\nline two \\\\ end.", text)

    def test_a_counter_only_goes_up_and_names_its_total(self):
        r = metrics.Registry()
        with self.assertRaises(ValueError):
            r.counter("x_calls", "No _total.")
        c = r.counter("x_total", "Calls.")
        with self.assertRaises(ValueError):
            c.inc(-1)
        with self.assertRaises(ValueError):
            c.labels().set(3)

    def test_labels_must_match_and_names_are_unique(self):
        r = metrics.Registry()
        c = r.counter("x_total", "Calls.", ("a", "b"))
        with self.assertRaises(ValueError):
            c.labels("only one")
        with self.assertRaises(ValueError):
            c.labels(a="1", c="2")
        with self.assertRaises(ValueError):
            c.inc()  # it has labels
        with self.assertRaises(ValueError):
            r.gauge("x_total", "Again.")
        with self.assertRaises(ValueError):
            r.histogram("x_h", "No le.", ("le",))

    def test_a_computed_family_is_read_on_each_scrape_and_its_failure_hides_nothing_else(self):
        r = metrics.Registry()
        level = {"v": 1}
        r.computed("x_now", "Now.", ("side",), lambda: [(("left",), level["v"])])
        r.computed("x_broken", "Broken.", (), lambda: 1 / 0)
        r.gauge("x_other", "Other.").set(7)
        self.assertIn('x_now{side="left"} 1\n', r.render().decode())
        level["v"] = 2
        text = r.render().decode()
        assert_exposition(self, text)
        self.assertIn('x_now{side="left"} 2\n', text)
        self.assertIn("# x_broken not collected: ZeroDivisionError", text)
        self.assertIn("x_other 7\n", text)

    def test_names_cover_a_histograms_series(self):
        r = metrics.Registry()
        r.histogram("x_seconds", "Time.")
        r.counter("x_total", "Calls.")
        self.assertEqual(r.names(), {"x_seconds", "x_seconds_bucket", "x_seconds_sum", "x_seconds_count", "x_total"})

    def test_numbers(self):
        self.assertEqual([metrics.number(v) for v in (3, 3.0, 0.5, float("inf"), float("-inf"), True)], ["3", "3", "0.5", "+Inf", "-Inf", "1"])
        self.assertEqual(metrics.number(float("nan")), "NaN")

    def test_a_reason_is_a_bounded_label(self):
        self.assertEqual(metrics.reason("budget_exceeded: 128 tool calls"), metrics.reason("budget_exceeded: 129 tool calls"))
        self.assertEqual(metrics.reason("the same gesture was sent 1.2 s ago"), "the same gesture was sent N s ago")
        self.assertEqual(metrics.reason(""), "none")
        self.assertEqual(len(metrics.reason("x" * 500)), 80)

    def test_many_threads_count_every_increment(self):
        r = metrics.Registry()
        c = r.counter("x_total", "Calls.", ("t",))
        threads = [threading.Thread(target=lambda: [c.labels("a").inc() for _ in range(1000)]) for _ in range(8)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        self.assertEqual(c.get("a"), 8000)

    def test_the_process_metrics(self):
        r = metrics.Registry()
        metrics.process_metrics(r, started=123.0)
        text = r.render().decode()
        assert_exposition(self, text)
        self.assertIn("process_start_time_seconds 123\n", text)
        self.assertRegex(text, r"process_resident_memory_bytes [1-9]\d*\n")


class EndpointTest(unittest.TestCase):
    def setUp(self):
        self.h, self.o, _, _, _ = hands()
        hosts = mcp_http.Hosts(public=["orquesta.example"], internal=["score"])
        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), server.make_handler(self.o, None, CONFIG, self.h, hosts))
        self.httpd.daemon_threads = True
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()
        self.port = self.httpd.server_address[1]

    def tearDown(self):
        self.httpd.shutdown()
        self.httpd.server_close()

    def get(self, path, host=None):
        request = urllib.request.Request(f"http://127.0.0.1:{self.port}{path}")
        if host:
            request.add_header("Host", host)
        try:
            with urllib.request.urlopen(request, timeout=5) as response:
                return response.status, response.headers.get("Content-Type"), response.read().decode()
        except urllib.error.HTTPError as exc:
            return exc.code, exc.headers.get("Content-Type"), exc.read().decode()

    def test_loopback_and_the_internal_name_read_the_metrics(self):
        status, ctype, body = self.get("/metrics")
        self.assertEqual((status, ctype), (200, metrics.CONTENT_TYPE))
        assert_exposition(self, body)
        self.assertIn("# TYPE orchestra_chair_calls_total counter", body)
        self.assertEqual(self.get("/metrics", "score:8790")[0], 200)

    def test_the_public_name_does_not(self):
        status, _, body = self.get("/metrics", "orquesta.example")
        self.assertEqual(status, 403)
        self.assertIn("not public", body)

    def test_each_response_is_counted_by_route(self):
        before = telemetry.HTTP.get("/api/state", "200")
        self.get("/api/state")
        self.assertEqual(telemetry.HTTP.get("/api/state", "200"), before + 1)
        before = telemetry.HTTP.get("/static", "404")
        self.assertEqual(self.get("/static/none.js")[0], 404)
        self.assertEqual(telemetry.HTTP.get("/static", "404"), before + 1)  # the route, not the file: a bounded label


if __name__ == "__main__":
    unittest.main()
