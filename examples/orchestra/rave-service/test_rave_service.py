"""The rave without Node: a fake sound (its clock is the test's), a fake evaluator (events made here)."""

import json
import os
import shutil
import threading
import time
import unittest

import bus
import server
from evaluator import Evaluator
from rave import Rave, Refused

HERE = os.path.dirname(os.path.abspath(__file__))


def load(name):
    with open(os.path.join(HERE, name)) as f:
        return json.load(f)


class FakeSound:
    rate, block, channels, samples = 44100, 2048, 2, "loaded"
    sounds = ("bd", "sd", "hh", "cp", "808oh", "808cy", "rs", "clubkick", "hardkick", "hoover", "sawtooth", "square", "sine", "supersaw", "white")

    def __init__(self):
        self.hits, self.muted = [], []

    def time(self, frames):
        return frames / self.rate

    def hit(self, value, at, dur, cps, cycle):
        self.hits.append({"value": value, "at": at, "dur": dur, "cps": cps, "cycle": cycle})

    def mute(self, on=True):
        self.muted.append(on)

    def master(self, gain):
        pass


class FakeEvaluator:
    """Four events a cycle for anything with bd, one a cycle otherwise; the code's words are its sounds."""

    def pattern(self, code, from_cycle, cycles, max_haps=None):
        if "broken" in code:
            return {"id": "p", "ok": False, "error": "does not parse: broken"}
        sounds = [w for w in ("bd", "hh", "sawtooth", "nosuch") if w in code]
        per = 4 if "bd" in code or "hh" in code else 1
        haps = [{"b": c + k / per, "e": c + (k + 1) / per, "v": {"s": sounds[0] if sounds else "sine"}} for c in range(from_cycle, from_cycle + cycles) for k in range(per)]
        if "empty" in code:
            haps = []
        return {"id": "p", "ok": True, "haps": haps, "sounds": sounds, "perCycle": per}

    def evaluate(self, requests):
        return [dict(self.pattern(r["code"], r["from"], r["to"] - r["from"]), id=r["id"]) for r in requests]


def make(clock=None, **over):
    config = load("config.json")
    config["audio"]["lookaheadSeconds"] = 0.25
    for k, v in over.items():
        config[k] = v
    sound = FakeSound()
    rave = Rave(config, load("patterns.json"), sound, FakeEvaluator(), clock=clock or (lambda: 0.0))
    return rave, sound


def seconds(rave, s):
    """Advance the fake sound by s seconds, a block at a time."""
    frames = int(s * rave.rate)
    while frames > 0:
        n = min(2048, frames)
        rave.advance(n)
        frames -= n


