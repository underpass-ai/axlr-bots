#!/usr/bin/env python3
"""Piece 3's check, by hand: two parts enter, the operator stops them, the sound goes.

With score-service running with --record and following a team's bus, this
script plays two chairs by calling their MCP tools as a bot would (one plays
phrase 1, which starts the music with its count-in; the other phrase 1 a few
seconds later, and both their next phrases), publishes stop.all on
the team as the operator (the axlr-bots CLI, over the socket), then reads
the recording and says:

  heard       the bars where each part sounded (the section's state)
  bus → cut   seconds from the event on the control plane to the service's cut
  cut → quiet seconds from the cut to the first 100 ms under -60 dBFS in the WAV
  bar         one bar at the service's tempo, the limit the plan sets

Run:  python3 check_take.py --team stage --wav take.wav [--chairs vn1-1 ob-1] [--play 12]
"""

import argparse
import array
import json
import math
import subprocess
import time
import urllib.request
import uuid
import wave

BASE = "http://127.0.0.1:8790"


def call(chair, tool, arguments):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": tool, "arguments": arguments}}).encode()
    request = urllib.request.Request(f"{BASE}/mcp/{chair}", body, {"Content-Type": "application/json"})
    with urllib.request.urlopen(request, timeout=30) as r:
        return json.loads(json.load(r)["result"]["content"][0]["text"])


def state():
    with urllib.request.urlopen(f"{BASE}/api/state", timeout=10) as r:
        return json.load(r)


def quiet_after(path, frame, floor_db=-60.0, window=0.1):
    """Seconds from frame to the first window whose RMS is under floor_db, or None."""
    with wave.open(path) as w:
        rate, channels = w.getframerate(), w.getnchannels()
        w.setpos(min(frame, w.getnframes()))
        step = int(rate * window)
        waited = 0.0
        while True:
            data = w.readframes(step)
            if len(data) < step * channels * 2:
                return None
            samples = array.array("h", data)
            rms = math.sqrt(sum(s * s for s in samples) / len(samples))
            if 20 * math.log10(max(rms, 1) / 32768) < floor_db:
                return round(waited, 2)
            waited += window


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    p.add_argument("--team", required=True)
    p.add_argument("--wav", required=True, help="the file score-service --record writes")
    p.add_argument("--chairs", nargs=2, default=["vn1-1", "ob-1"])
    p.add_argument("--play", type=float, default=20, help="seconds to play before the stop (the count-in is 9.5 s)")
    a = p.parse_args(argv)

    first, second = a.chairs
    print("play", first, call(first, "play", {"phrase": 1, "dynamics": "f"}))
    time.sleep(3)
    print("play", second, call(second, "play", {"phrase": 1, "dynamics": "f"}))
    for chair in a.chairs:
        print("play", chair, call(chair, "play", {"phrase": 2, "dynamics": "f"}))
    time.sleep(a.play)
    before = state()
    run = uuid.uuid4().hex[:8]
    subprocess.run(["axlr-bots", "group", "publish", a.team, "stop.all", "--message", "Stop now.", "--key", f"check-{run}"],
                   check=True, capture_output=True)
    deadline = time.time() + 10
    while time.time() < deadline and not state().get("cut"):
        time.sleep(0.1)
    after = state()
    cut = after.get("cut")
    if not cut:
        raise SystemExit("no cut: is score-service following the team's bus?")
    time.sleep(1.5)
    quiet = quiet_after(a.wav, cut["frame"])
    bar = 60.0 / after["bpm"] * 4
    playing = {s["id"]: s["playing"] for s in before["sections"]}
    print(json.dumps({"playingBeforeStop": playing, "barAtStop": cut["bar"], "busToCut": cut["afterEventSeconds"],
                      "cutToQuiet": quiet, "oneBar": round(bar, 2),
                      "underOneBar": quiet is not None and cut["afterEventSeconds"] + quiet < bar}, indent=2))


if __name__ == "__main__":
    main()
