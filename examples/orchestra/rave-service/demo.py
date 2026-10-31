#!/usr/bin/env python3
"""The rave checked by hand: four bots, one cue, one stop.all, the sound captured.

It plays the four chairs' bots itself, as the fixed program of a Jev chair
would (phrases → a choice → play → listen → …), against a running
rave-service; captures /audio.wav to a file for the whole run; applies one
info.cue.all and, later, one stop.all; and measures what happened: when each
pattern began, how long after the stop the sound was gone, and whether the
chairs' states on /api/state followed. Without --control-plane the two
events go through POST /api/event (applied locally, as the bus would apply
them); with it they are published on the team as the operator, so the
service hears them on the bus, as the orchestra's are. The choice is not
Jev's here: the first candidate, or a different one each phrase with
--rotate; a real chair asks axlr_judge.

  python3 demo.py --seconds 60 --wav demo/take.wav --json demo/take.json
"""

import argparse
import array
import json
import math
import sys
import threading
import time
import urllib.request
import wave


class Service:
    def __init__(self, base):
        self.base = base.rstrip("/")

    def _json(self, path, body=None, timeout=30):
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(self.base + path, data=data, headers={"Content-Type": "application/json"} if data else {})
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.load(r)

    def call(self, chair, tool, args=None, rid=1):
        reply = self._json(f"/mcp/{chair}", {"jsonrpc": "2.0", "id": rid, "method": "tools/call", "params": {"name": tool, "arguments": args or {}}})
        result = reply["result"]
        text = result["content"][0]["text"]
        return (not result.get("isError")), (json.loads(text) if not result.get("isError") else text)

    def state(self):
        return self._json("/api/state")

    def take(self):
        return self._json("/api/take")

    def event(self, name, message, data=None):
        return self._json("/api/event", {"name": name, "message": message, "data": data or {}})