class Clock(unittest.TestCase):
    def test_idle_until_a_cue_then_count_in_then_bars(self):
        rave, sound = make()
        seconds(rave, 1)
        self.assertEqual(rave.snapshot()["status"], "idle")
        self.assertEqual(rave.cue("info.cue.all", {}), "count-in")
        s = rave.snapshot()
        self.assertEqual((s["status"], s["bar"], s["countIn"], s["take"]), ("playing", 1, 2, 1))
        self.assertEqual(sound.muted[-1], False)
        seconds(rave, 2 * 4 * 60 / 128 + 0.3)  # the two bars of count-in, and the lookahead
        s = rave.snapshot()
        self.assertEqual((s["bar"], s["countIn"]), (1, 0))
        seconds(rave, 4 * 60 / 128)
        self.assertEqual(rave.snapshot()["bar"], 2)
        self.assertEqual(rave.snapshot()["beat"] < 1, True)

    def test_a_play_schedules_from_the_next_bar_and_the_hits_fall_on_the_sound_clock(self):
        rave, sound = make()
        out = rave.play("kick-1", pattern="kick-1")
        self.assertEqual((out["fromBar"], out["pattern"]["id"], out["eventsPerBar"]), (1, "kick-1", 4))
        self.assertEqual(rave.snapshot()["status"], "playing")
        seconds(rave, 2 * 4 * 60 / 128 + 60 / 128 * 4 + 0.5)  # count-in and one bar and a bit
        self.assertGreaterEqual(len(sound.hits), 4)
        first = sound.hits[0]
        self.assertEqual(first["value"]["s"], "bd")
        self.assertAlmostEqual(first["at"], 2 * 4 * 60 / 128 + 0.25, places=3)  # bar 1 is at the end of the count-in, plus the lookahead
        self.assertAlmostEqual(sound.hits[1]["at"] - first["at"], 60 / 128, places=3)  # a quarter note apart
        self.assertEqual(first["cycle"], 0)
        self.assertAlmostEqual(first["cps"], 128 / 60 / 4, places=4)
        self.assertEqual(first["value"]["gain"], 0.75)  # the fake's gain 1 at mf (the library's own gain multiplies it)
        self.assertIn("pan", first["value"])
        view = rave.snapshot()["sections"][0]["chairs"][0]
        self.assertEqual((view["state"], view["pattern"]["playing"]["id"], view["enteredBar"]), ("playing", "kick-1", 1))

    def test_a_second_play_swaps_at_the_bar_boundary_only(self):
        rave, sound = make()
        rave.play("hats-1", pattern='s("hh*4")')
        seconds(rave, 2 * 4 * 60 / 128 + 1.0)  # into bar 1
        bar_seconds = 4 * 60 / 128
        out = rave.play("hats-1", pattern='s("bd*4")')
        self.assertEqual(out["fromBar"], 2)
        self.assertTrue(out["pattern"]["custom"])
        seconds(rave, 2 * bar_seconds)
        hh = [h for h in sound.hits if h["value"]["s"] == "hh"]
        bd = [h for h in sound.hits if h["value"]["s"] == "bd"]
        self.assertEqual(len(hh), 4, "the old pattern plays bar 1 only")
        boundary = 2 * bar_seconds + 0.25 + bar_seconds
        self.assertTrue(all(h["at"] < boundary - 1e-6 for h in hh))
        self.assertAlmostEqual(bd[0]["at"], boundary, places=3)
        self.assertEqual(rave.snapshot()["sections"][2]["chairs"][0]["pattern"]["playing"]["id"], "hats-own-2")  # the second of the bot's own

    def test_stop_all_mutes_at_once_drops_patterns_and_refuses_play_until_the_next_cue(self):
        rave, sound = make(clock=lambda: 100.0)
        rave.play("kick-1", candidate=None, pattern="kick-1")
        rave.play("bass-1", pattern="bass-1")
        seconds(rave, 5)
        cut = rave.stop_all("stop.all from operator", event_at=99.7)
        self.assertEqual(sound.muted[-1], True)
        self.assertEqual(cut["afterEventSeconds"], 0.3)
        s = rave.snapshot()
        self.assertEqual(s["status"], "stopped")
        self.assertEqual([c["state"] for sec in s["sections"] for c in sec["chairs"]], ["stopped", "stopped", "waiting", "waiting"])
        n = len(sound.hits)
        seconds(rave, 2)
        self.assertEqual(len(sound.hits), n, "nothing more is scheduled")
        with self.assertRaisesRegex(Refused, "stop.all"):
            rave.play("kick-1", pattern="kick-1")
        self.assertEqual(rave.cue("info.cue.all", {}), "count-in")
        self.assertEqual(sound.muted[-1], False)
        self.assertEqual(rave.snapshot()["take"], 2)

    def test_hold_fades_a_section_out_and_the_cue_brings_it_back(self):
        rave, sound = make()
        rave.play("hats-1", pattern="hats-2")
        rave.play("kick-1", pattern="kick-1")
        seconds(rave, 5)
        held = rave.hold(rave.chairs_of("hats"), "stop.hats from operator")
        self.assertEqual(held["held"], ["hats-1"])
        seconds(rave, 2)
        hats = rave.snapshot()["sections"][2]["chairs"][0]
        self.assertEqual((hats["gain"], hats["held"], hats["state"]), (0.0, "stop.hats from operator", "playing"))
        n = len([h for h in sound.hits if h["value"]["s"] == "hh"])
        seconds(rave, 2)
        self.assertEqual(len([h for h in sound.hits if h["value"]["s"] == "hh"]), n, "a held chair schedules nothing")
        self.assertEqual(rave.cue("info.cue.drums", {"dynamics": "f"}), "f for 2 chairs")
        seconds(rave, 1)
        hats = rave.snapshot()["sections"][2]["chairs"][0]
        self.assertEqual((hats["gain"], hats["held"], hats["dynamics"]), (1.0, None, "f"))
        self.assertGreater(len([h for h in sound.hits if h["value"]["s"] == "hh"]), n)

    def test_a_pattern_that_goes_on_is_extended_past_its_horizon(self):
        rave, sound = make(patterns={"file": "patterns.json", "bars": 8, "candidates": 3, "leadBars": 2, "maxPerBar": 128, "horizonBars": 4})
        rave.play("kick-1", pattern="kick-1")
        plan = rave.chairs["kick-1"]["next"]
        self.assertEqual((plan["horizon"], len(plan["haps"])), (5, 16))
        bar_seconds = 4 * 60 / 128
        for _ in range(int((2 + 10) * bar_seconds / 0.5)):  # the count-in and ten bars, in slices, so the extension thread runs between them
            seconds(rave, 0.5)
            time.sleep(0.01)
        bd = [h for h in sound.hits if h["value"]["s"] == "bd"]
        self.assertGreater(len(bd), 4 * 8, "hits go on past the first horizon of four bars")
        self.assertGreaterEqual(plan["horizon"], 13)
        self.assertEqual([h["b"] for h in plan["haps"]], sorted(h["b"] for h in plan["haps"]))
        gaps = [round(b["at"] - a["at"], 3) for a, b in zip(bd, bd[1:])]
        self.assertEqual(set(gaps), {round(60 / 128, 3)}, "a quarter note apart across every horizon")

    def test_tempo_from_the_bus_is_clamped_and_changes_the_spacing(self):
        rave, sound = make()
        self.assertEqual(rave.set_tempo(300), 150.0)
        self.assertEqual(rave.set_tempo(135), 135.0)
        rave.play("kick-1", pattern="kick-1")
        seconds(rave, 2 * 4 * 60 / 135 + 2)
        self.assertAlmostEqual(sound.hits[1]["at"] - sound.hits[0]["at"], 60 / 135, places=3)


