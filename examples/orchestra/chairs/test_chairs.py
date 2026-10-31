"""The 36 definitions, with no control plane."""

import json
import os
import tempfile
import unittest

import chairs
import measure_take

MOVEMENT_BARS = 414  # Mozart, Symphony No. 25, I: what the score service's /api/state reports as bars
BEATS_PER_BAR = 4


class ChairsTest(unittest.TestCase):
    def setUp(self):
        self.config, self.template = chairs.load()

    def test_36_chairs_by_section(self):
        every = chairs.chairs(self.config)
        self.assertEqual(len(every), 36)
        self.assertEqual(every[0][0], "vn1-1")
        self.assertEqual(every[-1][0], "hn-2")

    def test_a_chair_is_placed_on_its_own_node_and_hears_its_family_cue(self):
        section = next(s for s in self.config["sections"] if s["id"] == "hn")
        bot = chairs.definition(self.template, section, 2)
        self.assertEqual((bot["id"], bot["name"]), ("hn-2", "Horns 2"))
        self.assertEqual(bot["placement"], {"nodes": ["stage-hn-2"]})
        self.assertEqual([s["topic"] for s in bot["subscriptions"]], ["info.cue.winds", "info.cue.hn", "info.cue.all", "info.expression"])
        self.assertTrue(all(s["mode"] == "message" and s["from"] == ["operator"] for s in bot["subscriptions"]))
        self.assertTrue(bot["mission"].startswith("Horns, chair 2 of 2. "))
        self.assertEqual(bot["groups"], ["orchestra"])
        self.assertEqual(bot["level"], "act-within-policy")

    def test_no_placeholder_is_left(self):
        for chair, section, n in chairs.chairs(self.config):
            text = str(chairs.definition(self.template, section, n))
            for placeholder in ("CHAIR", "SECTION", "TOTAL", "FAMILY", "chair N of", "SECTIONKEY"):
                self.assertNotIn(placeholder, text, chair)

    def test_the_score_services_tools_and_jevs(self):
        self.assertEqual(sorted(self.template["allowedTools"]), ["axlr_judge", "listen", "phrases", "play", "stop"])
        self.assertIn("axlr_judge", self.template["mission"])

    def test_a_whole_movement_fits_the_budget(self):
        """One cue, one task, the whole movement: 414 bars in 8-bar phrases, at any tempo the conductor may ask."""
        phrases = -(-MOVEMENT_BARS // self.config["phrases"]["bars"])  # 52
        slowest = (MOVEMENT_BARS + self.config["countIn"]["bars"]) * BEATS_PER_BAR * 60 / self.config["tempo"]["min"]  # 28 min at 60 bpm
        for runtime, jev_calls_a_phrase, model_calls_a_phrase in (("axlr", 0, 3), ("jev", 1, 0)):
            budgets = chairs.load(runtime)[1]["budgets"]
            # phrases at the cue; then play, listen and a second listen (a slow phrase outlasts listen.maxSeconds) each phrase
            self.assertGreaterEqual(budgets["maxToolCallsPerTask"], 1 + 3 * phrases, runtime)
            # a Jev chair asks Jev once a phrase (and may retry); a model chair spends a model call on each tool call
            self.assertGreaterEqual(budgets["maxModelCallsPerTask"], (jev_calls_a_phrase + model_calls_a_phrase) * phrases, runtime)
            self.assertGreaterEqual(budgets["taskTimeoutSeconds"], slowest, runtime)
            self.assertLessEqual(budgets["taskTimeoutSeconds"], 2 * slowest, runtime)  # a chair the world never ends still ends

    def test_an_unknown_chair_is_refused(self):
        with self.assertRaises(SystemExit):
            chairs.selected(self.config, ["tuba-1"])


class MeasureTest(unittest.TestCase):
    """What measure_take reads from a chair's node: the run's receipt and Jev's answers in its session."""

    def setUp(self):
        self.stage = tempfile.mkdtemp()
        measure_take.STAGE = self.stage
        traces = os.path.join(self.stage, "ob-1", "traces")
        sessions = os.path.join(self.stage, "ob-1", ".local", "state", "axlr", "sessions")
        os.makedirs(traces), os.makedirs(sessions)
        receipt = {"session_id": "s1", "started_at": "2026-10-30T08:46:51.5Z",
                   "requests": [{"cost_usd": 0.001}], "tool_calls": [{"tool": "axlr_judge", "duration_ms": 140}]}
        with open(os.path.join(traces, "exec-1.jsonl"), "w") as f:
            f.write(json.dumps({"type": "session"}) + "\n" + "not json\n" + json.dumps({"type": "receipt", "receipt": receipt}) + "\n")
        messages = [
            {"role": "assistant", "tool_calls": [{"id": "a", "name": "axlr_judge", "arguments": {}}, {"id": "b", "name": "axlr_call_tool", "arguments": {}}]},
            {"role": "tool", "tool_call_id": "a", "content": json.dumps({"choice": "phrase 1: bars 1–8", "probabilities": {"phrase 1: bars 1–8": 0.75}})},
            {"role": "tool", "tool_call_id": "b", "content": "{}"},
        ]
        with open(os.path.join(sessions, "s1.json"), "w") as f:
            json.dump({"messages": messages}, f)

    def test_the_runs_after_the_cue_and_jevs_answers(self):
        cue = measure_take.parse_time("2026-10-30T08:46:51.095Z")
        runs = measure_take.runs("ob-1", cue)
        self.assertEqual([r["session_id"] for r in runs], ["s1"])
        self.assertEqual(measure_take.runs("ob-1", cue + 3600), [])
        answers = measure_take.jev_answers("ob-1", "s1")
        self.assertEqual([a["choice"] for a in answers], ["phrase 1: bars 1–8"])
        self.assertEqual(measure_take.jev_answers("ob-1", "missing"), [])

    def test_a_time_with_nanoseconds(self):
        self.assertAlmostEqual(measure_take.parse_time("2026-10-30T08:46:51.123456789Z") % 60, 51.123456, places=5)


if __name__ == "__main__":
    unittest.main()


class JevChairsTest(unittest.TestCase):
    """The same 36 chairs as Jev bots: a fixed program on the node, no model."""

    def setUp(self):
        self.config, self.template = chairs.load("jev")

    def test_the_template_is_a_jev_program_over_the_scores_tools(self):
        runtime = self.template["runtime"]
        self.assertEqual(runtime["kind"], "jev")
        program = runtime["jev"]
        self.assertEqual((program["server"], program["start"], program["wait"], program["stop"]), ("score", "phrases", "listen", "stop"))
        self.assertEqual(program["act"], {"tool": "play", "choice": "phrase", "verdict": "judge", "pass": ["dynamics", "character"]})
        self.assertEqual(program["tell"], {"name": "info.chair.played", "fields": ["phrase", "probability"]})
        self.assertIn({"topic": "info.chair.*", "mode": "message"}, self.template["subscriptions"])
        self.assertEqual(sorted(self.template["allowedTools"]), ["listen", "phrases", "play", "stop"])
        self.assertNotIn("axlr_judge", self.template["allowedTools"])
        self.assertEqual(self.template["model"]["model"], "")
        self.assertEqual(self.template["level"], "act-within-policy")

    def test_a_jev_chair_is_placed_and_cued_like_an_axlr_one(self):
        _, axlr = chairs.load("axlr")
        section = next(s for s in self.config["sections"] if s["id"] == "ob")
        jev, model = chairs.definition(self.template, section, 1), chairs.definition(axlr, section, 1)
        for key in ("id", "name", "placement", "groups", "events", "tenant", "owner"):
            self.assertEqual(jev[key], model[key], key)
        self.assertEqual(jev["subscriptions"][:4], model["subscriptions"])  # the cues and the conductor's expression; the Jev chair also hears its teammates
        for placeholder in ("CHAIR", "SECTION", "TOTAL", "FAMILY", "chair N of", "SECTIONKEY"):
            self.assertNotIn(placeholder, str(jev), placeholder)
        self.assertTrue(jev["mission"].startswith("Oboes, chair 1 of 2: a Jev bot."))

    def test_an_update_from_either_template_sets_the_runtime(self):
        _, axlr = chairs.load("axlr")
        section = self.config["sections"][0]
        self.assertEqual(chairs.patch_of(chairs.definition(axlr, section, 1))["runtime"], {"kind": "axlr"})
        self.assertEqual(chairs.patch_of(chairs.definition(self.template, section, 1))["runtime"]["kind"], "jev")
        self.assertNotIn("id", chairs.patch_of(chairs.definition(axlr, section, 1)))

    def test_an_unknown_runtime_is_refused(self):
        with self.assertRaises(SystemExit):
            chairs.load("haiku")
