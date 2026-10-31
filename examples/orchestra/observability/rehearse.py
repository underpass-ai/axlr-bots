#!/usr/bin/env python3
"""A rehearsal with no headset and no bots: something for the dashboards to show.

It drives a score service (a test instance, never the one the headset
conducts) the way the headset and the chairs do:

  the chairs      --chairs of them (one per section first) call the MCP tools
                  as a chair's program does: phrases, play the first
                  candidate, listen, play the next; no Jev, a made-up probability
  the conductor   one WebSocket on /ws/hands: a posture every --posture-every
                  seconds (the page's made-up open, palm down, fist, rest), the
                  hands' heights ten times a second, the headset's own gestures
                  (tap, swipes, pinches, a double pinch, a beat) and a motion sum
                  every --motion-every seconds (dolce … appassionato)
  a listener      /audio.wav, read and dropped

The postures and the motion are read by Jev for real: about one call every
1.4 s, some 800 input tokens each, $0.042 per million, so about $0.0015 a
minute. Run the service with --hands-dry-run so nothing is published:

  server.py --listen 127.0.0.1:8795 --hands-dry-run
  python3 rehearse.py --url http://127.0.0.1:8795 --minutes 5
"""

import argparse
import base64
import itertools
import json
import math
import os
import random
import socket
import sys
import threading
import time
import urllib.parse
import urllib.request

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "score-service"))
import ws  # noqa: E402  the service's own frames

SECTIONS = ("vn1", "vn2", "va", "vc", "cb", "ob", "hn")
SIZES = {"vn1": 10, "vn2": 8, "va": 6, "vc": 5, "cb": 3, "ob": 2, "hn": 2}
# The page's made-up postures and motion sums (static/hands.js), so Jev reads what it would read from the page.
MADE_UP = {
    "open": {"fingers": {"thumb": 25, "index": 8, "middle": 6, "ring": 10, "little": 12}, "palm": {"away": 0.1, "toward": -0.1, "floor": -0.93, "up": 0.93, "side": 0.05}, "spread": 0.1},
    "down": {"fingers": {"thumb": 25, "index": 10, "middle": 8, "ring": 9, "little": 12}, "palm": {"away": 0.1, "toward": -0.1, "floor": 0.93, "up": -0.93, "side": 0.05}, "spread": 0.1},
    "fist": {"fingers": {"thumb": 90, "index": 195, "middle": 210, "ring": 205, "little": 200}, "palm": {"away": 0.3, "toward": -0.3, "floor": 0.2, "up": -0.2, "side": 0.85}, "spread": 0.04},
    "rest": {"fingers": {"thumb": 40, "index": 70, "middle": 80, "ring": 85, "little": 90}, "palm": {"away": 0.3, "toward": -0.3, "floor": 0.2, "up": -0.2, "side": 0.85}, "spread": 0.05},
}
MOTION = {
    "dolce": {"seconds": 2.4, "speed": 0.12, "peak": 0.3, "rise": 0.08, "sweep": 0.06, "turns": 0.4, "height": -0.3, "open": 0.85},
    "cantabile": {"seconds": 2.4, "speed": 0.45, "peak": 0.9, "rise": 0.22, "sweep": 0.3, "turns": 1.2, "height": -0.15, "open": 0.8},
    "brillante": {"seconds": 2.4, "speed": 0.9, "peak": 2.2, "rise": 0.25, "sweep": 0.2, "turns": 2.6, "height": 0.1, "open": 0.9},
    "appassionato": {"seconds": 2.4, "speed": 1.6, "peak": 4.5, "rise": 0.6, "sweep": 0.5, "turns": 2.4, "height": 0.05, "open": 0.4},
}
# What the conductor does, in turn: a posture (side: gesture, target), and the headset's gestures between them.
POSTURES = [({"left": "open"}, "violins"), ({"right": "down"}, "oboes"), ({"left": "rest"}, None), ({"left": "open", "right": "open"}, None),
            ({"right": "fist"}, "horns"), ({"left": "open"}, "cellos"), ({"right": "rest"}, None), ({"left": "down"}, "violas")]
NATIVES = [("tap-thumb", "violins"), ("swipe-forward", None), ("pinch-start", "cellos"), ("pinch-move", "cellos"), ("pinch-end", "cellos"),
           ("pinch-double", "violins"), ("swipe-backward", None), ("beat", None), ("tap-thumb", "basses")]


def chairs(n):
    """One chair per section first, then the second of each, …"""
    out = []
    for k in range(1, 11):
        out += [f"{s}-{k}" for s in SECTIONS if k <= SIZES[s]]
    return out[:n]


