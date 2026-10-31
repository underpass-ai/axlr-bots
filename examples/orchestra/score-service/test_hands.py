"""The conductor's hands with no headset, no Jev and no control plane: what is read, what it means, what is sent and what is not."""

import io
import json
import os
import threading
import unittest
import urllib.error

import jev
from hands import GESTURES, Hands
from test_score_service import CONFIG, Clock, orchestra, play_until

OPEN = {"fingers": {"thumb": 30, "index": 8, "middle": 6, "ring": 10, "little": 12}, "spread": 0.12,
        "palm": {"away": 0.88, "floor": 0.15, "toward": -0.88, "up": -0.15, "side": 0.1},
        "position": {"side": -0.25, "height": -0.25, "ahead": 0.4}}
FIST = {"fingers": {"thumb": 90, "index": 195, "middle": 210, "ring": 205, "little": 200}, "palm": {"side": 0.85}}


class FakeJev:
    """Answers each hand with the gesture named for it, at the probability given."""

    def __init__(self, **reads):
        self.reads, self.asked = reads, []

    def __call__(self, state, questions):
        self.asked.append((state, questions))
        answers = {}
        for side, (_, options) in questions.items():
            gesture, p = self.reads.get(side, ("nothing", 0.9))
            rest = (1 - p) / (len(options) - 1)
            answers[side] = {"choice": GESTURES[gesture], "confidence": p,
                             "probabilities": {o: (p if o == GESTURES[gesture] else rest) for o in options}}
        return {"model": "jev-1.13.0", "ms": 150, "answers": answers, "inputTokens": 600}


class FakePublish:
    def __init__(self):
        self.events = []

    def __call__(self, name, message, data, to):
        self.events.append((name, data, to))
        return f"ev-{len(self.events)}"


def hands(config=None, log=None, **reads):
    o, synth, clock = orchestra()
    judge, publish = FakeJev(**reads), FakePublish()
    h = Hands(o, dict({"threshold": 0.8, "cooldownSeconds": 3, "minIntervalSeconds": 0.5, "publishWaitSeconds": 3,
                       "instruments": CONFIG["hands"]["instruments"]}, **(config or {})), judge, publish, clock, log=log)
    return h, o, judge, publish, clock


def posture(**sides):
    return {"hands": sides, "heldMs": 300}


class ReadTest(unittest.TestCase):
    def test_nothing_is_the_default(self):
        h, o, judge, publish, _ = hands(left=("open", 0.7))
        record = h.observe(posture(left=OPEN))
        self.assertEqual((record["read"], record["sent"], publish.events), ({"left": "nothing"}, [], []))
        self.assertEqual(record["jev"]["answers"]["left"]["choice"], "open")  # Jev's pick is kept, below the threshold

    def test_no_hand_asks_nobody(self):
        h, _, judge, _, _ = hands()
        record = h.observe(posture())
        self.assertEqual((judge.asked, record["notSent"][0]["why"]), ([], "no hand tracked"))

    def test_jev_failing_is_no_signal(self):
        h, _, _, publish, _ = hands()

        def broken(state, questions):
            raise jev.JevError("TypeSafe HTTP 503")
        h.judge = broken
        record = h.observe(posture(left=OPEN))
        self.assertEqual((record["jev"]["error"], publish.events, record["read"]), ("TypeSafe HTTP 503", [], {}))

    def test_jev_reads_the_measurements_one_question_per_hand(self):
        h, _, judge, _, _ = hands()
        h.observe(posture(left=OPEN, right=FIST))
        state, questions = judge.asked[0]
        self.assertEqual(sorted(questions), ["left", "right"])
        self.assertEqual(questions["left"][1], list(GESTURES.values()))
        self.assertIn("finger bend: thumb 30°, index 8°", state)
        self.assertIn("palm facing: away from the conductor, toward the orchestra +0.88", state)
        self.assertIn("25 cm to the left of the head", state)
        self.assertNotIn("not tracked", state)
        h2, _, judge2, _, clock = hands()
        h2.observe(posture(right=FIST))
        self.assertIn("Left hand: not tracked.", judge2.asked[0][0])

    def test_readings_are_at_least_half_a_second_apart(self):
        h, _, judge, _, clock = hands()
        h.observe(posture(left=OPEN))
        clock.now += 0.2
        record = h.observe(posture(left=FIST))
        self.assertEqual(len(judge.asked), 1)
        self.assertIn("too soon", record["notSent"][0]["why"])
        clock.now += 0.4
        h.observe(posture(left=FIST))
        self.assertEqual(len(judge.asked), 2)


