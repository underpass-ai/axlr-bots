"""The team exporter with no control plane: timeline records shaped as the control plane writes them (31 Oct 2026), folded into metrics."""

import datetime
import threading
import unittest
import urllib.request
from http.server import ThreadingHTTPServer

import team_exporter
from test_metrics import assert_exposition



def at(seconds):
    """A createdAt seconds after 20:43:37, with nanoseconds as the control plane writes them."""
    t = datetime.datetime(2026, 10, 31, 20, 43, 37, tzinfo=datetime.timezone.utc) + datetime.timedelta(seconds=int(seconds))
    return t.strftime("%Y-%m-%dT%H:%M:%S") + f".{round((seconds % 1) * 1e9):09d}Z"


def heard(bot, event_id, mode, t):
    return {"eventId": f"evt-h-{bot}-{t}", "type": "bot.event.heard", "botId": bot, "taskId": f"task-{bot}", "createdAt": at(t),
            "payload": {"by": "operator", "eventId": event_id, "from": "operator", "mode": mode, "name": "info.cue.all", "subscription": "info.cue.all"}}


def published(event_id, t, deliveries, skipped=()):
    return {"eventId": f"evt-p-{event_id}", "type": "group.event.published", "groupName": "orchestra", "createdAt": at(t),
            "payload": {"by": "operator", "data": {"dynamics": "f"}, "deliveries": deliveries, "eventId": event_id, "from": "operator",
                        "message": "Everyone, play loud.", "name": "info.cue.all", "skipped": list(skipped), "to": []}}


def task(kind, bot, t, **payload):
    return {"eventId": f"evt-{kind}-{bot}-{t}", "type": kind, "botId": bot, "taskId": f"task-{bot}", "createdAt": at(t), "payload": payload}


EXECUTION = {"id": "exec-vn1-3", "botId": "vn1-3", "startedAt": at(1.0), "endedAt": at(41.0), "outcome": "Failed",
             "usage": {"requests": 43, "promptTokens": 28573, "toolCalls": 128, "costUsd": 0.0012}}


class FoldTest(unittest.TestCase):
    def setUp(self):
        self.asked = []
        self.view = team_exporter.Team("orchestra", execution=lambda x: self.asked.append(x) or dict(EXECUTION, id=x))

    def fold(self, *records):
        for r in records:
            self.view.record(r)

    def test_an_event_its_deliveries_and_its_fanout(self):
        self.fold(heard("vn1-1", "evt-op", "task", 0.0), heard("vn1-2", "evt-op", "task", 0.05),
                  published("evt-op", 0.12, [{"botId": "vn1-1", "mode": "task", "created": True},
                                             {"botId": "vn1-2", "mode": "task", "created": True},
                                             {"botId": "vn2-1", "mode": "message", "created": False}], skipped=["cb-1"]))
        v = self.view
        self.assertEqual(v.published.get("orchestra", "info.cue.all", "operator"), 1)
        self.assertEqual(v.deliveries.get("orchestra", "info.cue.all", "task", "yes"), 2)
        self.assertEqual(v.deliveries.get("orchestra", "info.cue.all", "message", "no"), 1)
        self.assertEqual(v.skipped.get("orchestra", "info.cue.all"), 1)
        self.assertEqual(v.heard.get("orchestra", "vn1-1", "task"), 1)
        fanout = v.fanout.labels("orchestra")
        self.assertEqual(fanout.count, 1)
        self.assertAlmostEqual(fanout.sum, 0.12, places=6)  # from the first delivery, on the control plane's clock
        self.assertEqual(v.records.get("orchestra", "bot.event.heard"), 2)

    def test_a_task_submitted_started_and_failed_on_its_budget(self):
        self.fold(task("bot.task.submitted", "vn1-3", 0.0), task("bot.task.started", "vn1-3", 0.6, nodeId="stage-vn1-3"))
        text = self.view.registry.render().decode()
        self.assertIn('axlr_team_tasks_open{team="orchestra",state="running"} 1\n', text)
        self.fold(task("bot.task.message", "vn1-3", 5.0, event="info.cue.vn1"),
                  task("bot.task.failed", "vn1-3", 40.6, outcome="Failed", reason="budget_exceeded: 128 tool calls"),
                  task("bot.task.failed", "vn1-4", 41.0, outcome="Failed", reason="budget_exceeded: 129 tool calls"))
        v = self.view
        self.assertEqual(v.task_events.get("orchestra", "vn1-3", "submitted"), 1)
        self.assertEqual(v.task_events.get("orchestra", "vn1-3", "failed"), 1)
        # 128 and 129 are one reason
        self.assertEqual(v.failures.get("orchestra", "vn1-3", "budget_exceeded: N tool calls"), 1)
        self.assertEqual(v.failures.get("orchestra", "vn1-4", "budget_exceeded: N tool calls"), 1)
        self.assertEqual(v.messages.get("orchestra", "vn1-3"), 1)
        start = v.start.labels("orchestra")
        self.assertAlmostEqual(start.sum, 0.6, places=6)
        self.assertAlmostEqual(v.duration.labels("orchestra", "failed").sum, 40.0, places=6)
        self.assertIn('axlr_team_tasks_open{team="orchestra",state="running"} 0\n', v.registry.render().decode())

    def test_an_execution_reported_is_read_for_its_usage_and_budget(self):
        self.view.members_from({"members": [{"botId": "vn1-3", "status": "Enabled"}, {"botId": "vn1-4", "status": "Paused"}]},
                               [{"id": "vn1-3", "budgets": {"maxToolCallsPerTask": 128, "maxModelCallsPerTask": 64, "taskTimeoutSeconds": 600},
                                 "updatedAt": "2026-10-31T20:00:00Z"},
                                {"id": "someone-else", "budgets": {"maxToolCallsPerTask": 1}}])
        self.fold({"eventId": "evt-r", "type": "execution.reported", "botId": "vn1-3", "createdAt": at(41.0),
                   "payload": {"executionId": "exec-vn1-3", "nodeId": "stage-vn1-3", "outcome": "Failed"}})
        v = self.view
        self.assertEqual(self.asked, ["exec-vn1-3"])
        self.assertEqual(v.executions.get("orchestra", "vn1-3", "Failed"), 1)
        self.assertAlmostEqual(v.cost.get("orchestra", "vn1-3"), 0.0012)
        self.assertEqual(v.tokens.get("orchestra", "vn1-3"), 28573)
        self.assertEqual(v.used.get("orchestra", "vn1-3", "tool_calls"), 1.0)
        self.assertAlmostEqual(v.used.get("orchestra", "vn1-3", "model_calls"), 43 / 64)
        self.assertAlmostEqual(v.used.get("orchestra", "vn1-3", "seconds"), 40 / 600)
        text = v.registry.render().decode()
        assert_exposition(self, text)
        for line in ('axlr_team_bots{team="orchestra",status="Enabled"} 1', 'axlr_team_bot_status{team="orchestra",bot="vn1-4",status="Paused"} 1',
                     'axlr_team_bot_budget{team="orchestra",bot="vn1-3",budget="tool_calls"} 128'):
            self.assertIn(line + "\n", text)
        self.assertNotIn("someone-else", text)

    def test_an_execution_from_before_the_bots_last_update_is_not_measured_against_its_budget_now(self):
        self.view.members_from({"members": [{"botId": "vn1-3", "status": "Enabled"}]},
                               [{"id": "vn1-3", "budgets": {"maxToolCallsPerTask": 400}, "updatedAt": "2026-10-31T20:53:38Z"}])
        self.fold({"eventId": "evt-r", "type": "execution.reported", "botId": "vn1-3", "createdAt": at(41.0), "payload": {"executionId": "exec-vn1-3"}})
        self.assertEqual(self.view.executions.get("orchestra", "vn1-3", "Failed"), 1)
        self.assertNotIn("axlr_team_budget_used_ratio{", self.view.registry.render().decode())

    def test_an_execution_that_cannot_be_read_is_an_error_not_a_stop(self):
        def broken(x):
            raise OSError("refused")

        v = team_exporter.Team("orchestra", execution=broken)
        v.record({"eventId": "e", "type": "execution.reported", "botId": "a", "createdAt": at(1), "payload": {"executionId": "x"}})
        self.assertEqual(v.errors.get("orchestra", "execution"), 1)


