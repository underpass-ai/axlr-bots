"""The conductor's hands with no headset, no Jev and no control plane: what is read, what it means, what is sent and what is not."""

import io
import json
import os
import unittest
import urllib.error

import jev
from hands import GESTURES, Hands
from test_score_service import Clock, orchestra, play_until

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


def hands(config=None, **reads):
    o, synth, clock = orchestra()
    judge, publish = FakeJev(**reads), FakePublish()
    h = Hands(o, dict({"threshold": 0.8, "cooldownSeconds": 3, "minIntervalSeconds": 0.5}, **(config or {})), judge, publish, clock)
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

    def test_a_fist_stops_each_section_of_its_group_addressed_to_its_chairs(self):
        h, o, _, publish, _ = hands(right=("fist", 0.95))
        o.play("ob-1", 1)
        o.play("vn1-1", 1)
        play_until(o, 2)
        h.observe(posture(right=FIST))
        self.assertEqual(publish.events, [("stop.ob", None, ["ob-1", "ob-2"]), ("stop.hn", None, ["hn-1", "hn-2"])])

    def test_the_service_does_what_the_gesture_means_at_once(self):
        h, o, _, publish, _ = hands(right=("fist", 0.95))
        o.play("ob-1", 1)
        o.play("vn1-1", 1)
        play_until(o, 2)
        record = h.observe(posture(right=FIST))
        self.assertEqual(record["applied"], "held 4 chairs")
        self.assertEqual((o.chairs["ob-1"]["held"], o.chairs["vn1-1"]["held"], o.status), ("fist from the conductor's hands", None, "playing"))
        self.assertTrue(all("publishMs" in e for e in record["sent"]))
        h2, o2, _, _, _ = hands(left=("open", 0.95))
        self.assertEqual(h2.observe(posture(left=OPEN))["applied"], "count-in")
        self.assertEqual((o2.status, o2.take), ("playing", 1))

    def test_the_strings_fist_is_five_events_none_over_sixteen_chairs(self):
        h, o, _, publish, _ = hands(left=("fist", 0.95))
        o.play("vc-1", 1)
        h.observe(posture(left=FIST))
        self.assertEqual([e[0] for e in publish.events], ["stop.vn1", "stop.vn2", "stop.va", "stop.vc", "stop.cb"])
        self.assertTrue(all(len(e[2]) <= 16 for e in publish.events))

    def test_two_fists_are_stop_all(self):
        h, o, _, publish, _ = hands(left=("fist", 0.9), right=("fist", 0.9))
        o.play("vc-1", 1)
        h.observe(posture(left=FIST, right=FIST))
        self.assertEqual(publish.events, [("stop.all", None, None)])

    def test_a_stop_when_nothing_of_the_group_plays_is_not_sent(self):
        h, o, _, publish, _ = hands(left=("fist", 0.9), right=("fist", 0.9))
        record = h.observe(posture(left=FIST, right=FIST))  # idle: stop.all would still wake every bot
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
