"""What the score service measures, with no Prometheus, no Jev, no synthesizer and no control plane.

The counters are the service's own (telemetry's module registry), so each
test reads how much a counter moved, not its value.
"""

import datetime
import io
import json
import os
import socket
import tempfile
import threading
import time
import unittest
from http.server import ThreadingHTTPServer

import bus
import metrics
import server
import synth
import telemetry
import ws
from test_hands import FakeExpressionJev, OPEN, expression, hands, posture
from test_score_service import CONFIG, orchestra, play_until
from test_ws import MASK, read_frame

MOVED = {"seconds": 2.4, "speed": 0.12, "peak": 0.3, "rise": 0.08, "sweep": 0.06, "turns": 0.4, "open": 0.85, "height": -0.3}


def call(tools, name, arguments, rid=1):
    return tools.handle({"jsonrpc": "2.0", "id": rid, "method": "tools/call", "params": {"name": name, "arguments": arguments}})


def buckets(histogram, *labels):
    child = histogram.labels(*labels)
    return child.count, child.sum


def stamp(t):
    return datetime.datetime.fromtimestamp(t, tz=datetime.timezone.utc).isoformat().replace("+00:00", "Z")


class ChairCallsTest(unittest.TestCase):
    def test_each_call_is_counted_and_timed_and_a_play_says_if_it_was_late(self):
        o, _, _ = orchestra()
        tools = server.chair_tools(o, "vn1-1", 20)
        ok, refused = telemetry.CHAIR_CALLS.get("vn1-1", "vn1", "play", "ok"), telemetry.CHAIR_CALLS.get("vn1-1", "vn1", "play", "refused")
        timed = buckets(telemetry.CHAIR_SECONDS, "vn1", "play")[0]
        on_time, judged = telemetry.PLAYS.get("vn1-1", "vn1", "no"), buckets(telemetry.JUDGE_P, "vn1")[0]
        reply = call(tools, "play", {"phrase": 1, "judge": {"choice": "phrase 1: bars 1–8", "probability": 0.7}})
        self.assertFalse(reply["result"]["isError"])
        self.assertTrue(call(tools, "play", {"phrase": 9999})["result"]["isError"])
        self.assertEqual(telemetry.CHAIR_CALLS.get("vn1-1", "vn1", "play", "ok"), ok + 1)
        self.assertEqual(telemetry.CHAIR_CALLS.get("vn1-1", "vn1", "play", "refused"), refused + 1)
        self.assertEqual(buckets(telemetry.CHAIR_SECONDS, "vn1", "play")[0], timed + 2)
        self.assertEqual(telemetry.PLAYS.get("vn1-1", "vn1", "no"), on_time + 1)
        self.assertEqual(buckets(telemetry.JUDGE_P, "vn1")[0], judged + 1)

    def test_a_phrase_joined_late_counts_its_missed_bars(self):
        o, _, _ = orchestra()
        o.cue("info.cue.all", {})
        play_until(o, 3)
        late, bars = telemetry.PLAYS.get("va-2", "va", "yes"), telemetry.LATE_BARS.get("va-2", "va")
        out = json.loads(call(server.chair_tools(o, "va-2", 20), "play", {"phrase": 1})["result"]["content"][0]["text"])
        self.assertGreater(out["lateBars"], 0)
        self.assertEqual(telemetry.PLAYS.get("va-2", "va", "yes"), late + 1)
        self.assertEqual(telemetry.LATE_BARS.get("va-2", "va"), bars + out["lateBars"])


