#!/usr/bin/env python3
"""A Prometheus exporter for one team of bots, from the control plane's own records.

The control plane and the nodes have no /metrics of their own. This reads
what the operator API already answers, read-only, with the operator token:

  GET /v1/groups/<team>/events   the team's timeline, long-polled: the
                                 operator's events with their deliveries, and
                                 every member's task and execution events
  GET /v1/groups/<team>          the members and their status
  GET /v1/bots                   their budgets
  GET /v1/executions/<id>        on each execution.reported: what it used
                                 (tool calls, model or Jev calls, tokens, cost)

and exposes on --listen (loopback by default):

  axlr_team_records_total{type}               every timeline record
  axlr_team_events_published_total{name,from} the events published, by whom
  axlr_team_deliveries_total{name,mode,created}  each published event's deliveries
  axlr_team_fanout_seconds                    from an event's first delivery to its
                                              published record (the control plane's clock)
  axlr_team_heard_total{bot,mode}             each member that heard an event
  axlr_team_tasks_total{bot,state}            submitted, started, completed, failed, …
  axlr_team_task_failures_total{bot,reason}   why (numbers as N: budget_exceeded: N tool calls)
  axlr_team_task_messages_total{bot}          messages delivered to running tasks
  axlr_team_task_start_seconds                submitted → started
  axlr_team_task_seconds{outcome}             started → ended
  axlr_team_executions_total{bot,outcome}, axlr_team_execution_tool_calls,
  axlr_team_execution_model_calls, axlr_team_cost_usd_total{bot},
  axlr_team_prompt_tokens_total{bot}, axlr_team_budget_used_ratio{bot,budget} (the last
  execution's use of its budget; only for one started after the bot's last update, as
  an execution carries no budget of its own)
  axlr_team_bot_status{bot,status}, axlr_team_bots{status}, axlr_team_bot_budget{bot,budget},
  axlr_team_tasks_open{state}

every one with team as its first label. Nothing here knows the orchestra:
any team works. On start it reads the timeline's newest --backfill records
once (the control plane keeps 1,000 a team), so the totals cover the recent
runs; rate() and increase() count from then on.

Run:  python3 team_exporter.py --team orchestra [--listen 127.0.0.1:8797]
Test: python3 -m unittest -v test_team_exporter
"""

import argparse
import collections
import datetime
import json
import logging
import os
import ssl
import threading
import time
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import metrics

log = logging.getLogger("team-exporter")
ENDED = {"bot.task.completed": "completed", "bot.task.failed": "failed", "bot.task.cancelled": "cancelled",
         "bot.task.expired": "expired", "bot.task.preempted": "preempted"}
STATES = dict(ENDED, **{"bot.task.submitted": "submitted", "bot.task.started": "started", "bot.task.waiting_approval": "waiting_approval",
                        "bot.task.decided": "decided", "bot.task.delegated": "delegated"})
BUDGETS = {"tool_calls": "maxToolCallsPerTask", "model_calls": "maxModelCallsPerTask", "seconds": "taskTimeoutSeconds"}
QUICK = (0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 300)
LONG = (1, 5, 10, 30, 60, 120, 300, 600, 900, 1200, 1800, 3600)
CALLS = (8, 16, 32, 64, 96, 128, 160, 192, 256, 400, 512)


def parse_time(text):
    """RFC 3339 with nanoseconds, as the control plane writes it → seconds since the epoch."""
    text = str(text).replace("Z", "+00:00")
    head, _, tail = text.partition(".")
    if tail:
        digits = tail[:len(tail) - len(tail.lstrip("0123456789"))]
        text = f"{head}.{digits[:6]}{tail[len(digits):]}"
    return datetime.datetime.fromisoformat(text).timestamp()