class SendTest(unittest.TestCase):
    def test_left_open_cues_the_strings_loud_and_starts_the_music(self):
        h, o, _, publish, _ = hands(left=("open", 0.95))
        record = h.observe(posture(left=OPEN))
        self.assertEqual(record["decision"], [{"gesture": "open", "group": "strings"}])
        self.assertEqual(publish.events, [("info.cue.strings", {"dynamics": "f"}, None)])
        self.assertEqual(record["sent"][0]["eventId"], "ev-1")

    def test_both_hands_the_same_is_everyone(self):
        h, _, _, publish, _ = hands(left=("down", 0.9), right=("down", 0.85))
        h.observe(posture(left=OPEN, right=OPEN))
        self.assertEqual(publish.events, [("info.cue.all", {"dynamics": "p"}, None)])

    def test_two_different_gestures_act_on_each_hands_group(self):
        h, o, _, publish, _ = hands(left=("open", 0.9), right=("down", 0.9))
        h.observe(posture(left=OPEN, right=OPEN))
        self.assertEqual([e[0] for e in publish.events], ["info.cue.strings", "info.cue.winds"])

    def test_a_fist_fades_its_group_out_and_sends_no_stop(self):
        h, o, _, publish, _ = hands(right=("fist", 0.95))
        o.play("ob-1", 1)
        o.play("vn1-1", 1)
        play_until(o, 2)
        record = h.observe(posture(right=FIST))
        self.assertEqual((record["applied"], record["sent"], publish.events), ("fading out 4 chairs", [], []))
        self.assertEqual((o.chairs["ob-1"]["held"], o.chairs["vn1-1"]["held"], o.status), ("fist from the conductor's hands", None, "playing"))

    def test_the_service_does_what_the_gesture_means_at_once(self):
        h2, o2, _, _, _ = hands(left=("open", 0.95))
        self.assertEqual(h2.observe(posture(left=OPEN))["applied"], "count-in")
        self.assertEqual((o2.status, o2.take), ("playing", 1))

    def test_the_answer_does_not_wait_for_the_control_plane(self):
        o, _, clock = orchestra()
        gate = threading.Event()

        def slow(name, message, data, to):
            gate.wait(5)
            return "ev-slow"
        h = Hands(o, {"threshold": 0.8}, FakeJev(left=("open", 0.95)), slow, clock)
        record = h.observe(posture(left=OPEN))
        self.assertEqual((record["applied"], o.status, record["sent"][0]["eventId"]), ("count-in", "playing", None))
        gate.set()
        for _ in range(100):
            if h.view()["last"]["sent"][0]["eventId"]:
                break
            threading.Event().wait(0.01)
        self.assertEqual(h.view()["last"]["sent"][0]["eventId"], "ev-slow")

    def test_two_fists_fade_everyone_out_and_the_music_goes_on(self):
        h, o, _, publish, _ = hands(left=("fist", 0.9), right=("fist", 0.9))
        o.play("vc-1", 1)
        o.play("ob-1", 1)
        record = h.observe(posture(left=FIST, right=FIST))
        self.assertEqual((record["applied"], publish.events, o.status), ("fading out 36 chairs", [], "playing"))
        self.assertTrue(all(c["held"] for c in o.chairs.values()))

    def test_open_hands_after_a_fade_out_bring_the_sound_back(self):
        h, o, _, publish, clock = hands(left=("fist", 0.9), right=("fist", 0.9))
        synth = o.synth
        o.play("vc-1", 1)
        play_until(o, 3)
        h.observe(posture(left=FIST, right=FIST))
        play_until(o, 5)
        vc = o.chairs["vc-1"]["channel"]
        self.assertEqual(synth.last_expression(vc), 0)
        h.judge.reads = {"left": ("open", 0.95), "right": ("open", 0.95)}
        clock.now += 1
        record = h.observe(posture(left=OPEN, right=OPEN))
        self.assertEqual((record["sent"][0]["name"], record["applied"]), ("info.cue.all", "f for 36 chairs"))
        play_until(o, 6)
        self.assertEqual(synth.last_expression(vc), 127)

    def test_a_hand_cues_the_instrument_it_points_at_section_by_section(self):
        h, o, _, publish, _ = hands(left=("open", 0.95))
        record = h.observe(posture(left=dict(OPEN, aim=150)))  # far left: the violins, I and II
        self.assertEqual((record["aim"], record["decision"]), ({"left": "violins"}, [{"gesture": "open", "group": "violins"}]))
        self.assertEqual(publish.events, [("info.cue.vn1", {"dynamics": "f"}, None), ("info.cue.vn2", {"dynamics": "f"}, None)])
        h2, _, _, publish2, _ = hands(right=("open", 0.95))
        h2.observe(posture(right=dict(OPEN, aim=40)))
        self.assertEqual(publish2.events, [("info.cue.vc", {"dynamics": "f"}, None)])
        h3, _, _, publish3, _ = hands(right=("open", 0.95))
        h3.observe(posture(right=OPEN))  # no aim: the hand's old group
        self.assertEqual(publish3.events, [("info.cue.winds", {"dynamics": "f"}, None)])

    def test_a_fist_at_the_horns_fades_only_the_horns(self):
        h, o, _, _, _ = hands(right=("fist", 0.95))
        o.play("hn-1", 1)
        o.play("ob-1", 1)
        record = h.observe(posture(right=dict(FIST, aim=80)))
        self.assertEqual(record["applied"], "fading out 2 chairs")
        self.assertEqual((o.chairs["hn-1"]["held"] is not None, o.chairs["ob-1"]["held"]), (True, None))

    def test_the_height_fades_the_instrument_the_hand_conducts(self):
        h, o, _, _, clock = hands(left=("down", 0.95))
        h.observe(posture(left=dict(OPEN, aim=60)))  # the violas
        h.height({"hands": {"left": {"height": -0.2, "aim": 150}}})  # where the hand is now: the level starts from here
        clock.now += 0.5
        h.height({"hands": {"left": {"height": -0.55, "aim": 150}}})  # 35 cm down over 70 cm: half the way; moving the hand does not change the instrument
        self.assertEqual({round(o.chairs[k]["gain"], 2) for k in o.chairs_of("va")}, {0.5})
        self.assertEqual({o.chairs[k]["gain"] for k in o.chairs_of("vn1")}, {1.0})

    def test_the_height_of_a_hand_that_conducts_is_a_provisional_level_from_where_it_began(self):
        h, o, _, _, clock = hands(left=("open", 0.95))
        self.assertEqual(h.height({"hands": {"left": {"height": 0.15}}}), {})  # not read yet: nothing moves
        h.observe(posture(left=OPEN))
        self.assertEqual(h.height({"hands": {"left": {"height": -0.2}, "right": {"height": 0.1}}}), {"left": 1.0})  # the first height is where it begins
        clock.now += 0.5
        self.assertEqual(h.height({"hands": {"left": {"height": -0.55}}}), {"left": 0.5})  # 35 cm down over 70 cm
        self.assertEqual({o.chairs[k]["gain"] for k in o.chairs_of("strings")}, {0.5})
        self.assertEqual({o.chairs[k]["gain"] for k in o.chairs_of("winds")}, {1.0})
        self.assertEqual((h.view()["pending"]["left"]["group"], h.view()["pending"]["left"]["from"]), ("strings", 1.0))
        clock.now += 0.5
        self.assertEqual(h.height({"hands": {"left": {"height": 0.5}}}), {"left": 1.0})  # never above 1
        h.judge.reads = {"left": ("fist", 0.95)}
        clock.now += 1
        h.observe(posture(left=FIST))  # a fist conducts no level: what the hand held comes back
        self.assertEqual(h.view()["lastGain"]["why"], "the hand changed")
        self.assertEqual(h.height({"hands": {"left": {"height": -0.5}}}), {})
        self.assertEqual({o.chairs[k]["gain"] for k in o.chairs_of("strings")}, {1.0})

    def test_a_fade_out_when_nothing_of_the_group_plays_does_nothing(self):
        h, o, _, publish, _ = hands(left=("fist", 0.9), right=("fist", 0.9))
        record = h.observe(posture(left=FIST, right=FIST))
        self.assertEqual((publish.events, record["notSent"][0]["why"]), ([], "nothing of the group plays"))
        o.play("vc-1", 1)
        h2, o2, _, publish2, _ = hands(right=("fist", 0.9))
        o2.play("vc-1", 1)
        self.assertIn("nothing of the group plays", h2.observe(posture(right=FIST))["notSent"][0]["why"])

    def test_a_cue_that_changes_nothing_is_not_sent(self):
        h, o, _, publish, clock = hands(right=("open", 0.9))
        o.play("ob-1", 1, "f")
        o.play("ob-2", 1, "f")
        o.play("hn-1", 1, "f")
        o.play("hn-2", 1, "f")
        record = h.observe(posture(right=OPEN))
        self.assertEqual((publish.events, record["notSent"][0]["why"]), ([], "the group already plays f"))

    def test_the_same_gesture_again_waits_for_the_cooldown(self):
        h, o, _, publish, clock = hands(left=("open", 0.9))
        h.observe(posture(left=OPEN))
        clock.now += 1
        record = h.observe(posture(left=OPEN))
        self.assertIn("sent 1.0 s ago", record["notSent"][0]["why"])
        self.assertEqual(len(publish.events), 1)

    def test_dry_run_publishes_nothing_and_says_so(self):
        h, _, _, publish, _ = hands({"dryRun": True}, left=("open", 0.9))
        record = h.observe(posture(left=OPEN))
        self.assertEqual((publish.events, record["sent"][0]["note"], record["dryRun"]), ([], "dry run: not published", True))
        self.assertTrue(h.view()["dryRun"])

    def test_the_view_keeps_what_was_read_and_sent(self):
        h, _, _, _, _ = hands(left=("open", 0.9))
        h.observe(posture(left=OPEN))
        view = h.view()
        self.assertEqual(view["last"]["read"], {"left": "open"})
        self.assertIn("no image leaves the headset", view["source"])
        self.assertEqual(view["groups"], {"left": "strings", "right": "winds", "both": "all"})
        self.assertFalse(view["last"]["simulated"])
        self.assertTrue(h.observe({"hands": {"left": OPEN}, "simulated": True})["simulated"])