class OrchestraTest(unittest.TestCase):
    def setUp(self):
        self.o, self.synth, self.clock = orchestra()
        self.registry = metrics.Registry()
        telemetry.watch_orchestra(self.o, registry=self.registry)

    def test_facts_and_cues_are_counted_as_they_happen(self):
        takes, cues = telemetry.TAKES.get(), telemetry.CUES.get("all", "none", "count-in")
        begun, cuts = telemetry.BEGUN.get("vn1"), telemetry.CUTS.get()
        cut_timed = buckets(telemetry.BUS_TO_SOUND, "cut")[0]
        self.o.cue("info.cue.all", {})
        self.o.play("vn1-1", 1)
        play_until(self.o, 2)
        self.o.stop_all("stop.all from operator", event_at=self.clock.now - 0.2)
        self.assertEqual(telemetry.TAKES.get(), takes + 1)
        self.assertEqual(telemetry.CUES.get("all", "none", "count-in"), cues + 1)
        self.assertEqual(telemetry.BEGUN.get("vn1"), begun + 1)
        self.assertEqual(telemetry.CUTS.get(), cuts + 1)
        self.assertEqual(buckets(telemetry.BUS_TO_SOUND, "cut")[0], cut_timed + 1)

    def test_a_level_cue_while_playing_and_a_hold(self):
        self.o.cue("info.cue.all", {})
        self.o.play("ob-1", 1)
        play_until(self.o, 1)
        levels, held = telemetry.LEVELS_SET.get("winds", "f"), telemetry.HELD.get("ob")
        self.assertEqual(self.o.cue("info.cue.winds", {"dynamics": "f"}), "f for 4 chairs")
        self.o.hold(["ob-1", "ob-2"], "fist")
        self.assertEqual(telemetry.LEVELS_SET.get("winds", "f"), levels + 1)
        self.assertEqual(telemetry.HELD.get("ob"), held + 2)
        self.assertGreaterEqual(telemetry.CUES.get("winds", "f", "level"), 1)

    def test_the_take_is_read_when_prometheus_asks(self):
        text = self.registry.render().decode()
        self.assertIn('orchestra_status{status="idle"} 1\n', text)
        self.assertIn('orchestra_chairs{section="vn1",state="waiting"} 10\n', text)
        self.o.cue("info.cue.all", {})
        self.o.play("vn1-1", 1)
        self.o.character(["vn1-1", "vn1-2"], "dolce")
        play_until(self.o, 3)  # up to bar 3, not into it
        time.sleep(1.05)  # one snapshot serves a scrape for a second
        text = self.registry.render().decode()
        for line in ('orchestra_status{status="playing"} 1', "orchestra_bar 2", "orchestra_bars 414", 'orchestra_chairs{section="vn1",state="playing"} 1',
                     'orchestra_section_character{section="vn1",character="dolce"} 2', 'orchestra_section_gain{section="vn1"} 1'):
            self.assertIn(line + "\n", text)


class HandsTest(unittest.TestCase):
    """The records on main: postures, native gestures, motion."""

    def test_a_posture_read_and_sent(self):
        h, o, _, _, _ = hands(left=("open", 0.95))
        telemetry.watch_hands(h, registry=metrics.Registry())
        read, chose = telemetry.POSTURES.get("read", "no"), telemetry.JEV_CHOICES.get("left", "open")
        decided, sent = telemetry.DECISIONS.get("posture", "open", "strings"), telemetry.HANDS_EVENTS.get("posture", "info.cue.strings", "published")
        h.observe(posture(left=OPEN))
        self.assertEqual(telemetry.POSTURES.get("read", "no"), read + 1)
        self.assertEqual(telemetry.JEV_CHOICES.get("left", "open"), chose + 1)
        self.assertEqual(telemetry.DECISIONS.get("posture", "open", "strings"), decided + 1)
        self.assertEqual(telemetry.HANDS_EVENTS.get("posture", "info.cue.strings", "published"), sent + 1)

    def test_a_reading_under_the_threshold_is_nothing_and_says_so(self):
        h, _, _, _, clock = hands(left=("fist", 0.6))
        telemetry.watch_hands(h, registry=metrics.Registry())
        under, nothing = telemetry.UNDER.get("left", "fist"), telemetry.GESTURES.get("left", "nothing")
        h.observe(posture(left=OPEN))
        self.assertEqual(telemetry.UNDER.get("left", "fist"), under + 1)
        self.assertEqual(telemetry.GESTURES.get("left", "nothing"), nothing + 1)
        too_soon = telemetry.NOT_SENT.get("posture", "too soon after the last reading (N s apart at most)")
        h.observe(posture(left=OPEN))  # the clock did not move
        self.assertEqual(telemetry.NOT_SENT.get("posture", "too soon after the last reading (N s apart at most)"), too_soon + 1)

    def test_native_gestures_acted_on_or_refused(self):
        h, o, _, _, _ = hands()
        telemetry.watch_hands(h, registry=metrics.Registry())
        acted, refused = telemetry.NATIVE.get("swipe-forward", "right", "acted"), telemetry.NATIVE.get("wave", "none", "refused")
        h.native({"gesture": "swipe-forward", "side": "right"})
        h.native({"gesture": "wave"})
        self.assertEqual(telemetry.NATIVE.get("swipe-forward", "right", "acted"), acted + 1)
        self.assertEqual(telemetry.NATIVE.get("wave", "none", "refused"), refused + 1)

    def test_the_expression_read_and_applied(self):
        h, o, _, _, _, _ = expression("dolce", 0.9)
        telemetry.watch_hands(h, registry=metrics.Registry())
        reads, changes = telemetry.EXPRESSION_READS.get("dolce"), telemetry.EXPRESSION_CHANGES.get("dolce", "all")
        published = telemetry.HANDS_EVENTS.get("motion", "info.expression", "published")
        h.motion({"hands": {"right": MOVED}})
        self.assertEqual(telemetry.EXPRESSION_READS.get("dolce"), reads + 1)
        self.assertEqual(telemetry.EXPRESSION_CHANGES.get("dolce", "all"), changes + 1)
        self.assertEqual(telemetry.HANDS_EVENTS.get("motion", "info.expression", "published"), published + 1)


