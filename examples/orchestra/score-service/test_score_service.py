"""The score service with no synthesizer, no sound card and no control plane: the score, the clock, the chairs, the bus."""

import json
import os
import threading
import unittest

import bus
from orchestra import Orchestra, Refused
from score import Score

HERE = os.path.dirname(os.path.abspath(__file__))


class FakeSynth:
    def __init__(self):
        self.calls = []

    def program(self, channel, program):
        self.calls.append(("program", channel, program))

    def note_on(self, channel, key, velocity):
        self.calls.append(("on", channel, key))

    def note_off(self, channel, key):
        self.calls.append(("off", channel, key))

    def volume(self, channel, value):
        self.calls.append(("volume", channel, value))

    def all_off(self, channel):
        self.calls.append(("all_off", channel))

    def silence(self, channel):
        self.calls.append(("silence", channel))

    def samples(self, frames):
        return bytes(frames * 4)

    def last_volume(self, channel):
        return [c[2] for c in self.calls if c[0] == "volume" and c[1] == channel][-1]


def load():
    with open(os.path.join(HERE, "config.json")) as f:
        config = json.load(f)
    return config, Score(os.path.join(HERE, config["score"]["file"]), config["sections"])


CONFIG, SCORE = load()


class Clock:
    def __init__(self):
        self.now = 1000.0

    def __call__(self):
        return self.now


def orchestra():
    synth, clock = FakeSynth(), Clock()
    return Orchestra(SCORE, CONFIG, synth, clock), synth, clock