class Team:
    """The team's records folded into metrics; execution(id) answers an execution's record, or None."""

    KEEP = 5000  # tasks and events followed at once

    def __init__(self, team, registry=None, execution=None):
        self.team, self.execution = team, execution
        self.registry = registry or metrics.Registry()
        r = self.registry
        self.lock = threading.Lock()
        self.tasks = collections.OrderedDict()  # task id → {"bot", "state", "submitted", "started"}
        self.first = collections.OrderedDict()  # operator event id → its first delivery's createdAt
        self.members, self.budgets, self.updated = {}, {}, {}  # bot → status; bot → budgets; bot → its updatedAt
        t = ("team",)
        self.records = r.counter("axlr_team_records_total", "Records on the team's timeline, per type.", t + ("type",))
        self.published = r.counter("axlr_team_events_published_total", "Events published on the team, per name and publisher.", t + ("name", "from"))
        self.deliveries = r.counter("axlr_team_deliveries_total", "Deliveries of the team's events to its members, per event, mode and whether a task was created.",
                                    t + ("name", "mode", "created"))
        self.skipped = r.counter("axlr_team_deliveries_skipped_total", "Members an event skipped (not subscribed, paused, …), per event.", t + ("name",))
        self.fanout = r.histogram("axlr_team_fanout_seconds", "From an event's first delivery to its published record, on the control plane's clock.", t, QUICK)
        self.heard = r.counter("axlr_team_heard_total", "Events a member heard, per bot and mode (task, message, note).", t + ("bot", "mode"))
        self.task_events = r.counter("axlr_team_tasks_total", "Task events per bot and state (submitted, started, completed, failed, …).", t + ("bot", "state"))
        self.failures = r.counter("axlr_team_task_failures_total", "Failed tasks per bot and reason (numbers as N).", t + ("bot", "reason"))
        self.messages = r.counter("axlr_team_task_messages_total", "Messages delivered to a member's running task.", t + ("bot",))
        self.start = r.histogram("axlr_team_task_start_seconds", "From a task submitted to its execution started.", t, QUICK)
        self.duration = r.histogram("axlr_team_task_seconds", "From a task started to its end, per outcome.", t + ("outcome",), LONG)
        self.executions = r.counter("axlr_team_executions_total", "Executions reported, per bot and outcome.", t + ("bot", "outcome"))
        self.tool_calls = r.histogram("axlr_team_execution_tool_calls", "Tool calls one execution made.", t, CALLS)
        self.model_calls = r.histogram("axlr_team_execution_model_calls", "Model calls one execution made (a Jev bot: its questions to Jev).", t, CALLS)
        self.cost = r.counter("axlr_team_cost_usd_total", "What the executions cost, as the control plane recorded it.", t + ("bot",))
        self.tokens = r.counter("axlr_team_prompt_tokens_total", "Prompt (input) tokens the executions used.", t + ("bot",))
        self.used = r.gauge("axlr_team_budget_used_ratio", "The share of its budget a bot's last execution used: tool_calls, model_calls, seconds.",
                            t + ("bot", "budget"))
        self.errors = r.counter("axlr_team_exporter_errors_total", "The exporter's errors reading the control plane, per what it read.", t + ("what",))
        self.last = r.gauge("axlr_team_exporter_last_record_timestamp_seconds", "createdAt of the newest record folded.", t)
        r.computed("axlr_team_bot_status", "Each member's status: 1 for the one it is in.", t + ("bot", "status"),
                   lambda: [((self.team, b, s), 1) for b, s in self._members().items()])
        r.computed("axlr_team_bots", "Members per status.", t + ("status",),
                   lambda: [((self.team, s), n) for s, n in collections.Counter(self._members().values()).items()])
        r.computed("axlr_team_bot_budget", "Each member's budget per task: tool_calls, model_calls, seconds.", t + ("bot", "budget"), self._budget_samples)
        r.computed("axlr_team_tasks_open", "Tasks seen submitted (pending) or started (running) and not ended yet.", t + ("state",), self._open)
        metrics.process_metrics(r)

    # --- the records ----------------------------------------------------------

    def record(self, e):
        """Fold one timeline record."""
        kind, payload, bot = e.get("type") or "none", e.get("payload") or {}, e.get("botId") or ""
        created = parse_time(e["createdAt"]) if e.get("createdAt") else None
        self.records.labels(self.team, kind).inc()
        if created is not None:
            self.last.labels(self.team).set(max(created, self.last.labels(self.team).get()))
        if kind == "group.event.published":
            name = payload.get("name") or "none"
            self.published.labels(self.team, name, payload.get("from") or payload.get("by") or "none").inc()
            for d in payload.get("deliveries") or ():
                self.deliveries.labels(self.team, name, d.get("mode") or "none", "yes" if d.get("created") else "no").inc()
            if payload.get("skipped"):
                self.skipped.labels(self.team, name).inc(len(payload["skipped"]))
            with self.lock:
                first = self.first.pop(payload.get("eventId"), None)
            if first is not None and created is not None:
                self.fanout.labels(self.team).observe(max(0.0, created - first))
        elif kind == "bot.event.heard":
            self.heard.labels(self.team, bot, payload.get("mode") or "none").inc()
            if payload.get("eventId") and created is not None:
                with self.lock:
                    self.first.setdefault(payload["eventId"], created)
                    self._trim(self.first)
        elif kind == "bot.task.message":
            self.messages.labels(self.team, bot).inc()
        elif kind in STATES:
            self._task(kind, e, bot, payload, created)
        elif kind == "execution.reported":
            self._execution(payload.get("executionId") or e.get("executionId"), bot)

    def _task(self, kind, e, bot, payload, created):
        state = STATES[kind]
        self.task_events.labels(self.team, bot, state).inc()
        if kind == "bot.task.failed":
            self.failures.labels(self.team, bot, metrics.reason(payload.get("reason"))).inc()
        task = e.get("taskId")
        if not task or created is None:
            return
        with self.lock:
            t = self.tasks.setdefault(task, {"bot": bot, "state": "pending", "submitted": None, "started": None})
            if kind == "bot.task.submitted":
                t["submitted"] = created
            elif kind == "bot.task.started":
                t["state"], t["started"] = "running", created
                if t["submitted"] is not None:
                    self.start.labels(self.team).observe(max(0.0, created - t["submitted"]))
            elif kind in ENDED:
                self.tasks.pop(task, None)
                if t["started"] is not None:
                    self.duration.labels(self.team, state).observe(max(0.0, created - t["started"]))
            self._trim(self.tasks)

    def _execution(self, execution_id, bot):
        if not execution_id or self.execution is None:
            return
        try:
            x = self.execution(execution_id)
        except Exception:  # noqa: BLE001  one execution unread never stops the timeline
            self.errors.labels(self.team, "execution").inc()
            return
        if not isinstance(x, dict):
            return
        bot = x.get("botId") or bot
        usage = x.get("usage") or {}
        tool_calls, model_calls = usage.get("toolCalls") or 0, usage.get("requests") or 0
        self.executions.labels(self.team, bot, x.get("outcome") or "none").inc()
        self.tool_calls.labels(self.team).observe(tool_calls)
        self.model_calls.labels(self.team).observe(model_calls)
        self.cost.labels(self.team, bot).inc(max(0.0, float(usage.get("costUsd") or 0)))
        self.tokens.labels(self.team, bot).inc(max(0, int(usage.get("promptTokens") or 0)))
        started = parse_time(x["startedAt"]) if x.get("startedAt") else None
        seconds = parse_time(x["endedAt"]) - started if started is not None and x.get("endedAt") else None
        with self.lock:
            budgets, updated = self.budgets.get(bot) or {}, self.updated.get(bot)
        # The execution carries no budget: it ran with the bot's budgets as they were at its assignment.
        # Those are the bot's budgets now only if it started after the bot's last update.
        if started is None or updated is None or started < updated:
            return
        for name, used in (("tool_calls", tool_calls), ("model_calls", model_calls), ("seconds", seconds)):
            limit = budgets.get(BUDGETS[name])
            if used is not None and isinstance(limit, (int, float)) and limit > 0:
                self.used.labels(self.team, bot, name).set(used / limit)

    def _trim(self, d):
        while len(d) > self.KEEP:
            d.popitem(last=False)

    # --- the members ------------------------------------------------------------

    def members_from(self, group, bots):
        """GET /v1/groups/<team> and GET /v1/bots: each member's status and budgets."""
        members = {m["botId"]: m.get("status") or "none" for m in (group or {}).get("members") or () if m.get("botId")}
        mine = [b for b in bots or () if b.get("id") in members]
        budgets = {b["id"]: b.get("budgets") or {} for b in mine}
        updated = {b["id"]: parse_time(b["updatedAt"]) for b in mine if b.get("updatedAt")}
        with self.lock:
            self.members, self.budgets, self.updated = members, budgets, updated

    def _members(self):
        with self.lock:
            return dict(self.members)

    def _budget_samples(self):
        with self.lock:
            budgets = dict(self.budgets)
        return [((self.team, bot, name), b[key]) for bot, b in budgets.items() for name, key in BUDGETS.items()
                if isinstance(b.get(key), (int, float))]

    def _open(self):
        with self.lock:
            counts = collections.Counter(t["state"] for t in self.tasks.values())
        return [((self.team, s), counts.get(s, 0)) for s in ("pending", "running")]


