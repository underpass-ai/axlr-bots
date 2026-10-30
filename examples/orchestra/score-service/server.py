#!/usr/bin/env python3
"""score-service: the orchestra's world.

  POST /mcp/<chair>      MCP over HTTP for one chair: phrases, play, listen, stop.
                         Each chair's node names its own URL, so the chair is who
                         calls; a chair may only speak for itself
  GET  /api/state        the orchestra now: take, bar, tempo, every chair's state,
                         its phrase playing and next, Jev's pick as the bot
                         reported it, the last facts; ?version=N&wait=20 answers
                         when it changes
  GET  /audio.wav        the sound, live, as an open-ended WAV (OBS, ffplay, a browser)
  GET  /api/score        each part's notes per bar, for the page's score strip, and
                         each line's phrases
  GET  /api/take         every play and every phrase begun in this take (the measurement)
  GET  /stage            the stage: 36 chairs, the score with its playhead, the conductor (?hud=0 for the recording)
  GET  /hands            the conductor's page for the headset (WebXR hand input): see hands.py
  POST /api/hands        one posture held still: Jev reads it, the service publishes what it means
  GET  /api/hands        what Jev read and what was sent, the last readings
  GET  /ws/hands         the same over one WebSocket (ws.py): postures and the hands' heights up; the state,
                         as soon as it changes, and Jev's readings down

It listens on loopback only (a headset reaches it through adb reverse). With
controlPlane.url it follows the team's bus and cuts the sound on stop.all
itself, and publishes the hands' events as the operator.

Run:  python3 server.py [config.json] [--record take.wav]
Test: python3 -m unittest -v test_score_service test_hands test_ws; node --test static/posture.test.mjs
"""

import argparse
import json
import logging
import os
import sys
import threading
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import bus
import hands as hands_module
import jev
import mcp_http
import synth
import ws
from orchestra import Orchestra, Refused
from score import Score

HERE = os.path.dirname(os.path.abspath(__file__))
STATIC = os.path.realpath(os.path.join(HERE, "static"))
TYPES = {".html": "text/html; charset=utf-8", ".css": "text/css", ".js": "text/javascript", ".svg": "image/svg+xml",
         ".ttf": "font/ttf", ".txt": "text/plain; charset=utf-8"}
log = logging.getLogger("score")

JUDGE = {"type": "object", "description": "What axlr_judge answered, as you read it: its choice, the probability it gave that choice, its confidence",
         "properties": {"choice": {"type": "string", "maxLength": 200}, "probability": {"type": "number", "minimum": 0, "maximum": 1},
                        "confidence": {"type": "number", "minimum": 0, "maximum": 1}}}
TOOLS = [
    {"name": "phrases", "description": "Your line's phrases you may play next (a phrase is 8 bars), from the playhead or from bar, and ask: the question to pass to axlr_judge as it is (state, question, options).",
     "inputSchema": {"type": "object", "properties": {"bar": {"type": "integer", "minimum": 1}}}},
    {"name": "play", "description": "Play one phrase of your line: it sounds at its own bars. A phrase already begun is joined at the next bar. When nothing plays yet, it starts the music with a count-in. Pass judge: what axlr_judge answered.",
     "inputSchema": {"type": "object", "properties": {"phrase": {"type": "integer", "minimum": 1}, "dynamics": {"type": "string", "enum": ["p", "mf", "f"]}, "judge": JUDGE},
                     "required": ["phrase"]}},
    {"name": "listen", "description": "Play on and wait (at most 20 seconds) until it is time to choose your next phrase, a few bars pass, you stop or the music stops. Answers where the music is, your phrases and, when it is time to choose, candidates and ask.",
     "inputSchema": {"type": "object", "properties": {"bars": {"type": "integer", "minimum": 1, "maximum": 32}}}},
    {"name": "stop", "description": "Stop playing at once. Your section goes on with the chairs still playing.",
     "inputSchema": {"type": "object", "properties": {}}},
]