class Response:
    def __init__(self, body):
        self.body = body

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False

    def read(self, n):
        return self.body[:n]


class JevClientTest(unittest.TestCase):
    def setUp(self):
        os.environ["TYPESAFE_API_KEY"] = "secret-key-123"
        self.addCleanup(os.environ.pop, "TYPESAFE_API_KEY", None)

    def client(self, answer=None, error=None):
        sent = []

        def opener(request, timeout):
            sent.append(request)
            if error:
                raise error
            return Response(json.dumps(answer).encode())
        return jev.Jev(opener=opener), sent

    def test_one_choice_per_question_on_the_pinned_model(self):
        options = ["a", "b"]
        c, sent = self.client({"model": "jev-1.13.0", "usage": {"input_tokens": 600},
                               "answers": {"left": {"type": "choice", "choice": "b", "confidence": 0.9, "probabilities": {"a": 0.1, "b": 0.9}}}})
        out = c.choose("the state", {"left": ("which?", options)})
        body = json.loads(sent[0].data)
        self.assertEqual(body["questions"]["left"], {"type": "choice", "instructions": "which?", "criteria": {"a": None, "b": None}})
        self.assertEqual((body["model"], body["state"]), ("jev-1.13.0", "the state"))
        self.assertEqual((out["answers"]["left"]["choice"], out["inputTokens"]), ("b", 600))
        self.assertEqual(sent[0].get_header("Authorization"), "Bearer secret-key-123")

    def test_an_answer_that_does_not_fit_is_an_error_and_the_key_never_shows(self):
        bad = [{"model": "jev-latest", "answers": {}},
               {"model": "jev-1.13.0", "answers": {"left": {"type": "choice", "choice": "c", "probabilities": {"a": 0.5, "b": 0.5}}}},
               {"model": "jev-1.13.0", "answers": {}}]
        for answer in bad:
            c, _ = self.client(answer)
            with self.assertRaises(jev.JevError) as caught:
                c.choose("s", {"left": ("q", ["a", "b"])})
            self.assertNotIn("secret-key-123", str(caught.exception))
        c, _ = self.client(error=urllib.error.HTTPError("u", 401, "Unauthorized secret-key-123", {}, io.BytesIO()))
        with self.assertRaises(jev.JevError) as caught:
            c.choose("s", {"left": ("q", ["a", "b"])})
        self.assertEqual(str(caught.exception), "TypeSafe HTTP 401")

    def test_no_key_no_call(self):
        os.environ.pop("TYPESAFE_API_KEY")
        c = jev.Jev(key_file="/nonexistent/env", opener=lambda *a, **k: self.fail("called"))
        with self.assertRaises(jev.JevError):
            c.choose("s", {"left": ("q", ["a", "b"])})
        with self.assertRaises(ValueError):
            jev.Jev(model="jev-latest")


