"""The score service with no synthesizer, no sound card and no control plane: the score, its lines and phrases, the clock, the chairs, the bus."""

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
        self.calls, self.ons = [], 0

    def program(self, channel, program):
        self.calls.append(("program", channel, program))

    def note_on(self, channel, key, velocity):
        self.calls.append(("on", channel, key))
        self.ons += 1

    def note_off(self, channel, key):
        self.calls.append(("off", channel, key))

    def volume(self, channel, value):
        self.calls.append(("volume", channel, value))

    def pan(self, channel, value):
        self.calls.append(("pan", channel, value))

    def bend(self, channel, cents):
        self.calls.append(("bend", channel, cents))

    def all_off(self, channel):
        self.calls.append(("all_off", channel))

    def silence(self, channel):
        self.calls.append(("silence", channel))

    def samples(self, frames):
        return bytes(frames * 4)

    def last_volume(self, channel):
        return [c[2] for c in self.calls if c[0] == "volume" and c[1] == channel][-1]

    def ons_on(self, channel):
        return sum(1 for c in self.calls if c[0] == "on" and c[1] == channel)


def load():
    with open(os.path.join(HERE, "config.json")) as f:
        config = json.load(f)
    return config, Score(os.path.join(HERE, config["score"]["file"]), config["sections"], config["phrases"]["bars"])


CONFIG, SCORE = load()


class Clock:
    def __init__(self):
        self.now = 1000.0

    def __call__(self):
        return self.now


def orchestra():
    synth, clock = FakeSynth(), Clock()
    return Orchestra(SCORE, CONFIG, synth, clock), synth, clock


def play_until(o, bar):
    """Render up to the start of bar, not past it."""
    while o.tick + 441 / o.rate * o.bpm / 60 * SCORE.ticks_per_beat <= SCORE.tick_of(bar):
        o.render(441)


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
        for notes in list(SCORE.parts.values()) + list(SCORE.lines.values()):
            for a, b in zip(notes, notes[1:]):
                self.assertLessEqual((a[0], a[1]), (b[0], b[1]))

    def test_the_lines_strings_in_unison_oboes_and_horns_apart(self):
        self.assertEqual(set(SCORE.lines), {"vn1", "vn2", "va", "vc", "cb", "ob-1", "ob-2", "hn-1", "hn-2"})
        self.assertIs(SCORE.lines["vn1"], SCORE.parts["vn1"])
        upper = {t: k for t, on, k, _ in SCORE.lines["ob-1"] if on}
        lower = {t: k for t, on, k, _ in SCORE.lines["ob-2"] if on}
        self.assertEqual(set(upper), set(lower))
        self.assertTrue(all(upper[t] >= lower[t] for t in upper))
        self.assertTrue(any(upper[t] > lower[t] for t in upper))
        self.assertLess(len(SCORE.lines["hn-1"]), len(SCORE.parts["hn"]))
        self.assertEqual(len(SCORE.lines["hn-1"]) + len(SCORE.lines["hn-2"]), len(SCORE.parts["hn"]))
        self.assertEqual((SCORE.line_names["ob-1"], SCORE.line_names["hn-2"]), ("Oboe I", "Horns III–IV"))

    def test_phrases_are_eight_bars_and_say_what_they_hold(self):
        self.assertEqual((SCORE.phrase_count, SCORE.phrase_span(1), SCORE.phrase_span(2), SCORE.phrase_span(52)), (52, (1, 8), (9, 16), (409, 414)))
        self.assertEqual(SCORE.phrase_of(17), 3)
        horn = SCORE.phrase_table["hn-1"][0]
        self.assertEqual(horn["bars"], [1, 8])
        self.assertGreaterEqual(horn["restBars"], 4)
        violin = SCORE.phrase_table["vn1"][0]
        self.assertGreater(violin["notes"], 20)
        self.assertRegex(violin["range"], r"^[A-G][#b]?\d–[A-G][#b]?\d$")

    def test_36_chairs(self):
        o, _, _ = orchestra()
        self.assertEqual(len(o.chairs), 36)
        self.assertIn("vn1-10", o.chairs)
        self.assertIn("hn-2", o.chairs)


