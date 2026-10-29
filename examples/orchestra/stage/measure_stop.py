#!/usr/bin/env python3
"""Measure what the operator's stop.all does to bots in the middle of a long task.

Each bot is given a task that holds (a tool call that blocks for minutes);
once every one is Running and its tool has started, the operator publishes
stop.all on the team, and the script watches, every 100 ms, four clocks per bot:

  cancelled   the long task ends Cancelled (preempted by the urgent task)
  run gone    the long task's process is gone from this host (the node
              stops the run at its next lease renewal)
  urgent      the urgent task stop.all made starts
  answered    that urgent task ends (the bot read the stop and answered)

All four are counted from the publish, in seconds. The control plane's own
timestamps are used for the first, third and fourth; the second is this
host's process table, since a cancelled run reports nothing.

Run:  python3 measure_stop.py --team stage --bot stage-probe-1 --bot stage-probe-2 \
          --objective "Hold: call the sleep once as your mission says." --process "^/usr/bin/sleep 120$"
Test: python3 -m unittest -v test_measure_stop
"""

import argparse
import datetime
import json
import subprocess
import sys
import time
import uuid

CLI = "axlr-bots"


def parse_time(text):
    """An RFC 3339 time with up to nanoseconds, as the control plane writes it."""
    text = text.replace("Z", "+00:00")
    head, _, tail = text.partition(".")
    if tail:
        digits = tail[:len(tail) - len(tail.lstrip("0123456789"))]
        zone = tail[len(digits):]
        text = f"{head}.{digits[:6]}{zone}"
    return datetime.datetime.fromisoformat(text).timestamp()


def first(events, kind):
    """The time of the first event of a kind in a task's timeline, or None."""
    for e in events:
        if e.get("type") == kind:
            return parse_time(e["createdAt"])
    return None


def row(bot, published, hold_events, gone_at, urgent_events):
    """One bot's line: each clock in seconds after the publish, None when it never came."""
    def since(t):
        return None if t is None else round(t - published, 2)
    ended = None
    for kind in ("bot.task.completed", "bot.task.failed", "bot.task.cancelled"):
        ended = ended or first(urgent_events, kind)
    return {
        "bot": bot,
        "cancelled": since(first(hold_events, "bot.task.preempted") or first(hold_events, "bot.task.cancelled")),
        "runGone": since(gone_at),
        "urgentStarted": since(first(urgent_events, "bot.task.started")),
        "answered": since(ended),
    }


def table(rows):
    """The rows as a Markdown table."""
    cols = ["bot", "cancelled", "runGone", "urgentStarted", "answered"]
    out = ["| " + " | ".join(cols) + " |", "|" + "---|" * len(cols)]
    for r in rows:
        out.append("| " + " | ".join("—" if r[c] is None else str(r[c]) for c in cols) + " |")
    return "\n".join(out)


def cli(*args):
    return json.loads(subprocess.run([CLI, *args], check=True, capture_output=True, text=True).stdout)


def processes(pattern):
    found = subprocess.run(["pgrep", "-f", pattern], capture_output=True, text=True).stdout.split()
    return set(found)


def wait_for(check, seconds, what):
    deadline = time.time() + seconds
    while time.time() < deadline:
        if check():
            return
        time.sleep(0.1)
    sys.exit(f"timed out waiting for {what}")


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    p.add_argument("--team", required=True)
    p.add_argument("--bot", action="append", required=True)
    p.add_argument("--objective", required=True)
    p.add_argument("--process", required=True, help="a pgrep -f pattern for the long tool's process")
    p.add_argument("--settle", type=float, default=3, help="seconds to wait after the tools started")
    p.add_argument("--watch", type=float, default=90, help="seconds to watch after the publish")
    a = p.parse_args(argv)

    run = uuid.uuid4().hex[:8]
    hold = {}
    for bot in a.bot:
        hold[bot] = cli("task", "submit", bot, "--objective", a.objective, "--key", f"hold-{run}-{bot}")["task"]["id"]
    wait_for(lambda: len(processes(a.process)) >= len(a.bot), 120, f"{len(a.bot)} × {a.process!r}")
    pids = processes(a.process)
    time.sleep(a.settle)

    out = cli("group", "publish", a.team, "stop.all", "--message", "Stop now.", "--key", f"stop-{run}")
    record = next(e for e in cli("group", "events", a.team, "--limit", "200")
                  if e.get("type") == "group.event.published" and (e.get("payload") or {}).get("eventId") == out["eventId"])
    published = parse_time(record["createdAt"])
    urgent = {d["botId"]: d.get("taskId") for d in record["payload"].get("deliveries") or []}

    gone = {}
    deadline = time.time() + a.watch
    while time.time() < deadline and not gone:
        if not (processes(a.process) & pids):
            gone["all"] = time.time()
        time.sleep(0.1)
    rows = []
    for bot in a.bot:
        hold_events = cli("task", "events", hold[bot])
        urgent_events = []
        if urgent.get(bot):
            wait_for(lambda: cli("task", "get", urgent[bot])["status"] in ("Completed", "Failed", "Cancelled"), a.watch, "the urgent task")
            urgent_events = cli("task", "events", urgent[bot])
        rows.append(row(bot, published, hold_events, gone.get("all"), urgent_events))
    print(json.dumps({"run": run, "team": a.team, "published": published, "rows": rows}, indent=2))
    print(table(rows))


if __name__ == "__main__":
    main()
