#!/usr/bin/env python3
"""rave-service: the rave's world. Four chairs live-code techno in Strudel.

  POST /mcp/<chair>      MCP over HTTP for one chair: phrases, play, listen, stop.
                         Each chair's node names its own URL, so the chair is who
                         calls; a chair may only speak for itself
  GET  /api/state        the rave now: take, bar, beat, tempo, every chair's state,
                         its pattern playing and next, Jev's pick as the bot
                         reported it, the last facts; ?version=N&wait=20 answers
                         when it changes. The same shape as the orchestra's, so
                         /hands lights its lanterns from it
  GET  /audio.wav        the sound, live, as an open-ended WAV (OBS, ffplay, a browser)
  GET  /api/score        the beats per bar, the phrase, the sections and the pattern library
  GET  /api/take         every play and every pattern begun in this take (the measurement)
  POST /api/event        an operator's event applied here as if the bus had brought it
                         ({name, message, data?, to?}): the check without a control
                         plane. It is not the bus: the page shows it as "local"
  GET  /hands            the conductor's page, served from ../score-service/static as it
                         is (?world=rave&view=1 to look without a headset)
  GET  /api/hands        what the page asks first: no hands here, the instruments by section
  GET  /ws/hands         the page's WebSocket (ws.py, copied from the orchestra): the state down as soon
                         as it changes; a posture up is answered "nothing": the rave reads no hands yet

It listens on loopback only. With controlPlane.url it follows the team's bus
and cuts the sound on stop.all itself. The sound is superdough in a Node
process (strudel/sound.mjs), the patterns are evaluated in another
(strudel/evaluate.mjs): both need Node.

Run:  python3 server.py [config.json] [--record take.wav]
Test: python3 -m unittest -v test_rave_service; (cd strudel && npm test)
"""

import argparse
import datetime
import json
import logging
import os
import sys
import threading
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import bus
import mcp_http
import synth
import ws
from evaluator import Evaluator
from rave import Rave, Refused

HERE = os.path.dirname(os.path.abspath(__file__))
STATIC = os.path.realpath(os.path.join(HERE, "..", "score-service", "static"))
TYPES = {".html": "text/html; charset=utf-8", ".css": "text/css", ".js": "text/javascript", ".svg": "image/svg+xml",
         ".ttf": "font/ttf", ".txt": "text/plain; charset=utf-8"}
log = logging.getLogger("rave")

JUDGE = {"type": "object", "description": "What axlr_judge answered, as you read it: its choice, the probability it gave that choice, its confidence",
         "properties": {"choice": {"type": "string", "maxLength": 200}, "probability": {"type": "number", "minimum": 0, "maximum": 1},
                        "confidence": {"type": "number", "minimum": 0, "maximum": 1}}}
TOOLS = [
    {"name": "phrases", "description": "Your state and the candidate patterns for your part (Strudel code, one cycle = one bar; a phrase is 8 bars), and ask: the question to pass to axlr_judge as it is (state, question, options).",
     "inputSchema": {"type": "object", "properties": {"bar": {"type": "integer", "minimum": 1}}}},
    {"name": "play", "description": "Play a pattern on your part from the next bar; it loops until your next play or stop. candidate = the number of one offered, or pattern = Strudel code (a candidate's, or your own: it is checked before it sounds). When nothing plays yet, it starts the music with a count-in. Pass judge: what axlr_judge answered.",
     "inputSchema": {"type": "object", "properties": {"candidate": {"type": "integer", "minimum": 1}, "pattern": {"type": "string", "maxLength": 2000},
                                                      "dynamics": {"type": "string", "enum": ["p", "mf", "f"]}, "judge": JUDGE}}},
    {"name": "listen", "description": "Play on and wait (at most 20 seconds) until it is time to choose your next pattern, a few bars pass, you stop or the music stops. Answers where the music is, your pattern and, when it is time to choose, candidates and ask.",
     "inputSchema": {"type": "object", "properties": {"bars": {"type": "integer", "minimum": 1, "maximum": 32}}}},
    {"name": "stop", "description": "Stop playing at once: your part is silent from here. The others go on.",
     "inputSchema": {"type": "object", "properties": {}}},
]