class VoiceTest(unittest.TestCase):
    def test_each_chair_has_its_own_channel_never_the_drums(self):
        o, synth, _ = orchestra()
        channels = [c["channel"] for c in o.chairs.values()]
        self.assertEqual(len(set(channels)), 36)
        self.assertFalse([ch for ch in channels if ch % 16 == 9])
        programs = {c[1]: c[2] for c in synth.calls if c[0] == "program"}
        self.assertEqual(programs[o.chairs["vc-2"]["channel"]], 42)
        self.assertEqual(programs[o.chairs["hn-1"]["channel"]], 60)

    def test_a_section_is_spread_on_the_stage_and_in_tune_by_a_few_cents(self):
        o, _, _ = orchestra()
        violins = [o.chairs[f"vn1-{n}"] for n in range(1, 11)]
        self.assertEqual((violins[0]["pan"], violins[-1]["pan"]), (-0.9, -0.5))
        self.assertEqual(len({v["cents"] for v in violins}), 10)
        self.assertTrue(all(abs(v["cents"]) <= 6 for v in violins))
        self.assertGreater(o.chairs["vc-1"]["pan"], 0)

    def test_nothing_sounds_that_no_chair_asked_for(self):
        o, synth, _ = orchestra()
        self.assertEqual(o.cue("info.cue.all", {}), "count-in")
        play_until(o, 12)
        self.assertEqual(synth.ons, 0)

    def test_a_phrase_sounds_at_its_own_bars_on_its_chairs_channel_only(self):
        o, synth, _ = orchestra()
        o.cue("info.cue.strings", {})
        out = o.play("vn1-3", 2, "f")
        self.assertEqual((out["bars"], out["lateBars"]), ([9, 16], 0))
        mine = o.chairs["vn1-3"]["channel"]
        play_until(o, 9)
        self.assertEqual(synth.ons, 0)
        play_until(o, 17)
        self.assertGreater(synth.ons_on(mine), 20)
        self.assertEqual(synth.ons, synth.ons_on(mine))
        before = synth.ons
        play_until(o, 20)
        self.assertEqual(synth.ons, before)

    def test_one_violin_is_quieter_than_one_oboe_and_p_than_f(self):
        o, synth, _ = orchestra()
        o.cue("info.cue.all", {})
        o.play("vn1-1", 1, "f")
        o.play("ob-1", 1, "f")
        o.play("va-1", 1, "p")
        play_until(o, 2)
        violin, oboe, viola = (synth.last_volume(o.chairs[c]["channel"]) for c in ("vn1-1", "ob-1", "va-1"))
        self.assertLess(violin, oboe)
        self.assertEqual(oboe, 127)
        self.assertLess(viola, round(127 * 6 ** -0.25))


