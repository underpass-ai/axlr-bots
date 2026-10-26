"""The ground station's image analysis.

Every photo a vehicle takes is a CAMERA_FEEDBACK with where it was taken.
For a share of them (one in VISION_EVERY, at most VISION_MAX_PER_FLIGHT a
flight) the station fetches the aerial imagery of that footprint (the
vehicles are simulated; the port is not) and asks a vision model what it
shows. The findings go on the map and into fleet.json, where the dispatcher
reads them.
"""

import base64
import json
import logging
import os
import queue
import re
import threading
import time
import urllib.request

import geo

log = logging.getLogger("vision")

IMAGERY = "https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/export"
PROMPT = (
    "You are the image analyst of an inspection fleet at the Port of Rotterdam (Maasvlakte 2). "
    "This aerial photo was taken by {kind} {vehicle} at {alt:.0f} m during mission '{mission}'. "
    "Report what matters for port operations and safety: ships, quay cranes, container stacks, trucks, "
    "people, oil sheen or other water pollution, debris, fire or smoke, damage, anything unusual. "
    'Answer with JSON only: {{"objects": ["short noun phrases, with counts"], "findings": ["short notable observations"], '
    '"alert": true or false (true only for a safety or pollution issue), "summary": "one sentence"}}'
)
PHOTO_ID = re.compile(r"^[a-z0-9-]{1,80}$")


class Vision:
    def __init__(self, store_dir, model, api_key, every, max_per_flight):
        self.store = store_dir
        self.model = model
        self.api_key = api_key
        self.every = max(1, every)
        self.max_per_flight = max_per_flight
        self.lock = threading.Lock()
        self.results = []
        self.seen = set()
        self.per_flight = {}
        self.jobs = queue.Queue(maxsize=64)
        os.makedirs(self.store, exist_ok=True)
        threading.Thread(target=self._work, name="vision", daemon=True).start()

    def offer(self, vehicle, kind, flight):
        """Queue the new photos of a flight worth a look."""
        started = flight.get("started_at")
        for shot in flight.get("shots") or []:
            key = (vehicle, started, shot["n"])
            if key in self.seen:
                continue
            self.seen.add(key)
            taken = self.per_flight.get((vehicle, started), 0)
            if shot["n"] % self.every or taken >= self.max_per_flight:
                continue
            self.per_flight[(vehicle, started)] = taken + 1
            try:
                self.jobs.put_nowait(dict(shot, vehicle=vehicle, kind=kind, mission=flight.get("name", "")))
            except queue.Full:
                pass

    def recent(self, n=60):
        with self.lock:
            return list(self.results[-n:])

    def findings(self, n=15):
        """What the bots read: the alerts first, then the latest observations."""
        with self.lock:
            notable = [r for r in self.results if r["analysis"].get("findings") or r["analysis"].get("alert")]
        notable.sort(key=lambda r: (not r["analysis"].get("alert"), -r["t"]))
        return [{"vehicle": r["vehicle"], "mission": r["mission"], "lat": r["lat"], "lon": r["lon"], "alert": bool(r["analysis"].get("alert")),
                 "summary": r["analysis"].get("summary", ""), "findings": r["analysis"].get("findings", [])} for r in notable[:n]]

    def image_path(self, photo_id):
        if not PHOTO_ID.match(photo_id):
            return None
        path = os.path.join(self.store, photo_id + ".jpg")
        return path if os.path.isfile(path) else None

    def _work(self):
        while True:
            job = self.jobs.get()
            try:
                self._analyse(job)
            except Exception as exc:
                log.warning("photo %s/%s: %s", job["vehicle"], job["n"], exc)

    def _analyse(self, job):
        # The footprint of an 84-degree lens, no smaller than 120 m so the imagery says something.
        width = max(120.0, 1.8 * max(job["alt_m"], 10.0))
        lat1, lon1 = geo.point_at(job["lat"], job["lon"], -width * 0.375, -width / 2)
        lat2, lon2 = geo.point_at(job["lat"], job["lon"], width * 0.375, width / 2)
        url = f"{IMAGERY}?bbox={lon1:.6f},{lat1:.6f},{lon2:.6f},{lat2:.6f}&bboxSR=4326&imageSR=3857&size=640,480&format=jpg&f=image"
        with urllib.request.urlopen(urllib.request.Request(url, headers={"User-Agent": "axlr-bots-fleet-demo/0.1"}), timeout=20) as r:
            image = r.read()
        photo_id = f"{job['vehicle']}-{int(job['t'])}-{job['n']}"
        with open(os.path.join(self.store, photo_id + ".jpg"), "wb") as f:
            f.write(image)
        analysis = self._ask(image, job) if self.api_key else {"summary": "no vision model configured", "objects": [], "findings": [], "alert": False}
        result = {"id": photo_id, "vehicle": job["vehicle"], "kind": job["kind"], "mission": job["mission"], "n": job["n"],
                  "lat": job["lat"], "lon": job["lon"], "alt_m": job["alt_m"], "t": job["t"], "analysis": analysis}
        with self.lock:
            self.results.append(result)
            del self.results[:-200]
        log.info("%s photo %d: %s%s", job["vehicle"], job["n"], analysis.get("summary", ""), " [ALERT]" if analysis.get("alert") else "")

    def _ask(self, image, job):
        body = {
            "model": self.model,
            "max_tokens": 500,
            "messages": [{"role": "user", "content": [
                {"type": "text", "text": PROMPT.format(kind=job["kind"], vehicle=job["vehicle"], alt=job["alt_m"], mission=job["mission"])},
                {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64," + base64.b64encode(image).decode()}},
            ]}],
        }
        request = urllib.request.Request(
            "https://openrouter.ai/api/v1/chat/completions", data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "Authorization": "Bearer " + self.api_key,
                     "X-Title": "AXLR Bots port fleet"})
        with urllib.request.urlopen(request, timeout=60) as r:
            reply = json.load(r)
        text = reply["choices"][0]["message"]["content"] or ""
        match = re.search(r"\{.*\}", text, re.S)
        try:
            analysis = json.loads(match.group(0)) if match else {}
        except ValueError:
            analysis = {}
        return {
            "objects": [str(x)[:80] for x in analysis.get("objects", [])][:12],
            "findings": [str(x)[:160] for x in analysis.get("findings", [])][:6],
            "alert": bool(analysis.get("alert")),
            "summary": str(analysis.get("summary", text[:200]))[:240],
        }


def from_env():
    return Vision(
        os.environ.get("PHOTOS_DIR", "/var/lib/fleet/photos"),
        os.environ.get("VISION_MODEL", "anthropic/claude-haiku-5.5"),
        os.environ.get("OPENROUTER_API_KEY", ""),
        int(os.environ.get("VISION_EVERY", "4")),
        int(os.environ.get("VISION_MAX_PER_FLIGHT", "12")),
    )
