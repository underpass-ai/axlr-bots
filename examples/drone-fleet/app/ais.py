"""Real ships around the port, live, from aisstream.io.

One websocket per key (the service allows a few per account): the fleet
base is the only client and passes the ships on in its own stream. The key
is read from a file (a Secret) at each connection, never logged. Without a
key file the layer is off and the map says so.

Messages kept: PositionReport and StandardClassBPositionReport (position,
speed and course over ground, heading) and ShipStaticData (name, type,
dimensions). A ship not heard from for MAX_AGE seconds is dropped.
"""

import json
import logging
import os
import threading
import time

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
                length = (dim.get("A") or 0) + (dim.get("B") or 0)
                beam = (dim.get("C") or 0) + (dim.get("D") or 0)
                if 0 < length < 500:
                    ship["length_m"] = length
                if 0 < beam < 80:
                    ship["beam_m"] = beam
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


def from_env():
    key_file = os.environ.get("AIS_KEY_FILE", "")
    raw = os.environ.get("AIS_BBOX", "51.90,3.80,52.05,4.15")
    lat1, lon1, lat2, lon2 = (float(x) for x in raw.split(","))
    return AIS(key_file, [[lat1, lon1], [lat2, lon2]])
