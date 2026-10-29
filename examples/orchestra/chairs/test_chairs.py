"""The 36 definitions, with no control plane."""

import unittest

import chairs


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
        self.assertEqual([s["topic"] for s in bot["subscriptions"]], ["info.cue.winds", "info.cue.all"])
        self.assertTrue(all(s["mode"] == "message" and s["from"] == ["operator"] for s in bot["subscriptions"]))
        self.assertTrue(bot["mission"].startswith("Horns, chair 2 of 2. "))
        self.assertEqual(bot["groups"], ["orchestra"])
        self.assertEqual(bot["level"], "act-within-policy")

    def test_no_placeholder_is_left(self):
        for chair, section, n in chairs.chairs(self.config):
            text = str(chairs.definition(self.template, section, n))
            for placeholder in ("CHAIR", "SECTION", "TOTAL", "FAMILY", "chair N of"):
                self.assertNotIn(placeholder, text, chair)

    def test_every_tool_is_the_score_services(self):
        self.assertEqual(sorted(self.template["allowedTools"]), ["dynamics", "enter", "listen", "stop"])

    def test_an_unknown_chair_is_refused(self):
        with self.assertRaises(SystemExit):
            chairs.selected(self.config, ["tuba-1"])


if __name__ == "__main__":
    unittest.main()