class Tools(unittest.TestCase):
    def test_phrases_offers_candidates_for_the_role_and_the_question_for_jev(self):
        rave, _ = make()
        out = rave.phrases("bass-1")
        self.assertEqual(out["role"], "bass")
        self.assertEqual([c["n"] for c in out["candidates"]], [1, 2, 3])
        self.assertTrue(all(c["id"].startswith("bass-") for c in out["candidates"]))
        self.assertEqual(set(out["ask"]), {"state", "question", "options"})
        self.assertEqual(out["ask"]["options"][0], f"pattern 1: {out['candidates'][0]['name']}")
        self.assertIn("one cycle of a pattern is one bar", out["ask"]["state"])
        again = rave.phrases("bass-1")
        self.assertNotEqual([c["id"] for c in again["candidates"]], [c["id"] for c in out["candidates"]], "the next ask rotates")

    def test_play_by_candidate_number_reports_jev_and_whether_it_matches(self):
        rave, _ = make()
        out = rave.phrases("lead-1")
        pick = out["candidates"][1]
        played = rave.play("lead-1", candidate=2, judge={"choice": f"pattern 2: {pick['name']}", "probability": 0.8, "confidence": 0.9})
        self.assertEqual(played["pattern"]["id"], pick["id"])
        chair = rave.snapshot()["sections"][3]["chairs"][0]
        self.assertEqual((chair["judge"]["matchesPattern"], chair["judge"]["probability"]), (True, 0.8))
        with self.assertRaisesRegex(Refused, "not offered"):
            rave.play("lead-1", candidate=7)
        with self.assertRaisesRegex(Refused, "no candidates"):
            rave.play("kick-1", candidate=1)

    def test_refusals_the_bot_can_read(self):
        rave, _ = make()
        with self.assertRaisesRegex(Refused, "no such chair"):
            rave.phrases("dj-1")
        with self.assertRaisesRegex(Refused, "does not parse"):
            rave.play("kick-1", pattern="broken")
        with self.assertRaisesRegex(Refused, "no such sound: nosuch"):
            rave.play("kick-1", pattern='s("nosuch")')
        with self.assertRaisesRegex(Refused, "no events"):
            rave.play("kick-1", pattern="empty")
        with self.assertRaisesRegex(Refused, "kick pattern"):
            rave.play("bass-1", pattern="kick-1")
        with self.assertRaisesRegex(Refused, "dynamics is one of"):
            rave.play("kick-1", pattern="kick-1", dynamics="fff")
        self.assertEqual(rave.snapshot()["status"], "idle", "a refused play starts nothing")

    def test_listen_waits_for_the_fork_and_answers_with_ask(self):
        now = [0.0]
        rave, _ = make(clock=lambda: now[0])
        rave.play("kick-1", pattern="kick-1")
        bar_seconds = 4 * 60 / 128

        done = threading.Event()

        def run():  # the sound's clock, until listen has answered; paced a little, so the listener gets the lock
            while not done.is_set() and now[0] < 60:
                seconds(rave, 0.5)
                now[0] += 0.5
                time.sleep(0.005)

        t = threading.Thread(target=run, daemon=True)
        t.start()
        out = rave.listen("kick-1", bars=32, max_seconds=60)
        done.set()
        t.join()
        self.assertIn("ask", out)
        self.assertGreaterEqual(out["bar"], 1 + 8 - 2 - 1)
        self.assertEqual(out["pattern"]["playing"]["id"], "kick-1")
        self.assertEqual(out["pattern"]["choiceBar"], 9)

    def test_listen_returns_done_when_stopped(self):
        rave, _ = make()
        rave.play("kick-1", pattern="kick-1")
        rave.stop_all("stop.all from operator")
        out = rave.listen("kick-1", max_seconds=1)
        self.assertEqual((out["done"], out["status"]), (True, "stopped"))
        self.assertNotIn("ask", out)
        out = rave.stop("kick-1")
        self.assertEqual((out["state"], out["done"]), ("stopped", True))