class PhraseTest(unittest.TestCase):
    def test_the_first_play_starts_the_music_with_a_count_in(self):
        o, _, _ = orchestra()
        out = o.play("vn1-1", 1, "f")
        self.assertEqual((o.status, o.take, out["fromBar"], out["lateBars"]), ("playing", 1, 1, 0))
        self.assertAlmostEqual(out["inSeconds"], 8 * 4 * 60 / 152, places=1)
        snap = o.snapshot()
        self.assertEqual((snap["bar"], snap["countIn"]), (1, 8))
        play_until(o, 2)
        self.assertEqual(o.snapshot()["sections"][0]["chairs"][0]["state"], "playing")

    def test_a_cue_with_a_bar_starts_there(self):
        o, _, _ = orchestra()
        o.cue("info.cue.all", {"bar": 17})
        r = o.phrases("va-2")
        self.assertEqual([c["phrase"] for c in r["candidates"]], [3, 4, 5])
        with self.assertRaises(Refused):
            o.play("va-2", 1)

    def test_a_late_play_is_joined_at_the_next_bar(self):
        o, synth, _ = orchestra()
        o.cue("info.cue.all", {})
        play_until(o, 3)
        o.render(441 * 20)
        out = o.play("ob-1", 1, "mf")
        self.assertEqual((out["fromBar"], out["lateBars"]), (4, 3))
        self.assertIn("3 bars late", out["note"])
        play_until(o, 4)
        self.assertEqual(synth.ons_on(o.chairs["ob-1"]["channel"]), 0)
        play_until(o, 6)
        self.assertGreater(synth.ons_on(o.chairs["ob-1"]["channel"]), 0)

    def test_candidates_follow_what_the_chair_has(self):
        o, _, _ = orchestra()
        o.cue("info.cue.all", {})
        self.assertEqual([c["phrase"] for c in o.phrases("vc-1")["candidates"]], [1, 2, 3])
        o.play("vc-1", 1)
        self.assertEqual([c["phrase"] for c in o.phrases("vc-1")["candidates"]], [2, 3, 4])
        play_until(o, 6)
        o.render(441 * 30)
        r = o.phrases("cb-1")
        self.assertEqual(r["candidates"][0]["phrase"], 1)
        self.assertIn("from bar 7", r["candidates"][0]["late"])
        play_until(o, 8)
        o.render(441 * 30)
        self.assertEqual(o.phrases("cb-1")["candidates"][0]["phrase"], 2)

    def test_the_question_for_jev(self):
        o, _, _ = orchestra()
        o.heard("info.cue.strings", "Strings, come in.", {"dynamics": "f"}, 1000.0)
        o.cue("info.cue.strings", {"dynamics": "f"})
        ask = o.phrases("vn2-4")["ask"]
        self.assertEqual(ask["options"], ["phrase 1: bars 1–8", "phrase 2: bars 9–16", "phrase 3: bars 17–24"])
        self.assertEqual(ask["question"], "Which phrase should Violins II chair 4 play next?")
        for text in ("Violins II, chair 4 of 8", "unison", 'info.cue.strings {"dynamics": "f"}', "counting in", "- phrase 2: bars 9–16, starts in 16.0 bars"):
            self.assertIn(text, ask["state"])
        self.assertLess(len(ask["state"]), 2000)

    def test_listen_returns_when_the_fork_opens_with_the_candidates(self):
        o, _, _ = orchestra()
        o.cue("info.cue.all", {})
        o.play("vn1-1", 1)
        self.assertEqual(o.chairs["vn1-1"]["forkBeats"], 0)
        result = {}
        t = threading.Thread(target=lambda: result.update(o.listen("vn1-1", bars=32, max_seconds=60)))
        t.start()
        play_until(o, 1)
        o.render(441)
        t.join(3)
        self.assertFalse(t.is_alive())
        self.assertEqual([c["phrase"] for c in result["candidates"]], [2, 3, 4])
        self.assertEqual(result["ask"]["options"][0], "phrase 2: bars 9–16")
        self.assertEqual(result["playing"]["phrase"], 1)

    def test_the_chairs_forks_are_spread_over_two_bars(self):
        o, _, _ = orchestra()
        beats = [c["forkBeats"] for c in o.chairs.values()]
        self.assertEqual(set(beats), set(range(8)))

    def test_listen_returns_when_the_chair_is_stopped(self):
        o, _, _ = orchestra()
        o.play("vn1-1", 1)
        o.play("vn1-1", 2)
        result = {}
        t = threading.Thread(target=lambda: result.update(o.listen("vn1-1", bars=8, max_seconds=5)))
        t.start()
        o.stop_all("stop")
        t.join(3)
        self.assertFalse(t.is_alive())
        self.assertEqual((result["state"], result["status"]), ("stopped", "stopped"))
        self.assertIn("Violins I: 0 of 10 playing", result["section"])

    def test_refusals_the_bot_reads(self):
        o, _, _ = orchestra()
        for chair, phrase, dynamics in (("tuba-1", 1, None), ("vn1-1", 0, None), ("vn1-1", 53, None), ("vn1-1", "2", None), ("vn1-1", 1, "fff")):
            with self.assertRaises(Refused):
                o.play(chair, phrase, dynamics)
        o.play("vn1-1", 1)
        with self.assertRaises(Refused):
            o.play("vn1-1", 1)
        play_until(o, 10)
        with self.assertRaises(Refused) as late:
            o.play("vn1-2", 1)
        self.assertIn("phrase 2 (bars 9–16)", str(late.exception))
        with self.assertRaises(Refused):
            o.phrases("vn1-1", bar=999)


