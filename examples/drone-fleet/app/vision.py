"""The ground station's image analysis.

Every photo a vehicle takes is a CAMERA_FEEDBACK with where it was taken.
For a share of them (one in VISION_EVERY, at most VISION_MAX_PER_FLIGHT a
flight) the station fetches the aerial photo of that footprint (the
vehicles are simulated; the port is not) and asks a vision model what it
shows. The photo is PDOK's Dutch aerial photo where it has one, Esri's
where PDOK answers white (out at sea), and none at all if both do. The findings go on the map and into fleet.json, where the dispatcher
reads them.

During a search (incident.py) every photo is analysed, up to
INCIDENT_MAX_PER_FLIGHT a flight: the containers the simulation puts inside
the photo's frame are drawn into the imagery first (sprites.py), and the
model is asked for the floating containers it sees, with their place in
the frame, which the incident maps back to the sea.
"""

import base64
import io
import json
import logging
import math
import os
import queue
import re
import threading
import time
import urllib.request

from PIL import Image, ImageStat

import geo
import sprites

log = logging.getLogger("vision")

# Where a photo's frame comes from, first that answers with an image: PDOK's
# Dutch aerial photo (8 cm, CC BY 4.0) covers the land and the sea about 2 km
# out; past that it answers white, and the frame comes from Esri instead.
PDOK = ("https://service.pdok.nl/hwh/luchtfotorgb/wms/v1_0?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=Actueel_orthoHR&STYLES="
        "&CRS=EPSG:3857&BBOX={x1:.2f},{y1:.2f},{x2:.2f},{y2:.2f}&WIDTH=640&HEIGHT=480&FORMAT=image/jpeg")
ESRI = ("https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/export"
        "?bbox={x1:.2f},{y1:.2f},{x2:.2f},{y2:.2f}&bboxSR=3857&imageSR=3857&size=640,480&format=jpg&f=image")
SOURCES = {"pdok": PDOK, "esri": ESRI}
CREDITS = {
    "pdok": "Luchtfoto © Beeldmateriaal Nederland / PDOK (CC BY 4.0)",
    "esri": "Source: Esri, Maxar, Earthstar Geographics, and the GIS User Community",
}
BLANK_MEAN = 240      # a frame brighter than this on average is "no data", not a photo
PROMPT = (
    "You are the image analyst of an inspection fleet at the Port of Rotterdam (Maasvlakte 2). "
    "This aerial photo was taken by {kind} {vehicle} at {alt:.0f} m during mission '{mission}'. "
    "Report what matters for port operations and safety: ships, quay cranes, container stacks, trucks, "
    "people, oil sheen or other water pollution, debris, fire or smoke, damage, anything unusual. "
    'Answer with JSON only: {{"objects": ["short noun phrases, with counts"], "findings": ["short notable observations"], '
    '"alert": true or false (true only for a safety or pollution issue), "summary": "one sentence"}}'
)
SEARCH_PROMPT = (
    "You are the image analyst of a search for shipping containers lost overboard off Maasvlakte 2, Rotterdam. "
    "This aerial photo was taken by {kind} {vehicle} at {alt:.0f} m during mission '{mission}'. "
    "Report what matters: floating shipping containers first, then ships, debris, oil sheen, people in the water. "
    "For each floating container you can see, give its centre as fractions of the image width and height from the top-left corner. "
    'Answer with JSON only: {{"containers": [{{"x": 0.0-1.0, "y": 0.0-1.0, "size": "20ft|40ft|unsure"}}], '
    '"objects": ["short noun phrases, with counts"], "findings": ["short notable observations"], '
    '"alert": true or false (true for a container or a safety or pollution issue), "summary": "one sentence"}}'
)
PHOTO_ID = re.compile(r"^[a-z0-9-]{1,80}$")


class NoImagery(Exception):
    """No source had a real photo of the frame."""


