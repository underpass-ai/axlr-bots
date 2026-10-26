"""Real ships around the port, live, from aisstream.io.

One websocket per key (the service allows a few per account). In the fleet
it runs as a service of its own, `ais` (python ais.py): it holds the one
connection and serves the ships over HTTP, and the fleet base reads them
from there (AIS_URL) for the map, the 3D view, the bots' fleet.json and the
vehicles' telemetry. Without AIS_URL the base can still hold the connection
itself (AIS_KEY_FILE). The key is read from a file (a Secret) at each
connection, never logged. Without a key file the layer is off and the map
says so.

  GET /ships    {"ships": [...], "summary": {...}}
  GET /stream   the same as server-sent events, every 2 s
  GET /healthz

Messages kept: PositionReport and StandardClassBPositionReport (position,
speed and course over ground, heading) and ShipStaticData (name, IMO, call
sign, type, the antenna's distances A/B/C/D to the bow, stern, port and
starboard, so length, beam and where the hull lies around the position,
draught, destination). A ship not heard from for MAX_AGE seconds is dropped.
"""

import json
import logging
import os
import threading
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

log = logging.getLogger("ais")

URL = "wss://stream.aisstream.io/v0/stream"
MAX_AGE = 900
TYPES = [(20, 29, "wing in ground"), (30, 30, "fishing"), (31, 32, "towing"), (33, 34, "dredging or diving"), (35, 35, "military"),
         (36, 36, "sailing"), (37, 37, "pleasure craft"), (40, 49, "high speed craft"), (50, 50, "pilot"), (51, 51, "search and rescue"),
         (52, 52, "tug"), (53, 53, "port tender"), (55, 55, "law enforcement"), (58, 58, "medical"), (60, 69, "passenger"),
         (70, 79, "cargo"), (80, 89, "tanker"), (90, 99, "other")]


def type_name(code):
    for lo, hi, name in TYPES:
        if isinstance(code, int) and lo <= code <= hi:
            return name
    return ""


class AIS:
    def __init__(self, key_file, bbox):
        self.key_file = key_file
        self.bbox = bbox                  # [[lat1, lon1], [lat2, lon2]]
        self.lock = threading.Lock()
        self.vessels = {}
        self.status = "off"
        self.error = ""
        self.updated = 0.0
        self.messages = 0

    def start(self):
        if not self.key_file:
            return
        threading.Thread(target=self._run, name="ais", daemon=True).start()

    def _key(self):
        try:
            with open(self.key_file) as f:
                return f.read().strip()
        except OSError:
            return ""

    def _run(self):
        from websockets.sync.client import connect   # only when the layer is on
        backoff = 5
        while True:
            key = self._key()
            if not key:
                self._set("off", "no key file")
                time.sleep(60)
                continue
            self._set("connecting", "")
            try:
                with connect(URL, open_timeout=15, close_timeout=5, max_size=1 << 20) as ws:
                    ws.send(json.dumps({"APIKey": key, "BoundingBoxes": [self.bbox],
                                        "FilterMessageTypes": ["PositionReport", "StandardClassBPositionReport", "ShipStaticData"]}))
                    del key
                    backoff = 5
                    for raw in ws:
                        self.handle(json.loads(raw))
            except Exception as exc:
                # The library's messages never carry the key; keep only the class and the reason.
                self._set("error", f"{type(exc).__name__}: {str(exc)[:120]}")
                log.warning("aisstream: %s; again in %d s", self.error, backoff)
            time.sleep(backoff)
            backoff = min(120, backoff * 2)

    def _set(self, status, error):
        with self.lock:
            self.status, self.error = status, error

    def handle(self, msg):
        """One message from the stream."""
        if not isinstance(msg, dict):
            return
        if msg.get("error"):
            self._set("error", str(msg["error"])[:160])
            return
        kind = msg.get("MessageType")
        meta = msg.get("MetaData") or {}
        body = (msg.get("Message") or {}).get(kind) or {}
        mmsi = str(meta.get("MMSI") or body.get("UserID") or "")
        if not mmsi:
            return
        now = time.time()
        with self.lock:
            self.status, self.error = "live", ""
            self.messages += 1
            self.updated = now
            ship = self.vessels.setdefault(mmsi, {"mmsi": mmsi})
            name = (meta.get("ShipName") or "").strip()
            if name:
                ship["name"] = name
            if kind in ("PositionReport", "StandardClassBPositionReport"):
                lat, lon = meta.get("latitude", meta.get("Latitude")), meta.get("longitude", meta.get("Longitude"))
                if lat is None:
                    lat, lon = body.get("Latitude"), body.get("Longitude")
                if not isinstance(lat, (int, float)) or not isinstance(lon, (int, float)) or abs(lat) > 90 or abs(lon) > 180:
                    return
                # AIS sends 102.3 kn for "no speed" and 360 degrees for "no course".
                sog, cog = body.get("Sog"), body.get("Cog")
                ship.update(lat=lat, lon=lon, updated=now,
                            sog=sog if isinstance(sog, (int, float)) and 0 <= sog < 102.2 else None,
                            cog=cog if isinstance(cog, (int, float)) and 0 <= cog < 360 else None)
                heading = body.get("TrueHeading")
                ship["heading"] = heading if isinstance(heading, (int, float)) and 0 <= heading < 360 else None
                status = body.get("NavigationalStatus")
                if isinstance(status, int):
                    ship["nav_status"] = status
            elif kind == "ShipStaticData":
                code = body.get("Type")
                dim = body.get("Dimension") or {}
                ship.update(type=code, type_name=type_name(code), destination=(body.get("Destination") or "").strip()[:30])
                a, b, c, d = (dim.get(k) if isinstance(dim.get(k), int) and dim.get(k) >= 0 else 0 for k in "ABCD")
                if 0 < a + b < 500 and 0 < c + d < 80:
                    ship.update(length_m=a + b, beam_m=c + d, dim={"a": a, "b": b, "c": c, "d": d})
                draught = body.get("MaximumStaticDraught")
                if isinstance(draught, (int, float)) and 0 < draught < 25.5:
                    ship["draught_m"] = round(float(draught), 1)
                imo = body.get("ImoNumber")
                if isinstance(imo, int) and 1000000 <= imo <= 9999999:
                    ship["imo"] = imo
                callsign = (body.get("CallSign") or "").strip()
                if callsign:
                    ship["callsign"] = callsign[:10]
                if (body.get("Name") or "").strip():
                    ship["name"] = body["Name"].strip()

    def ships(self):
        now = time.time()
        with self.lock:
            for mmsi in [m for m, s in self.vessels.items() if now - s.get("updated", now) > MAX_AGE]:
                del self.vessels[mmsi]
            return [dict(s) for s in self.vessels.values() if "lat" in s]

    def summary(self):
        ships = self.ships()
        with self.lock:
            return {"status": self.status, "ships": len(ships), "updated": self.updated, "error": self.error}