def chair_tools(rave, chair, max_seconds):
    tools = mcp_http.Tools("rave-service", "0.1.0")

    def guard(fn):
        def call(args):
            try:
                return fn(args)
            except Refused as exc:
                raise mcp_http.ToolError(str(exc))
        return call

    tools.add(TOOLS[0], guard(lambda a: rave.phrases(chair, a.get("bar"))))
    tools.add(TOOLS[1], guard(lambda a: rave.play(chair, a.get("pattern"), a.get("candidate"), a.get("dynamics"), a.get("judge"))))
    tools.add(TOOLS[2], guard(lambda a: rave.listen(chair, a.get("bars", 8), max_seconds)))
    tools.add(TOOLS[3], guard(lambda a: rave.stop(chair)))
    return tools


def local_event(rave, body):
    """An operator's event applied here, as bus.apply would apply the control plane's record of it."""
    name = body.get("name")
    if not isinstance(name, str) or not name:
        raise ValueError("name is the event's name (info.cue.all, stop.all, info.tempo…)")
    now = datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z")
    event = {"type": "group.event.published", "createdAt": now,
             "payload": {"name": name, "message": body.get("message", ""), "data": body.get("data") or {}, "to": body.get("to") or [], "from": "local"}}
    return {"applied": bus.apply(rave, event), "event": event}