if __name__ == "__main__":
    unittest.main()


class TargetTest(unittest.TestCase):
    """The section the conductor looks at, locked with a pinch, is the one a gesture acts on."""

    def test_the_target_wins_over_the_aim_and_the_old_group(self):
        h, _, _, publish, _ = hands(left=("open", 0.95))
        record = h.observe(dict(posture(left=dict(OPEN, aim=150)), target="horns", locked=True))
        self.assertEqual((record["target"], record["locked"], record["decision"]), ("horns", True, [{"gesture": "open", "group": "horns"}]))
        self.assertEqual(publish.events, [("info.cue.hn", {"dynamics": "f"}, None)])

    def test_a_target_that_is_not_an_instrument_is_none(self):
        h, _, _, publish, _ = hands(right=("open", 0.95))
        record = h.observe(dict(posture(right=OPEN), target="piano"))
        self.assertEqual((record["target"], publish.events), (None, [("info.cue.winds", {"dynamics": "f"}, None)]))

    def test_both_hands_the_same_with_a_target_is_still_everyone(self):
        h, _, _, publish, _ = hands(left=("fist", 0.9), right=("fist", 0.9))
        o = h.orchestra
        o.play("vn1-1", 1)
        o.play("ob-1", 1)
        record = h.observe(dict(posture(left=FIST, right=FIST), target="horns"))
        self.assertEqual(record["decision"], [{"gesture": "fist", "group": "all"}])
        self.assertTrue(o.chairs["vn1-1"]["held"] and o.chairs["ob-1"]["held"])


class HushAndPointTest(unittest.TestCase):
    def test_a_finger_on_the_lips_is_pianissimo_for_everyone(self):
        h, o, _, publish, _ = hands(left=("hush", 0.92))
        o.play("vn1-1", 1, "f")
        o.play("ob-1", 1, "f")
        record = h.observe(dict(posture(left=OPEN), target="violins"))
        self.assertEqual(record["decision"], [{"gesture": "hush", "group": "all"}])
        self.assertEqual(publish.events, [("info.cue.all", {"dynamics": "p"}, None)])
        self.assertEqual(record["applied"], "everyone p at 55%")
        self.assertEqual({o.chairs[k]["dynamics"] for k in ("vn1-1", "ob-1")}, {"p"})

    def test_a_hush_when_nothing_plays_does_nothing(self):
        h, _, _, publish, _ = hands(right=("hush", 0.92))
        record = h.observe(posture(right=OPEN))
        self.assertEqual((publish.events, record["notSent"][0]["why"]), ([], "nothing plays"))

    def test_a_pointing_hand_is_no_signal_and_does_not_conduct_the_gain(self):
        h, o, _, publish, _ = hands(left=("point", 0.95))
        record = h.observe(posture(left=OPEN))
        self.assertEqual((record["read"], record["decision"], publish.events), ({"left": "point"}, [], []))
        self.assertEqual(h.height({"hands": {"left": {"height": -0.5}}}), {})