class JudgeTest(unittest.TestCase):
    def test_the_pick_is_kept_as_the_bot_reported_it(self):
        o, _, _ = orchestra()
        o.play("vn1-1", 1, judge={"choice": "phrase 1: bars 1–8", "probability": 0.97, "confidence": 0.94})
        judge = o.snapshot()["sections"][0]["chairs"][0]["judge"]
        self.assertEqual((judge["phrase"], judge["choice"], judge["probability"], judge["confidence"], judge["matchesPhrase"]),
                         (1, "phrase 1: bars 1–8", 0.97, 0.94, True))
        self.assertIn("reported by the bot", judge["source"])

    def test_a_choice_that_names_another_phrase_is_flagged(self):
        o, _, _ = orchestra()
        o.play("vn1-1", 2, judge={"choice": "phrase 21: bars 161–168", "probability": 0.6})
        self.assertFalse(o.chairs["vn1-1"]["judge"]["matchesPhrase"])

    def test_no_report_or_nonsense_is_no_pick(self):
        o, _, _ = orchestra()
        o.play("vn1-1", 1)
        o.play("vn1-2", 1, judge={"probability": 7, "choice": ""})
        o.play("vn1-3", 1, judge="phrase 1")
        self.assertEqual([o.chairs[f"vn1-{n}"]["judge"] for n in (1, 2, 3)], [None, None, None])