def hands_view(rave):
    """What the page asks first (GET /api/hands): no hands here; the groups a hand conducts (the families, left and right),
    the instruments by section (each a slice of the arc ahead) and the world the page shows when its URL names none."""
    families = list(dict.fromkeys(s["family"] for s in rave.sections.values()))
    n = len(rave.sections)
    return {"last": None, "source": "none: the rave has no hands yet; the page conducts nothing here", "dryRun": True, "history": [],
            "world": "rave",
            "groups": {"left": families[0] if families else "all", "right": families[1] if len(families) > 1 else "all", "both": "all"},
            "instruments": [{"id": sid, "name": s["name"], "sections": [sid], "from": 180 * i // n, "to": 180 * (i + 1) // n}
                            for i, (sid, s) in enumerate(rave.sections.items())]}


def make_handler(rave, stream, config):
    label = config["label"]
    score_json = json.dumps(rave.score()).encode()
    hands_json = json.dumps(hands_view(rave)).encode()
    per_chair = {c: chair_tools(rave, c, config["listen"]["maxSeconds"]) for c in rave.chairs}

    def state_now(version=None, wait=20):
        state = rave.snapshot() if version is None else rave.wait_change(version, wait)
        state["label"] = label
        return state

    def conduct(sock):
        """One page on one WebSocket: each state as soon as it changes goes down; a posture up is answered with nothing read."""
        def push():
            version = None
            while sock.open:
                state = state_now(version)
                if state["version"] == version:
                    continue
                version = state["version"]
                try:
                    sock.send_json({"type": "state", "state": state})
                except ws.Closed:
                    return

        threading.Thread(target=push, daemon=True).start()
        while True:
            text = sock.recv()
            if text is None:
                return
            try:
                message = json.loads(text)
            except ValueError:
                continue
            kind = message.get("type") if isinstance(message, dict) else None
            if kind in ("posture", "native"):
                record = {"at": rave.clock(), "read": {"left": "nothing", "right": "nothing"}, "sent": None,
                          "note": "the rave reads no hands yet: the page shows them, the service does nothing"}
                try:
                    sock.send_json({"type": "hands" if kind == "posture" else "native", "id": message.get("id"), "record": record})
                except ws.Closed:
                    return

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, fmt, *args):
            pass

        def _send(self, status, body=b"", ctype="application/json"):
            self.send_response(status)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            try:
                self.wfile.write(body)
            except (BrokenPipeError, ConnectionResetError):
                self.close_connection = True

        def _loopback(self):
            host = (self.headers.get("Host") or "")
            host = host.split("]")[0] + "]" if host.startswith("[") else host.rsplit(":", 1)[0]
            return host in mcp_http.LOOPBACK_HOSTS

        def _body(self, limit):
            length = int(self.headers.get("Content-Length") or 0)
            if length <= 0 or length > limit:
                return None
            return self.rfile.read(length)

        def do_POST(self):
            if not self._loopback():
                return self._send(403, b'{"error":"loopback only"}')
            path = self.path.split("?")[0]
            if path == "/api/event":
                raw = self._body(16384)
                if raw is None:
                    return self._send(400, b'{"error":"bad body"}')
                try:
                    body = json.loads(raw)
                    out = local_event(rave, body)
                except (ValueError, TypeError) as exc:
                    return self._send(400, json.dumps({"error": str(exc)}).encode())
                return self._send(200, json.dumps(out).encode())
            parts = path.strip("/").split("/")
            if len(parts) != 2 or parts[0] != "mcp" or parts[1] not in per_chair:
                return self._send(404, b'{"error":"no such chair"}')
            raw = self._body(mcp_http.MAX_BODY)
            if raw is None:
                return self._send(400, b'{"error":"bad body"}')
            try:
                message = json.loads(raw)
            except ValueError:
                return self._send(400, json.dumps(mcp_http.error(None, -32700, "parse error")).encode())
            reply = per_chair[parts[1]].handle(message)
            if reply is None:
                return self._send(202)
            self._send(200, json.dumps(reply).encode())

        def do_GET(self):
            if not self._loopback():
                return self._send(403, b'{"error":"loopback only"}')
            url = urllib.parse.urlparse(self.path)
            query = dict(urllib.parse.parse_qsl(url.query))
            if url.path == "/api/state":
                version = int(query["version"]) if "version" in query else None
                return self._send(200, json.dumps(state_now(version, min(float(query.get("wait", 20)), 25))).encode())
            if url.path == "/ws/hands":
                return self._ws_hands()
            if url.path == "/api/score":
                return self._send(200, score_json)
            if url.path == "/api/hands":
                return self._send(200, hands_json)
            if url.path == "/api/take":
                return self._send(200, json.dumps(rave.take_log(query.get("take"))).encode())
            if url.path == "/audio.wav":
                return self._audio()
            if url.path in ("/", "/hands"):
                return self._file("hands.html")
            if url.path == "/stage":
                return self._file("stage.html")
            if url.path.startswith("/static/"):
                return self._file(url.path[len("/static/"):])
            self._send(404, b'{"error":"not found"}')

        def _ws_hands(self):
            origin = self.headers.get("Origin")
            if origin and urllib.parse.urlparse(origin).netloc.rsplit(":", 1)[0] not in mcp_http.LOOPBACK_HOSTS:
                return self._send(403, b'{"error":"only a page served here may conduct"}')
            key = self.headers.get("Sec-WebSocket-Key")
            if (self.headers.get("Upgrade") or "").lower() != "websocket" or not key:
                return self._send(400, b'{"error":"a WebSocket upgrade"}')
            self.send_response(101)
            self.send_header("Upgrade", "websocket")
            self.send_header("Connection", "Upgrade")
            self.send_header("Sec-WebSocket-Accept", ws.accept_key(key))
            self.end_headers()
            self.close_connection = True
            conduct(ws.Socket(self.rfile, self.wfile))

        def _file(self, name):
            path = os.path.realpath(os.path.join(STATIC, name))
            if not path.startswith(STATIC + os.sep) or not os.path.isfile(path):
                return self._send(404, b'{"error":"not found"}')
            with open(path, "rb") as f:
                self._send(200, f.read(), TYPES.get(os.path.splitext(path)[1], "application/octet-stream"))

        def _audio(self):
            q = stream.listen()
            try:
                self.send_response(200)
                self.send_header("Content-Type", "audio/wav")
                self.send_header("Cache-Control", "no-store")
                self.send_header("Connection", "close")
                self.end_headers()
                self.wfile.write(synth.wav_header(rave.rate))
                while True:
                    self.wfile.write(q.get())
            except (BrokenPipeError, ConnectionResetError):
                pass
            finally:
                stream.leave(q)
                self.close_connection = True

    return Handler