class PendingLevels:
    """The hands of PR #79 as far as telemetry sees them: _keep passes every record to _log, and a level
    left pending goes to _log alone; view() has pending, confirmed and engaged as dicts."""

    def __init__(self):
        self.kept, self.logged = [], []

    def _keep(self, record):
        self.kept.append(record)
        self._log(record)
        return record

    def _log(self, record):
        self.logged.append(record)

    def view(self):
        return {"pending": {"left": {"group": "violins", "gain": 0.6, "from": 0.9}}, "confirmed": {"cellos": 0.7},
                "engaged": {"left": {"gesture": "open", "group": "violins", "height": -0.2, "gain": 0.9}, "right": None},
                "character": None, "dryRun": False}


class LevelsTest(unittest.TestCase):
    def test_levels_pending_kept_and_reverted_are_counted_once_each(self):
        h = PendingLevels()
        registry = metrics.Registry()
        telemetry.watch_hands(h, registry=registry)
        before = {k: telemetry.LEVELS.get(*k) for k in [("pending", "violins", "flat hand"), ("kept", "violins", "double pinch"),
                                                       ("reverted", "violins", "not kept within N s"), ("kept", "cellos", "pinch released")]}
        double = telemetry.NATIVE.get("pinch-double", "right", "acted")
        h._log({"at": 1, "gain": {"kind": "pending", "side": "left", "group": "violins", "from": 0.9}})
        h._keep({"at": 2, "gain": {"kind": "kept", "side": "left", "group": "violins", "gain": 0.6, "from": 0.9}})
        h._keep({"at": 3, "native": "pinch-double", "side": "right", "decision": [{"gesture": "pinch-double", "group": "violins"}],
                 "applied": "Violins kept at 60%", "sent": [], "notSent": []})
        h._keep({"at": 4, "gain": {"kind": "reverted", "side": "left", "group": "violins", "gain": 0.9, "from": 0.6, "why": "not kept within 5 s"}})
        h._keep({"at": 5, "native": "pinch-end", "side": "right", "gain": {"kind": "kept", "group": "cellos", "gain": 0.7, "from": 0.5},
                 "applied": "Cellos kept at 70%", "decision": [], "sent": [], "notSent": []})
        for key, value in before.items():
            self.assertEqual(telemetry.LEVELS.get(*key), value + 1, key)
        self.assertEqual(telemetry.NATIVE.get("pinch-double", "right", "acted"), double + 1)
        self.assertEqual(len(h.logged), 5)  # the hands' own log still gets every record
        text = registry.render().decode()
        for line in ('orchestra_hands_level_pending{side="left",group="violins"} 0.6', 'orchestra_hands_level_kept{group="cellos"} 0.7',
                     'orchestra_hands_engaged{side="left",gesture="open",group="violins"} 1', "orchestra_hands_dry_run 0"):
            self.assertIn(line + "\n", text)

    def test_on_main_the_keep_is_watched_and_engaged_is_a_pair(self):
        h, _, _, _, _ = hands(left=("open", 0.95))
        registry = metrics.Registry()
        telemetry.watch_hands(h, registry=registry)
        h.observe(posture(left=OPEN))  # main keeps engaged as (gesture, group); PR #79 as a dict: the same sample
        self.assertIn('orchestra_hands_engaged{side="left",gesture="open",group="strings"} 1\n', registry.render().decode())


