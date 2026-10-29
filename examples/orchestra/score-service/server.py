#!/usr/bin/env python3
"""score-service: the orchestra's world.

  POST /mcp/<chair>      MCP over HTTP for one chair: enter, dynamics, stop, listen.
                         Each chair's node names its own URL, so the chair is who
                         calls; a chair may only speak for itself
  GET  /api/state        the orchestra now: take, bar, tempo, every chair's state,
                         the last facts; ?version=N&wait=20 answers when it changes
  GET  /audio.wav        the sound, live, as an open-ended WAV (OBS, ffplay, a browser)
  GET  /api/score        each part's notes per bar, for the page's score strip
  GET  /stage            the stage: 36 chairs, the score with its playhead, the conductor (?hud=0 for the recording)

It listens on loopback only. With controlPlane.url it follows the team's
bus and cuts the sound on stop.all itself.

Run:  python3 server.py [config.json] [--record take.wav]
Test: python3 -m unittest -v test_score_service
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
import mcp_http
import synth
from orchestra import Orchestra, Refused
from score import Score

HERE = os.path.dirname(os.path.abspath(__file__))
STATIC = os.path.realpath(os.path.join(HERE, "static"))
TYPES = {".html": "text/html; charset=utf-8", ".css": "text/css", ".js": "text/javascript", ".svg": "image/svg+xml",
         ".ttf": "font/ttf", ".txt": "text/plain; charset=utf-8"}
log = logging.getLogger("score")

TOOLS = [
    {"name": "enter", "description": "Come in: your part starts sounding at the start of a bar. Without bar, the next bar. When nothing plays yet, your entry starts the music at bar 1. Answers the bar you come in at.",
     "inputSchema": {"type": "object", "properties": {"bar": {"type": "integer", "minimum": 1}, "dynamics": {"type": "string", "enum": ["p", "mf", "f"]}}}},
    {"name": "dynamics", "description": "Play softer or louder from now on: p, mf or f.",
     "inputSchema": {"type": "object", "properties": {"level": {"type": "string", "enum": ["p", "mf", "f"]}}, "required": ["level"]}},
    {"name": "stop", "description": "Stop playing at once. Your section goes on with the chairs still playing.",
     "inputSchema": {"type": "object", "properties": {}}},
    {"name": "listen", "description": "Play on and listen for a few bars (at most 20 seconds); answers where the music is, your state and your section's.",
     "inputSchema": {"type": "object", "properties": {"bars": {"type": "integer", "minimum": 1, "maximum": 32}}}},
]


def chair_tools(orchestra, chair, max_seconds):
    tools = mcp_http.Tools("score-service", "0.1.0")

    def guard(fn):
        def call(args):
            try:
                return fn(args)
            except Refused as exc:
                raise mcp_http.ToolError(str(exc))
        return call

    tools.add(TOOLS[0], guard(lambda a: orchestra.enter(chair, a.get("bar"), a.get("dynamics"))))
    tools.add(TOOLS[1], guard(lambda a: orchestra.dynamics(chair, a.get("level"))))
    tools.add(TOOLS[2], guard(lambda a: orchestra.stop(chair)))
    tools.add(TOOLS[3], guard(lambda a: orchestra.listen(chair, a.get("bars", 8), max_seconds)))
    return tools


def make_handler(orchestra, stream, config):
    label = config["label"]
    rate = config["audio"]["rate"]
    score_json = json.dumps(orchestra.density()).encode()
    per_chair = {c: chair_tools(orchestra, c, config["listen"]["maxSeconds"]) for c in orchestra.chairs}

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
            self.wfile.write(body)

        def _loopback(self):
            host = (self.headers.get("Host") or "")
            host = host.split("]")[0] + "]" if host.startswith("[") else host.rsplit(":", 1)[0]
            return host in mcp_http.LOOPBACK_HOSTS

        def do_POST(self):
            if not self._loopback():
                return self._send(403, b'{"error":"loopback only"}')
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
                if "version" in query:
                    state = orchestra.wait_change(int(query["version"]), min(float(query.get("wait", 20)), 25))
                else:
                    state = orchestra.snapshot()
                state["label"] = label
                return self._send(200, json.dumps(state).encode())
            if url.path == "/api/score":
                return self._send(200, score_json)
            if url.path == "/audio.wav":
                return self._audio()
            if url.path in ("/", "/stage"):
                return self._file("stage.html")
            if url.path.startswith("/static/"):
                return self._file(url.path[len("/static/"):])
            self._send(404, b'{"error":"not found"}')

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
    a = p.parse_args(argv)
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s: %(message)s")
    config = load_config(a.config)
    config["controlPlane"]["url"] = a.control_plane or config["controlPlane"]["url"]
    config["team"] = a.team or config["team"]

    score = Score(os.path.join(os.path.dirname(os.path.abspath(a.config)), config["score"]["file"]), config["sections"])
    font = synth.soundfont(config["audio"]["soundfonts"])
    orchestra = Orchestra(score, config, synth.FluidSynth(font, config["audio"]["rate"]))
    stream = synth.Stream()
    log.info("%s: %d bars, %s bpm, %d chairs, soundfont %s", config["score"]["title"], score.bars, score.bpm, len(orchestra.chairs), font)

    threading.Thread(target=synth.run, args=(orchestra, stream, config["audio"]["rate"], config["audio"]["block"], a.record), daemon=True).start()
    cp = config["controlPlane"]
    if cp.get("url"):
        b = bus.Bus(cp["url"], config["team"], cp["tokenFile"], cp["caFile"])
        threading.Thread(target=b.follow, args=(orchestra,), daemon=True).start()
    else:
        log.info("no controlPlane.url: not following the bus; stop.all will not cut the sound")

    host, port = (a.listen or config["listenAddress"]).rsplit(":", 1)
    if host not in ("127.0.0.1", "localhost", "::1"):
        sys.exit("score-service listens on loopback only")
    server = ThreadingHTTPServer((host, int(port)), make_handler(orchestra, stream, config))
    server.daemon_threads = True
    log.info("listening on http://%s:%s (mcp at /mcp/<chair>, state at /api/state, sound at /audio.wav)", host, port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