class NativeTest(unittest.TestCase):
    """The headset's own gestures: no Jev, the target's entrance, the tempo, a pinch held."""

    def test_a_thumb_tap_is_the_locked_targets_entrance_while_the_music_plays(self):
        h, o, _, publish, _ = hands()
        o.play("vn1-1", 1)
        o.play("vn2-1", 1)
        play_until(o, 2)
        o.hold(o.chairs_of("vn1") + o.chairs_of("vn2"), "a fist")
        record = h.native({"gesture": "tap-thumb", "side": "right", "target": "violins", "locked": True})
        self.assertEqual(record["decision"], [{"gesture": "tap-thumb", "group": "violins"}])
        self.assertEqual(publish.events, [("info.cue.vn1", None, None), ("info.cue.vn2", None, None)])
        self.assertEqual(record["sent"][0]["message"], "Violins, come in.")
        self.assertEqual((o.chairs["vn1-1"]["held"], o.chairs["vn2-1"]["held"]), (None, None))

    def test_a_thumb_tap_when_nothing_plays_or_no_section_is_looked_at_does_nothing(self):
        h, o, _, publish, _ = hands()
        record = h.native({"gesture": "tap-thumb", "side": "right", "target": "violins", "locked": True})
        self.assertEqual(record["notSent"][0]["why"], "nothing plays: a palm up starts the music")
        o.play("vn1-1", 1)
        record = h.native({"gesture": "tap-thumb", "side": "right", "target": None, "locked": False})
        self.assertEqual(record["notSent"][0]["why"], "no section: look at one")
        self.assertEqual(publish.events, [])
        record = h.native({"gesture": "tap-thumb", "side": "right", "target": "violins", "locked": False})  # looked at, not locked: enough
        self.assertEqual(record["decision"], [{"gesture": "tap-thumb", "group": "violins"}])
        self.assertEqual([e[0] for e in publish.events], ["info.cue.vn1", "info.cue.vn2"])

    def test_the_same_tap_again_waits_a_second(self):
        h, o, _, publish, clock = hands()
        o.play("vn1-1", 1)
        h.native({"gesture": "tap-thumb", "side": "right", "target": "violins", "locked": True})
        clock.now += 0.5
        record = h.native({"gesture": "tap-thumb", "side": "right", "target": "violins", "locked": True})
        self.assertIn("sent 0.5 s ago", record["notSent"][0]["why"])
        self.assertEqual(len(publish.events), 2)

    def test_swipes_step_the_tempo_and_the_bus_hears_it_seldom(self):
        h, o, _, publish, clock = hands()
        record = h.native({"gesture": "swipe-forward", "side": "right"})
        self.assertEqual((o.bpm, record["applied"], publish.events), (156.0, "tempo 156", [("info.tempo", {"bpm": 156.0}, None)]))
        clock.now += 1
        record = h.native({"gesture": "swipe-backward", "side": "right"})
        self.assertEqual((o.bpm, len(publish.events)), (152.0, 1))
        self.assertIn("published 1.0 s ago", record["notSent"][0]["why"])
        clock.now += 3
        h.native({"gesture": "swipe-backward", "side": "left"})
        self.assertEqual((o.bpm, publish.events[-1]), (148.0, ("info.tempo", {"bpm": 148.0}, None)))

    def test_a_beat_sets_the_clock_at_once(self):
        h, o, _, publish, _ = hands()
        record = h.native({"gesture": "beat", "side": "right", "bpm": 140})
        self.assertEqual((o.bpm, record["applied"], publish.events[-1]), (140.0, "tempo 140, as beaten", ("info.tempo", {"bpm": 140.0}, None)))
        self.assertEqual(h.native({"gesture": "beat", "side": "right"})["notSent"][0]["why"], "no tempo in the beat")

    def test_a_pinch_held_moves_the_targets_gain_from_where_it_was(self):
        h, o, _, publish, _ = hands()
        o.fade(o.chairs_of("vn1") + o.chairs_of("vn2"), 0.8, 0)
        record = h.native({"gesture": "pinch-start", "side": "left", "target": "violins", "height": -0.1})
        self.assertEqual(record["applied"], "holding Violins at 80%")
        record = h.native({"gesture": "pinch-move", "side": "left", "height": -0.275})  # 17.5 cm down over a 35 cm span: half the way
        self.assertEqual(record["applied"], "Violins 30%")
        self.assertEqual({round(o.chairs[k]["gain"], 2) for k in o.chairs_of("vn1")}, {0.3})
        self.assertEqual({o.chairs[k]["gain"] for k in o.chairs_of("ob")}, {1.0})
        record = h.native({"gesture": "pinch-move", "side": "left", "height": 0.4})
        self.assertEqual(record["applied"], "Violins 100%")
        h.native({"gesture": "pinch-end", "side": "left"})
        self.assertEqual(h.native({"gesture": "pinch-move", "side": "left", "height": 0.0})["notSent"][0]["why"], "no pinch began")
        self.assertEqual(publish.events, [])

    def test_a_pinch_released_keeps_its_level_and_the_height_starts_again_from_there(self):
        h, o, _, _, clock = hands(left=("open", 0.95))
        h.observe({"hands": {"left": OPEN}, "heldMs": 300, "target": "violins"})
        h.height({"hands": {"left": {"height": -0.1}}})
        record = h.native({"gesture": "pinch-start", "side": "left", "target": "violins", "height": -0.1})  # the pinch takes the level over
        self.assertEqual(h.view()["pending"], {})
        h.native({"gesture": "pinch-move", "side": "left", "height": -0.275})  # 17.5 cm down over 35 cm: half the way
        record = h.native({"gesture": "pinch-end", "side": "left"})
        self.assertEqual(record["applied"], "Violins kept at 50%")
        self.assertEqual(h.view()["confirmed"], {"violins": 0.5})
        clock.now += 0.3
        self.assertEqual(h.height({"hands": {"left": {"height": 0.2}}}), {})  # the open hand after the pinch moves nothing by itself
        self.assertEqual({round(o.chairs[k]["gain"], 2) for k in o.chairs_of("vn1")}, {0.5})
        clock.now += 1
        h.observe({"hands": {"left": OPEN}, "heldMs": 300, "target": "violins"})  # read again: the hand conducts from 50%, from where it is
        h.height({"hands": {"left": {"height": 0.2}}})
        clock.now += 0.5
        self.assertEqual(h.height({"hands": {"left": {"height": 0.34}}}), {"left": 0.7})  # 14 cm up over 70 cm
        self.assertEqual(h.view()["pending"]["left"]["from"], 0.5)
        clock.now += 0.7
        self.assertEqual([r["why"] for r in h.tick()], ["the hand left"])
        self.assertEqual({round(o.chairs[k]["gain"], 2) for k in o.chairs_of("vn1")}, {0.5})

    def test_a_pinch_with_no_target_holds_nothing(self):
        h, _, _, _, _ = hands()
        record = h.native({"gesture": "pinch-start", "side": "left", "height": 0.0})
        self.assertEqual(record["notSent"][0]["why"], "no section to hold")

    def test_what_the_headset_cannot_give_is_refused(self):
        h, _, _, _, _ = hands()
        self.assertEqual(h.native({"gesture": "wave"})["notSent"][0]["why"], "not a gesture the headset gives")
        self.assertIn("tap-thumb", h.view()["gestures"])