class Client:
    """The operator API, read-only; the token is read from its file on each call and never logged."""

    def __init__(self, url, token_file, ca_file, timeout=40):
        self.url, self.timeout = url.rstrip("/"), timeout
        self.token_file = os.path.expanduser(token_file)
        self.context = ssl.create_default_context(cafile=os.path.expanduser(ca_file)) if url.startswith("https:") else None

    def get(self, path, query=None):
        url = self.url + path + ("?" + urllib.parse.urlencode(query) if query else "")
        request = urllib.request.Request(url)
        with open(self.token_file) as f:
            request.add_header("Authorization", "Bearer " + f.read().strip())
        with urllib.request.urlopen(request, context=self.context, timeout=self.timeout) as response:
            return json.load(response)


def follow(view, client, backfill=1000, wait="25s", stop=None, pause=2.0):
    """Fold the team's timeline from now on (and its newest backfill records once)."""
    path, after = f"/v1/groups/{urllib.parse.quote(view.team)}/events", None
    while not (stop and stop.is_set()):
        try:
            if after is None:
                newest = client.get(path, {"limit": str(max(1, min(int(backfill), 1000)))})
                if backfill:
                    for e in newest:
                        view.record(e)
                if not newest:
                    time.sleep(pause)
                    continue
                after = newest[-1]["eventId"]
                log.info("following team %s after %s (%d records read)", view.team, after, len(newest) if backfill else 0)
                continue
            for e in client.get(path, {"after": after, "wait": wait, "limit": "1000"}):
                after = e["eventId"]
                view.record(e)
        except Exception as exc:  # noqa: BLE001  the control plane going away never stops the exporter
            view.errors.labels(view.team, "events").inc()
            log.warning("%s; again in %g s", exc, pause)
            time.sleep(pause)