class StopTest(unittest.TestCase):
    def test_a_chair_stops_and_its_section_goes_on(self):
        o, synth, _ = orchestra()
        o.play("va-1", 1)
        o.play("va-1", 2)
        o.play("va-2", 1)
        play_until(o, 2)
        out = o.stop("va-1")
        self.assertEqual((out["state"], out["was"]), ("stopped", "playing"))
        self.assertIn(("all_off", o.chairs["va-1"]["channel"]), synth.calls)
        before = synth.ons_on(o.chairs["va-1"]["channel"])
        play_until(o, 12)
        self.assertEqual(synth.ons_on(o.chairs["va-1"]["channel"]), before)
        self.assertEqual(o.snapshot()["sections"][2]["playing"], 0)
        self.assertEqual(o.chairs["va-1"]["plan"], [])

    def test_stop_all_silences_every_chair_at_once(self):
        o, synth, clock = orchestra()
        o.play("vn1-1", 1)
        o.play("vc-1", 1)
        play_until(o, 4)
        clock.now = 1000.4
        cut = o.stop_all("stop.all from operator", event_at=1000.1)
        self.assertEqual(o.status, "stopped")
        self.assertAlmostEqual(cut["afterEventSeconds"], 0.3)
        self.assertEqual(cut["bar"], 3)
        for c in o.chairs.values():
            self.assertIn(("silence", c["channel"]), synth.calls)
        self.assertEqual({c["state"] for s in o.snapshot()["sections"] for c in s["chairs"] if c["state"] != "waiting"}, {"stopped"})
        self.assertIsNone(o.stop_all("again"))

    def test_after_stop_all_only_a_cue_starts_the_music_again(self):
        o, _, _ = orchestra()
        o.play("vn1-1", 1)
        o.play("vn1-1", 2)
        play_until(o, 2)
        o.stop_all("stop")
        self.assertEqual(o.chairs["vn1-1"]["plan"], [])
        with self.assertRaises(Refused) as refused:
            o.play("vn1-1", 3)
        self.assertIn("stopped the music", str(refused.exception))
        reading = o.phrases("vn1-1")
        self.assertNotIn("ask", reading)
        self.assertIn("wait for the next cue", reading["note"])
        self.assertEqual(o.cue("info.cue.all", {}), "count-in")
        out = o.play("vn2-1", 1)
        self.assertEqual((out["fromBar"], o.take, o.take_log(1)["cut"]["why"]), (1, 2, "stop"))

    def test_a_group_stop_holds_its_chairs_and_the_others_play_on(self):
        o, synth, clock = orchestra()
        for chair in ("ob-1", "ob-2", "hn-1", "vn1-1"):
            o.play(chair, 1)
        play_until(o, 3)
        clock.now = 1000.5
        held = o.hold(o.chairs_of("winds"), "stop.ob from operator", event_at=1000.25)
        self.assertEqual((held["held"], held["afterEventSeconds"]), (["ob-1", "ob-2", "hn-1", "hn-2"], 0.25))
        self.assertIn(("silence", o.chairs["ob-1"]["channel"]), synth.calls)
        self.assertNotIn(("silence", o.chairs["vn1-1"]["channel"]), synth.calls)
        self.assertEqual(o.status, "playing")
        before = synth.ons_on(o.chairs["vn1-1"]["channel"]), synth.ons_on(o.chairs["ob-1"]["channel"])
        play_until(o, 6)
        self.assertGreater(synth.ons_on(o.chairs["vn1-1"]["channel"]), before[0])
        self.assertEqual(synth.ons_on(o.chairs["ob-1"]["channel"]), before[1])
        chair = next(c for s in o.snapshot()["sections"] for c in s["chairs"] if c["id"] == "ob-1")
        self.assertEqual((chair["state"], chair["held"]), ("paused", "stop.ob from operator"))
        self.assertTrue(o.chairs["ob-1"]["plan"])  # a pause keeps the phrases: only the notes are not sounded
        reading = o.phrases("ob-1")
        self.assertIn("paused your section", reading["paused"])
        self.assertNotIn("done", reading)

    def test_a_cue_that_names_paused_chairs_sounds_them_at_their_next_note(self):
        o, synth, _ = orchestra()
        o.play("ob-1", 1)
        o.play("vn1-1", 1)
        play_until(o, 3)
        o.hold(["ob-1"], "fist")
        self.assertEqual(o.play("ob-1", 2)["phrase"], 2)  # its bot goes on choosing, silent
        silent = synth.ons_on(o.chairs["ob-1"]["channel"])
        play_until(o, 5)
        self.assertEqual(synth.ons_on(o.chairs["ob-1"]["channel"]), silent)
        self.assertIsNone(o.cue("info.cue.strings", {}))  # a cue that names other chairs keeps it paused
        o.cue("info.cue.winds", {})
        play_until(o, 6)
        self.assertGreater(synth.ons_on(o.chairs["ob-1"]["channel"]), silent)  # no new phrase, no new run

    def test_a_cue_with_dynamics_changes_the_level_at_once_and_on_the_phrases_asked_for(self):
        o, synth, _ = orchestra()
        o.play("vc-1", 1, "f")
        o.play("vc-1", 2, "f")
        o.play("ob-1", 1, "f")
        play_until(o, 3)
        loud = synth.last_volume(o.chairs["vc-1"]["channel"])
        self.assertEqual(o.cue("info.cue.strings", {"dynamics": "p"}), "p for 32 chairs")
        soft = synth.last_volume(o.chairs["vc-1"]["channel"])
        self.assertLess(soft, loud)
        self.assertEqual(synth.last_volume(o.chairs["ob-1"]["channel"]), round(127 * o.levels["f"] ** 0.5))
        play_until(o, 10)  # phrase 2 begins: it keeps the cue's level, not the one it was asked with
        self.assertEqual(synth.last_volume(o.chairs["vc-1"]["channel"]), soft)
        self.assertEqual(o.snapshot()["sections"][3]["chairs"][0]["dynamics"], "p")
        self.assertIn("cue", o.snapshot()["dynamicsSource"])

    def test_tempo_is_clamped_and_moves_the_clock(self):
        o, _, _ = orchestra()
        self.assertEqual(o.set_tempo(500), 200.0)
        self.assertEqual(o.set_tempo(108), 108.0)
        o.play("vn1-1", 1)
        play_bars(o, 10)  # the count-in, then two bars at 108
        self.assertEqual(o.snapshot()["bar"], 3)