class Capture(threading.Thread):
    """GET /audio.wav into memory until told to stop; the WAV's header is dropped and written anew with the length."""

    def __init__(self, base):
        super().__init__(daemon=True)
        self.base, self.chunks, self.stop, self.started_at = base, [], threading.Event(), None

    def run(self):
        with urllib.request.urlopen(self.base + "/audio.wav", timeout=30) as r:
            r.read(44)
            self.started_at = time.time()
            while not self.stop.is_set():
                chunk = r.read(8192)
                if not chunk:
                    break
                self.chunks.append(chunk)

    def samples(self):
        data = b"".join(self.chunks)
        return array.array("h", data[: len(data) // 4 * 4])


def bot(service, chair, log, stop, rotate, cue_at):
    """One chair's fixed program: wait for the cue, then phrases → choice → play → listen, until stopped."""
    cue_at.wait()
    n = 0
    ok, reading = service.call(chair, "phrases")
    while not stop.is_set():
        if not ok:
            log.append({"at": time.time(), "chair": chair, "error": reading})
            return
        if reading.get("done"):
            service.call(chair, "stop")
            log.append({"at": time.time(), "chair": chair, "stopped": True, "note": reading.get("note")})
            return
        if "ask" in reading:
            candidates = reading["candidates"]
            pick = candidates[n % len(candidates)] if rotate else candidates[0]
            n += 1
            ok, played = service.call(chair, "play", {"candidate": pick["n"], "judge": {"choice": f"pattern {pick['n']}: {pick['name']}", "probability": 1.0, "confidence": 1.0}})
            log.append({"at": time.time(), "chair": chair, "play": pick["id"], "ok": ok, "fromBar": played.get("fromBar") if ok else None, "error": None if ok else played})
            if not ok:
                return
        ok, reading = service.call(chair, "listen", {"bars": 8})


def dbfs(x):
    return -120.0 if x <= 0 else 20 * math.log10(x / 32767)


def main(argv=None):
    p = argparse.ArgumentParser(description="four bots, one cue, one stop.all, the sound captured")
    p.add_argument("--service", default="http://127.0.0.1:8794")
    p.add_argument("--seconds", type=float, default=60, help="how long to capture")
    p.add_argument("--stop-at", type=float, default=None, help="seconds after the cue to publish stop.all (default: seconds - 6)")
    p.add_argument("--chairs", default="kick-1,bass-1,hats-1,lead-1")
    p.add_argument("--rotate", action="store_true", help="each phrase a different candidate, not always the first")
    p.add_argument("--wav", default="demo/take.wav")
    p.add_argument("--json", default="demo/take.json")
    a = p.parse_args(argv)
    service = Service(a.service)
    chairs = a.chairs.split(",")
    stop_at = a.stop_at if a.stop_at is not None else max(10, a.seconds - 6)

    capture = Capture(a.service)
    capture.start()
    while capture.started_at is None:
        time.sleep(0.05)
    t_capture = capture.started_at
    log, stop, cue_at = [], threading.Event(), threading.Event()
    threads = [threading.Thread(target=bot, args=(service, c, log, stop, a.rotate, cue_at), daemon=True) for c in chairs]
    for t in threads:
        t.start()
    time.sleep(1.0)  # a second of silence first: the capture's clock is seen to start before the cue
    t_cue = time.time()
    cue = service.event("info.cue.all", "Everybody in.", {"dynamics": "mf"})
    cue_at.set()
    print(f"cue applied: {cue['applied']} at +{t_cue - t_capture:.2f} s", flush=True)
    states = []
    t_stop = None
    while time.time() - t_capture < a.seconds:
        time.sleep(1.0)
        s = service.state()
        states.append({"at": round(time.time() - t_capture, 2), "status": s["status"], "bar": s["bar"], "bpm": s["bpm"],
                       "chairs": {c["id"]: c["state"] for sec in s["sections"] for c in sec["chairs"]}})
        if t_stop is None and time.time() - t_cue >= stop_at:
            t_stop = time.time()
            out = service.event("stop.all", "Stop now.")
            print(f"stop.all applied: {out['applied']} at +{t_stop - t_capture:.2f} s, bar {s['bar']}", flush=True)
    stop.set()
    capture.stop.set()
    time.sleep(0.3)
    samples = capture.samples()
    rate = 44100
    frames = len(samples) // 2
    with wave.open(a.wav, "wb") as w:
        w.setnchannels(2), w.setsampwidth(2), w.setframerate(rate)
        w.writeframes(samples.tobytes())

    def peak(t0, t1):
        seg = samples[max(0, int(t0 * rate)) * 2: max(0, int(t1 * rate)) * 2]
        return max((abs(x) for x in seg), default=0)

    final = service.state()
    take = service.take()
    cut = final.get("cut") or {}
    # The cut on the sound's clock: the frame the service cut at, against the capture's own first frame.
    cut_s = (t_stop - t_capture) if t_stop else None
    after_cut = None
    if cut_s is not None:
        # from the cut on, the first 100 ms window under -60 dBFS
        step = 0.05
        t = cut_s
        while t + 0.1 <= frames / rate:
            if dbfs(peak(t, t + 0.1)) < -60:
                after_cut = round(t - cut_s, 2)
                break
            t += step
    report = {
        "captureSeconds": round(frames / rate, 2), "cueAt": round(t_cue - t_capture, 2), "stopAt": round(t_stop - t_capture, 2) if t_stop else None,
        "silentBeforeCue": dbfs(peak(0, t_cue - t_capture - 0.1)) < -60,
        "peakDbfs": round(dbfs(peak(0, frames / rate)), 1),
        "peakWhilePlayingDbfs": round(dbfs(peak(t_cue - t_capture + 5, (t_stop - t_capture) if t_stop else frames / rate)), 1),
        "silentAfterCutWithinSeconds": after_cut,
        "cut": cut, "finalStatus": final["status"], "bpm": final["bpm"],
        "chairsAtEnd": {c["id"]: c["state"] for sec in final["sections"] for c in sec["chairs"]},
        "plays": [e for e in log if "play" in e], "errors": [e for e in log if "error" in e and e["error"]],
        "began": [e for e in take.get("log", []) if e["what"] == "began"], "states": states,
    }
    for e in report["plays"]:
        e["at"] = round(e["at"] - t_capture, 2)
    for e in report["errors"]:
        e["at"] = round(e["at"] - t_capture, 2)
    with open(a.json, "w") as f:
        json.dump(report, f, indent=1)
    print(json.dumps({k: report[k] for k in ("captureSeconds", "cueAt", "stopAt", "silentBeforeCue", "peakDbfs", "peakWhilePlayingDbfs",
                                              "silentAfterCutWithinSeconds", "finalStatus", "chairsAtEnd")}, indent=1))
    print("plays:", " ".join(f"{e['chair']}:{e['play']}@{e['fromBar']}" for e in report["plays"]))
    print("began:", " ".join(f"{e['chair']}:{e['pattern']}@{e['fromBar']}" for e in report["began"]))
    if report["errors"]:
        print("errors:", report["errors"])
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