def chair_tools(orchestra, chair, max_seconds):
    tools = mcp_http.Tools("score-service", "0.2.0")

    def guard(fn):
        def call(args):
            try:
                return fn(args)
            except Refused as exc:
                raise mcp_http.ToolError(str(exc))
        return call

    tools.add(TOOLS[0], guard(lambda a: orchestra.phrases(chair, a.get("bar"))))
    tools.add(TOOLS[1], guard(lambda a: orchestra.play(chair, a.get("phrase"), a.get("dynamics"), a.get("judge"))))
    tools.add(TOOLS[2], guard(lambda a: orchestra.listen(chair, a.get("bars", 8), max_seconds)))
    tools.add(TOOLS[3], guard(lambda a: orchestra.stop(chair)))
    return tools


def make_handler(orchestra, stream, config, hands=None):
    label = config["label"]
    rate = config["audio"]["rate"]
    score_json = json.dumps(orchestra.density()).encode()
    per_chair = {c: chair_tools(orchestra, c, config["listen"]["maxSeconds"]) for c in orchestra.chairs}

    def state_now(version=None, wait=20):
        state = orchestra.snapshot() if version is None else orchestra.wait_change(version, wait)
        state["label"] = label
        if hands is not None:
            view = hands.view()
            state["hands"] = {"last": view["last"], "source": view["source"], "dryRun": view["dryRun"]}
        return state

    def conduct(sock):
        """One headset on one WebSocket: each state as soon as it changes goes
        down; each posture that comes up is read (Jev) and its record goes down."""
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

        def answer(message):
            record = hands.observe(message)
            try:
                sock.send_json({"type": "hands", "id": message.get("id"), "record": record})
            except ws.Closed:
                pass

        threading.Thread(target=push, daemon=True).start()
        while True:
            text = sock.recv()
            if text is None:
                return
            try:
                message = json.loads(text)
            except ValueError:
                continue
            if isinstance(message, dict) and message.get("type") == "posture":
                threading.Thread(target=answer, args=(message,), daemon=True).start()
            elif isinstance(message, dict) and message.get("type") == "height":
                hands.height(message)  # no Jev, no answer: the state shows the gains

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
                self.close_connection = True  # a run its node stopped while it listened

        def _loopback(self):
            host = (self.headers.get("Host") or "")
            host = host.split("]")[0] + "]" if host.startswith("[") else host.rsplit(":", 1)[0]
            return host in mcp_http.LOOPBACK_HOSTS

        def do_POST(self):
            if not self._loopback():
                return self._send(403, b'{"error":"loopback only"}')
            if self.path.split("?")[0] == "/api/hands":
                return self._hands()
            parts = self.path.split("?")[0].strip("/").split("/")
            if len(parts) != 2 or parts[0] != "mcp" or parts[1] not in per_chair:
                return self._send(404, b'{"error":"no such chair"}')
            length = int(self.headers.get("Content-Length") or 0)
            if length <= 0 or length > mcp_http.MAX_BODY:
                return self._send(400, b'{"error":"bad body"}')
            try:
                message = json.loads(self.rfile.read(length))
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
            if url.path == "/api/hands":
                if hands is None:
                    return self._send(404, b'{"error":"no hands: the service runs without them"}')
                return self._send(200, json.dumps(hands.view()).encode())
            if url.path == "/api/score":
                return self._send(200, score_json)
            if url.path == "/api/take":
                return self._send(200, json.dumps(orchestra.take_log(query.get("take"))).encode())
            if url.path == "/audio.wav":
                return self._audio()
            if url.path in ("/", "/stage"):
                return self._file("stage.html")
            if url.path == "/hands":
                return self._file("hands.html")
            if url.path.startswith("/static/"):
                return self._file(url.path[len("/static/"):])
            self._send(404, b'{"error":"not found"}')

        def _ws_hands(self):
            if hands is None:
                return self._send(404, b'{"error":"no hands: the service runs without them"}')
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

        def _hands(self):
            if hands is None:
                return self._send(404, b'{"error":"no hands: the service runs without them"}')
            length = int(self.headers.get("Content-Length") or 0)
            if length <= 0 or length > 16384:
                return self._send(400, b'{"error":"bad body"}')
            try:
                posture = json.loads(self.rfile.read(length))
            except ValueError:
                return self._send(400, b'{"error":"not JSON"}')
            self._send(200, json.dumps(hands.observe(posture)).encode())

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
                self.wfile.write(synth.wav_header(rate))
                while True:
                    self.wfile.write(q.get())
            except (BrokenPipeError, ConnectionResetError):
                pass
            finally:
                stream.leave(q)
                self.close_connection = True

    return Handler