def refresh(view, client, every=30.0, stop=None):
    while not (stop and stop.is_set()):
        try:
            view.members_from(client.get(f"/v1/groups/{urllib.parse.quote(view.team)}"), client.get("/v1/bots"))
        except Exception as exc:  # noqa: BLE001
            view.errors.labels(view.team, "members").inc()
            log.warning("members: %s", exc)
        if stop:
            stop.wait(every)
        else:
            time.sleep(every)


def handler(registry):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, fmt, *args):
            pass

        def do_GET(self):
            path = self.path.split("?")[0]
            if path == "/metrics":
                body, ctype, status = registry.render(), metrics.CONTENT_TYPE, 200
            elif path == "/healthz":
                body, ctype, status = b"ok\n", "text/plain", 200
            else:
                body, ctype, status = b"not found\n", "text/plain", 404
            self.send_response(status)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    return Handler


def main(argv=None):
    p = argparse.ArgumentParser(description="Prometheus metrics for a team of bots, from the control plane's records")
    p.add_argument("--control-plane", default="https://127.0.0.1:7879", help="the operator API")
    p.add_argument("--team", default="orchestra")
    p.add_argument("--token-file", default="~/.local/state/axlr-bots/api.token", help="the operator token (read-only use)")
    p.add_argument("--ca-file", default="~/.local/state/axlr-bots/ca/ca.crt", help="the control plane's CA")
    p.add_argument("--listen", default="127.0.0.1:8797", help="host:port for /metrics")
    p.add_argument("--backfill", type=int, default=1000, help="records of the timeline to read once on start (0: from now on)")
    p.add_argument("--members-every", type=float, default=30.0, help="seconds between reads of the members and their budgets")
    a = p.parse_args(argv)
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s: %(message)s")
    client = Client(a.control_plane, a.token_file, a.ca_file)
    view = Team(a.team, execution=lambda x: client.get(f"/v1/executions/{urllib.parse.quote(x)}"))
    try:  # the budgets before the backfill, so its executions are measured against them
        view.members_from(client.get(f"/v1/groups/{urllib.parse.quote(a.team)}"), client.get("/v1/bots"))
    except Exception as exc:  # noqa: BLE001
        log.warning("members: %s", exc)
    threading.Thread(target=refresh, args=(view, client, a.members_every), daemon=True).start()
    threading.Thread(target=follow, args=(view, client, a.backfill), daemon=True).start()
    host, port = a.listen.rsplit(":", 1)
    server = ThreadingHTTPServer((host, int(port)), handler(view.registry))
    server.daemon_threads = True
    log.info("team %s from %s: metrics on http://%s:%s/metrics", a.team, a.control_plane, host, port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