def _get(url):
    with urllib.request.urlopen(urllib.request.Request(url, headers={"User-Agent": "axlr-bots-fleet-demo/0.1"}), timeout=20) as r:
        return r.read()


def blank(image):
    """True for a frame that is not a photo: unreadable, or all but white."""
    try:
        rgb = Image.open(io.BytesIO(image)).convert("RGB")
    except Exception:
        return True
    return sum(ImageStat.Stat(rgb).mean) / 3 > BLANK_MEAN


def fetch_imagery(bbox, sources=("pdok", "esri"), get=_get):
    """The frame's photo and where it came from: the first source with a real image."""
    x1, y1, x2, y2 = bbox
    tried = []
    for name in sources:
        try:
            image = get(SOURCES[name].format(x1=x1, y1=y1, x2=x2, y2=y2))
        except Exception as exc:
            tried.append(f"{name}: {exc}")
            continue
        if not blank(image):
            return image, name
        tried.append(f"{name}: blank")
    raise NoImagery("; ".join(tried))


def frame(lat, lon, alt_m):
    """The photo's frame in Web Mercator metres, 4:3, for an 84-degree lens no narrower than 120 m."""
    width = max(120.0, 1.8 * max(alt_m, 10.0))
    mx, my = sprites.mercator(lat, lon)
    k = 1 / math.cos(math.radians(lat))
    return mx - width * k / 2, my - width * 0.375 * k, mx + width * k / 2, my + width * 0.375 * k


class Vision:
    def __init__(self, store_dir, model, api_key, every, max_per_flight, workers=3, search_max_per_flight=80, sources=("pdok", "esri")):
        self.sources = tuple(x for x in sources if x in SOURCES) or ("pdok", "esri")
        self.search = None                # the incident's hooks (fleet_base.Base), set by the base
        self.search_max_per_flight = search_max_per_flight
        self.store = store_dir
        self.model = model
        self.api_key = api_key
        self.every = max(1, every)
        self.max_per_flight = max_per_flight
        self.lock = threading.Lock()
        self.results = []
        self.seen = set()
        self.per_flight = {}
        self.jobs = queue.Queue(maxsize=160)
        os.makedirs(self.store, exist_ok=True)
        for i in range(max(1, workers)):
            threading.Thread(target=self._work, name=f"vision-{i}", daemon=True).start()

    def offer(self, vehicle, kind, flight):
        """Queue the new photos of a flight worth a look."""
        started = flight.get("started_at")
        for shot in flight.get("shots") or []:
            key = (vehicle, started, shot["n"])
            if key in self.seen:
                continue
            self.seen.add(key)
            taken = self.per_flight.get((vehicle, started), 0)
            searching = self.search is not None and self.search.searching()
            if searching:
                if taken >= self.search_max_per_flight:
                    continue
            elif shot["n"] % self.every or taken >= self.max_per_flight:
                continue
            self.per_flight[(vehicle, started)] = taken + 1
            try:
                self.jobs.put_nowait(dict(shot, vehicle=vehicle, kind=kind, mission=flight.get("name", ""), searching=searching))
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
        return [{"vehicle": r["vehicle"], "mission": r["mission"], "lat": round(r["lat"], 5), "lon": round(r["lon"], 5),
                 "alert": bool(r["analysis"].get("alert")), "summary": r["analysis"].get("summary", ""),
                 "findings": r["analysis"].get("findings", [])} for r in notable[:n]]

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
        bbox = frame(job["lat"], job["lon"], job["alt_m"])
        try:
            image, source = fetch_imagery(bbox, self.sources)
        except NoImagery as exc:
            # Never a white frame to the model: the photo is left out.
            log.info("%s photo %d: no imagery (%s)", job["vehicle"], job["n"], exc)
            return
        searching = job.get("searching") and self.search is not None
        if searching:
            # The simulation's containers inside this frame, drawn where they float now.
            image, _ = sprites.compose(image, bbox, self.search.floaters())
        photo_id = f"{job['vehicle']}-{int(job['t'])}-{job['n']}"
        with open(os.path.join(self.store, photo_id + ".jpg"), "wb") as f:
            f.write(image)
        if self.api_key:
            analysis, cost = self._ask(image, job, searching)
        else:
            analysis, cost = {"summary": "no vision model configured", "objects": [], "findings": [], "alert": False, "containers": []}, 0.0
        detections = []
        if searching:
            detections = self.search.observe(job["vehicle"], job["kind"], job["alt_m"], job["t"], analysis.get("containers", []), bbox, photo_id, cost)
        analysis.pop("containers", None)
        result = {"id": photo_id, "vehicle": job["vehicle"], "kind": job["kind"], "mission": job["mission"], "n": job["n"],
                  "lat": job["lat"], "lon": job["lon"], "alt_m": job["alt_m"], "t": job["t"], "analysis": analysis,
                  "imagery": source, "credit": CREDITS[source]}
        if detections:
            result["detections"] = detections
        with self.lock:
            self.results.append(result)
            del self.results[:-200]
        log.info("%s photo %d: %s%s", job["vehicle"], job["n"], analysis.get("summary", ""), " [ALERT]" if analysis.get("alert") else "")

    def _ask(self, image, job, searching=False):
        prompt = SEARCH_PROMPT if searching else PROMPT
        body = {
            "model": self.model,
            "max_tokens": 1500,
            "reasoning": {"effort": "low"},
            "usage": {"include": True},
            "messages": [{"role": "user", "content": [
                {"type": "text", "text": prompt.format(kind=job["kind"], vehicle=job["vehicle"], alt=job["alt_m"], mission=job["mission"])},
                {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64," + base64.b64encode(image).decode()}},
            ]}],
        }
        request = urllib.request.Request(
            "https://openrouter.ai/api/v1/chat/completions", data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "Authorization": "Bearer " + self.api_key,
                     "X-Title": "AXLR Bots port fleet"})
        with urllib.request.urlopen(request, timeout=90) as r:
            reply = json.load(r)
        cost = float((reply.get("usage") or {}).get("cost") or 0.0)
        return parse(reply["choices"][0]["message"].get("content") or ""), cost