MOVED = {"seconds": 2.4, "speed": 0.12, "peak": 0.3, "rise": 0.08, "sweep": 0.06, "turns": 0.4, "height": -0.3, "open": 0.85, "moving": True}
FORCEFUL = {"seconds": 2.4, "speed": 1.6, "peak": 4.5, "rise": 0.6, "sweep": 0.5, "turns": 2.4, "height": 0.05, "open": 0.4, "moving": True}


class LevelTest(unittest.TestCase):
    """A flat hand's height is a provisional level: a double pinch keeps it; dropping, leaving or waiting brings it back."""

    def level(self, gain=0.5, **reads):
        h, o, judge, publish, clock = hands(**(reads or {"left": ("open", 0.95)}))
        h.observe(posture(left=OPEN))
        h.height({"hands": {"left": {"height": -0.2}}})
        clock.now += 0.5
        h.height({"hands": {"left": {"height": -0.2 - (1 - gain) * 0.7}}})
        self.assertEqual({round(o.chairs[k]["gain"], 2) for k in o.chairs_of("strings")}, {gain})
        return h, o, publish, clock

    def test_a_double_pinch_of_either_hand_keeps_the_level_and_frees_the_hand(self):
        h, o, publish, clock = self.level(0.5)
        record = h.native({"gesture": "pinch-double", "side": "right"})
        self.assertEqual((record["applied"], record["decision"]), ("Strings kept at 50%", [{"gesture": "pinch-double", "group": "strings"}]))
        self.assertEqual((h.view()["pending"], h.view()["confirmed"], h.view()["lastGain"]["kind"]), ({}, {"strings": 0.5}, "kept"))
        clock.now += 0.3
        self.assertEqual(h.height({"hands": {"left": {"height": -0.9}}}), {})  # the hand is free: lowering it changes nothing
        clock.now += 2
        self.assertEqual(h.tick(), [])
        self.assertEqual({o.chairs[k]["gain"] for k in o.chairs_of("strings")}, {0.5})
        self.assertEqual(publish.events, [("info.cue.strings", {"dynamics": "f"}, None)])  # the palm up's cue; a level is the service's alone

    def test_a_level_not_kept_comes_back_when_the_hand_leaves(self):
        h, o, _, clock = self.level(0.5)
        clock.now += 0.5
        self.assertEqual(h.tick(), [])  # a pinching hand says it is still there
        h.height({"hands": {"left": {"height": -0.4, "pinch": True}}})
        clock.now += 0.5
        self.assertEqual(h.tick(), [])
        clock.now += 0.7
        self.assertEqual([(r["why"], r["gain"]) for r in h.tick()], [("the hand left", 1.0)])
        self.assertEqual({o.chairs[k]["gain"] for k in o.chairs_of("strings")}, {1.0})
        self.assertEqual(h.view()["engaged"]["left"], None)

    def test_a_level_not_kept_comes_back_when_it_waits_too_long(self):
        h, o, _, clock = self.level(0.5)
        for _ in range(10):  # the hand holds still, no double pinch
            clock.now += 0.5
            h.height({"hands": {"left": {"height": -0.55}}})
            self.assertEqual(h.tick(), [])
        clock.now += 0.5
        h.height({"hands": {"left": {"height": -0.55}}})
        self.assertEqual([r["why"] for r in h.tick()], ["not kept within 5 s"])
        self.assertEqual({o.chairs[k]["gain"] for k in o.chairs_of("strings")}, {1.0})

    def test_a_hand_that_drops_fast_brings_the_level_back_at_once(self):
        h, o, _, clock = self.level(0.8)
        clock.now += 0.1
        self.assertEqual(h.height({"hands": {"left": {"height": -0.45}}}), {})  # 11 cm down in 0.1 s: 1.1 m/s, the arm resting
        self.assertEqual(h.view()["lastGain"]["why"], "the hand dropped")
        self.assertEqual({o.chairs[k]["gain"] for k in o.chairs_of("strings")}, {1.0})

    def test_a_double_pinch_with_nothing_waiting_says_so(self):
        h, _, _, _, _ = hands()
        record = h.native({"gesture": "pinch-double", "side": "left"})
        self.assertEqual(record["notSent"], [{"what": "keep", "why": "no level waiting: raise or lower a flat hand first"}])

    def test_the_log_keeps_the_session_line_by_line(self):
        import tempfile
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "hands.jsonl")
            h, o, judge, publish, clock = hands(log=path, left=("open", 0.95))
            h.observe(posture(left=OPEN))
            h.height({"hands": {"left": {"height": -0.2}}})
            clock.now += 0.5
            h.height({"hands": {"left": {"height": -0.55}}})
            h.native({"gesture": "pinch-double", "side": "right"})
            with open(path) as f:
                lines = [json.loads(line) for line in f]
        kinds = [("gain", r["gain"]["kind"]) if "gain" in r else ("native", r["native"]) if "native" in r else ("posture", r["read"]) for r in lines]
        self.assertEqual(kinds, [("posture", {"left": "open"}), ("gain", "pending"), ("gain", "kept"), ("native", "pinch-double")])