def load_json(path):
    with open(path) as f:
        return json.load(f)


def main(argv=None):
    p = argparse.ArgumentParser(description="the rave's service")
    p.add_argument("config", nargs="?", default=os.path.join(HERE, "config.json"))
    p.add_argument("--record", help="also write the sound to this WAV file")
    p.add_argument("--listen", help="host:port, default the config's listenAddress")
    p.add_argument("--control-plane", help="the operator API, default the config's controlPlane.url")
    p.add_argument("--team", help="the team whose bus to follow, default the config's team")
    p.add_argument("--no-network", action="store_true", help="the sound takes samples from its cache only")
    a = p.parse_args(argv)
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s: %(message)s")
    config = load_json(a.config)
    config["controlPlane"]["url"] = a.control_plane or config["controlPlane"]["url"]
    config["team"] = a.team or config["team"]
    library = load_json(os.path.join(os.path.dirname(os.path.abspath(a.config)), config["patterns"]["file"]))

    audio = config["audio"]
    sound = synth.Superdough(audio["rate"], audio["block"], audio.get("master", 0.6), audio.get("cache"),
                             audio.get("network", True) and not a.no_network)
    ready = sound.start()
    log.info("sound up: %s Hz, blocks of %d, samples %s, %d sounds", ready["rate"], ready["block"], ready["samples"], len(ready["sounds"]))
    evaluator = Evaluator()
    rave = Rave(config, library, sound, evaluator)
    # Every candidate in the library is evaluated once at boot: a bad one is a bad config, not a bad night.
    checks = evaluator.evaluate([{"id": e["id"], "code": e["code"], "from": 0, "to": 4} for e in rave.by_id.values()])
    bad = [f"{r['id']}: {r.get('error')}" for r in checks if not r.get("ok")]
    unknown = {s.split(":")[0] for r in checks for s in r.get("sounds", [])} - rave.sounds
    if bad or unknown:
        sys.exit("patterns.json: " + "; ".join(bad + [f"no such sound: {s}" for s in sorted(unknown)]))
    log.info("%s: %d chairs, %d candidates, %s bpm, phrases of %d bars", config["title"], len(rave.chairs), len(rave.by_id), rave.bpm, rave.phrase_bars)

    stream = synth.Stream()
    threading.Thread(target=synth.run, args=(rave, sound, stream, a.record), daemon=True).start()
    cp = config["controlPlane"]
    if cp.get("url"):
        b = bus.Bus(cp["url"], config["team"], cp["tokenFile"], cp["caFile"])
        threading.Thread(target=b.follow, args=(rave,), daemon=True).start()
    else:
        log.info("no controlPlane.url: not following the bus; POST /api/event applies an operator's event locally")

    host, port = (a.listen or config["listenAddress"]).rsplit(":", 1)
    if host not in ("127.0.0.1", "localhost", "::1"):
        sys.exit("rave-service listens on loopback only")
    server = ThreadingHTTPServer((host, int(port)), make_handler(rave, stream, config))
    server.daemon_threads = True
    log.info("listening on http://%s:%s (mcp at /mcp/<chair>, state at /api/state, sound at /audio.wav, the page at /hands?world=rave)", host, port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        sound.close()


if __name__ == "__main__":
    main()