def load_config(path):
    with open(path) as f:
        return json.load(f)


def main(argv=None):
    p = argparse.ArgumentParser(description="the orchestra's score service")
    p.add_argument("config", nargs="?", default=os.path.join(HERE, "config.json"))
    p.add_argument("--record", help="also write the sound to this WAV file")
    p.add_argument("--listen", help="host:port, default the config's listenAddress")
    p.add_argument("--control-plane", help="the operator API, default the config's controlPlane.url")
    p.add_argument("--team", help="the team whose bus to follow, default the config's team")
    p.add_argument("--hands-dry-run", action="store_true", help="Jev reads the hands, nothing is published")
    a = p.parse_args(argv)
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s: %(message)s")
    config = load_config(a.config)
    config["controlPlane"]["url"] = a.control_plane or config["controlPlane"]["url"]
    config["team"] = a.team or config["team"]

    score = Score(os.path.join(os.path.dirname(os.path.abspath(a.config)), config["score"]["file"]), config["sections"],
                  config.get("phrases", {}).get("bars", 8))
    font = synth.soundfont(config["audio"]["soundfonts"])
    audio = config["audio"]
    orchestra = Orchestra(score, config, synth.FluidSynth(font, audio["rate"], audio.get("gain", 0.2), audio.get("polyphony", 1024)))
    stream = synth.Stream()
    log.info("%s: %d bars, %s bpm, %d chairs, soundfont %s", config["score"]["title"], score.bars, score.bpm, len(orchestra.chairs), font)

    threading.Thread(target=synth.run, args=(orchestra, stream, config["audio"]["rate"], config["audio"]["block"], a.record), daemon=True).start()
    cp = config["controlPlane"]
    publish = None
    if cp.get("url"):
        b = bus.Bus(cp["url"], config["team"], cp["tokenFile"], cp["caFile"])
        threading.Thread(target=b.follow, args=(orchestra,), daemon=True).start()
        publish = b.publish
    else:
        log.info("no controlPlane.url: not following the bus; stop.all will not cut the sound")
    hc = dict(config.get("hands") or {})
    if a.hands_dry_run:
        hc["dryRun"] = True
    judge = jev.Jev(hc.get("jev", {}).get("model", "jev-1.13.0"), hc.get("jev", {}).get("keyFile", "~/.config/axlr-bots/env"),
                    hc.get("jev", {}).get("timeoutSeconds", 10))
    hands = hands_module.Hands(orchestra, hc, judge.choose, publish)
    log.info("hands at /hands: Jev %s, threshold %s, %s", judge.model, hands.threshold, "dry run" if hands.dry_run else "publishing on " + config["team"])

    host, port = (a.listen or config["listenAddress"]).rsplit(":", 1)
    if host not in ("127.0.0.1", "localhost", "::1"):
        sys.exit("score-service listens on loopback only")
    server = ThreadingHTTPServer((host, int(port)), make_handler(orchestra, stream, config, hands))
    server.daemon_threads = True
    log.info("listening on http://%s:%s (mcp at /mcp/<chair>, state at /api/state, sound at /audio.wav)", host, port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