class StateTest(unittest.TestCase):
    def test_each_chair_says_its_phrase_now_and_next_and_where_that_comes_from(self):
        o, _, _ = orchestra()
        o.play("vn1-1", 1, "f")
        o.play("vn1-1", 2, "p")
        play_until(o, 3)
        chair = o.snapshot()["sections"][0]["chairs"][0]
        self.assertEqual((chair["state"], chair["dynamics"], chair["enteredBar"], chair["line"]), ("playing", "p", 1, "Violins I"))
        self.assertEqual((chair["phrase"]["playing"]["phrase"], chair["phrase"]["next"]["phrase"]), (1, 2))
        self.assertEqual(chair["phrase"]["next"]["startsInBars"], 6.0)
        self.assertIn("score service", chair["phrase"]["source"])

    def test_the_take_log_has_every_play_and_every_phrase_begun(self):
        o, _, _ = orchestra()
        o.play("vn1-1", 1, judge={"choice": "phrase 1: bars 1–8", "probability": 0.9})
        o.play("ob-1", 1)
        play_until(o, 2)
        log = o.take_log()["log"]
        self.assertEqual([(e["what"], e["chair"]) for e in log], [("play", "vn1-1"), ("play", "ob-1"), ("began", "vn1-1"), ("began", "ob-1")])
        self.assertAlmostEqual(log[0]["leadSeconds"], 12.63, places=1)
        self.assertEqual(log[0]["judge"]["probability"], 0.9)

    def test_snapshot_lists_sections_and_keeps_the_last_bus_event(self):
        o, _, _ = orchestra()
        o.heard("info.cue.strings", "Strings, come in.", {"dynamics": "f"}, 1000.0)
        for n in range(1, 11):
            o.play(f"vn1-{n}", 1)
        play_until(o, 9)
        for n in range(1, 11):
            for k in range(2, 25):
                o.play(f"vn1-{n}", k)
        snap = o.snapshot()
        self.assertEqual([s["id"] for s in snap["sections"]], ["vn1", "vn2", "va", "vc", "cb", "ob", "hn"])
        self.assertEqual(snap["lastBus"]["name"], "info.cue.strings")
        self.assertEqual(snap["sections"][0]["playing"], 10)
        self.assertGreater(o.wait_change(-1, 0.1)["version"], 0)


class StagePageTest(unittest.TestCase):
    def test_density_is_one_strip_per_section_and_each_line_has_its_phrases(self):
        o, _, _ = orchestra()
        d = o.density()
        self.assertEqual((d["bars"], d["beatsPerBar"], d["phraseBars"], len(d["sections"]), len(d["lines"])), (414, 4, 8, 7, 9))
        horns = next(s for s in d["sections"] if s["id"] == "hn")["density"]
        self.assertEqual(horns[:4], [0, 0, 0, 0])
        self.assertTrue(all(0 <= x <= 1 for s in d["sections"] for x in s["density"]))
        violins = next(line for line in d["lines"] if line["id"] == "vn1")
        self.assertEqual((len(violins["chairs"]), len(violins["phrases"])), (10, 52))

    def test_the_page_and_its_files_are_there(self):
        for name in ("stage.html", "stage.js", "stage.css", "hands.html", "hands.js", "posture.js", "room.js", "stage3d-3.three.module.min.js", "stage3d-3.three.core.min.js", "fonts/Lexend-Regular.ttf", "fonts/OFL-Lexend.txt"):
            self.assertTrue(os.path.isfile(os.path.join(HERE, "static", name)), name)
        for page in ("stage.html", "hands.html"):
            with open(os.path.join(HERE, "static", page)) as f:
                self.assertIn("SIMULATION", f.read())