class FakeExpressionJev:
    """Answers the one expression question with the character named, at the probability given."""

    def __init__(self, character="nothing", p=0.9):
        self.character, self.p, self.asked = character, p, []

    def __call__(self, state, questions):
        from hands import CHARACTERS
        self.asked.append((state, questions))
        (key, (_, options)), = questions.items()
        rest = (1 - self.p) / (len(options) - 1)
        return {"model": "jev-1.13.0", "ms": 160, "inputTokens": 500, "answers": {key: {
            "choice": CHARACTERS[self.character], "confidence": self.p,
            "probabilities": {o: (self.p if o == CHARACTERS[self.character] else rest) for o in options}}}}


def expression(character="nothing", p=0.9, config=None):
    o, synth, clock = orchestra()
    judge, publish = FakeExpressionJev(character, p), FakePublish()
    h = Hands(o, dict({"threshold": 0.8, "publishWaitSeconds": 3, "instruments": CONFIG["hands"]["instruments"],
                       "expression": {"threshold": 0.55, "everySeconds": 2.0, "publishSeconds": 6.0}}, **(config or {})), judge, publish, clock)
    o.cue("info.cue.all", {})
    o.play("vn1-1", 1, "f")
    o.play("ob-1", 1, "f")
    return h, o, synth, judge, publish, clock