def parse(text):
    """The model's JSON answer, bounded, whatever came around it."""
    match = re.search(r"\{.*\}", text, re.S)
    try:
        analysis = json.loads(match.group(0)) if match else {}
    except ValueError:
        analysis = {}
    if not isinstance(analysis, dict):
        analysis = {}
    containers = []
    for c in analysis.get("containers") or []:
        try:
            x, y = float(c["x"]), float(c["y"])
        except (KeyError, TypeError, ValueError):
            continue
        if 0 <= x <= 1 and 0 <= y <= 1:
            containers.append({"x": x, "y": y})
    return {
        "objects": [str(x)[:80] for x in analysis.get("objects") or []][:12],
        "findings": [str(x)[:160] for x in analysis.get("findings") or []][:6],
        "alert": bool(analysis.get("alert")),
        "summary": str(analysis.get("summary", text[:200]))[:240],
        "containers": containers[:30],
    }


def from_env():
    return Vision(
        os.environ.get("PHOTOS_DIR", "/var/lib/fleet/photos"),
        os.environ.get("VISION_MODEL", "anthropic/claude-haiku-5.5"),
        os.environ.get("OPENROUTER_API_KEY", ""),
        int(os.environ.get("VISION_EVERY", "4")),
        int(os.environ.get("VISION_MAX_PER_FLIGHT", "12")),
        int(os.environ.get("VISION_WORKERS", "3")),
        int(os.environ.get("INCIDENT_MAX_PER_FLIGHT", "80")),
        sources=tuple(x.strip() for x in os.environ.get("IMAGERY_SOURCES", "pdok,esri").split(",")),
    )