def play_bars(o, bars):
    frames = int(bars * 60.0 / o.bpm * SCORE.beats_per_bar * o.rate)
    for _ in range(frames // 441 + 1):
        o.render(441)


class ScoreTest(unittest.TestCase):
    def test_the_movement_and_its_parts(self):
        self.assertEqual((SCORE.beats_per_bar, SCORE.bpm, SCORE.ticks_per_bar), (4, 152.0, 1536))
        self.assertEqual(SCORE.bars, 414)
        self.assertEqual(set(SCORE.parts), {"vn1", "vn2", "va", "vc", "cb", "ob", "hn"})
        cello, bass = SCORE.parts["vc"], SCORE.parts["cb"]
        self.assertEqual(len(cello), len(bass))
        self.assertEqual(bass[0][2], cello[0][2] - 12)

    def test_the_horns_start_at_bar_5(self):
        first = next(n for n in SCORE.parts["hn"] if n[1])
        self.assertEqual(SCORE.bar_of(first[0]), 5)

    def test_a_note_off_comes_before_a_note_on_at_the_same_tick(self):
        for notes in SCORE.parts.values():
            for a, b in zip(notes, notes[1:]):
                self.assertLessEqual((a[0], a[1]), (b[0], b[1]))

    def test_36_chairs(self):
        o, _, _ = orchestra()
        self.assertEqual(len(o.chairs), 36)
        self.assertIn("vn1-10", o.chairs)
        self.assertIn("hn-2", o.chairs)


class OrchestraTest(unittest.TestCase):
    def test_the_first_entry_starts_the_music_at_bar_1(self):
        o, synth, _ = orchestra()
        out = o.enter("vn1-1", dynamics="f")
        self.assertEqual((out["enteredAt"], out["dynamics"], o.status, o.take), (1, "f", "playing", 1))
        o.render(441)
        self.assertEqual(o.chairs["vn1-1"]["state"], "playing")
        self.assertTrue(any(c[0] == "on" and c[1] == o.channel["vn1"] for c in synth.calls))

    def test_a_late_entry_comes_in_at_the_next_bar(self):
        o, synth, _ = orchestra()
        o.enter("vn1-1")
        play_bars(o, 2.5)
        out = o.enter("ob-1", bar=1)
        self.assertEqual(out["enteredAt"], 4)
        self.assertIn("bar 1 had passed", out["note"])
        self.assertEqual(o.chairs["ob-1"]["state"], "scheduled")
        oboe = o.channel["ob"]
        self.assertFalse(any(c[0] == "on" and c[1] == oboe for c in synth.calls))
        play_bars(o, 1)
        self.assertEqual(o.chairs["ob-1"]["state"], "playing")

    def test_a_section_with_nobody_playing_is_silent(self):
        o, synth, _ = orchestra()
        o.enter("vn1-1")
        play_bars(o, 4)
        for section in ("vn2", "va", "vc", "cb", "ob", "hn"):
            self.assertFalse(any(c[0] == "on" and c[1] == o.channel[section] for c in synth.calls), section)

    def test_loudness_follows_the_chairs_playing(self):
        o, synth, _ = orchestra()
        for n in range(1, 9):
            o.enter(f"vn1-{n}", dynamics="f")
        play_bars(o, 1.2)
        self.assertEqual(o.snapshot()["sections"][0]["playing"], 8)
        eight = synth.last_volume(o.channel["vn1"])
        o.stop("vn1-8")
        seven = synth.last_volume(o.channel["vn1"])
        self.assertLess(seven, eight)
        o.dynamics("vn1-1", "p")
        self.assertLess(synth.last_volume(o.channel["vn1"]), seven)

    def test_the_last_chair_stopping_silences_the_section(self):
        o, synth, _ = orchestra()
        o.enter("va-1")
        o.render(441)
        o.stop("va-1")
        self.assertEqual(synth.last_volume(o.channel["va"]), 0)
        self.assertIn(("all_off", o.channel["va"]), synth.calls)

    def test_stop_all_silences_everything_at_once(self):
        o, synth, clock = orchestra()
        o.enter("vn1-1")
        o.enter("vc-1", bar=1)
        play_bars(o, 3)
        clock.now = 1000.4
        cut = o.stop_all("stop.all from operator", event_at=1000.1)
        self.assertEqual(o.status, "stopped")
        self.assertAlmostEqual(cut["afterEventSeconds"], 0.3)
        self.assertEqual(cut["bar"], 4)
        for section in o.sections:
            self.assertIn(("silence", o.channel[section]), synth.calls)
        self.assertEqual({c["state"] for c in o.chairs.values() if c["state"] != "waiting"}, {"stopped"})
        self.assertIsNone(o.stop_all("again"))

    def test_an_entry_after_a_stop_is_a_new_take(self):
        o, _, _ = orchestra()
        o.enter("vn1-1")
        play_bars(o, 2)
        o.stop_all("stop")
        out = o.enter("vn2-1")
        self.assertEqual((out["enteredAt"], o.take, o.chairs["vn1-1"]["state"]), (1, 2, "waiting"))

    def test_refusals_the_bot_reads(self):
        o, _, _ = orchestra()
        with self.assertRaises(Refused):
            o.enter("tuba-1")
        with self.assertRaises(Refused):
            o.enter("vn1-1", dynamics="fff")
        o.enter("vn1-1")
        with self.assertRaises(Refused):
            o.enter("vn1-2", bar=9999)

    def test_tempo_is_clamped_and_moves_the_clock(self):
        o, _, _ = orchestra()
        self.assertEqual(o.set_tempo(500), 200.0)
        self.assertEqual(o.set_tempo(108), 108.0)
        o.enter("vn1-1")
        play_bars(o, 2)
        self.assertEqual(o.snapshot()["bar"], 3)

    def test_listen_returns_when_the_chair_is_stopped(self):
        o, _, _ = orchestra()
        o.enter("vn1-1")
        o.render(441)
        result = {}
        t = threading.Thread(target=lambda: result.update(o.listen("vn1-1", bars=8, max_seconds=5)))
        t.start()
        o.stop_all("stop")
        t.join(3)
        self.assertFalse(t.is_alive())
        self.assertEqual((result["state"], result["status"]), ("stopped", "stopped"))
        self.assertIn("Violins I: 0 of 10 playing", result["section"])

    def test_snapshot_lists_sections_and_facts(self):
        o, _, _ = orchestra()
        o.enter("ob-1")
        snap = o.snapshot()
        self.assertEqual([s["id"] for s in snap["sections"]], ["vn1", "vn2", "va", "vc", "cb", "ob", "hn"])
        self.assertEqual(snap["facts"][-1]["what"], "enter")
        self.assertGreater(o.wait_change(-1, 0.1)["version"], 0)


class StagePageTest(unittest.TestCase):
    def test_density_is_one_strip_per_section(self):
        o, _, _ = orchestra()
        d = o.density()
        self.assertEqual((d["bars"], d["beatsPerBar"], len(d["sections"])), (414, 4, 7))
        horns = next(s for s in d["sections"] if s["id"] == "hn")["density"]
        self.assertEqual(horns[:4], [0, 0, 0, 0])
        self.assertTrue(all(0 <= x <= 1 for s in d["sections"] for x in s["density"]))

    def test_the_last_bus_event_is_kept_as_published(self):
        o, _, _ = orchestra()
        o.heard("info.cue.strings", "Strings, come in.", {"dynamics": "f"}, 1000.0)
        o.enter("vn1-1")
        last = o.snapshot()["lastBus"]
        self.assertEqual((last["name"], last["message"], last["data"]), ("info.cue.strings", "Strings, come in.", {"dynamics": "f"}))

    def test_the_page_and_its_files_are_there(self):
        for name in ("stage.html", "stage.js", "stage.css", "fonts/Lexend-Regular.ttf", "fonts/OFL-Lexend.txt"):
            self.assertTrue(os.path.isfile(os.path.join(HERE, "static", name)), name)
        with open(os.path.join(HERE, "static", "stage.html")) as f:
            self.assertIn("SIMULATION", f.read())


class BusTest(unittest.TestCase):
    def published(self, name, data=None):
        return {"type": "group.event.published", "createdAt": "2026-10-29T15:00:00.25Z",
                "payload": {"name": name, "from": "operator", "data": data}}

    def test_stop_all_cuts(self):
        o, _, clock = orchestra()
        o.enter("vn1-1")
        clock.now = bus.parse_time("2026-10-29T15:00:00.55Z")
        self.assertEqual(bus.apply(o, self.published("stop.all")), "cut")
        self.assertAlmostEqual(o.cut["afterEventSeconds"], 0.3, places=3)
        self.assertEqual(bus.apply(o, self.published("stop.all")), "already stopped")

    def test_tempo_from_data(self):
        o, _, _ = orchestra()
        self.assertEqual(bus.apply(o, self.published("info.tempo", {"bpm": 108})), "tempo")
        self.assertEqual(o.bpm, 108.0)
        self.assertIsNone(bus.apply(o, self.published("info.tempo", {"bpm": "fast"})))

    def test_other_events_change_nothing(self):
        o, _, _ = orchestra()
        self.assertIsNone(bus.apply(o, self.published("info.cue.strings")))
        self.assertIsNone(bus.apply(o, {"type": "bot.task.started", "createdAt": "2026-10-29T15:00:00Z"}))


if __name__ == "__main__":
    unittest.main()