class BusTest(unittest.TestCase):
    def published(self, name, data=None):
        return {"type": "group.event.published", "createdAt": "2026-10-29T15:00:00.25Z",
                "payload": {"name": name, "from": "operator", "data": data}}

    def test_stop_all_cuts(self):
        o, _, clock = orchestra()
        o.play("vn1-1", 1)
        clock.now = bus.parse_time("2026-10-29T15:00:00.55Z")
        self.assertEqual(bus.apply(o, self.published("stop.all")), "cut")
        self.assertAlmostEqual(o.cut["afterEventSeconds"], 0.3, places=3)
        self.assertEqual(bus.apply(o, self.published("stop.all")), "already stopped")

    def test_tempo_from_data(self):
        o, _, _ = orchestra()
        self.assertEqual(bus.apply(o, self.published("info.tempo", {"bpm": 108})), "tempo")
        self.assertEqual(o.bpm, 108.0)
        self.assertIsNone(bus.apply(o, self.published("info.tempo", {"bpm": "fast"})))

    def test_a_cue_starts_a_count_in_once(self):
        o, _, _ = orchestra()
        self.assertEqual(bus.apply(o, self.published("info.cue.strings", {"dynamics": "f"})), "count-in")
        self.assertEqual((o.status, o.take, o.snapshot()["countIn"]), ("playing", 1, 8))
        self.assertIsNone(bus.apply(o, self.published("info.cue.winds")))
        self.assertEqual(o.take, 1)
        self.assertEqual(set(o.cues), {"strings", "winds"})

    def test_a_stop_addressed_to_chairs_holds_only_them(self):
        o, _, _ = orchestra()
        o.play("ob-1", 1)
        o.play("vn1-1", 1)
        event = self.published("stop.ob")
        event["payload"]["to"] = ["ob-1", "ob-2", "someone-else"]
        self.assertEqual(bus.apply(o, event), "held 2 chairs")
        self.assertEqual((o.status, o.chairs["ob-1"]["held"], o.chairs["vn1-1"]["held"]), ("playing", "stop.ob from operator", None))

    def test_a_stop_named_for_a_family_holds_it_and_an_unknown_stop_cuts_all(self):
        o, _, _ = orchestra()
        o.play("vn1-1", 1)
        self.assertEqual(bus.apply(o, self.published("stop.strings")), "held 32 chairs")
        self.assertEqual(o.status, "playing")
        self.assertEqual(bus.apply(o, self.published("stop.everything")), "cut")
        self.assertEqual(o.status, "stopped")
        self.assertEqual(bus.apply(o, self.published("stop.winds")), "nothing plays")

    def test_the_services_own_events_are_heard_not_applied_again(self):
        b = bus.Bus.__new__(bus.Bus)  # no control plane: only the record of what it published
        b.own, b.own_lock = [bus.own_mark("stop.ob", "Oboes, stop.", ["ob-2", "ob-1"])], threading.Lock()
        mine = self.published("stop.ob")
        mine["payload"].update(message="Oboes, stop.", to=["ob-1", "ob-2"])
        self.assertTrue(b.is_own(mine))
        self.assertFalse(b.is_own(mine))  # once: the same event from the panel later is applied
        self.assertFalse(b.is_own(self.published("stop.ob")))

    def test_other_events_change_nothing(self):
        o, _, _ = orchestra()
        self.assertIsNone(bus.apply(o, self.published("info.note")))
        self.assertIsNone(bus.apply(o, {"type": "bot.task.started", "createdAt": "2026-10-29T15:00:00Z"}))
        self.assertEqual(o.status, "idle")


if __name__ == "__main__":
    unittest.main()


class DoneForAFixedProgram(unittest.TestCase):
    """A Jev bot's program reads done, where a model reads the note."""

    def test_a_reading_with_a_choice_to_make_is_not_done(self):
        o, _, _ = orchestra()
        o.cue("info.cue.all", {})
        out = o.phrases("vn1-1")
        self.assertIn("ask", out)
        self.assertNotIn("done", out)

    def test_stop_all_and_a_chairs_own_stop_are_done(self):
        o, _, _ = orchestra()
        o.play("vn1-1", 1)
        o.stop_all("stop")
        out = o.listen("vn1-1", bars=1, max_seconds=1)
        self.assertEqual((out["status"], out.get("done")), ("stopped", True))
        o, _, _ = orchestra()
        o.play("va-1", 1)
        self.assertTrue(o.stop("va-1")["done"])
        out = o.phrases("va-1")
        self.assertEqual((out.get("done"), "ask" in out), (True, False))
        self.assertIn("you stopped", out["note"])

    def test_no_phrase_left_is_done(self):
        o, _, _ = orchestra()
        o.cue("info.cue.all", {})
        out = o.phrases("vn1-1", bar=o.score.bars)
        if "ask" in out:
            self.skipTest("the last bar still offers a phrase")
        self.assertEqual((out.get("done"), out["note"]), (True, "no phrase left to play"))

    def test_a_paused_chair_is_never_done(self):
        o, _, _ = orchestra()
        o.play("ob-1", 1)
        play_until(o, 2)
        o.hold(["ob-1"], "fist from the conductor's hands")
        out = o.listen("ob-1", bars=8, max_seconds=1)
        self.assertEqual(("done" in out, out["state"]), (False, "paused"))
        self.assertNotIn("done", o.phrases("ob-1"))
