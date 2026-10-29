"""The measurement's arithmetic, with no control plane and no bot."""

import unittest

import measure_stop as m


def ev(kind, at):
    return {"type": kind, "createdAt": at}


class MeasureStopTest(unittest.TestCase):
    def test_parse_time_nanoseconds_utc(self):
        self.assertAlmostEqual(m.parse_time("2026-10-29T15:06:54.809080228Z") - m.parse_time("2026-10-29T15:06:54Z"), 0.809080, places=5)

    def test_parse_time_keeps_the_zone(self):
        self.assertEqual(m.parse_time("2026-10-29T16:00:00+01:00"), m.parse_time("2026-10-29T15:00:00Z"))

    def test_row_counts_each_clock_from_the_publish(self):
        published = m.parse_time("2026-10-29T15:00:00Z")
        hold = [ev("bot.task.started", "2026-10-29T14:59:50Z"), ev("bot.task.preempted", "2026-10-29T15:00:00.01Z")]
        urgent = [ev("bot.task.started", "2026-10-29T15:00:05.5Z"), ev("bot.task.completed", "2026-10-29T15:00:07Z")]
        r = m.row("b", published, hold, published + 5.4, urgent)
        self.assertEqual(r, {"bot": "b", "cancelled": 0.01, "runGone": 5.4, "urgentStarted": 5.5, "answered": 7.0})

    def test_a_clock_that_never_came_is_a_dash(self):
        r = m.row("b", 0.0, [], None, [])
        self.assertIsNone(r["cancelled"])
        self.assertIn("| b | — | — | — | — |", m.table([r]))


if __name__ == "__main__":
    unittest.main()