class StateForTheHands(unittest.TestCase):
    """What /hands (room.js, rave.js, hands.js) reads: the orchestra's names, here."""

    def test_the_fields_the_pages_read_are_there(self):
        rave, _ = make()
        rave.play("kick-1", pattern="kick-1")
        s = rave.snapshot()
        for key in ("version", "status", "bar", "beat", "bpm", "countIn", "lastBus", "cut", "sections", "phraseBars", "take"):
            self.assertIn(key, s)
        self.assertIn(s["status"], ("idle", "playing", "stopped", "ended"))
        for section in s["sections"]:
            for key in ("id", "name", "family", "playing", "chairs"):
                self.assertIn(key, section)
            for chair in section["chairs"]:
                for key in ("id", "state", "dynamics", "gain", "enteredBar", "held"):
                    self.assertIn(key, chair)
                self.assertIn(chair["state"], ("waiting", "scheduled", "playing", "stopped"))
        score = rave.score()
        self.assertEqual((score["beatsPerBar"], score["phraseBars"], len(score["sections"])), (4, 8, 4))
        self.assertGreater(score["bars"], 1000)

    def test_the_version_moves_once_a_bar_so_the_page_resyncs(self):
        rave, _ = make()
        rave.play("kick-1", pattern="kick-1")
        seconds(rave, 2 * 4 * 60 / 128 + 0.3)
        v = rave.snapshot()["version"]
        seconds(rave, 4 * 60 / 128)
        self.assertGreater(rave.snapshot()["version"], v)


class TheBus(unittest.TestCase):
    def event(self, name, data=None, to=None):
        return {"type": "group.event.published", "createdAt": "2026-10-31T00:00:00Z",
                "payload": {"name": name, "message": name, "data": data or {}, "to": to or [], "from": "operator"}}

    def test_apply_cue_tempo_hold_and_stop_all_as_bus_py_does(self):
        rave, sound = make()
        self.assertEqual(bus.apply(rave, self.event("info.cue.all", {"dynamics": "f"})), "count-in")
        self.assertEqual(rave.snapshot()["status"], "playing")
        self.assertEqual(bus.apply(rave, self.event("info.tempo", {"bpm": 140})), "tempo")
        self.assertEqual(rave.snapshot()["bpm"], 140.0)
        rave.play("kick-1", pattern="kick-1")
        seconds(rave, 5)
        self.assertEqual(bus.apply(rave, self.event("stop.drums")), "held 2 chairs")
        self.assertEqual(bus.apply(rave, self.event("stop.all")), "cut")
        self.assertEqual(rave.snapshot()["status"], "stopped")
        self.assertEqual(rave.snapshot()["lastBus"]["name"], "stop.all")

    def test_a_local_event_is_applied_the_same_way_and_marked_local(self):
        rave, _ = make()
        out = server.local_event(rave, {"name": "info.cue.all", "message": "in"})
        self.assertEqual(out["applied"], "count-in")
        self.assertEqual(out["event"]["payload"]["from"], "local")
        with self.assertRaises(ValueError):
            server.local_event(rave, {"message": "no name"})


@unittest.skipUnless(shutil.which("node") and os.path.isdir(os.path.join(HERE, "strudel", "node_modules")), "needs node and npm install in strudel/")
class WithStrudel(unittest.TestCase):
    def test_the_evaluator_answers_and_refuses(self):
        ev = Evaluator()
        out = ev.pattern('s("bd*4, [~ hh]*4")', 7, 2)
        self.assertTrue(out["ok"])
        self.assertEqual(out["perCycle"], 8)
        self.assertEqual(sorted(out["sounds"]), ["bd", "hh"])
        self.assertEqual(out["haps"][0]["b"], 7)
        self.assertFalse(ev.pattern('s("bd*1e9")', 0, 1)["ok"])
        self.assertFalse(ev.pattern("require('fs')", 0, 1)["ok"])

    def test_every_candidate_in_the_library_evaluates(self):
        library = load("patterns.json")
        entries = [e for k, v in library.items() if isinstance(v, list) for e in v]
        results = Evaluator().evaluate([{"id": e["id"], "code": e["code"], "from": 0, "to": 4} for e in entries])
        self.assertEqual([r["id"] for r in results if not r["ok"]], [])
        self.assertTrue(all(r["perCycle"] <= 128 for r in results))


if __name__ == "__main__":
    unittest.main()