class Remote:
    """The ships of the `ais` service, read every 2 s: what the base shows and the bots read."""

    def __init__(self, url):
        self.url = url.rstrip("/")
        self.lock = threading.Lock()
        self._ships = []
        self._summary = {"status": "connecting", "ships": 0, "updated": 0.0, "error": ""}

    def start(self):
        threading.Thread(target=self._run, name="ais-client", daemon=True).start()

    def _run(self):
        while True:
            try:
                with urllib.request.urlopen(self.url + "/ships", timeout=3) as r:
                    data = json.load(r)
                with self.lock:
                    self._ships = [s for s in data.get("ships") or [] if isinstance(s, dict)]
                    self._summary = dict(data.get("summary") or {})
            except Exception as exc:
                with self.lock:
                    self._summary = dict(self._summary, status="error", error=f"the ais service: {type(exc).__name__}")
            time.sleep(2)

    def ships(self):
        with self.lock:
            return [dict(s) for s in self._ships]

    def summary(self):
        with self.lock:
            return dict(self._summary)


def bbox_from_env():
    lat1, lon1, lat2, lon2 = (float(x) for x in os.environ.get("AIS_BBOX", "51.90,3.80,52.05,4.15").split(","))
    return [[lat1, lon1], [lat2, lon2]]


def from_env():
    """The ships as the base reads them: from the ais service, or its own connection."""
    if os.environ.get("AIS_URL"):
        return Remote(os.environ["AIS_URL"])
    return AIS(os.environ.get("AIS_KEY_FILE", ""), bbox_from_env())


class Handler(BaseHTTPRequestHandler):
    """The ais service's port: the ships, as JSON or as a stream."""

    ais = None
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        pass

    def _body(self):
        return json.dumps({"ships": self.ais.ships(), "summary": self.ais.summary()})

    def do_GET(self):
        if self.path == "/healthz":
            body = b"ok\n"
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
        elif self.path == "/ships":
            body = self._body().encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
        elif self.path == "/stream":
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Connection", "close")
            self.end_headers()
            self.close_connection = True
            try:
                while True:
                    self.wfile.write(f"data: {self._body()}\n\n".encode())
                    self.wfile.flush()
                    time.sleep(2)
            except (BrokenPipeError, ConnectionResetError, OSError):
                return
        else:
            body = b"not found\n"
            self.send_response(404)
            self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s %(message)s")
    source = AIS(os.environ.get("AIS_KEY_FILE", ""), bbox_from_env())
    source.start()
    Handler.ais = source
    host, _, port = os.environ.get("LISTEN", "0.0.0.0:8080").rpartition(":")
    server = ThreadingHTTPServer((host, int(port)), Handler)
    server.daemon_threads = True
    log.info("ais: %s, on %s", "key file " + ("set" if source.key_file else "missing"), server.server_address)
    server.serve_forever()


if __name__ == "__main__":
    main()