class JevTest(unittest.TestCase):
    def test_each_call_is_counted_with_its_tokens_and_cost_and_logged(self):
        with tempfile.TemporaryDirectory() as d:
            log = telemetry.JsonLines(d + os.sep)
            judge = telemetry.judged(FakeExpressionJev("brillante", 0.9), log, usd_per_million=0.042)
            calls, tokens, cost = (telemetry.JEV_CALLS.get("expression", "ok"), telemetry.JEV_TOKENS.get("expression"),
                                   telemetry.JEV_COST.get("expression"))
            from hands import CHARACTERS
            judge("the hands moved", {"expression": ("Which expression?", list(CHARACTERS.values()))})
            self.assertEqual(telemetry.JEV_CALLS.get("expression", "ok"), calls + 1)
            self.assertEqual(telemetry.JEV_TOKENS.get("expression"), tokens + 500)
            self.assertAlmostEqual(telemetry.JEV_COST.get("expression"), cost + 500 * 0.042 / 1e6)

            def failing(state, questions):
                raise RuntimeError("TypeSafe HTTP 503")

            errors = telemetry.JEV_ERRORS.get("posture", "TypeSafe HTTP N")
            with self.assertRaises(RuntimeError):
                telemetry.judged(failing, log)("a hand", {"left": ("Which signal?", ["a", "b"])})
            self.assertEqual(telemetry.JEV_ERRORS.get("posture", "TypeSafe HTTP N"), errors + 1)
            files = os.listdir(d)
            self.assertEqual(len(files), 1)
            self.assertRegex(files[0], r"^jev-\d{8}T\d{6}Z-\d+\.jsonl$")
            with open(os.path.join(d, files[0])) as f:
                lines = [json.loads(line) for line in f]
        self.assertEqual([x["kind"] for x in lines], ["expression", "posture"])
        self.assertEqual(lines[0]["state"], "the hands moved")
        self.assertEqual(lines[0]["questions"]["expression"]["instructions"], "Which expression?")
        self.assertEqual(lines[0]["answers"]["expression"]["choice"], CHARACTERS["brillante"])
        self.assertEqual((lines[0]["inputTokens"], lines[0]["model"]), (500, "jev-1.13.0"))
        self.assertEqual(lines[1]["error"], "TypeSafe HTTP 503")

    def test_a_file_path_is_appended_to(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "session.jsonl")
            telemetry.JsonLines(path).write({"a": 1})
            telemetry.JsonLines(path).write({"a": 2})
            with open(path) as f:
                self.assertEqual([json.loads(x)["a"] for x in f], [1, 2])


class Response(io.BytesIO):
    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


class ScriptedBus(bus.Bus):
    """No control plane: the timeline is a list of batches, and a publish answers with the id given."""

    def __init__(self, batches, token_file):
        super().__init__("https://cp.example", "orchestra", token_file, "", meter=telemetry.BUS)
        self.batches, self.stop = [[]] + list(batches), threading.Event()

    def events(self, after=None, wait=None):
        if not self.batches:
            self.stop.set()
            return []
        return self.batches.pop(0)


def heard(event_id, mode, at):
    return {"eventId": f"rec-heard-{at}-{mode}", "type": "bot.event.heard", "createdAt": stamp(at), "botId": "vn1-1",
            "payload": {"eventId": event_id, "mode": mode, "name": "info.cue.vn1"}}


def published(event_id, name, message, at):
    return {"eventId": f"rec-pub-{event_id}", "type": "group.event.published", "createdAt": stamp(at),
            "payload": {"eventId": event_id, "name": name, "message": message, "data": {"dynamics": "f"}, "to": []}}


class BusTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.token = os.path.join(self.dir.name, "token")
        with open(self.token, "w") as f:
            f.write("t0ken")
        self.urlopen = bus.urllib.request.urlopen
        bus.ssl.create_default_context, self.context = (lambda cafile=None: None), bus.ssl.create_default_context

    def tearDown(self):
        bus.urllib.request.urlopen = self.urlopen
        bus.ssl.create_default_context = self.context
        self.dir.cleanup()

    def test_deliveries_before_the_id_is_known_are_measured_when_the_echo_names_it(self):
        o, _, _ = orchestra()
        start = time.time() - 1.0
        b = ScriptedBus([[heard("evt-x", "message", start + 0.1), heard("evt-x", "task", start + 0.2),
                          published("evt-x", "info.cue.vn1", "Violins, play loud.", start + 0.3)],
                         [heard("evt-x", "message", start + 0.4)]], self.token)
        mark = bus.own_mark("info.cue.vn1", "Violins, play loud.", None)
        b.own.append(mark)  # as publish does before its POST answers
        b.started[mark] = [start]
        echoes, echo_timed = telemetry.ECHOES.get("info.cue"), buckets(telemetry.ECHO_SECONDS, "info.cue")
        message, task = buckets(telemetry.DELIVERY_SECONDS, "info.cue", "message"), buckets(telemetry.DELIVERY_SECONDS, "info.cue", "task")
        b.follow(o, b.stop)
        self.assertEqual(telemetry.ECHOES.get("info.cue"), echoes + 1)
        count, total = buckets(telemetry.ECHO_SECONDS, "info.cue")
        self.assertEqual(count, echo_timed[0] + 1)
        self.assertGreaterEqual(total - echo_timed[1], 1.0)
        count, total = buckets(telemetry.DELIVERY_SECONDS, "info.cue", "message")
        self.assertEqual(count, message[0] + 2)  # one waited for the echo, one came after it
        self.assertAlmostEqual(total - message[1], 0.5, places=3)
        self.assertAlmostEqual(buckets(telemetry.DELIVERY_SECONDS, "info.cue", "task")[1] - task[1], 0.2, places=3)
        self.assertEqual(o.status, "idle")  # heard, not applied again
        self.assertEqual(o.last_bus["name"], "info.cue.vn1")

    def test_a_publish_is_timed_and_its_deliveries_measured_from_its_start(self):
        o, _, _ = orchestra()
        b = ScriptedBus([], self.token)
        bus.urllib.request.urlopen = lambda request, context=None, timeout=None: Response(b'{"eventId":"evt-y"}')
        ok, timed = telemetry.PUBLISHES.get("info.tempo", "ok"), buckets(telemetry.PUBLISH_SECONDS, "info.tempo")[0]
        self.assertEqual(b.publish("info.tempo", "Tempo 140.", {"bpm": 140}), "evt-y")
        self.assertEqual(telemetry.PUBLISHES.get("info.tempo", "ok"), ok + 1)
        self.assertEqual(buckets(telemetry.PUBLISH_SECONDS, "info.tempo")[0], timed + 1)
        started = b.sent["evt-y"][1]
        delivered = buckets(telemetry.DELIVERY_SECONDS, "info.tempo", "message")
        b.batches = [[], [heard("evt-y", "message", started + 0.05), published("evt-y", "info.tempo", "Tempo 140.", started + 0.06)]]
        b.batches[1][1]["payload"].update(data={"bpm": 140})
        b.follow(o, b.stop)
        count, total = buckets(telemetry.DELIVERY_SECONDS, "info.tempo", "message")
        self.assertEqual(count, delivered[0] + 1)
        self.assertAlmostEqual(total - delivered[1], 0.05, places=3)

    def test_a_failed_publish_is_an_error_and_forgets_its_mark(self):
        b = ScriptedBus([], self.token)

        def refuse(request, context=None, timeout=None):
            raise OSError("connection refused")

        bus.urllib.request.urlopen = refuse
        errors = telemetry.PUBLISHES.get("info.cue.all", "error")
        with self.assertRaises(OSError):
            b.publish("info.cue.all", "Everyone, play loud.", {"dynamics": "f"})
        self.assertEqual(telemetry.PUBLISHES.get("info.cue.all", "error"), errors + 1)
        self.assertEqual((b.own, b.started), ([], {}))

    def test_an_operators_event_applied_is_counted_with_what_it_did(self):
        o, _, _ = orchestra()
        b = ScriptedBus([[published("evt-panel", "info.cue.all", "Everyone, play loud.", time.time())]], self.token)
        applied, records = telemetry.APPLIED.get("info.cue.all", "count-in"), telemetry.RECORDS.get("group.event.published")
        b.follow(o, b.stop)
        self.assertEqual(o.status, "playing")
        self.assertEqual(telemetry.APPLIED.get("info.cue.all", "count-in"), applied + 1)
        self.assertEqual(telemetry.RECORDS.get("group.event.published"), records + 1)


