"""The three tests every world adapter passes: python3 -m unittest -v test_adapter (no network, no model)."""

import io
import unittest

import adapter


class Clock:
    """A clock the tests move by hand."""

    def __init__(self):
        self.t = 0.0

    def __call__(self):
        return self.t


class FrameTest(unittest.TestCase):
    def test_a_frame_becomes_the_expected_text(self):
        world = adapter.World(things=2, seed=1)
        world.place("thing-0", 0.25, 0.75)
        world.place("thing-1", 0.30, 0.75)
        a = adapter.Adapter(world, "anthill", per_minute=6, now=Clock())
        self.assertEqual(
            a.observe("thing-0"),
            "thing-0 at 0.25, 0.75, inside the fence; the mark at 0.50, 0.50 is 12 steps north-east; "
            "nearest: thing-1 at 0.30, 0.75, 2 steps east. Reading 0 s old; exact, simulated.")

    def test_a_bot_outside_the_world_is_refused(self):
        a = adapter.Adapter(adapter.World(things=1), "anthill")
        with self.assertRaises(LookupError):
            a.observe("thing-9")


class ChangeTest(unittest.TestCase):
    def test_a_change_becomes_the_expected_event(self):
        world = adapter.World(things=1, seed=1, drift=False)  # a still world: only the hand moves things
        world.place("thing-0", 0.50, 0.50)
        a = adapter.Adapter(world, "anthill", now=Clock())
        a.observe("thing-0")                                 # the frame before the change
        world.place("thing-0", 0.95, 0.50)
        events = list(a.watch(ticks=1))
        self.assertEqual(events, [adapter.Event(
            "warn.fence", "thing-0 crossed the fence at 0.95, 0.50; come back inside",
            {"thing": "thing-0", "x": 0.95, "y": 0.5, "tick": 1}, ["thing-0"])])
        self.assertEqual(list(a.watch(ticks=1)), [])          # still outside: no second event

    def test_the_event_is_the_route_body_with_a_key_from_it(self):
        event = adapter.Event("info.reached", "thing-0 reached the mark at 0.50, 0.50", {"thing": "thing-0", "tick": 3})
        sent = []
        publisher = adapter.Publisher("anthill", "https://cp.example:7879", token_file="/dev/null",
                                      post=lambda url, body, headers: sent.append((url, body, headers)) or {"id": "x"})
        publisher.publish(event)
        url, body, headers = sent[0]
        self.assertEqual(url, "https://cp.example:7879/v1/groups/anthill/events")
        self.assertEqual(body, {"name": "info.reached", "message": "thing-0 reached the mark at 0.50, 0.50",
                                "data": {"thing": "thing-0", "tick": 3}})
        self.assertEqual(headers["Idempotency-Key"], adapter.key_of(body))
        self.assertEqual(headers["Authorization"], "Bearer ")
        out = io.StringIO()
        self.assertEqual(adapter.Publisher("anthill", out=out).publish(event)["dryRun"], True)
        self.assertIn("would publish evt-", out.getvalue())


class FloodTest(unittest.TestCase):
    def test_a_flood_of_calls_holds_the_rate(self):
        clock = Clock()
        world = adapter.World(things=300, seed=1)
        a = adapter.Adapter(world, "anthill", per_minute=6, now=clock)
        answers = []
        for i in range(300):
            clock.t = i * 0.1                                 # 300 looks in 30 s
            answers.append(a.observe(f"thing-{i}"))
        self.assertEqual(world.reads, 6)                      # the world was read six times, not 300
        self.assertEqual(len(answers), 300)
        self.assertTrue(all(text.endswith("exact, simulated.") for text in answers))
        self.assertIn("Reading 29 s old", answers[-1])        # a stale answer says its age
        clock.t = 61.0
        a.observe("thing-0")
        self.assertEqual(world.reads, 7)                      # a minute later a reading is allowed again


if __name__ == "__main__":
    unittest.main()