class MotionTest(unittest.TestCase):
    """The conductor's expression, read by Jev from how the hands move."""

    def test_jev_reads_the_sums_as_text_one_question_for_both_hands(self):
        h, o, synth, judge, publish, _ = expression("dolce", 0.9)
        record = h.motion({"hands": {"right": MOVED}, "target": None})
        state, questions = judge.asked[0]
        self.assertEqual(list(questions), ["expression"])
        self.assertEqual(len(questions["expression"][1]), 5)
        for text in ("Right hand: moved at 0.12 m/s", "peaks of 0.30 m/s (2.5 times the average)", "rose and fell over 8 cm and swept 6 cm",
                     "turned from rising to falling 0.4 times a second", "30 cm below the eyes", "fingers 0.85 open", "Left hand: not tracked",
                     "playing bar", "the last 2.4 s"):
            self.assertIn(text, state)
        self.assertEqual(record["read"], {"expression": "dolce", "probability": 0.9})
        self.assertEqual(record["jev"]["answers"]["expression"]["choice"], "dolce")

    def test_a_character_read_goes_on_everyone_at_once_and_is_published_once(self):
        h, o, synth, judge, publish, clock = expression("dolce", 0.9)
        record = h.motion({"hands": {"right": MOVED, "left": MOVED}})
        self.assertEqual(record["decision"], [{"gesture": "dolce", "group": "all"}])
        self.assertEqual(record["applied"], "Everyone: dolce")
        self.assertTrue(all(c["character"] == "dolce" for c in o.chairs.values()))
        self.assertEqual(synth.last("brightness", o.chairs["ob-1"]["channel"]), -2400)
        self.assertEqual(publish.events, [("info.expression", {"character": "dolce", "target": "all"}, None)])
        self.assertEqual(record["sent"][0]["eventId"], "ev-1")
        self.assertEqual(h.character["character"], "dolce")
        clock.now += 2.5
        again = h.motion({"hands": {"right": MOVED}})  # the same character: nothing changes, nothing is sent
        self.assertEqual(again["applied"], "Everyone: dolce already")
        self.assertEqual(again["sent"], [])
        self.assertEqual(again["notSent"][0]["why"], "the group already plays dolce")

    def test_under_the_threshold_or_nothing_changes_nothing(self):
        h, o, synth, judge, publish, _ = expression("appassionato", 0.5)
        record = h.motion({"hands": {"right": FORCEFUL}})
        self.assertEqual(record["read"], {"expression": "nothing", "probability": 0.5})
        self.assertEqual(record["notSent"], [{"what": "expression", "why": "Jev read appassionato at 0.50, under 0.55"}])
        self.assertTrue(all(c["character"] is None for c in o.chairs.values()))
        self.assertEqual(publish.events, [])
        h2, o2, _, _, publish2, _ = expression("nothing", 0.8)
        record = h2.motion({"hands": {"right": MOVED}})
        self.assertEqual(record["notSent"], [{"what": "expression", "why": "Jev read nothing at 0.80"}])
        self.assertEqual(publish2.events, [])

    def test_the_section_looked_at_takes_the_character_the_others_keep_theirs(self):
        h, o, synth, judge, publish, _ = expression("brillante", 0.8)
        record = h.motion({"hands": {"right": FORCEFUL}, "target": "oboes", "locked": False})  # looked at is enough; a lock only keeps it
        self.assertEqual(record["decision"], [{"gesture": "brillante", "group": "oboes"}])
        self.assertEqual(record["applied"], "Oboes: brillante")
        self.assertEqual({c["character"] for k, c in o.chairs.items() if k.startswith("ob-")}, {"brillante"})
        self.assertTrue(all(c["character"] is None for k, c in o.chairs.items() if not k.startswith("ob-")))
        self.assertEqual(publish.events, [("info.expression", {"character": "brillante", "target": "oboes"}, None)])
        record = h.motion({"hands": {"right": FORCEFUL}, "target": "oboes", "locked": False})  # not locked: everyone
        self.assertEqual(record["notSent"][0]["why"], "too soon after the last reading (2 s apart at most)")

    def test_readings_are_spaced_and_a_change_is_published_at_most_every_publish_seconds(self):
        h, o, synth, judge, publish, clock = expression("dolce", 0.9)
        h.motion({"hands": {"right": MOVED}})
        clock.now += 1.0
        soon = h.motion({"hands": {"right": MOVED}})
        self.assertEqual(soon["notSent"][0]["why"], "too soon after the last reading (2 s apart at most)")
        self.assertEqual(len(judge.asked), 1)
        clock.now += 2.0
        judge.character = "appassionato"
        changed = h.motion({"hands": {"right": FORCEFUL}})
        self.assertEqual(changed["applied"], "Everyone: appassionato")
        self.assertTrue(all(c["character"] == "appassionato" for c in o.chairs.values()))  # the sound changed
        self.assertEqual(changed["sent"], [])  # the bus hears it later
        self.assertIn("was published 3.0 s ago", changed["notSent"][0]["why"])
        self.assertEqual(len(publish.events), 1)

    def test_while_nothing_plays_nothing_is_applied_or_sent(self):
        o, synth, clock = orchestra()
        h = Hands(o, {"publishWaitSeconds": 3, "instruments": CONFIG["hands"]["instruments"]}, FakeExpressionJev("dolce", 0.95), FakePublish(), clock)
        record = h.motion({"hands": {"right": MOVED}})
        self.assertEqual(record["read"], {"expression": "dolce", "probability": 0.95})
        self.assertEqual(record["notSent"], [{"what": "dolce", "why": "nothing plays: a palm up starts the music"}])
        self.assertEqual((record["decision"], record["sent"], h.publish.events), ([], [], []))
        self.assertTrue(all(c["character"] is None for c in o.chairs.values()))
        self.assertEqual(o.snapshot()["status"], "idle")

    def test_jev_failing_or_no_hand_is_no_change(self):
        h, o, synth, judge, publish, _ = expression("dolce", 0.9)
        self.assertEqual(h.motion({"hands": {}})["notSent"], [{"what": "expression", "why": "no hand tracked"}])

        def boom(state, questions):
            raise jev.JevError("TypeSafe HTTP 503")
        h.judge = boom
        record = h.motion({"hands": {"right": MOVED}})
        self.assertEqual((record["jev"]["error"], record["notSent"][0]["why"]), ("TypeSafe HTTP 503", "Jev did not answer"))
        self.assertTrue(all(c["character"] is None for c in o.chairs.values()))
        self.assertIn("motion", h.view()["gestures"])
