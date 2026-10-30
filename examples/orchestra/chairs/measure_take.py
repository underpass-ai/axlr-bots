#!/usr/bin/env python3
"""One take, measured: the conductor's cue, the chairs' phrases, Jev's picks, the cost.

  measure_take.py --chairs vn1-1 ob-1 [--bars 40] [--cue info.cue.all] [--data '{"dynamics": "f"}']
                  [--score http://127.0.0.1:8790] [--team orchestra] [--json take.json]

It publishes the cue on the team as the operator (the axlr-bots CLI), waits
until the score service's playhead is past --bars, publishes stop.all, waits
for the chairs' runs to end, and then reads, per chair:

  the service   every play (phrase, bars, how many bars late, how long before
                its first bar it came) and the pick the bot reported (/api/take)
  the node      each run's receipt: model requests and their cost, every
                axlr_judge call and its duration (<stage>/<chair>/traces)
  the bot       Jev's real answers, from the AXLR session the run wrote
                (<stage>/<chair>/.local/state/axlr/sessions), checked
                against what the bot reported to the service

and prints one row per chair and an estimate for 36 chairs over the same
bars. It plays nothing itself: the chairs named must be Enabled bots of the
team, on their nodes, pointed at --score. Other Enabled chairs of the team
hear the cue too.
"""

import argparse
import glob
import json
import math
import os
import statistics
import subprocess
import time
import urllib.request
import uuid

STAGE = os.environ.get("AXLR_STAGE", os.path.expanduser("~/.local/state/axlr-stage"))


def get(base, path):
    with urllib.request.urlopen(base + path, timeout=10) as r:
        return json.load(r)


def publish(team, name, message, data=None):
    args = ["axlr-bots", "group", "publish", team, name, "--message", message, "--key", f"measure-{uuid.uuid4().hex[:10]}"]
    if data:
        args += ["--data", json.dumps(data)]
    subprocess.run(args, check=True, capture_output=True)
    return time.time()


def runs(chair, since):
    """Each run of the chair's node that started after since: its receipt and its session id."""
    out = []
    for path in sorted(glob.glob(os.path.join(STAGE, chair, "traces", "*.jsonl"))):
        if os.path.getmtime(path) < since:
            continue
        receipt = None
        with open(path) as f:
            for line in f:
                try:
                    record = json.loads(line)
                except ValueError:
                    continue
                if record.get("type") == "receipt":
                    receipt = record["receipt"]
        if receipt and parse_time(receipt["started_at"]) >= since - 1:
            out.append(receipt)
    return out


def parse_time(text):
    import datetime

    head, _, tail = text.replace("Z", "").partition(".")
    return datetime.datetime.fromisoformat(head + "." + (tail + "000000")[:6] + "+00:00").timestamp()