class RenderTest(unittest.TestCase):
    def test_each_block_is_timed_and_a_slow_one_is_late(self):
        class Slow:
            def render(self, frames):
                time.sleep(0.03)  # three times a 10 ms block
                return bytes(frames * 4)

        stop = threading.Event()
        blocks, late = buckets(telemetry.RENDER_SECONDS)[0], telemetry.RENDER_LATE.get()
        threading.Timer(0.3, stop.set).start()
        synth.run(Slow(), synth.Stream(), 44100, 441, stop=stop, meter=telemetry.RENDER_METER)
        self.assertGreaterEqual(buckets(telemetry.RENDER_SECONDS)[0] - blocks, 3)
        self.assertGreater(telemetry.RENDER_LATE.get(), late)
        self.assertGreater(telemetry.RENDER_LAG.get(), 0)

    def test_a_slow_listener_loses_blocks_and_they_are_counted(self):
        dropped = []
        stream = synth.Stream(depth=1, on_drop=dropped.append)
        q = stream.listen()
        for _ in range(3):
            stream.publish(b"x")
        self.assertEqual((dropped, stream.count(), q.qsize()), ([1, 1], 1, 1))


class WebSocketTest(unittest.TestCase):
    def test_connections_and_messages_are_counted(self):
        h, o, _, _, _ = hands(left=("open", 0.95))
        httpd = ThreadingHTTPServer(("127.0.0.1", 0), server.make_handler(o, None, CONFIG, h))
        httpd.daemon_threads = True
        threading.Thread(target=httpd.serve_forever, daemon=True).start()
        port = httpd.server_address[1]
        opened, up, down = telemetry.WS_OPENED.get(), telemetry.WS_MESSAGES.get("up", "posture"), telemetry.WS_MESSAGES.get("down", "hands")
        open_before = telemetry.WS_OPEN.get()
        try:
            s = socket.create_connection(("127.0.0.1", port), timeout=5)
            s.sendall((f"GET /ws/hands HTTP/1.1\r\nHost: localhost:{port}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
                       "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n").encode())
            f = s.makefile("rb")
            while f.readline() not in (b"\r\n", b""):
                pass
            s.sendall(ws.frame(ws.TEXT, json.dumps({"type": "posture", "id": 1, "hands": {"left": OPEN}, "heldMs": 300}).encode(), MASK))
            while json.loads(read_frame(f)[1])["type"] != "hands":
                pass
            self.assertEqual(telemetry.WS_OPENED.get(), opened + 1)
            self.assertEqual(telemetry.WS_OPEN.get(), open_before + 1)
            self.assertEqual(telemetry.WS_MESSAGES.get("up", "posture"), up + 1)
            self.assertEqual(telemetry.WS_MESSAGES.get("down", "hands"), down + 1)
            self.assertGreaterEqual(telemetry.WS_MESSAGES.get("down", "state"), 1)
            s.sendall(ws.frame(ws.CLOSE, b"\x03\xe8", MASK))
            f.close()
            s.close()
            for _ in range(50):
                if telemetry.WS_OPEN.get() == open_before:
                    break
                time.sleep(0.05)
            self.assertEqual(telemetry.WS_OPEN.get(), open_before)
        finally:
            httpd.shutdown()
            httpd.server_close()


if __name__ == "__main__":
    unittest.main()