class Mcp:
    def __init__(self, url, chair):
        self.url, self.n = f"{url}/mcp/{chair}", itertools.count(1)

    def call(self, tool, **arguments):
        body = json.dumps({"jsonrpc": "2.0", "id": next(self.n), "method": "tools/call", "params": {"name": tool, "arguments": arguments}}).encode()
        request = urllib.request.Request(self.url, data=body, method="POST", headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(request, timeout=40) as response:
            result = json.load(response)["result"]
        text = result["content"][0]["text"]
        return {"error": text} if result.get("isError") else json.loads(text)


def chair_loop(url, chair, until, log):
    mcp, r = Mcp(url, chair), None
    while time.time() < until:
        try:
            r = r or mcp.call("phrases")
            candidates = r.get("candidates") or []
            if r.get("status") in ("stopped", "ended") or r.get("done") or not candidates:
                time.sleep(2)
                r = None
                continue
            pick = candidates[0]["phrase"]
            p = round(random.uniform(0.35, 0.95), 2)
            out = mcp.call("play", phrase=pick, judge={"choice": f"phrase {pick}", "probability": p})
            if "error" in out:
                log(f"{chair}: {out['error'][:80]}")
                r = None
                continue
            r = mcp.call("listen", bars=8)
            while time.time() < until and not r.get("candidates") and not r.get("done"):
                r = mcp.call("listen", bars=8)
        except Exception as exc:  # noqa: BLE001  a rehearsal goes on
            log(f"{chair}: {exc}")
            time.sleep(2)
            r = None


class Conductor:
    def __init__(self, url):
        u = urllib.parse.urlparse(url)
        self.sock = socket.create_connection((u.hostname, u.port or 80), timeout=10)
        key = base64.b64encode(os.urandom(16)).decode()
        self.sock.sendall((f"GET /ws/hands HTTP/1.1\r\nHost: {u.netloc}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
                           f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n").encode())
        self.f = self.sock.makefile("rb")
        status = self.f.readline().decode()
        if " 101 " not in status:
            raise RuntimeError(f"no WebSocket: {status.strip()}")
        while self.f.readline() not in (b"\r\n", b""):
            pass
        self.lock, self.n, self.engaged, self.height = threading.Lock(), itertools.count(1), set(), -0.2
        threading.Thread(target=self._drain, daemon=True).start()

    def _drain(self):
        try:
            while self.f.read(1):
                pass
        except OSError:
            pass

    def send(self, message):
        with self.lock:
            self.sock.sendall(ws.frame(ws.TEXT, json.dumps(message).encode(), os.urandom(4)))

    def posture(self, hands, target):
        self.engaged = {side for side, g in hands.items() if g in ("open", "down")}
        shaped = {side: dict(MADE_UP[g], position={"side": -0.25 if side == "left" else 0.25, "height": -0.25, "ahead": 0.4}) for side, g in hands.items()}
        self.send({"type": "posture", "id": next(self.n), "hands": shaped, "heldMs": 300, "simulated": True, "target": target, "locked": bool(target)})

    def heights(self, t):
        if self.engaged:
            h = -0.2 + 0.15 * math.sin(t * 1.3)
            self.send({"type": "height", "hands": {side: {"height": round(h, 3)} for side in self.engaged}})

    def native(self, gesture, target, t):
        message = {"type": "native", "id": next(self.n), "gesture": gesture, "side": "right", "target": target, "locked": bool(target)}
        if gesture.startswith("pinch-") and gesture != "pinch-double":
            message["height"] = round(-0.2 + 0.1 * ((t % 3) - 1), 3)
        if gesture == "beat":
            message["bpm"] = random.choice((140, 148, 152, 156))
        self.send(message)

    def motion(self, character):
        self.send({"type": "motion", "id": next(self.n), "hands": {"right": MOTION[character], "left": dict(MOTION[character])}, "simulated": True,
                   "target": None, "locked": False})


def listen_audio(url, until):
    try:
        with urllib.request.urlopen(f"{url}/audio.wav", timeout=10) as r:
            while time.time() < until and r.read(65536):
                pass
    except Exception:  # noqa: BLE001
        pass


def main(argv=None):
    p = argparse.ArgumentParser(description="drive a test score service so the dashboards fill")
    p.add_argument("--url", default="http://127.0.0.1:8795", help="the score service to rehearse with (never the live one)")
    p.add_argument("--minutes", type=float, default=5.0)
    p.add_argument("--chairs", type=int, default=14, help="chairs to play (one per section first)")
    p.add_argument("--posture-every", type=float, default=3.0)
    p.add_argument("--motion-every", type=float, default=2.5)
    p.add_argument("--native-every", type=float, default=4.0)
    a = p.parse_args(argv)
    if urllib.parse.urlparse(a.url).port == 8790:
        sys.exit("8790 is the score service the headset conducts: rehearse with a test instance (server.py --listen 127.0.0.1:8795 --hands-dry-run)")
    until = time.time() + a.minutes * 60
    log = lambda text: print(time.strftime("%H:%M:%S"), text, flush=True)  # noqa: E731
    calls = a.minutes * 60 * (1 / a.posture_every + 1 / a.motion_every)
    log(f"rehearsing on {a.url} for {a.minutes:g} min: {a.chairs} chairs, about {calls:.0f} Jev readings (~${calls * 800 * 0.042 / 1e6:.3f})")
    for chair in chairs(a.chairs):
        threading.Thread(target=chair_loop, args=(a.url, chair, until, log), daemon=True).start()
        time.sleep(0.1)
    threading.Thread(target=listen_audio, args=(a.url, until), daemon=True).start()
    conductor = Conductor(a.url)
    postures, natives, characters = itertools.cycle(POSTURES), itertools.cycle(NATIVES), itertools.cycle(MOTION)
    next_posture = next_motion = next_native = time.time() + 2
    while time.time() < until:
        t = time.time()
        if t >= next_posture:
            conductor.posture(*next(postures))
            next_posture = t + a.posture_every
        if t >= next_motion:
            conductor.motion(next(characters))
            next_motion = t + a.motion_every
        if t >= next_native:
            gesture, target = next(natives)
            conductor.native(gesture, target, t)
            next_native = t + (0.4 if gesture in ("pinch-start", "pinch-move") else a.native_every)
        conductor.heights(t)
        time.sleep(0.1)
    log("done")


if __name__ == "__main__":
    main()