def jev_answers(chair, session):
    """Jev's answers in the run's session, in order, as AXLR handed them to the model."""
    path = os.path.join(STAGE, chair, ".local", "state", "axlr", "sessions", session + ".json")
    if not os.path.exists(path):
        return []
    with open(path) as f:
        messages = json.load(f).get("messages") or []
    judge_calls = {call["id"] for m in messages for call in (m.get("tool_calls") or []) if call.get("name") == "axlr_judge"}
    out = []
    for m in messages:
        if m.get("role") == "tool" and m.get("tool_call_id") in judge_calls:
            try:
                out.append(json.loads(m["content"]))
            except ValueError:
                out.append({"error": m["content"][:200]})
    return out


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    p.add_argument("--chairs", nargs="+", required=True)
    p.add_argument("--bars", type=int, default=40)
    p.add_argument("--cue", default="info.cue.all")
    p.add_argument("--message", default="Everyone, come in.")
    p.add_argument("--data", default='{"dynamics": "f"}')
    p.add_argument("--score", default=os.environ.get("ORCHESTRA_SCORE_URL", "http://127.0.0.1:8790"))
    p.add_argument("--team", default="orchestra")
    p.add_argument("--settle", type=float, default=60, help="seconds to wait for the runs to end after stop.all")
    p.add_argument("--json", help="also write everything measured here")
    a = p.parse_args(argv)

    before = get(a.score, "/api/state")
    if before["status"] == "playing":
        raise SystemExit("the score service is playing: stop it first (stop.all)")
    cue_at = publish(a.team, a.cue, a.message, json.loads(a.data) if a.data else None)
    deadline = cue_at + 30 + a.bars * 2.0
    while time.time() < deadline:
        state = get(a.score, "/api/state")
        if state["take"] > before["take"] and (state["status"] != "playing" or state["bar"] > a.bars and not state["countIn"]):
            break
        time.sleep(0.5)
    number = state["take"]
    stop_at = publish(a.team, "stop.all", "Stop.")
    for _ in range(40):
        take = get(a.score, f"/api/take?take={number}")
        if take.get("cut"):
            break
        time.sleep(0.25)
    time.sleep(a.settle)

    take = get(a.score, f"/api/take?take={number}")
    bar_seconds = 4 * 60 / take["bpm"]
    phrases_due = math.ceil(a.bars / 8)
    rows, every = [], {"take": take, "cueAt": cue_at, "stopAt": stop_at, "chairs": {}}
    for chair in a.chairs:
        plays = [e for e in take["log"] if e["what"] == "play" and e["chair"] == chair]
        receipts = runs(chair, cue_at)
        requests = [r for receipt in receipts for r in receipt.get("requests", [])]
        judges = [t for receipt in receipts for t in receipt.get("tool_calls", []) if t["tool"] == "axlr_judge"]
        answers = [x for receipt in receipts for x in jev_answers(chair, receipt["session_id"])]
        reported = [e["judge"] for e in plays if e.get("judge")]
        agree = sum(1 for r, x in zip(reported, answers)
                    if r.get("choice") == x.get("choice") and abs(r.get("probability", -1) - (x.get("probabilities") or {}).get(x.get("choice"), -2)) < 0.011)
        in_excerpt = [e for e in plays if e["bars"][0] <= a.bars]
        cost = sum(r.get("cost_usd") or 0 for r in requests)
        latency = [t["duration_ms"] for t in judges]
        row = {
            "chair": chair,
            "phrases": [e["phrase"] for e in plays],
            "inExcerpt": len(in_excerpt), "due": phrases_due,
            "onTime": sum(1 for e in in_excerpt if e["lateBars"] == 0),
            "late": [(e["phrase"], e["lateBars"]) for e in in_excerpt if e["lateBars"]],
            "leadSeconds": [e["leadSeconds"] for e in in_excerpt],
            "jevCalls": len(judges), "jevMs": {"median": statistics.median(latency) if latency else None, "max": max(latency) if latency else None},
            "jevPicks": [(x.get("choice"), (x.get("probabilities") or {}).get(x.get("choice"))) for x in answers],
            "reportedAgree": f"{agree} of {len(reported)} reported, {len(answers)} answered",
            "runs": len(receipts), "requests": len(requests), "costUsd": round(cost, 5),
            "promptTokens": sum(r.get("prompt_tokens") or 0 for r in requests),
            "completionTokens": sum(r.get("completion_tokens") or 0 for r in requests),
        }
        row["costPerPhrase"] = round(cost / len(plays), 5) if plays else None
        row["jevPerPhrase"] = round(len(judges) / len(plays), 2) if plays else None
        rows.append(row)
        every["chairs"][chair] = {"row": row, "plays": plays, "receipts": receipts, "jev": answers}

    print(f"take {take['take']}: cue {a.cue} → stop.all at bar {take['cut']['bar'] if take.get('cut') else '?'}, "
          f"silence {take['cut']['afterEventSeconds'] if take.get('cut') else '?'} s after the event; one bar {bar_seconds:.2f} s")
    print("| chair | phrases | on time (first %d bars) | late | lead s | Jev calls | Jev ms (median/max) | reported = Jev | requests | cost $ | $/phrase |" % a.bars)
    print("|---|---|---|---|---|---|---|---|---|---|---|")
    for r in rows:
        print(f"| {r['chair']} | {r['phrases']} | {r['onTime']} of {r['due']} | {r['late'] or '–'} | "
              f"{', '.join(f'{x:.1f}' for x in r['leadSeconds'])} | {r['jevCalls']} | {r['jevMs']['median']}/{r['jevMs']['max']} | "
              f"{r['reportedAgree']} | {r['requests']} | {r['costUsd']} | {r['costPerPhrase']} |")
    per_phrase = [r["costPerPhrase"] for r in rows if r["costPerPhrase"]]
    if per_phrase:
        mean = statistics.mean(per_phrase)
        print(f"36 chairs × {phrases_due} phrases ({a.bars} bars, {a.bars * bar_seconds:.0f} s): "
              f"about ${36 * phrases_due * mean:.2f} of model, {36 * phrases_due} Jev calls, "
              f"{36 * phrases_due / (a.bars * bar_seconds):.1f} Jev calls a second at the stagger")
    if a.json:
        with open(a.json, "w") as f:
            json.dump(every, f, indent=1)


if __name__ == "__main__":
    main()