class Client:
    """The operator API as a script: each path answers the next of its answers."""

    def __init__(self, answers, stop):
        self.answers, self.stop, self.asked = answers, stop, []

    def get(self, path, query=None):
        self.asked.append((path, dict(query or {})))
        queue = self.answers.get(path) or []
        if not queue:
            self.stop.set()
            return []
        return queue.pop(0)


class FollowTest(unittest.TestCase):
    def test_the_backfill_is_folded_once_then_the_timeline_after_it(self):
        stop = threading.Event()
        view = team_exporter.Team("orchestra")
        path = "/v1/groups/orchestra/events"
        client = Client({path: [[task("bot.task.submitted", "a", 0.0), task("bot.task.started", "a", 0.1)],
                                [task("bot.task.completed", "a", 5.0)]]}, stop)
        team_exporter.follow(view, client, backfill=1000, stop=stop, pause=0)
        self.assertEqual(client.asked[0], (path, {"limit": "1000"}))
        self.assertEqual(client.asked[1], (path, {"after": "evt-bot.task.started-a-0.1", "wait": "25s", "limit": "1000"}))
        self.assertEqual(view.task_events.get("orchestra", "a", "completed"), 1)
        self.assertEqual(view.records.get("orchestra", "bot.task.submitted"), 1)

    def test_without_backfill_it_starts_from_now(self):
        stop = threading.Event()
        view = team_exporter.Team("orchestra")
        path = "/v1/groups/orchestra/events"
        client = Client({path: [[task("bot.task.failed", "a", 0.0, reason="old")]]}, stop)
        team_exporter.follow(view, client, backfill=0, stop=stop, pause=0)
        self.assertEqual(client.asked[0], (path, {"limit": "1"}))
        self.assertEqual(view.records.get("orchestra", "bot.task.failed"), 0)


class EndpointTest(unittest.TestCase):
    def test_metrics_and_health(self):
        view = team_exporter.Team("orchestra")
        view.record(task("bot.task.submitted", "a", 0.0))
        httpd = ThreadingHTTPServer(("127.0.0.1", 0), team_exporter.handler(view.registry))
        threading.Thread(target=httpd.serve_forever, daemon=True).start()
        try:
            base = f"http://127.0.0.1:{httpd.server_address[1]}"
            with urllib.request.urlopen(base + "/metrics", timeout=5) as r:
                body = r.read().decode()
                self.assertEqual(r.headers.get("Content-Type"), team_exporter.metrics.CONTENT_TYPE)
            assert_exposition(self, body)
            self.assertIn('axlr_team_tasks_total{team="orchestra",bot="a",state="submitted"} 1\n', body)
            with urllib.request.urlopen(base + "/healthz", timeout=5) as r:
                self.assertEqual(r.read(), b"ok\n")
        finally:
            httpd.shutdown()
            httpd.server_close()


if __name__ == "__main__":
    unittest.main()
